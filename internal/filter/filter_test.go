package filter

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/bcrypt"
)

func TestBcryptFilter(t *testing.T) {
	testCases := []struct {
		desc           string
		input          string
		expected       string
		wantErrResult  bool
		wantErrProcess bool
	}{
		{
			desc:           "empty input",
			input:          "",
			wantErrResult:  false,
			wantErrProcess: false,
		},
		{
			desc:           "with input",
			input:          "adgadgadgqadg",
			wantErrResult:  false,
			wantErrProcess: false,
		},
		{
			desc: "with input",
			// bcrypt fails if the input is longer than 72 bytes
			input:          "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
			wantErrResult:  false,
			wantErrProcess: true,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			filter := NewFilter(bcryptFilterKey)
			assert.NotNil(t, filter)
			assert.IsType(t, &FuncFilter{}, filter)

			result, err := filter.Process(tC.input)
			if tC.wantErrProcess {
				// if an error is expected, we don't need to check the result
				assert.Error(t, err)
			} else {
				// if an error is not expected, we need to check the result and compare it with the expected value
				assert.NoError(t, err)
				assert.NotEmpty(t, result)
				err = bcrypt.CompareHashAndPassword([]byte(result.(string)), []byte(tC.input))
				if tC.wantErrResult {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			}
		})
	}
}

// TestBcryptCost checks that the bcrypt filter hashes with the cost param, given by name or position, and with
// exactly 10 without it. Costs out of range are an error, since bcrypt would silently use 10 for costs below 4.
func TestBcryptCost(t *testing.T) {
	testCases := []struct {
		desc     string
		params   map[string]string
		wantCost int
		wantErr  string
	}{
		{desc: "without params", params: nil, wantCost: 10},
		{desc: "empty brackets", params: map[string]string{}, wantCost: 10},
		{desc: "by name", params: map[string]string{"cost": "5"}, wantCost: 5},
		{desc: "by position", params: map[string]string{"0": "4"}, wantCost: 4},
		{desc: "below the minimum", params: map[string]string{"cost": "3"}, wantErr: `filter bcrypt: the cost "3" is not an integer from 4 to 31`},
		{desc: "above the maximum", params: map[string]string{"cost": "32"}, wantErr: `filter bcrypt: the cost "32" is not an integer from 4 to 31`},
		{desc: "not an integer", params: map[string]string{"cost": "high"}, wantErr: `filter bcrypt: the cost "high" is not an integer from 4 to 31`},
		{desc: "empty", params: map[string]string{"cost": ""}, wantErr: `filter bcrypt: the cost "" is not an integer from 4 to 31`},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			filter := NewFilter(bcryptFilterKey)
			assert.NoError(t, ValidateParams(bcryptFilterKey, filter, tC.params))
			if tC.params != nil {
				filter.(FilterParams).AcceptParams(tC.params)
			}

			result, err := filter.Process("s3cret")
			if tC.wantErr != "" {
				assert.EqualError(t, err, tC.wantErr)
				return
			}
			assert.NoError(t, err)

			cost, err := bcrypt.Cost([]byte(result.(string)))
			assert.NoError(t, err)
			assert.Equal(t, tC.wantCost, cost)
			assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(result.(string)), []byte("s3cret")))
		})
	}
}

func TestMd5Filter(t *testing.T) {
	testCases := []struct {
		desc           string
		input          string
		expected       string
		wantErrResult  bool
		wantErrProcess bool
	}{
		{
			desc:           "empty input",
			input:          "",
			expected:       "d41d8cd98f00b204e9800998ecf8427e",
			wantErrResult:  false,
			wantErrProcess: false,
		},
		{
			desc:           "with input",
			input:          "adgadgadgqadg",
			expected:       "739c50cce04bb3f39181b05f0939c9d3",
			wantErrResult:  false,
			wantErrProcess: false,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			filter := NewFilter(md5FilterKey)
			assert.NotNil(t, filter)
			assert.IsType(t, &FuncFilter{}, filter)

			result, err := filter.Process(tC.input)
			if tC.wantErrProcess {
				// if an error is expected, we don't need to check the result
				assert.Error(t, err)
			} else {
				// if an error is not expected, we need to check the result and compare it with the expected value
				assert.NoError(t, err)
				assert.NotEmpty(t, result)
				assert.Equal(t, tC.expected, result)
			}
		})
	}
}

func TestFiltersWithNonStringInput(t *testing.T) {
	testCases := []struct {
		desc     string
		filter   string
		params   map[string]string
		input    any
		expected any
		wantErr  bool
	}{
		{desc: "upper on int", filter: "upper", input: 5, expected: "5"},
		{desc: "md5 on int", filter: md5FilterKey, input: 123, expected: "202cb962ac59075b964b07152d234b70"},
		{desc: "to_int on int", filter: "to_int", input: 5, expected: 5},
		{desc: "multiply on numeric string", filter: "multiply", params: map[string]string{"m": "2"}, input: "3", expected: 6},
		{desc: "multiply on non-numeric", filter: "multiply", input: true, wantErr: true},
		{desc: "to_float on string", filter: "to_float", input: "1.5", expected: 1.5},
		{desc: "to_float on int", filter: "to_float", input: 2, expected: 2.0},
		{desc: "to_float on float", filter: "to_float", input: 2.5, expected: 2.5},
		{desc: "to_float on non-numeric", filter: "to_float", input: "abc", wantErr: true},
		{desc: "to_float on infinity", filter: "to_float", input: "Inf", wantErr: true},
		{desc: "to_float on NaN", filter: "to_float", input: "NaN", wantErr: true},
		{desc: "to_float on bool", filter: "to_float", input: true, wantErr: true},
		{desc: "to_bool on true", filter: "to_bool", input: "true", expected: true},
		{desc: "to_bool on 0", filter: "to_bool", input: "0", expected: false},
		{desc: "to_bool on bool", filter: "to_bool", input: true, expected: true},
		{desc: "to_bool on yes", filter: "to_bool", input: "yes", wantErr: true},
		{desc: "to_bool on int", filter: "to_bool", input: 1, wantErr: true},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			filter := NewFilter(tC.filter)
			if tC.params != nil {
				filter.(FilterParams).AcceptParams(tC.params)
			}

			result, err := filter.Process(tC.input)
			if tC.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tC.expected, result)
		})
	}
}

// paramFilter is a plugin-like filter returning its parameter
type paramFilter struct {
	DefaultFilterParamsHandler
}

func (f *paramFilter) Process(_ any) (any, error) {
	return f.params["v"], nil
}

// TestPluginFiltersDoNotShareParams checks that every usage of a plugin filter gets its own copy receiving its params
func TestPluginFiltersDoNotShareParams(t *testing.T) {
	added := AddPluginFilters(map[string]interface{}{"test_param_filter": &paramFilter{}})
	t.Cleanup(func() { RemovePluginFilters(added) })

	first := NewFilter("test_param_filter")
	second := NewFilter("test_param_filter")
	first.(FilterParams).AcceptParams(map[string]string{"v": "first"})
	second.(FilterParams).AcceptParams(map[string]string{"v": "second"})

	result, err := first.Process("")
	assert.NoError(t, err)
	assert.Equal(t, "first", result)
}

// TestAddAndRemovePluginFilters checks that only added filters are returned, skipping invalid filters and taken
// names, and that removed filters are unknown again
func TestAddAndRemovePluginFilters(t *testing.T) {
	added := AddPluginFilters(map[string]interface{}{
		"test_added_filter": &paramFilter{},
		"upper":             &paramFilter{},
		"test_invalid":      "not a filter",
	})
	assert.Equal(t, []string{"test_added_filter"}, added)
	assert.True(t, IsKnown("test_added_filter"))

	RemovePluginFilters(added)
	assert.False(t, IsKnown("test_added_filter"))
	assert.True(t, IsKnown("upper"))
}

// TestStringManipulationFilters checks the encoding, replacing, URL and list filters, including their params and errors
func TestStringManipulationFilters(t *testing.T) {
	testCases := []struct {
		desc     string
		filter   string
		params   map[string]string
		input    any
		expected any
		wantErr  bool
	}{
		{desc: "base64 empty", filter: "base64", input: "", expected: ""},
		{desc: "base64", filter: "base64", input: "hello world", expected: "aGVsbG8gd29ybGQ="},
		{desc: "base64 binary", filter: "base64", input: "\x00\xff\x10", expected: "AP8Q"},
		{desc: "base64 on int", filter: "base64", input: 42, expected: "NDI="},
		{desc: "base64_decode empty", filter: "base64_decode", input: "", expected: ""},
		{desc: "base64_decode", filter: "base64_decode", input: "aGVsbG8gd29ybGQ=", expected: "hello world"},
		{desc: "base64_decode binary", filter: "base64_decode", input: "AP8Q", expected: "\x00\xff\x10"},
		{desc: "base64_decode invalid", filter: "base64_decode", input: "not base64!", wantErr: true},
		{desc: "replace positional", filter: "replace", params: map[string]string{"0": "foo", "1": "bar"}, input: "foo-foo", expected: "bar-bar"},
		{desc: "replace named", filter: "replace", params: map[string]string{"old": "foo", "new": "bar"}, input: "foo", expected: "bar"},
		{desc: "replace with empty", filter: "replace", params: map[string]string{"0": "-", "1": ""}, input: "a-b-c", expected: "abc"},
		{desc: "replace missing new", filter: "replace", params: map[string]string{"0": "foo"}, input: "foo", wantErr: true},
		{desc: "replace missing params", filter: "replace", input: "foo", wantErr: true},
		{desc: "regex back reference", filter: "regex", params: map[string]string{"0": `^(\w+)@.+`, "1": `\1`}, input: "jane@example.com", expected: "jane"},
		{desc: "regex go reference", filter: "regex", params: map[string]string{"0": `(\d+)`, "1": `<${1}>`}, input: "a1b22", expected: "a<1>b<22>"},
		{desc: "regex no match", filter: "regex", params: map[string]string{"0": `x`, "1": `y`}, input: "abc", expected: "abc"},
		{desc: "regex invalid pattern", filter: "regex", params: map[string]string{"0": `(`, "1": ``}, input: "abc", wantErr: true},
		{desc: "regex missing replacement", filter: "regex", params: map[string]string{"0": `a`}, input: "abc", wantErr: true},
		{desc: "split default delimiter", filter: "split", input: "a,b,c", expected: []string{"a", "b", "c"}},
		{desc: "split custom delimiter", filter: "split", params: map[string]string{"0": "; "}, input: "a; b", expected: []string{"a", "b"}},
		{desc: "split empty", filter: "split", input: "", expected: []string{}},
		{desc: "split with empty delimiter uses comma", filter: "split", params: map[string]string{"0": "", "1": ""}, input: "a,b", expected: []string{"a", "b"}},
		{desc: "join with empty delimiter uses comma", filter: "join", params: map[string]string{"0": "", "1": ""}, input: []string{"a", "b"}, expected: "a,b"},
		{desc: "join empty list", filter: "join", input: []string{}, expected: ""},
		{desc: "join default delimiter", filter: "join", input: []string{"a", "b"}, expected: "a,b"},
		{desc: "join custom delimiter", filter: "join", params: map[string]string{"delimiter": " "}, input: []string{"a", "b"}, expected: "a b"},
		{desc: "join single value", filter: "join", input: "a", expected: "a"},
		{desc: "upper on list", filter: "upper", input: []string{"a", "b"}, expected: []string{"A", "B"}},
		{desc: "base64_decode on list with invalid element", filter: "base64_decode", input: []string{"YQ==", "!"}, wantErr: true},
		{desc: "url_encode query", filter: "url_encode", input: "p@ss/w rd&=?", expected: "p%40ss%2Fw+rd%26%3D%3F"},
		{desc: "url_encode path", filter: "url_encode", params: map[string]string{"0": "path"}, input: "a b/c", expected: "a%20b%2Fc"},
		{desc: "url_encode named mode", filter: "url_encode", params: map[string]string{"mode": "query"}, input: "a b", expected: "a+b"},
		{desc: "url_encode unicode", filter: "url_encode", input: "ä", expected: "%C3%A4"},
		{desc: "url_encode invalid mode", filter: "url_encode", params: map[string]string{"0": "form"}, input: "a", wantErr: true},
		{desc: "url_encode on list", filter: "url_encode", input: []string{"a b", "c&d"}, expected: []string{"a+b", "c%26d"}},
		{desc: "url_decode query", filter: "url_decode", input: "p%40ss%2Fw+rd%26", expected: "p@ss/w rd&"},
		{desc: "url_decode path", filter: "url_decode", params: map[string]string{"0": "path"}, input: "a%20b+c", expected: "a b+c"},
		{desc: "url_decode invalid escape", filter: "url_decode", input: "%zz", wantErr: true},
		{desc: "url_decode invalid mode", filter: "url_decode", params: map[string]string{"0": "form"}, input: "a", wantErr: true},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			filter := NewFilter(tC.filter)
			assert.IsType(t, &FuncFilter{}, filter)
			if tC.params != nil {
				filter.(FilterParams).AcceptParams(tC.params)
			}

			result, err := filter.Process(tC.input)
			if tC.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tC.expected, result)
		})
	}
}

func TestUnknownFilterFails(t *testing.T) {
	filter := NewFilter("not_a_filter")

	result, err := filter.Process("s3cret")

	assert.ErrorContains(t, err, `unknown filter "not_a_filter"`)
	assert.NotEqual(t, "s3cret", result)
}

func TestUnknownFilterAllowed(t *testing.T) {
	assert.False(t, UnknownFiltersAllowed())
	SetAllowUnknownFilters(true)
	t.Cleanup(func() { SetAllowUnknownFilters(false) })
	assert.True(t, UnknownFiltersAllowed())

	result, err := NewFilter("not_a_filter").Process("value")

	assert.NoError(t, err)
	assert.Equal(t, "value", result)
}

func TestDefaultValueFilter(t *testing.T) {
	filter := NewDefaultValueFilter("fallback")

	for input, expected := range map[any]any{"": "fallback", "value": "value", 0: 0} {
		result, err := filter.Process(input)
		assert.NoError(t, err)
		assert.Equal(t, expected, result)
	}
}

func TestChainFilter(t *testing.T) {
	result, err := NewChainFilter([]Filter{NewFilter("trim"), NewFilter("upper")}).Process(" abc ")
	assert.NoError(t, err)
	assert.Equal(t, "ABC", result)

	_, err = NewChainFilter([]Filter{NewFilter("to_int")}).Process("abc")
	assert.Error(t, err)
}

func TestErrorFilter(t *testing.T) {
	_, err := NewErrorFilter(assert.AnError).Process("value")
	assert.ErrorIs(t, err, assert.AnError)
}

func TestIsKnown(t *testing.T) {
	assert.True(t, IsKnown("trim"))
	assert.True(t, IsKnown("url_encode"))
	assert.False(t, IsKnown("not_a_filter"))
}

func TestFragmentFilter(t *testing.T) {
	result, err := NewFilter("fragment").Process("a: 1")
	assert.NoError(t, err)
	assert.Equal(t, Fragment{Text: "a: 1"}, result)
	assert.Equal(t, "a: 1", result.(Fragment).String())

	// Values of other types are formatted as text
	result, err = NewFilter("fragment").Process(5)
	assert.NoError(t, err)
	assert.Equal(t, Fragment{Text: "5"}, result)
}

// TestFragmentFilterWithLists checks that lists become fragments containing JSON arrays, which YAML and JSON files insert as lists
func TestFragmentFilterWithLists(t *testing.T) {
	result, err := NewFilter("fragment").Process([]string{"alpha", "beta", `with "quotes", commas`})
	assert.NoError(t, err)
	assert.Equal(t, Fragment{Text: `["alpha","beta","with \"quotes\", commas"]`, IsList: true}, result)

	result, err = NewFilter("fragment").Process([]string{})
	assert.NoError(t, err)
	assert.Equal(t, Fragment{Text: "[]", IsList: true}, result)
}

// TestDefaultAndRequiredFilters checks that default replaces and required rejects empty values, leaving other values unchanged
func TestDefaultAndRequiredFilters(t *testing.T) {
	testCases := []struct {
		desc     string
		filter   string
		params   map[string]string
		input    any
		expected any
		wantErr  bool
	}{
		{desc: "default replaces empty value", filter: "default", params: map[string]string{"0": "5432"}, input: "", expected: "5432"},
		{desc: "default keeps value", filter: "default", params: map[string]string{"0": "5432"}, input: "6543", expected: "6543"},
		{desc: "default by name", filter: "default", params: map[string]string{"value": "x"}, input: "", expected: "x"},
		{desc: "default without param", filter: "default", input: "", expected: ""},
		{desc: "default keeps other types", filter: "default", params: map[string]string{"0": "x"}, input: 0, expected: 0},
		{desc: "required keeps value", filter: "required", input: "value", expected: "value"},
		{desc: "required keeps other types", filter: "required", input: 0, expected: 0},
		{desc: "required rejects empty value", filter: "required", input: "", wantErr: true},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			filter := NewFilter(tC.filter)
			if tC.params != nil {
				filter.(FilterParams).AcceptParams(tC.params)
			}

			result, err := filter.Process(tC.input)
			if tC.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tC.expected, result)
		})
	}
}

// TestEnvVarFilterWithFailOnMissing checks that only variables that are not set fail, and only without a default
func TestEnvVarFilterWithFailOnMissing(t *testing.T) {
	t.Setenv("ENV_FILTER_EMPTY", "")
	assert.False(t, FailOnMissing())

	// Missing variables are empty by default
	result, err := NewEnvVarFilter("ENV_FILTER_MISSING", false).Process(nil)
	assert.NoError(t, err)
	assert.Equal(t, "", result)

	SetFailOnMissing(true)
	t.Cleanup(func() { SetFailOnMissing(false) })
	assert.True(t, FailOnMissing())

	_, err = NewEnvVarFilter("ENV_FILTER_MISSING", false).Process(nil)
	assert.EqualError(t, err, `environment variable "ENV_FILTER_MISSING" is not set`)

	result, err = NewEnvVarFilter("ENV_FILTER_MISSING", true).Process(nil)
	assert.NoError(t, err)
	assert.Equal(t, "", result)

	// A variable that is set but empty is not missing
	result, err = NewEnvVarFilter("ENV_FILTER_EMPTY", false).Process(nil)
	assert.NoError(t, err)
	assert.Equal(t, "", result)
}

// TestErrorsDoNotContainValues runs every registered filter on inputs containing a secret and checks that no error
// message contains it, so filters added later are covered as well
func TestErrorsDoNotContainValues(t *testing.T) {
	const secret = "s3cret-%zz-value!"
	inputs := []any{
		secret,
		strings.Repeat(secret, 5), // longer than bcrypt allows
		[]string{secret, secret},  // a list, e.g. of split
	}

	failed := map[string]bool{}
	for name := range filterMap {
		for _, input := range inputs {
			_, err := NewFilter(name).Process(input)
			if err != nil {
				failed[name] = true
				assert.NotContains(t, err.Error(), secret, "filter %s", name)
			}
		}
	}

	// Make sure the inputs actually make the filters fail that used to include values in their errors
	for _, name := range []string{"to_int", "to_float", "to_bool", "url_decode", "base64_decode", "bcrypt", "multiply"} {
		assert.True(t, failed[name], "filter %s did not fail", name)
	}
}

// TestErrorsContainValuesIfEnabled checks that error messages include the value only after enabling SetValuesInErrors
func TestErrorsContainValuesIfEnabled(t *testing.T) {
	assert.False(t, ValuesInErrors())

	_, err := NewFilter("to_int").Process("eighty")
	assert.EqualError(t, err, "filter to_int: the value is not an integer")

	SetValuesInErrors(true)
	t.Cleanup(func() { SetValuesInErrors(false) })
	assert.True(t, ValuesInErrors())

	testCases := []struct {
		filter string
		input  any
		want   string
	}{
		{"to_int", "eighty", `filter to_int: the value is not an integer: "eighty"`},
		{"to_int", []string{"a", "b"}, `filter to_int: a value of type []string cannot be converted to an integer: "[a b]"`},
		{"to_float", "abc", `filter to_float: the value is not a number: "abc"`},
		{"to_float", "Inf", `filter to_float: the value is not a finite number: "Inf"`},
		{"to_float", true, `filter to_float: a value of type bool cannot be converted to a number: "true"`},
		{"to_bool", "yes", `filter to_bool: the value is not a boolean, use true, false, 1 or 0: "yes"`},
		{"to_bool", 1, `filter to_bool: a value of type int cannot be converted to a boolean: "1"`},
		{"url_decode", "a%zz", `filter url_decode: the value contains an invalid escape sequence: "a%zz"`},
	}
	for _, tC := range testCases {
		_, err := NewFilter(tC.filter).Process(tC.input)
		assert.EqualError(t, err, tC.want)
	}
}

// TestLookupRecorder checks that every lookup of an environment variable is recorded with its status and default until recording stops
func TestLookupRecorder(t *testing.T) {
	t.Setenv("RECORDER_SET", "value")
	t.Setenv("RECORDER_EMPTY", "")

	var lookups []VariableLookup
	SetLookupRecorder(func(lookup VariableLookup) { lookups = append(lookups, lookup) })
	t.Cleanup(func() { SetLookupRecorder(nil) })

	for _, f := range []*EnvVarFilter{NewEnvVarFilter("RECORDER_SET", false), NewEnvVarFilter("RECORDER_EMPTY", false), NewEnvVarFilter("RECORDER_MISSING", true)} {
		_, err := f.Process(nil)
		assert.NoError(t, err)
	}

	assert.Equal(t, []VariableLookup{
		{Name: "RECORDER_SET", Set: true},
		{Name: "RECORDER_EMPTY", Set: true, Empty: true},
		{Name: "RECORDER_MISSING", HasDefault: true},
	}, lookups)

	SetLookupRecorder(nil)
	_, err := NewEnvVarFilter("RECORDER_SET", false).Process(nil)
	assert.NoError(t, err)
	assert.Len(t, lookups, 3)
}
