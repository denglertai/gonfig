package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ProtocolVersion is the version of the protocol between gonfig and executable plugins.
//
// gonfig starts a plugin and reads its Handshake from stdout. Then it writes one Request per line to the plugin's
// stdin and reads one Response per line from its stdout, in the same order. When gonfig is done, it closes stdin
// and the plugin exits.
const ProtocolVersion = 1

// Handshake is the first line a plugin writes, naming the protocol version and the filters it provides.
// The other fields are optional, so gonfig shows them with gonfig plugin list if a plugin provides them. Older
// versions of gonfig ignore them, as they ignore unknown fields.
type Handshake struct {
	Protocol int      `json:"protocol"`
	Filters  []string `json:"filters"`
	// Version is the version of the plugin, e.g. "1.2.0"
	Version string `json:"version,omitempty"`
	// Description describes the plugin, e.g. "Reads secrets from OpenBao and HashiCorp Vault"
	Description string `json:"description,omitempty"`
	// FilterInfo describes filters by name. Filters without info have no description and accept any params.
	FilterInfo map[string]FilterInfo `json:"filter_info,omitempty"`
}

// FilterInfo describes a filter of a plugin
type FilterInfo struct {
	// Description describes the filter, e.g. "Reads a secret of the KV secrets engine"
	Description string `json:"description,omitempty"`
	// Params are the names of the params the filter accepts, in the order of their positions. gonfig rejects other
	// params before sending a request, like for its built-in filters. nil accepts any params, an empty list none.
	Params []string `json:"params"`
}

// Request asks a plugin to process a value with one of its filters
type Request struct {
	ID     uint64 `json:"id"`
	Filter string `json:"filter"`
	// Params are the params of the filter's usage, e.g. {"0": "base64"} for encrypt(base64), or nil without params
	Params map[string]string `json:"params,omitempty"`
	// Value is the value encoded by EncodeValue
	Value json.RawMessage `json:"value"`
}

// Response is the answer to the Request with the same ID: either the processed value or an error
type Response struct {
	ID uint64 `json:"id"`
	// Value is the processed value encoded by EncodeValue, if Error is empty
	Value json.RawMessage `json:"value,omitempty"`
	// Error describes why the value could not be processed. It must not contain the value, since it may be a secret.
	Error string `json:"error,omitempty"`
}

// EncodeValue encodes a value of a filter as JSON. Supported are strings, integers, floats, booleans and lists of
// strings, e.g. of the split filter. Floats always contain a decimal point or an exponent, so they are decoded as
// floats again. Values implementing fmt.Stringer, e.g. fragments, are encoded as their text.
func EncodeValue(value any) (json.RawMessage, error) {
	switch v := value.(type) {
	case string, bool, []string:
		return json.Marshal(v)
	case int:
		return json.RawMessage(strconv.Itoa(v)), nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, errors.New("floats have to be finite")
		}
		text := strconv.FormatFloat(v, 'g', -1, 64)
		if !strings.ContainsAny(text, ".eE") {
			text += ".0"
		}
		return json.RawMessage(text), nil
	case fmt.Stringer:
		return json.Marshal(v.String())
	default:
		return nil, fmt.Errorf("values of type %T are not supported", value)
	}
}

// DecodeValue decodes a value encoded by EncodeValue. Numbers containing a decimal point or an exponent become
// float64, other numbers int, and arrays of strings []string.
func DecodeValue(data json.RawMessage) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("invalid value: %w", err)
	}

	switch v := value.(type) {
	case string, bool:
		return v, nil
	case json.Number:
		if strings.ContainsAny(v.String(), ".eE") {
			return v.Float64()
		}
		i, err := strconv.Atoi(v.String())
		if err != nil {
			return nil, errors.New("integers have to fit into 64 bits")
		}
		return i, nil
	case []any:
		list := make([]string, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, errors.New("lists may only contain strings")
			}
			list[i] = s
		}
		return list, nil
	default:
		// The value itself is not part of the error, since it may be a secret
		return nil, errors.New("values have to be strings, numbers, booleans or lists of strings")
	}
}
