package cmd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestValue checks that results of gonfig value, including non-string ones, are printed exactly as they are
func TestValue(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		expected string
		env      map[string]string
	}{
		{
			name:     "nothing to do",
			input:    "value",
			expected: "value",
		},
		{
			name:     "simple value",
			input:    "${ABC | md5}",
			expected: "202cb962ac59075b964b07152d234b70",
			env: map[string]string{
				"ABC": "123",
			},
		},
		{
			name:     "non-string result",
			input:    "${PORT | to_int}",
			expected: "80",
			env: map[string]string{
				"PORT": "80",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			var out bytes.Buffer
			rootCmd.SetOut(&out)
			t.Cleanup(func() { rootCmd.SetOut(nil) })

			resetFlags(t)
			rootCmd.SetArgs([]string{"value", tc.input})
			assert.NoError(t, rootCmd.Execute())

			assert.Equal(t, tc.expected, out.String())
		})
	}
}
