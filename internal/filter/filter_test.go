package filter

import (
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

func TestPluginFiltersDoNotShareParams(t *testing.T) {
	AddPluginFilters(map[string]interface{}{"test_param_filter": &paramFilter{}})

	first := NewFilter("test_param_filter")
	second := NewFilter("test_param_filter")
	first.(FilterParams).AcceptParams(map[string]string{"v": "first"})
	second.(FilterParams).AcceptParams(map[string]string{"v": "second"})

	result, err := first.Process("")
	assert.NoError(t, err)
	assert.Equal(t, "first", result)
}

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

func TestFragmentFilterWithLists(t *testing.T) {
	result, err := NewFilter("fragment").Process([]string{"alpha", "beta", `with "quotes", commas`})
	assert.NoError(t, err)
	assert.Equal(t, Fragment{Text: `["alpha","beta","with \"quotes\", commas"]`, IsList: true}, result)

	result, err = NewFilter("fragment").Process([]string{})
	assert.NoError(t, err)
	assert.Equal(t, Fragment{Text: "[]", IsList: true}, result)
}
