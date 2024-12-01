package value

import (
	"os"
	"path"
	"testing"
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
				value: "${BLA_BLUB|to_int|multiply(m=\"2\")}",
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
