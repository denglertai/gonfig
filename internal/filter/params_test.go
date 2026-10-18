package filter

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestValidateParams checks which params of a usage are accepted for the params a filter declares
func TestValidateParams(t *testing.T) {
	tests := []struct {
		name    string
		filter  string
		params  map[string]string
		wantErr string
	}{
		{name: "no params", filter: "upper"},
		{name: "empty brackets", filter: "upper", params: map[string]string{}},
		{name: "positional", filter: "replace", params: map[string]string{"0": "a", "1": "b"}},
		{name: "named", filter: "replace", params: map[string]string{"old": "a", "new": "b"}},
		{name: "positional and named", filter: "replace", params: map[string]string{"0": "a", "new": "b"}},
		{name: "fewer than declared", filter: "regex", params: map[string]string{"0": "a"}},
		{name: "empty params of a comma", filter: "split", params: map[string]string{"0": "", "1": ""}},
		{name: "trailing empty param", filter: "regex", params: map[string]string{"0": "a", "1": "b", "2": ""}},
		{name: "empty params without declared params", filter: "upper", params: map[string]string{"0": "", "1": ""}},
		{
			name: "unknown name without declared params", filter: "bcrypt", params: map[string]string{"cost": "4"},
			wantErr: `filter bcrypt accepts no params, but got "cost"`,
		},
		{
			name: "unknown name", filter: "default", params: map[string]string{"vaule": "x"},
			wantErr: `filter default has no param "vaule", it accepts value`,
		},
		{
			name: "empty name", filter: "default", params: map[string]string{"": "x"},
			wantErr: `filter default has no param "", it accepts value`,
		},
		{
			name: "positional without declared params", filter: "trim", params: map[string]string{"0": "1", "1": "2"},
			wantErr: "filter trim accepts no params, but got 2",
		},
		{
			name: "more positional than one declared", filter: "split", params: map[string]string{"0": ";", "1": "x"},
			wantErr: "filter split accepts 1 param (delimiter), but got 2",
		},
		{
			name: "more positional than declared", filter: "replace", params: map[string]string{"0": "a", "1": "b", "2": "c"},
			wantErr: "filter replace accepts 2 params (old, new), but got 3",
		},
		{
			name: "non-empty param after empty ones", filter: "replace", params: map[string]string{"0": "", "1": "", "2": "", "3": "x"},
			wantErr: "filter replace accepts 2 params (old, new), but got 4",
		},
		{
			name: "by position and by name", filter: "replace", params: map[string]string{"0": "a", "old": "b"},
			wantErr: "filter replace got the param old twice, by position and by name",
		},
		{
			name: "negative position as name", filter: "split", params: map[string]string{"-1": "a"},
			wantErr: `filter split has no param "-1", it accepts delimiter`,
		},
		{
			name: "position with sign as name", filter: "multiply", params: map[string]string{"+0": "3"},
			wantErr: `filter multiply has no param "+0", it accepts m`,
		},
		{
			name: "position with leading zero as name", filter: "replace", params: map[string]string{"00": "a", "1": "b"},
			wantErr: `filter replace has no param "00", it accepts old, new`,
		},
		{
			name: "negative position without declared params", filter: "upper", params: map[string]string{"-1": "a"},
			wantErr: `filter upper accepts no params, but got "-1"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateParams(tt.filter, NewFilter(tt.filter), tt.params)
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tt.wantErr)
			}
		})
	}
}

// TestValidateParamsWithoutDeclaredParams checks that filters not declaring their params, e.g. of plugins or unknown ones, accept any params
func TestValidateParamsWithoutDeclaredParams(t *testing.T) {
	params := map[string]string{"0": "a", "encoding": "base64"}
	assert.NoError(t, ValidateParams("sha256", &DefaultFilterParamsHandlerFilter{}, params))
	assert.NoError(t, ValidateParams("not_a_filter", NewFilter("not_a_filter"), params))
}

// DefaultFilterParamsHandlerFilter is a filter accepting params like plugin filters, without declaring them
type DefaultFilterParamsHandlerFilter struct {
	DefaultFilterParamsHandler
}

// Process returns the value unchanged
func (f *DefaultFilterParamsHandlerFilter) Process(value any) (any, error) {
	return value, nil
}

// TestBuiltinFiltersDeclareParams checks that every built-in filter declares its params, so unknown params are rejected
func TestBuiltinFiltersDeclareParams(t *testing.T) {
	for name, constructor := range filterMap {
		_, declares := constructor(name).(ParamsDeclarer)
		assert.True(t, declares, name)
	}
}

// TestMultiplyPositionalParam checks that the multiplier may be given by position, since it is declared as the first param
func TestMultiplyPositionalParam(t *testing.T) {
	for _, params := range []map[string]string{{"0": "3"}, {"m": "3"}} {
		f := NewFilter("multiply")
		f.(FilterParams).AcceptParams(params)
		got, err := f.Process(2)
		assert.NoError(t, err)
		assert.Equal(t, 6, got, params)
	}
}
