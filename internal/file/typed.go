package file

import (
	"math"
	"reflect"
	"strconv"
	"strings"
)

// TypedConfigEntry is implemented by the entries of file types supporting typed values, e.g. numbers and booleans in YAML and JSON
type TypedConfigEntry interface {
	ConfigEntry
	// SetTypedValue sets a value of another type than string. It returns false if the entry cannot hold the value,
	// e.g. because it is the key of a mapping or the type is not supported, in which case the entry is not changed.
	SetTypedValue(value any) bool
}

// typedKind is the kind of a typed value
type typedKind int

const (
	typedInt typedKind = iota
	typedFloat
	typedBool
)

// typedValue is a typed value converted to the text representation shared by YAML and JSON
type typedValue struct {
	kind typedKind
	text string
}

// newTypedValue converts integers, finite floats and booleans of any size into a typedValue.
// Plugin filters may return any of these types, so they are handled by their kind.
func newTypedValue(value any) (typedValue, bool) {
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return typedValue{kind: typedInt, text: strconv.FormatInt(v.Int(), 10)}, true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return typedValue{kind: typedInt, text: strconv.FormatUint(v.Uint(), 10)}, true
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return typedValue{}, false
		}

		text := strconv.FormatFloat(f, 'g', -1, v.Type().Bits())
		// Keep a decimal point, otherwise e.g. 2.0 would be read as the integer 2
		if !strings.ContainsAny(text, ".e") {
			text += ".0"
		}
		return typedValue{kind: typedFloat, text: text}, true
	case reflect.Bool:
		return typedValue{kind: typedBool, text: strconv.FormatBool(v.Bool())}, true
	default:
		return typedValue{}, false
	}
}
