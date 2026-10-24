package value

import (
	"os"
	"path"
	"testing"

	"github.com/denglertai/gonfig/internal/filter"
	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/bcrypt"
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

// TestProcessValueWithUnknownParams checks that params a filter does not declare fail processing,
// while the documented empty params of split(,) use the default delimiter
func TestProcessValueWithUnknownParams(t *testing.T) {
	t.Setenv("PARAMS_TEST", "a,b")

	for value, wantErr := range map[string]string{
		"${PARAMS_TEST | upper(foo=bar)}":          `filter upper accepts no params, but got "foo"`,
		"${PARAMS_TEST | bcrypt(rounds=4)}":        `filter bcrypt has no param "rounds", it accepts cost`,
		"${PARAMS_TEST | replace(a,b,c)}":          "filter replace accepts 2 params (old, new), but got 3",
		"${PARAMS_TEST | trim | replace(a,old=b)}": "filter replace got the param old twice, by position and by name",
	} {
		_, err := ProcessValue(value)
		assert.EqualError(t, err, wantErr, value)
	}

	result, err := ProcessValue("${PARAMS_TEST | split(,) | join(;)}")
	assert.NoError(t, err)
	assert.Equal(t, "a;b", result)
}

// TestProcessValueBcryptCost checks the cost of bcrypt hashes for the params as written in placeholders,
// which is 10 without a cost param
func TestProcessValueBcryptCost(t *testing.T) {
	t.Setenv("BCRYPT_TEST", "s3cret")

	for value, wantCost := range map[string]int{
		"${BCRYPT_TEST | bcrypt}":         10,
		"${BCRYPT_TEST | bcrypt()}":       10,
		"${BCRYPT_TEST | bcrypt(cost=5)}": 5,
		"${BCRYPT_TEST | bcrypt(4)}":      4,
	} {
		result, err := ProcessValue(value)
		assert.NoError(t, err, value)

		cost, err := bcrypt.Cost([]byte(result.(string)))
		assert.NoError(t, err, value)
		assert.Equal(t, wantCost, cost, value)
	}
}

// TestProcessValueEdgeCases checks unusual but valid values, e.g. empty placeholders or brackets after placeholders
func TestProcessValueEdgeCases(t *testing.T) {
	t.Setenv("EDGE_TEST", "x")
	t.Setenv("EDGE_TEST_INT", "5")

	testCases := []struct {
		desc  string
		value string
		want  any
	}{
		{desc: "no placeholder", value: "plain value", want: "plain value"},
		{desc: "leading separator", value: "${|EDGE_TEST}", want: "x"},
		{desc: "repeated separators", value: "${EDGE_TEST || upper | | lower}", want: "x"},
		{desc: "empty placeholder", value: "${}", want: "${}"},
		{desc: "closing bracket after placeholder", value: "${EDGE_TEST}}", want: "x}"},
		{desc: "dollar before placeholder", value: "$$${EDGE_TEST}", want: "$x"},
		{desc: "escaped closing bracket", value: `${EDGE_TEST|replace(\},y)}`, want: "x"},
		{desc: "placeholders without separator", value: "${EDGE_TEST|upper}${EDGE_TEST}", want: "Xx"},
		{desc: "non-string result of whole value", value: "${EDGE_TEST_INT|to_int}", want: 5},
		{desc: "non-string result embedded at the end", value: "a ${EDGE_TEST_INT|to_int}", want: "a 5"},
		{desc: "non-string result embedded at the start", value: "${EDGE_TEST_INT|to_int} ${EDGE_TEST}", want: "5 x"},
		{desc: "non-string result surrounded by whitespace", value: " ${EDGE_TEST_INT|to_int} ", want: " 5 "},
		{desc: "float result embedded", value: "v${EDGE_TEST_INT|to_float}", want: "v5"},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			result, err := ProcessValue(tC.value)
			assert.NoError(t, err)
			assert.Equal(t, tC.want, result)
		})
	}
}

// TestFindPlaceholders checks the placeholders found within a value, of which escaped closing brackets are part
func TestFindPlaceholders(t *testing.T) {
	placeholders, err := findPlaceholders(`a ${A} b ${B|replace(\},x)}`)

	assert.NoError(t, err)
	assert.Equal(t, []placeholder{
		{token: "${A}", start: 2, end: 6},
		{token: `${B|replace(\},x)}`, start: 9, end: 27},
	}, placeholders)
	assert.Equal(t, `B|replace(\},x)`, placeholders[1].content())
}

// TestProcessValueEscapes checks that "$$" before "{" stands for a literal "$", so "$${" is not a placeholder,
// while dollar signs not followed by "{" are kept as is
func TestProcessValueEscapes(t *testing.T) {
	t.Setenv("EDGE_TEST", "x")
	t.Setenv("EDGE_TEST_INT", "5")

	testCases := []struct {
		desc  string
		value string
		want  any
	}{
		{desc: "escaped placeholder", value: "$${EDGE_TEST}", want: "${EDGE_TEST}"},
		{desc: "escaped placeholder with filters", value: "a $${EDGE_TEST | upper} b", want: "a ${EDGE_TEST | upper} b"},
		{desc: "dollar followed by a placeholder", value: "$$${EDGE_TEST}", want: "$x"},
		{desc: "two dollars followed by an escaped placeholder", value: "$$$${EDGE_TEST}", want: "$${EDGE_TEST}"},
		{desc: "two dollars followed by a placeholder", value: "$$$$${EDGE_TEST}", want: "$$x"},
		{desc: "escaped placeholder without closing bracket", value: "$${EDGE_TEST", want: "${EDGE_TEST"},
		{desc: "escaped placeholder and placeholder", value: "$${EDGE_TEST}=${EDGE_TEST}", want: "${EDGE_TEST}=x"},
		{desc: "placeholder followed by escaped placeholder", value: "${EDGE_TEST}$${EDGE_TEST}", want: "x${EDGE_TEST}"},
		{desc: "escape within shell syntax", value: `echo "$${HOME:-/root}" $$`, want: `echo "${HOME:-/root}" $$`},
		{desc: "dollars without bracket", value: "$$EDGE_TEST $ ${EDGE_TEST}", want: "$$EDGE_TEST $ x"},
		{desc: "escaped empty placeholder", value: "$${}", want: "${}"},
		{desc: "dollar before a typed result is text", value: "$$${EDGE_TEST_INT|to_int}", want: "$5"},
		// Like in docker compose, the escape covers the "${" only, so a shell default can be filled in
		{desc: "placeholder within escaped text", value: "$${TARGET:-${EDGE_TEST}}", want: "${TARGET:-x}"},
		{desc: "escaped placeholder within escaped text", value: "$${TARGET:-$${EDGE_TEST}}", want: "${TARGET:-${EDGE_TEST}}"},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			result, err := ProcessValue(tC.value)
			assert.NoError(t, err)
			assert.Equal(t, tC.want, result)
		})
	}

	// The escape doesn't hide an unterminated placeholder after it
	_, err := ProcessValue("$${EDGE_TEST} ${EDGE_TEST")
	assert.ErrorContains(t, err, "unterminated placeholder")
	_, err = ProcessValue("$$${EDGE_TEST")
	assert.ErrorContains(t, err, "unterminated placeholder")

	// Escaped text is not a secret, so it is not masked
	masked, err := ProcessMaskedValue("$${EDGE_TEST} ${EDGE_TEST}")
	assert.NoError(t, err)
	assert.Equal(t, "${EDGE_TEST} "+Mask, masked)
}

// TestScanValue checks the escape sequences and placeholders found within a value, and that findPlaceholders
// returns the placeholders only
func TestScanValue(t *testing.T) {
	value := "$${A} $$${B} $$$${C}"
	all, err := scanValue(value)
	assert.NoError(t, err)
	assert.Equal(t, []placeholder{
		{token: "$${", start: 0, end: 3, literal: "${"},
		{token: "$$", start: 6, end: 8, literal: "$"},
		{token: "${B}", start: 8, end: 12},
		{token: "$$$${", start: 13, end: 18, literal: "$${"},
	}, all)

	placeholders, err := findPlaceholders(value)
	assert.NoError(t, err)
	assert.Equal(t, []placeholder{{token: "${B}", start: 8, end: 12}}, placeholders)
}

// TestHasPlaceholders checks that escape sequences alone are no placeholders, but unterminated placeholders are
func TestHasPlaceholders(t *testing.T) {
	for value, want := range map[string]bool{
		"plain":          false,
		"$$HOME":         false,
		"$${A}":          false,
		"$${A} $${B":     false,
		"${A}":           true,
		"$$${A}":         true,
		"$${A} ${B":      true,
		"${":             true,
		"$${A} and ${B}": true,
	} {
		assert.Equal(t, want, HasPlaceholders(value), value)
	}
}

// TestFindPlaceholdersUnterminated checks that a placeholder without a closing bracket is an error,
// which returns the placeholders before it and quotes only the beginning of a long rest of the value
func TestFindPlaceholdersUnterminated(t *testing.T) {
	placeholders, err := findPlaceholders("a ${A} b ${B | upper")
	assert.EqualError(t, err, `unterminated placeholder "${B | upper", the closing } is missing`)
	assert.Equal(t, []placeholder{{token: "${A}", start: 2, end: 6}}, placeholders)

	_, err = findPlaceholders("${A | upper and then a long text äöü that goes on and on")
	assert.EqualError(t, err, `unterminated placeholder "${A | upper and then a long text äöü tha...", the closing } is missing`)
}

// TestProcessValueUnterminated checks that values with unterminated placeholders fail, including an escaped closing bracket
func TestProcessValueUnterminated(t *testing.T) {
	t.Setenv("EDGE_TEST", "x")

	for _, value := range []string{"a${EDGE_TEST", "${EDGE_TEST | upper", "${EDGE_TEST}${", "${", `${EDGE_TEST\}`, "${EDGE_TEST | upper\n"} {
		_, err := ProcessValue(value)
		assert.ErrorContains(t, err, "unterminated placeholder", value)
	}
}

// TestProcessValueUnterminatedParams checks that params without a closing bracket fail instead of being applied,
// for filters, filter templates and the source of a placeholder
func TestProcessValueUnterminatedParams(t *testing.T) {
	t.Setenv("EDGE_TEST", "x")

	testCases := []struct {
		desc    string
		value   string
		wantErr string
	}{
		{desc: "filter", value: "${EDGE_TEST | default(a}", wantErr: `unterminated params "(a" of default, the closing ) is missing`},
		{desc: "empty", value: "${EDGE_TEST|upper(}", wantErr: `unterminated params "(" of upper, the closing ) is missing`},
		{desc: "escaped bracket", value: `${EDGE_TEST | default(a\)}`, wantErr: `unterminated params "(a\\)" of default, the closing ) is missing`},
		{desc: "three backslashes", value: `${EDGE_TEST | default(a\\\)}`, wantErr: `of default, the closing ) is missing`},
		{desc: "embedded in text", value: "a ${EDGE_TEST | upper | replace(X,y}", wantErr: `unterminated params "(X,y" of replace, the closing ) is missing`},
		{desc: "source", value: "${EDGE_TEST(a | upper}", wantErr: `unterminated params "(a | upper" of EDGE_TEST, the closing ) is missing`},
		{desc: "filter template", value: "${EDGE_TEST | @clean(a}", wantErr: `unterminated params "(a" of @clean, the closing ) is missing`},
		{desc: "unknown filter", value: "${EDGE_TEST | not_a_filter(a}", wantErr: `unterminated params "(a" of not_a_filter, the closing ) is missing`},
		{
			desc:    "placeholder within params",
			value:   "${EDGE_TEST | default(${OTHER})}",
			wantErr: `unterminated params "(${OTHER" of default, the closing ) is missing; params cannot contain placeholders, escape { and } with a backslash`,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			_, err := ProcessValue(tC.value)
			assert.ErrorContains(t, err, tC.wantErr)
		})
	}
}

// TestProcessValueParamsWithoutName checks that params not directly following the name of a filter or variable fail,
// instead of being read as the name of the variable or ignored
func TestProcessValueParamsWithoutName(t *testing.T) {
	t.Setenv("EDGE_TEST", "x")

	testCases := []struct {
		desc    string
		value   string
		wantErr string
	}{
		{desc: "instead of the variable", value: "${(a)}", wantErr: `params "(a)" without a name in front of them`},
		{desc: "unterminated instead of the variable", value: "${(a}", wantErr: `params "(a" without a name in front of them`},
		{desc: "after a separator", value: "${EDGE_TEST | (a)}", wantErr: `params "(a)" without a name in front of them`},
		{desc: "unterminated after a separator", value: "${EDGE_TEST | upper | (a}", wantErr: `params "(a" without a name in front of them`},
		{desc: "after params", value: "${EDGE_TEST | replace(x,y)(a)}", wantErr: `params "(a)" without a name in front of them`},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			_, err := ProcessValue(tC.value)
			assert.EqualError(t, err, tC.wantErr)
		})
	}

	// Params separated from their filter by spaces still belong to it
	result, err := ProcessValue("${EDGE_TEST | replace (x,y)}")
	assert.NoError(t, err)
	assert.Equal(t, "y", result)
}

// TestProcessValueUnknownFilterWithUnterminatedParams checks that unterminated params fail even if unknown filters are allowed
func TestProcessValueUnknownFilterWithUnterminatedParams(t *testing.T) {
	filter.SetAllowUnknownFilters(true)
	t.Cleanup(func() { filter.SetAllowUnknownFilters(false) })

	_, err := ProcessValue("${EDGE_TEST | not_a_filter(a}")
	assert.EqualError(t, err, `unterminated params "(a" of not_a_filter, the closing ) is missing`)
}

// TestProcessValueUnterminatedParamsWithFailOnMissing checks that unterminated params are reported instead of
// a missing variable, since the variable is not read
func TestProcessValueUnterminatedParamsWithFailOnMissing(t *testing.T) {
	filter.SetFailOnMissing(true)
	t.Cleanup(func() { filter.SetFailOnMissing(false) })

	_, err := ProcessValue("${UNTERMINATED_UNSET | upper(}")
	assert.EqualError(t, err, `unterminated params "(" of upper, the closing ) is missing`)
}

// TestProcessValueTerminatedParams checks params ending with escaped backslashes, whose closing bracket is not escaped
func TestProcessValueTerminatedParams(t *testing.T) {
	for value, want := range map[string]string{
		`${TERMINATED_UNSET | default(a\\)}`:   `a\`,
		`${TERMINATED_UNSET | default(a\\\\)}`: `a\\`,
		`${TERMINATED_UNSET | default(\))}`:    `)`,
		`${TERMINATED_UNSET | default()}`:      ``,
	} {
		result, err := ProcessValue(value)
		assert.NoError(t, err, value)
		assert.Equal(t, want, result, value)
	}
}

// FuzzProcessValue checks that any value is processed without panicking, seeded with inputs that panicked before
func FuzzProcessValue(f *testing.F) {
	for _, value := range []string{"${A}", "a${A", "${A}${", "${A|replace(}", `${A|regex(\(x\),y)}`, "${${A}}", "${\\", "${A|split(-1=a)}", "${A|split(9223372036854775807=a)}", "$${A}", "$$${A", "$$$${A}${B}"} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		// Any input has to be processed without panicking
		_, _ = ProcessValue(value)
	})
}

func TestProcessValueWithFragments(t *testing.T) {
	t.Setenv("FRAGMENT_TEST", "a: 1")

	// A fragment making up the whole value is returned as is, so file types can insert it
	result, err := ProcessValue("${FRAGMENT_TEST | fragment}")
	assert.NoError(t, err)
	assert.Equal(t, filter.Fragment{Text: "a: 1"}, result)

	// Fragments cannot be embedded into text
	_, err = ProcessValue("key: ${FRAGMENT_TEST | fragment}")
	assert.EqualError(t, err, "the fragment of ${FRAGMENT_TEST | fragment} has to make up the whole value")
}

// TestProcessMaskedValue checks that the results of placeholders are masked, while errors and fragments are kept
func TestProcessMaskedValue(t *testing.T) {
	t.Setenv("MASKED_HOST", "db.internal")
	t.Setenv("MASKED_PORT", "5432")
	t.Setenv("MASKED_INVALID", "eighty")
	t.Setenv("MASKED_LIST", "a,b")

	tests := []struct {
		name    string
		value   string
		want    any
		wantErr string
	}{
		{name: "without placeholders", value: "static", want: "static"},
		{name: "whole value", value: "${MASKED_HOST}", want: Mask},
		{name: "typed whole value", value: "${MASKED_PORT | to_int}", want: Mask},
		{name: "embedded", value: "postgres://${MASKED_HOST}:${MASKED_PORT | to_int}/db", want: "postgres://***:***/db"},
		{name: "default", value: "${MASKED_UNSET | default(localhost)}", want: Mask},
		{name: "fragment", value: "${MASKED_LIST | split | fragment}", want: filter.Fragment{Text: `["a","b"]`, IsList: true}},
		{name: "error of a whole value", value: "${MASKED_INVALID | to_int}", wantErr: "filter to_int: the value is not an integer"},
		{name: "error of an embedded value", value: "port ${MASKED_INVALID | to_int}", wantErr: "filter to_int: the value is not an integer"},
		{name: "embedded fragment", value: "list ${MASKED_LIST | split | fragment}", wantErr: "the fragment of ${MASKED_LIST | split | fragment} has to make up the whole value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ProcessMaskedValue(tt.value)
			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
