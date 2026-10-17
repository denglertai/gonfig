package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// suffixFilter appends its param "suffix" to the value. It is provided by a pointer and keeps the params,
// so it shows whether every request gets its own copy.
type suffixFilter struct {
	params map[string]string
}

// Process appends the suffix of the params to the value
func (f *suffixFilter) Process(value any) (any, error) {
	return value.(string) + f.params["suffix"], nil
}

// AcceptParams keeps the params
func (f *suffixFilter) AcceptParams(params map[string]string) {
	f.params = params
}

// funcFilter is a filter provided by a function
type funcFilter func(value any) (any, error)

// Process calls the function
func (f funcFilter) Process(value any) (any, error) {
	return f(value)
}

// testFilters are the filters served in the tests
func testFilters() map[string]any {
	return map[string]any{
		"suffix": &suffixFilter{},
		"double": funcFilter(func(value any) (any, error) { return value.(int) * 2, nil }),
		"fail":   funcFilter(func(value any) (any, error) { return nil, errors.New("the value is not encrypted") }),
		"panic":  funcFilter(func(value any) (any, error) { panic("s3cret") }),
		"map":    funcFilter(func(value any) (any, error) { return map[string]string{}, nil }),
	}
}

// runServe serves the test filters for the request lines and returns the lines written by serve
func runServe(t *testing.T, requests ...string) ([]string, error) {
	t.Helper()

	var out bytes.Buffer
	err := serve(strings.NewReader(strings.Join(requests, "\n")), &out, testFilters())
	return strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n"), err
}

// TestServe checks the handshake and the responses to requests, including errors of filters
func TestServe(t *testing.T) {
	lines, err := runServe(t,
		`{"id":0,"filter":"suffix","params":{"suffix":"-a"},"value":"x"}`,
		`{"id":1,"filter":"suffix","value":"y"}`,
		`{"id":2,"filter":"double","value":21}`,
		`{"id":3,"filter":"fail","value":"s3cret"}`,
		`{"id":4,"filter":"panic","value":"s3cret"}`,
		`{"id":5,"filter":"map","value":"x"}`,
		`{"id":6,"filter":"unknown","value":"x"}`,
		`{"id":7,"filter":"suffix","value":{"s3cret":1}}`,
	)
	assert.NoError(t, err)
	assert.Equal(t, []string{
		`{"protocol":1,"filters":["double","fail","map","panic","suffix"]}`,
		`{"id":0,"value":"x-a"}`,
		// The params of the first request do not affect the second one
		`{"id":1,"value":"y"}`,
		`{"id":2,"value":42}`,
		`{"id":3,"error":"the value is not encrypted"}`,
		`{"id":4,"error":"the filter panicked"}`,
		`{"id":5,"error":"invalid result: values of type map[string]string are not supported"}`,
		`{"id":6,"error":"unknown filter unknown"}`,
		`{"id":7,"error":"values have to be strings, numbers, booleans or lists of strings"}`,
	}, lines)
}

// TestServeInvalidRequest checks that a request that is not JSON ends serving with an error
func TestServeInvalidRequest(t *testing.T) {
	lines, err := runServe(t, `{"id":0,"filter":"suffix","value":"x"}`, `not json`)
	assert.ErrorContains(t, err, "invalid request")
	assert.Len(t, lines, 2)
}

// TestServeInvalidFilter checks that a filter without Process is rejected before the handshake
func TestServeInvalidFilter(t *testing.T) {
	var out bytes.Buffer
	err := serve(strings.NewReader(""), &out, map[string]any{"invalid": "not a filter"})
	assert.ErrorContains(t, err, "filter invalid of type string does not implement Process")
	assert.Empty(t, out.String())
}

// failingWriter fails after writing the given number of lines
type failingWriter struct {
	lines int
}

// Write fails once the lines are written
func (w *failingWriter) Write(p []byte) (int, error) {
	if w.lines == 0 {
		return 0, errors.New("broken pipe")
	}
	w.lines--
	return len(p), nil
}

// TestServeWriteErrors checks that errors writing the handshake or a response end serving
func TestServeWriteErrors(t *testing.T) {
	err := serve(strings.NewReader(""), &failingWriter{lines: 0}, testFilters())
	assert.ErrorContains(t, err, "failed to write the handshake")

	err = serve(strings.NewReader(`{"id":0,"filter":"suffix","value":"x"}`), &failingWriter{lines: 1}, testFilters())
	assert.ErrorContains(t, err, "failed to write the response")
}

// TestResponseJson checks that responses omit the empty value or error
func TestResponseJson(t *testing.T) {
	data, err := json.Marshal(Response{ID: 1, Error: "failed"})
	assert.NoError(t, err)
	assert.Equal(t, `{"id":1,"error":"failed"}`, string(data))
}

// TestServeStdio checks that Serve uses stdin and stdout for the protocol and redirects os.Stdout to stderr,
// so output of filters cannot corrupt the protocol
func TestServeStdio(t *testing.T) {
	stdin, stdout := os.Stdin, os.Stdout
	t.Cleanup(func() { os.Stdin, os.Stdout = stdin, stdout })

	inRead, inWrite, err := os.Pipe()
	assert.NoError(t, err)
	outRead, outWrite, err := os.Pipe()
	assert.NoError(t, err)
	os.Stdin, os.Stdout = inRead, outWrite

	_, err = inWrite.WriteString(`{"id":0,"filter":"print","value":"x"}` + "\n")
	assert.NoError(t, err)
	assert.NoError(t, inWrite.Close())

	printed := ""
	err = Serve(map[string]any{"print": funcFilter(func(value any) (any, error) {
		printed = fmt.Sprint(os.Stdout == os.Stderr)
		return value, nil
	})})
	assert.NoError(t, err)
	assert.NoError(t, outWrite.Close())

	output, err := io.ReadAll(outRead)
	assert.NoError(t, err)
	assert.Equal(t, "{\"protocol\":1,\"filters\":[\"print\"]}\n{\"id\":0,\"value\":\"x\"}\n", string(output))
	assert.Equal(t, "true", printed)
}
