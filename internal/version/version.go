// Package version provides the version of the gonfig binary, e.g. for gonfig --version
package version

import (
	"fmt"
	"runtime/debug"
	"strings"
)

// Set by release builds with -ldflags "-X github.com/denglertai/gonfig/internal/version.version=...".
// Empty values are taken from the build info, e.g. for go install or go build.
var (
	// version is the version without a leading v, e.g. 0.0.29
	version string
	// commit is the full hash of the commit the binary was built from
	commit string
	// date is the date of the commit in RFC 3339 format, which keeps builds reproducible unlike the build time
	date string
)

// shortCommitLength is the number of characters of the commit hash printed by Info.String, like git's short hashes
const shortCommitLength = 7

// Info describes the version of a binary
type Info struct {
	// Version is the version without a leading v, or "dev" if it is unknown
	Version string
	// Commit is the hash of the commit the binary was built from, or empty if it is unknown
	Commit string
	// Date is the date of the commit, or empty if it is unknown
	Date string
	// Modified marks binaries built from sources with uncommitted changes
	Modified bool
}

// Get returns the version of the running binary
func Get() Info {
	buildInfo, _ := debug.ReadBuildInfo()
	return resolve(version, commit, date, buildInfo)
}

// resolve combines the values set with -ldflags and the build info, preferring the former.
// buildInfo may be nil, e.g. for binaries built without module support.
func resolve(version, commit, date string, buildInfo *debug.BuildInfo) Info {
	info := Info{Version: version, Commit: commit, Date: date}

	if buildInfo != nil {
		// go install sets the module version, go build since Go 1.24 a version derived from the git tags.
		// Binaries built outside of a module or git repository report (devel).
		if info.Version == "" && buildInfo.Main.Version != "(devel)" {
			info.Version = buildInfo.Main.Version
		}

		// The VCS settings are only available for go build within a git repository, not for go install.
		// Commit, date and modified describe the same commit, so they are only taken from the build info together,
		// if -ldflags set neither the commit nor the date. Otherwise a commit could be printed with the date of another one.
		if commit == "" && date == "" {
			for _, setting := range buildInfo.Settings {
				switch setting.Key {
				case "vcs.revision":
					info.Commit = setting.Value
				case "vcs.time":
					info.Date = setting.Value
				case "vcs.modified":
					info.Modified = setting.Value == "true"
				}
			}
		}
	}

	// The build info uses versions with a leading v, while releases set them without
	info.Version = strings.TrimPrefix(info.Version, "v")
	if info.Version == "" {
		info.Version = "dev"
	}

	return info
}

// String returns the version with the short commit hash and its date if they are known,
// e.g. "0.0.29 (commit 1a2b3c4, 2026-10-18T10:00:00Z)"
func (i Info) String() string {
	var details []string
	if i.Commit != "" {
		short := i.Commit[:min(len(i.Commit), shortCommitLength)]
		if i.Modified {
			short += "-dirty"
		}
		details = append(details, "commit "+short)
	}
	if i.Date != "" {
		details = append(details, i.Date)
	}

	if len(details) == 0 {
		return i.Version
	}
	return fmt.Sprintf("%s (%s)", i.Version, strings.Join(details, ", "))
}
