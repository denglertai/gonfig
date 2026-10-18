package filter

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestFileInterceptorFilter checks which values starting with "@" are read from files and when a missing file fails
func TestFileInterceptorFilter(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	assert.NoError(t, os.WriteFile("secret", []byte("s3cret\n"), 0o600))
	assert.NoError(t, os.Mkdir("sub", 0o700))
	absSecret := filepath.Join(dir, "secret")

	tests := []struct {
		name    string
		value   any
		want    any
		wantErr string
	}{
		{name: "absolute path", value: "@" + absSecret, want: "s3cret\n"},
		{name: "relative to the current directory", value: "@./secret", want: "s3cret\n"},
		{name: "relative to the parent directory", value: "@../" + filepath.Base(dir) + "/secret", want: "s3cret\n"},
		{name: "relative path without prefix", value: "@secret", want: "s3cret\n"},
		{name: "missing absolute path", value: "@" + filepath.Join(dir, "missing"), wantErr: "failed to read the file referenced with @"},
		{name: "missing path relative to the current directory", value: "@./missing", wantErr: "failed to read the file referenced with @"},
		{name: "missing path relative to the parent directory", value: "@../missing", wantErr: "failed to read the file referenced with @"},
		{name: "directory", value: "@./sub", wantErr: "failed to read the file referenced with @"},
		{name: "directory without prefix", value: "@sub", wantErr: "failed to read the file referenced with @"},
		{name: "value starting with @ that is not a file", value: "@x9!kQ", want: "@x9!kQ"},
		{name: "missing relative path without prefix", value: "@secrets/missing", want: "@secrets/missing"},
		{name: "only @", value: "@", want: "@"},
		{name: "@ within the value", value: "a@" + absSecret, want: "a@" + absSecret},
		{name: "empty value", value: "", want: ""},
		{name: "non-string value", value: 5, want: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewFileInterceptorFilter().Process(tt.value)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestIsExplicitPath checks which paths are clearly meant as file references, including the separators of the OS
func TestIsExplicitPath(t *testing.T) {
	tests := map[string]bool{
		"/run/secrets/db_password":            true,
		filepath.Join(os.TempDir(), "secret"): true,
		"./secret":                            true,
		"../secret":                           true,
		filepath.FromSlash("./secret"):        true,
		filepath.FromSlash("../secret"):       true,
		"secret":                              false,
		"secrets/db_password":                 false,
		".secret":                             false,
		"..secret":                            false,
		"x9!kQ":                               false,
		"":                                    false,
	}
	if runtime.GOOS == "windows" {
		// Paths with a drive letter, UNC paths and paths rooted at the current drive
		tests[`C:\secrets\db`] = true
		tests[`C:/secrets/db`] = true
		tests[`\\server\share\db`] = true
		tests[`\secrets\db`] = true
		// Drive relative paths look like values, e.g. "a:b"
		tests[`C:db`] = false
	} else {
		// Backslashes are no separators outside of Windows
		tests[`.\secret`] = false
		tests[`\secret`] = false
	}

	for path, want := range tests {
		assert.Equal(t, want, isExplicitPath(path), path)
	}
}
