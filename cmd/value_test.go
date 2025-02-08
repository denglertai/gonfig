package cmd

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
