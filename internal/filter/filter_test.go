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

func TestBase64Filter(t *testing.T) {
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
			expected:       "",
			wantErrResult:  false,
			wantErrProcess: false,
		},
		{
			desc:           "with input",
			input:          "hello",
			expected:       "aGVsbG8=",
			wantErrResult:  false,
			wantErrProcess: false,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			filter := NewFilter("base64")
			assert.NotNil(t, filter)
			assert.IsType(t, &FuncFilter{}, filter)

			result, err := filter.Process(tC.input)
			if tC.wantErrProcess {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tC.expected, result)
			}
		})
	}
}

func TestBase64DecodeFilter(t *testing.T) {
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
			expected:       "",
			wantErrResult:  false,
			wantErrProcess: false,
		},
		{
			desc:           "with valid base64",
			input:          "aGVsbG8=",
			expected:       "hello",
			wantErrResult:  false,
			wantErrProcess: false,
		},
		{
			desc:           "with invalid base64",
			input:          "not-base64!",
			expected:       "",
			wantErrResult:  true,
			wantErrProcess: false,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			filter := NewFilter("base64_decode")
			assert.NotNil(t, filter)
			assert.IsType(t, &FuncFilter{}, filter)

			result, err := filter.Process(tC.input)
			if tC.wantErrProcess {
				assert.Error(t, err)
			} else {
				if tC.wantErrResult {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
					assert.Equal(t, tC.expected, result)
				}
			}
		})
	}
}

func TestReplaceFilter(t *testing.T) {
	testCases := []struct {
		desc           string
		input          string
		params         map[string]string
		expected       string
		wantErrResult  bool
		wantErrProcess bool
	}{
		{
			desc:  "replace foo with bar",
			input: "foo is foo",
			params: map[string]string{
				"old": "foo",
				"new": "bar",
			},
			expected:       "bar is bar",
			wantErrResult:  false,
			wantErrProcess: false,
		},
		{
			desc:  "replace with empty string",
			input: "hello world",
			params: map[string]string{
				"old": "world",
				"new": "",
			},
			expected:       "hello ",
			wantErrResult:  false,
			wantErrProcess: false,
		},
		{
			desc:  "missing parameters",
			input: "foo",
			params: map[string]string{
				"old": "foo",
			},
			expected:       "",
			wantErrResult:  true,
			wantErrProcess: false,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			filter := NewFilter("replace")
			assert.NotNil(t, filter)
			assert.IsType(t, &FuncFilter{}, filter)

			if pf, ok := filter.(FilterParams); ok {
				pf.AcceptParams(tC.params)
			}

			result, err := filter.Process(tC.input)
			if tC.wantErrProcess {
				assert.Error(t, err)
			} else {
				if tC.wantErrResult {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
					assert.Equal(t, tC.expected, result)
				}
			}
		})
	}
}

func TestRegexFilter(t *testing.T) {
	testCases := []struct {
		desc           string
		input          string
		params         map[string]string
		expected       string
		wantErrResult  bool
		wantErrProcess bool
	}{
		{
			desc:  "extract username from email",
			input: "test@example.com",
			params: map[string]string{
				"pattern":     `^(\w+)@.+`,
				"replacement": "$1",
			},
			expected:       "test",
			wantErrResult:  false,
			wantErrProcess: false,
		},
		{
			desc:  "invalid regex pattern",
			input: "test",
			params: map[string]string{
				"pattern":     `[`,
				"replacement": "$1",
			},
			expected:       "",
			wantErrResult:  true,
			wantErrProcess: false,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			filter := NewFilter("regex")
			assert.NotNil(t, filter)
			assert.IsType(t, &FuncFilter{}, filter)

			if pf, ok := filter.(FilterParams); ok {
				pf.AcceptParams(tC.params)
			}

			result, err := filter.Process(tC.input)
			if tC.wantErrProcess {
				assert.Error(t, err)
			} else {
				if tC.wantErrResult {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
					assert.Equal(t, tC.expected, result)
				}
			}
		})
	}
}

func TestSplitFilter(t *testing.T) {
	testCases := []struct {
		desc           string
		input          string
		params         map[string]string
		expected       []string
		wantErrResult  bool
		wantErrProcess bool
	}{
		{
			desc:  "split by comma",
			input: "a,b,c",
			params: map[string]string{
				"delimiter": ",",
			},
			expected:       []string{"a", "b", "c"},
			wantErrResult:  false,
			wantErrProcess: false,
		},
		{
			desc:           "missing delimiter",
			input:          "a,b,c",
			params:         map[string]string{},
			expected:       nil,
			wantErrResult:  true,
			wantErrProcess: false,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			filter := NewFilter("split")
			assert.NotNil(t, filter)
			assert.IsType(t, &FuncFilter{}, filter)

			if pf, ok := filter.(FilterParams); ok {
				pf.AcceptParams(tC.params)
			}

			result, err := filter.Process(tC.input)
			if tC.wantErrProcess {
				assert.Error(t, err)
			} else {
				if tC.wantErrResult {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
					assert.Equal(t, tC.expected, result)
				}
			}
		})
	}
}

func TestJoinFilter(t *testing.T) {
	testCases := []struct {
		desc           string
		input          []string
		params         map[string]string
		expected       string
		wantErrResult  bool
		wantErrProcess bool
	}{
		{
			desc:  "join by comma",
			input: []string{"a", "b", "c"},
			params: map[string]string{
				"delimiter": ",",
			},
			expected:       "a,b,c",
			wantErrResult:  false,
			wantErrProcess: false,
		},
		{
			desc:           "join with missing delimiter",
			input:          []string{"a", "b", "c"},
			params:         map[string]string{},
			expected:       "",
			wantErrResult:  true,
			wantErrProcess: false,
		},
		{
			desc:  "join with non-slice input",
			input: nil, // This is tricky since input is any, let's pass something else
			params: map[string]string{
				"delimiter": ",",
			},
			expected:       "",
			wantErrResult:  true,
			wantErrProcess: false,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			filter := NewFilter("join")
			assert.NotNil(t, filter)
			assert.IsType(t, &FuncFilter{}, filter)

			if pf, ok := filter.(FilterParams); ok {
				pf.AcceptParams(tC.params)
			}

			var input any
			if tC.input != nil {
				input = tC.input
			} else {
				input = "not a slice"
			}

			result, err := filter.Process(input)
			if tC.wantErrProcess {
				assert.Error(t, err)
			} else {
				if tC.wantErrResult {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
					assert.Equal(t, tC.expected, result)
				}
			}
		})
	}
}
