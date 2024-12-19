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
