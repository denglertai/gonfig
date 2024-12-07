package main

import "unicode/utf8"

// DummyFilter is a filter
type DummyFilterRegister struct{}

// Filters returns a map of filters
func (f *DummyFilterRegister) Filters() map[string]interface{} {
	return map[string]interface{}{
		"reverse": ReverseFilter{},
	}
}

var Filter = DummyFilterRegister{}

type ReverseFilter struct {
}

// Process executes the filter's function
func (ReverseFilter) Process(value any) (any, error) {
	return reverse(value.(string)), nil
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
