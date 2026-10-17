package main

import (
	"fmt"
	"unicode/utf8"
)

// DummyFilterRegister provides the filters of this plugin
type DummyFilterRegister struct{}

// Filters returns a map of filters
func (f *DummyFilterRegister) Filters() map[string]interface{} {
	return map[string]interface{}{
		"reverse": ReverseFilter{},
	}
}

// Filter is the symbol gonfig looks up when loading the plugin
var Filter = DummyFilterRegister{}

// ReverseFilter reverses the value, e.g. ${VAL | reverse}
type ReverseFilter struct {
}

// Process executes the filter's function
func (ReverseFilter) Process(value any) (any, error) {
	// Values may be of any type, e.g. after to_int
	return reverse(fmt.Sprint(value)), nil
}

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
