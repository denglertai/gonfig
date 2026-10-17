package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
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

// Serve runs an executable plugin providing the filters: it writes the Handshake and answers requests of gonfig
// until stdin is closed. The keys of the map are the filter names, the values the filters, which are the same as
// for the deprecated Go plugins:
//
//	// Required: transforms the value, which is usually a string but may be an int, float64, bool or []string
//	Process(value any) (any, error)
//	// Optional: receives the params given in brackets, e.g. encrypt(encoding=base64)
//	AcceptParams(params map[string]string)
//
// Every request works on its own shallow copy of the filter, so params of one usage do not affect another one.
// Stdout is reserved for the protocol, so anything the filters write to os.Stdout goes to stderr instead.
// Serve returns an error if a filter is invalid or the protocol is violated.
func Serve(filters map[string]any) error {
	out := os.Stdout
	os.Stdout = os.Stderr
	return serve(os.Stdin, out, filters)
}

// serve answers the requests read from r by writing responses to w, see Serve
func serve(r io.Reader, w io.Writer, filters map[string]any) error {
	names := make([]string, 0, len(filters))
	for name, filter := range filters {
		if _, ok := filter.(processor); !ok {
			return fmt.Errorf("filter %s of type %T does not implement Process(value any) (any, error)", name, filter)
		}
		names = append(names, name)
	}
	slices.Sort(names)

	encoder := json.NewEncoder(w)
	if err := encoder.Encode(Handshake{Protocol: ProtocolVersion, Filters: names}); err != nil {
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
