package value

import (
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProcessValue(t *testing.T) {
	workingDir, _ := os.Getwd()

	type args struct {
		value string
	}
	tests := []struct {
		name    string
		envVars map[string]string
		args    args
		want    interface{}
		wantErr bool
	}{
		{
			name: "Simple Env Var Lookup",
			args: args{
				value: "${BLA_BLUB}",
			},
			envVars: map[string]string{
				"BLA_BLUB": "123",
			},
			want:    "123",
			wantErr: false,
		},
		{
			name: "Simple Env Var Lookup with file pointer",
			args: args{
				value: "${BLA_BLUB}",
			},
			envVars: map[string]string{
				"BLA_BLUB": "@" + path.Join(workingDir, "testdata", "file.txt"),
			},
			want:    "hi",
			wantErr: false,
		},
		{
			name: "Simple Env Var Lookup with file pointer with filter",
			args: args{
				value: "${BLA_BLUB|upper}",
			},
			envVars: map[string]string{
				"BLA_BLUB": "@" + path.Join(workingDir, "testdata", "file.txt"),
			},
			want:    "HI",
			wantErr: false,
		},
		{
			name: "Simple Env Var Lookup with filter",
			args: args{
				value: "${BLA_BLUB|upper}",
			},
			envVars: map[string]string{
				"BLA_BLUB": "abc",
			},
			want:    "ABC",
			wantErr: false,
		},
		{
			name: "Simple Env Var Lookup with multiple filters",
			args: args{
				value: "${BLA_BLUB|upper|trimleft}",
			},
			envVars: map[string]string{
				"BLA_BLUB": " a bc ",
			},
			want:    "A BC ",
			wantErr: false,
		},
		{
			name: "Simple Env Var Lookup with conversion and multiplication",
			args: args{
				value: `${BLA_BLUB|to_int|multiply(m=2)}`,
			},
			envVars: map[string]string{
				"BLA_BLUB": "123",
			},
			want:    246,
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			got, err := ProcessValue(tt.args.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("ProcessValue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ProcessValue() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComplexValues(t *testing.T) {
	testCases := []struct {
		desc    string
		input   string
		want    string
		wantErr bool
	}{
		{
			desc:  "Prefix URL test",
			input: "xx${TEST1}",
			want:  "xxhttp://url.tld",
		},
		{
			desc:  "Whitespace URL path test",
			input: "${TEST1} - ${TEST2} / ${TEST3}",
			want:  "http://url.tld - path / something",
		},
		{
			desc:  "Build URL",
			input: "${TEST1}/${TEST2}?param1=value&param2=${TEST3}",
			want:  "http://url.tld/path?param1=value&param2=something",
		},
		{
			desc:  "Concat TEST1 TEST2 TEST3",
			input: "${TEST1}${TEST2}${TEST3}",
			want:  "http://url.tldpathsomething",
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			t.Setenv("TEST1", "http://url.tld")
			t.Setenv("TEST2", "path")
			t.Setenv("TEST3", "something")

			result, err := ProcessValue(tC.input)
			if tC.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tC.want, result)
			}
		})
	}
}

func TestProcessValueNonASCIIPrefix(t *testing.T) {
	t.Setenv("NON_ASCII_TEST", "value")

	result, err := ProcessValue("ää ${NON_ASCII_TEST} ü ${NON_ASCII_TEST}")
	assert.NoError(t, err)
	assert.Equal(t, "ää value ü value", result)
}

func TestParseParams(t *testing.T) {
	testCases := []struct {
		desc  string
		input string
		want  map[string]string
	}{
		{desc: "empty", input: "()", want: map[string]string{}},
		{desc: "named", input: "(m=2)", want: map[string]string{"m": "2"}},
		{desc: "multiple named", input: "(a=1,b=2)", want: map[string]string{"a": "1", "b": "2"}},
		{desc: "positional", input: "(foo,bar)", want: map[string]string{"0": "foo", "1": "bar"}},
		{desc: "empty positional", input: "(foo,)", want: map[string]string{"0": "foo", "1": ""}},
		{desc: "mixed", input: "(old=foo,bar)", want: map[string]string{"old": "foo", "1": "bar"}},
		{desc: "equals in named value", input: "(a=b=c)", want: map[string]string{"a": "b=c"}},
		{desc: "escaped delimiters", input: `(\,,\=)`, want: map[string]string{"0": ",", "1": "="}},
		{desc: "escaped brackets", input: `(\(\w+\){2})`, want: map[string]string{"0": `(\w+){2}`}},
		{desc: "escaped backslash", input: `(a\\,b)`, want: map[string]string{"0": `a\`, "1": "b"}},
		{desc: "trailing backslash", input: `(a\)`, want: map[string]string{"0": `a\`}},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			assert.Equal(t, tC.want, parseParams(tC.input))
		})
	}
}

func TestProcessValueWithStringFilters(t *testing.T) {
	testCases := []struct {
		desc  string
		value string
		input string
		want  any
	}{
		{desc: "base64 round trip", value: "${FILTER_TEST|base64|base64_decode}", input: "secret", want: "secret"},
		{desc: "base64 within text", value: "Basic ${FILTER_TEST|base64}", input: "user:pass", want: "Basic dXNlcjpwYXNz"},
		{desc: "replace", value: "${FILTER_TEST|replace(foo,bar)}", input: "foo.foo", want: "bar.bar"},
		{desc: "replace escaped comma", value: `${FILTER_TEST|replace(\,,;)}`, input: "a,b", want: "a;b"},
		{desc: "regex with escaped group", value: `${FILTER_TEST|regex(^\(\w+\)@.+,\1)}`, input: "jane@example.com", want: "jane"},
		{desc: "regex with escaped quantifier", value: `${FILTER_TEST|regex(\d\{2\},xx)}`, input: "a123", want: "axx3"},
		{desc: "split upper join", value: "${FILTER_TEST|split|upper|join(\\, )}", input: "a,b,c", want: "A, B, C"},
		{desc: "split trim join", value: "${FILTER_TEST|split(;)|trim|join}", input: " a ; b ", want: "a,b"},
		{desc: "existing named params", value: "${FILTER_TEST|to_int|multiply(m=3)}", input: "2", want: 6},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			t.Setenv("FILTER_TEST", tC.input)

			result, err := ProcessValue(tC.value)
			assert.NoError(t, err)
			assert.Equal(t, tC.want, result)
		})
	}
}

func TestProcessValueWithInvalidFilterParams(t *testing.T) {
	t.Setenv("FILTER_TEST", "abc")

	for _, value := range []string{"${FILTER_TEST|not_a_filter}", "${FILTER_TEST|UPPER}", "${FILTER_TEST|replace}", "${FILTER_TEST|regex(\\()}", "${FILTER_TEST|base64_decode}"} {
		_, err := ProcessValue(value)
		assert.Error(t, err, value)
	}
}

func TestProcessValueEdgeCases(t *testing.T) {
	t.Setenv("EDGE_TEST", "x")
	t.Setenv("EDGE_TEST_INT", "5")

	testCases := []struct {
		desc  string
		value string
		want  any
	}{
		{desc: "no placeholder", value: "plain value", want: "plain value"},
		{desc: "unclosed placeholder", value: "a${EDGE_TEST", want: "a${EDGE_TEST"},
		{desc: "unclosed placeholder after placeholder", value: "${EDGE_TEST}${", want: "x${"},
		{desc: "unclosed params", value: "${EDGE_TEST|upper(}", want: "X"},
		{desc: "empty placeholder", value: "${}", want: "${}"},
		{desc: "closing bracket after placeholder", value: "${EDGE_TEST}}", want: "x}"},
		{desc: "dollar before placeholder", value: "$${EDGE_TEST}", want: "$x"},
		{desc: "escaped closing bracket", value: `${EDGE_TEST|replace(\},y)}`, want: "x"},
		{desc: "placeholders without separator", value: "${EDGE_TEST|upper}${EDGE_TEST}", want: "Xx"},
		{desc: "non-string result of last placeholder", value: "a ${EDGE_TEST_INT|to_int}", want: 5},
		{desc: "non-string result followed by string", value: "${EDGE_TEST_INT|to_int} ${EDGE_TEST}", want: "${EDGE_TEST_INT|to_int} x"},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			result, err := ProcessValue(tC.value)
			assert.NoError(t, err)
			assert.Equal(t, tC.want, result)
		})
	}
}

func TestFindPlaceholders(t *testing.T) {
	placeholders := findPlaceholders(`a ${A} b ${B|replace(\},x)} ${C`)

	assert.Equal(t, []placeholder{
		{token: "${A}", start: 2, end: 6},
		{token: `${B|replace(\},x)}`, start: 9, end: 27},
	}, placeholders)
	assert.Equal(t, `B|replace(\},x)`, placeholders[1].content())
}

func FuzzProcessValue(f *testing.F) {
	for _, value := range []string{"${A}", "a${A", "${A}${", "${A|replace(}", `${A|regex(\(x\),y)}`, "${${A}}", "${\\"} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		// Any input has to be processed without panicking
		_, _ = ProcessValue(value)
	})
}
