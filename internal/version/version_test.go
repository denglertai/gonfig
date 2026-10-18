package version

import (
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/assert"
)

// buildInfo returns build info with the module version and VCS settings, of which empty values are left out
func buildInfo(moduleVersion, revision, time, modified string) *debug.BuildInfo {
	info := &debug.BuildInfo{Main: debug.Module{Path: "github.com/denglertai/gonfig", Version: moduleVersion}}
	for key, value := range map[string]string{"vcs.revision": revision, "vcs.time": time, "vcs.modified": modified} {
		if value != "" {
			info.Settings = append(info.Settings, debug.BuildSetting{Key: key, Value: value})
		}
	}
	return info
}

// TestResolve checks how the values set with -ldflags and the build info are combined for each kind of build
func TestResolve(t *testing.T) {
	const hash = "0274352364c8a1b2c3d4e5f60718293a4b5c6d7e"

	tests := []struct {
		name                  string
		version, commit, date string
		buildInfo             *debug.BuildInfo
		want                  Info
		wantString            string
	}{
		{
			name:       "release build with ldflags",
			version:    "0.0.29",
			commit:     hash,
			date:       "2026-10-18T08:05:01Z",
			buildInfo:  buildInfo("(devel)", "", "", ""),
			want:       Info{Version: "0.0.29", Commit: hash, Date: "2026-10-18T08:05:01Z"},
			wantString: "0.0.29 (commit 0274352, 2026-10-18T08:05:01Z)",
		},
		{
			name:       "ldflags take precedence over the build info",
			version:    "0.0.29",
			commit:     hash,
			date:       "2026-10-18T08:05:01Z",
			buildInfo:  buildInfo("v0.0.28", "ffffffffffff", "2026-01-01T00:00:00Z", "true"),
			want:       Info{Version: "0.0.29", Commit: hash, Date: "2026-10-18T08:05:01Z"},
			wantString: "0.0.29 (commit 0274352, 2026-10-18T08:05:01Z)",
		},
		{
			name:       "melange build with the version only",
			version:    "0.0.29",
			buildInfo:  buildInfo("(devel)", "", "", ""),
			want:       Info{Version: "0.0.29"},
			wantString: "0.0.29",
		},
		{
			name:       "go install of a tag",
			buildInfo:  buildInfo("v0.0.29", "", "", ""),
			want:       Info{Version: "0.0.29"},
			wantString: "0.0.29",
		},
		{
			name:       "go build of modified sources",
			buildInfo:  buildInfo("v0.0.29-0.20261018080501-0274352364c8+dirty", hash, "2026-10-18T08:05:01Z", "true"),
			want:       Info{Version: "0.0.29-0.20261018080501-0274352364c8+dirty", Commit: hash, Date: "2026-10-18T08:05:01Z", Modified: true},
			wantString: "0.0.29-0.20261018080501-0274352364c8+dirty (commit 0274352-dirty, 2026-10-18T08:05:01Z)",
		},
		{
			name:       "go build without version information",
			buildInfo:  buildInfo("(devel)", "", "", ""),
			want:       Info{Version: "dev"},
			wantString: "dev",
		},
		{
			name:       "no build info",
			want:       Info{Version: "dev"},
			wantString: "dev",
		},
		{
			name:       "short commit hash",
			version:    "0.0.29",
			commit:     "abc",
			want:       Info{Version: "0.0.29", Commit: "abc"},
			wantString: "0.0.29 (commit abc)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolve(tt.version, tt.commit, tt.date, tt.buildInfo)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.wantString, got.String())
		})
	}
}

// TestGet checks that the version of the test binary is resolved, which has no ldflags set
func TestGet(t *testing.T) {
	assert.NotEmpty(t, Get().Version)
}
