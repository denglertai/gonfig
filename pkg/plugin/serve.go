package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"runtime/debug"
	"slices"
)

// processor is implemented by every filter, with the same method as gonfig's filters
type processor interface {
	Process(value any) (any, error)
}

// paramsAcceptor is implemented by filters accepting params, e.g. encrypt(base64)
type paramsAcceptor interface {
	AcceptParams(params map[string]string)
}

// describer is implemented by filters describing themselves for gonfig plugin list
type describer interface {
	Description() string
}

// paramsDeclarer is implemented by filters declaring the params they accept, which gonfig validates
type paramsDeclarer interface {
	Params() []string
}

// Option sets optional information about the plugin, which gonfig plugin list shows
type Option func(*Handshake)

// WithVersion sets the version of the plugin, e.g. set with -ldflags "-X main.version=1.2.0" when building it.
// Without it, or if it is empty, the version of the main module from the build info is used: the version of plugins
// installed with go install example.com/plugin@v1.2.0, or the pseudo-version Go derives from the commit for builds
// within a repository, e.g. v0.0.0-20261024211216-12c2cfce85a1, and no version for builds without it ("(devel)").
func WithVersion(version string) Option {
	return func(h *Handshake) {
		if version != "" {
			h.Version = version
		}
	}
}

// WithDescription sets the description of the plugin, e.g. "Reads secrets from OpenBao and HashiCorp Vault"
func WithDescription(description string) Option {
	return func(h *Handshake) {
		h.Description = description
	}
}

// buildVersion returns the version of the main module from the build info, see moduleVersion
func buildVersion() string {
	return moduleVersion(debug.ReadBuildInfo())
}

// moduleVersion returns the version of the main module of the build info, or an empty string without build info and
// for builds without a version ("(devel)")
func moduleVersion(info *debug.BuildInfo, ok bool) string {
	if !ok || info.Main.Version == "(devel)" {
		return ""
	}
	return info.Main.Version
}

// Serve runs an executable plugin providing the filters: it writes the Handshake and answers requests of gonfig
// until stdin is closed. The keys of the map are the filter names, the values the filters, which are the same as
// for the deprecated Go plugins:
//
//	// Required: transforms the value, which is usually a string but may be an int, float64, bool or []string
//	Process(value any) (any, error)
//	// Optional: receives the params given in brackets, e.g. encrypt(encoding=base64)
//	AcceptParams(params map[string]string)
//	// Optional: describes the filter for gonfig plugin list
//	Description() string
//	// Optional: the names of the params in the order of their positions, which gonfig validates like for its
//	// built-in filters. Filters without it receive any params.
//	Params() []string
//
// Every request works on its own shallow copy of the filter, so params of one usage do not affect another one.
// Stdout is reserved for the protocol, so anything the filters write to os.Stdout goes to stderr instead.
// The options set the version and description of the plugin, which gonfig plugin list shows.
// Serve returns an error if a filter is invalid or the protocol is violated.
func Serve(filters map[string]any, options ...Option) error {
	out := os.Stdout
	os.Stdout = os.Stderr
	return serve(os.Stdin, out, filters, options...)
}

// serve answers the requests read from r by writing responses to w, see Serve
func serve(r io.Reader, w io.Writer, filters map[string]any, options ...Option) error {
	handshake, err := newHandshake(filters, options...)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(w)
	if err := encoder.Encode(handshake); err != nil {
		return fmt.Errorf("failed to write the handshake: %w", err)
	}

	decoder := json.NewDecoder(r)
	for {
		var request Request
		if err := decoder.Decode(&request); errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return fmt.Errorf("invalid request: %w", err)
		}

		if err := encoder.Encode(handle(filters, request)); err != nil {
			return fmt.Errorf("failed to write the response: %w", err)
		}
	}
}

// newHandshake returns the handshake of the plugin: the filters with their descriptions and params, if they provide
// them, and the version and description of the options
func newHandshake(filters map[string]any, options ...Option) (Handshake, error) {
	handshake := Handshake{Protocol: ProtocolVersion, Filters: make([]string, 0, len(filters)), Version: buildVersion()}
	for name, filter := range filters {
		if _, ok := filter.(processor); !ok {
			return Handshake{}, fmt.Errorf("filter %s of type %T does not implement Process(value any) (any, error)", name, filter)
		}
		handshake.Filters = append(handshake.Filters, name)

		var info FilterInfo
		described, isDescriber := filter.(describer)
		if isDescriber {
			info.Description = described.Description()
		}
		declared, isDeclarer := filter.(paramsDeclarer)
		if isDeclarer {
			// An empty list declares that the filter accepts no params, unlike nil
			info.Params = append([]string{}, declared.Params()...)
		}
		if isDescriber || isDeclarer {
			if handshake.FilterInfo == nil {
				handshake.FilterInfo = map[string]FilterInfo{}
			}
			handshake.FilterInfo[name] = info
		}
	}
	slices.Sort(handshake.Filters)

	for _, option := range options {
		option(&handshake)
	}
	return handshake, nil
}

// handle processes the value of the request with a copy of the requested filter
func handle(filters map[string]any, request Request) Response {
	filter, found := filters[request.Filter]
	if !found {
		return Response{ID: request.ID, Error: fmt.Sprintf("unknown filter %s", request.Filter)}
	}

	value, err := DecodeValue(request.Value)
	if err != nil {
		return Response{ID: request.ID, Error: err.Error()}
	}

	result, err := process(clone(filter), request.Params, value)
	if err != nil {
		return Response{ID: request.ID, Error: err.Error()}
	}

	encoded, err := EncodeValue(result)
	if err != nil {
		return Response{ID: request.ID, Error: fmt.Sprintf("invalid result: %s", err)}
	}
	return Response{ID: request.ID, Value: encoded}
}

// process passes the params to the filter, like gonfig only if the usage has params, and processes the value.
// A panicking filter fails the request instead of the plugin.
func process(filter any, params map[string]string, value any) (result any, err error) {
	defer func() {
		if recover() != nil {
			// The panic is not part of the error, since it may contain the value
			result, err = nil, errors.New("the filter panicked")
		}
	}()

	if acceptor, ok := filter.(paramsAcceptor); ok && params != nil {
		acceptor.AcceptParams(params)
	}
	return filter.(processor).Process(value)
}

// clone returns a shallow copy of a filter provided by a pointer, so that params accepted by one request
// do not affect other requests. Filters provided by value are copied when their methods are called anyway.
func clone(filter any) any {
	v := reflect.ValueOf(filter)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return filter
	}

	copied := reflect.New(v.Elem().Type())
	copied.Elem().Set(v.Elem())
	return copied.Interface()
}
