package plugin

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// stringer is a value implementing fmt.Stringer, like gonfig's fragments
type stringer struct{}

// String returns the text of the value
func (stringer) String() string {
	return "a: 1"
}

// TestEncodeValue checks the JSON of the supported values and that they are decoded to the same value
func TestEncodeValue(t *testing.T) {
	testCases := []struct {
		desc  string
		value any
		want  string
		// decoded is the value after decoding, if it differs from value
		decoded any
	}{
		{desc: "string", value: "s3cret", want: `"s3cret"`},
		{desc: "empty string", value: "", want: `""`},
		{desc: "int", value: 42, want: `42`},
		{desc: "negative int", value: -7, want: `-7`},
		{desc: "float", value: 1.5, want: `1.5`},
		{desc: "whole float stays a float", value: 5.0, want: `5.0`},
		{desc: "large float", value: 1e21, want: `1e+21`},
		{desc: "bool", value: true, want: `true`},
		{desc: "list", value: []string{"a", "b"}, want: `["a","b"]`},
		{desc: "empty list", value: []string{}, want: `[]`},
		{desc: "stringer", value: stringer{}, want: `"a: 1"`, decoded: "a: 1"},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			data, err := EncodeValue(tC.value)
			assert.NoError(t, err)
			assert.Equal(t, tC.want, string(data))

			decoded, err := DecodeValue(data)
			assert.NoError(t, err)
			if tC.decoded == nil {
				tC.decoded = tC.value
			}
			assert.Equal(t, tC.decoded, decoded)
		})
	}
}

// TestEncodeValueErrors checks that unsupported values are rejected
func TestEncodeValueErrors(t *testing.T) {
	for _, value := range []any{math.NaN(), math.Inf(1), int64(1), map[string]string{}, nil} {
		_, err := EncodeValue(value)
		assert.Error(t, err, "%#v", value)
	}
}

// TestDecodeValueErrors checks that unsupported values are rejected without repeating them in the error
func TestDecodeValueErrors(t *testing.T) {
	for _, data := range []string{`{"s3cret": 1}`, `null`, `["s3cret", 1]`, `99999999999999999999`, `"s3cret`} {
		_, err := DecodeValue(json.RawMessage(data))
		if assert.Error(t, err, data) {
			assert.NotContains(t, err.Error(), "s3cret")
		}
	}
}
