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
