package cmd

import (
	"bytes"
	"io"
	"os"
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

			old := os.Stdout // keep backup of the real stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			outC := make(chan string)
			// copy the output in a separate goroutine so printing can't block indefinitely
			go func() {
				var buf bytes.Buffer
				io.Copy(&buf, r)
				outC <- buf.String()
			}()

			resetFlags(t)
			rootCmd.SetArgs([]string{"value", tc.input})
			err := rootCmd.Execute()
			assert.NoError(t, err)

			// back to normal state
			w.Close()
			os.Stdout = old // restoring the real stdout
			out := <-outC

			assert.Equal(t, tc.expected, out)
		})
	}
}

// TestValueNewline checks that results are printed byte for byte by default and end with a newline each with --newline
func TestValueNewline(t *testing.T) {
	t.Setenv("NEWLINE_A", "a")
	t.Setenv("NEWLINE_B", "b")

	testCases := []struct {
		desc string
		args []string
		want string
	}{
		{desc: "default", args: []string{"value", "${NEWLINE_A}"}, want: "a"},
		{desc: "default with several values", args: []string{"value", "${NEWLINE_A}", "${NEWLINE_B}"}, want: "ab"},
		{desc: "newline", args: []string{"value", "--newline", "${NEWLINE_A}"}, want: "a\n"},
		{desc: "newline with several values", args: []string{"value", "--newline", "${NEWLINE_A}", "${NEWLINE_B}"}, want: "a\nb\n"},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			var out bytes.Buffer
			rootCmd.SetOut(&out)
			t.Cleanup(func() { rootCmd.SetOut(nil) })

			resetFlags(t)
			rootCmd.SetArgs(tC.args)
			assert.NoError(t, rootCmd.Execute())
			assert.Equal(t, tC.want, out.String())
		})
	}
}

// TestValueWritesNothingOnError checks that no result is written if processing any of the values fails
func TestValueWritesNothingOnError(t *testing.T) {
	t.Setenv("PARTIAL_INVALID", "abc")

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	t.Cleanup(func() { rootCmd.SetOut(nil) })

	resetFlags(t)
	rootCmd.SetArgs([]string{"value", "--newline", "first", "${PARTIAL_INVALID | to_int}"})
	assert.Error(t, rootCmd.Execute())
	assert.Empty(t, out.String())
}
