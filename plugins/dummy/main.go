package main

import (
	"fmt"
	"os"
	"unicode/utf8"

	"github.com/denglertai/gonfig/pkg/plugin"
)

// main serves the filters to gonfig until gonfig is done
func main() {
	if err := plugin.Serve(Filter.Filters()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// DummyFilterRegister provides the filters of this plugin
type DummyFilterRegister struct{}

// Filters returns a map of filters
func (f *DummyFilterRegister) Filters() map[string]interface{} {
	return map[string]interface{}{
		"reverse": ReverseFilter{},
	}
}

// Filter provides the filters. It was the symbol gonfig looked up in Go plugins (.so files), and is kept to show
// that the filters of such a plugin can be served unchanged.
var Filter = DummyFilterRegister{}

// ReverseFilter reverses the value, e.g. ${VAL | reverse}
type ReverseFilter struct {
}

// Process executes the filter's function
func (ReverseFilter) Process(value any) (any, error) {
	// Values may be of any type, e.g. after to_int
	return reverse(fmt.Sprint(value)), nil
}

// reverse reverses the runes of s
func reverse(s string) string {
	size := len(s)
	buf := make([]byte, size)
	for start := 0; start < size; {
		r, n := utf8.DecodeRuneInString(s[start:])
		start += n
		utf8.EncodeRune(buf[size-start:], r)
	}
	return string(buf)
}
