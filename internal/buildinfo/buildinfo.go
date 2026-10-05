// Package buildinfo holds the version this binary was built as, so packages
// below cmd/vx (the sentinel records it, doctor compares it) can read it
// without importing package main.
package buildinfo

import (
	"runtime/debug"
	"strings"
)

// Version is the release version, "dev" for a plain "go build". cmd/vx sets it
// at startup from its own linker-set variable. It deliberately never carries
// the commit: the sentinel records it and doctor compares it.
var Version = "dev"

// shortCommitLen is the length of the abbreviated commit hash shown to users.
const shortCommitLen = 7

// VCS is the source-control identity of a build, as stamped by the Go
// toolchain. Every field is empty (Dirty false) when the build carries no VCS
// information, for example "go install" from the module cache.
type VCS struct {
	// Commit is the abbreviated commit hash.
	Commit string
	// Time is the commit time as an RFC 3339 string.
	Time string
	// Dirty reports whether the working tree had uncommitted changes.
	Dirty bool
}

// ReadVCS returns the VCS identity embedded in the running binary.
func ReadVCS() VCS {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return VCS{}
	}
	return vcsFromSettings(bi.Settings)
}

// vcsFromSettings extracts the VCS identity from build settings.
func vcsFromSettings(settings []debug.BuildSetting) VCS {
	var v VCS
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			v.Commit = shortCommit(s.Value)
		case "vcs.time":
			v.Time = s.Value
		case "vcs.modified":
			v.Dirty = s.Value == "true"
		}
	}
	return v
}

func shortCommit(rev string) string {
	if len(rev) > shortCommitLen {
		return rev[:shortCommitLen]
	}
	return rev
}

// FormatVersion renders the --version line for the binary called name. A dev
// build shows its commit, commit date and dirty flag so the installed build is
// identifiable; a release build keeps "name version" and appends only the
// short commit when it is known.
func FormatVersion(name, version string, v VCS) string {
	out := name + " " + version
	var parts []string
	if v.Commit != "" {
		parts = append(parts, v.Commit)
	}
	if version == "dev" {
		if day, _, _ := strings.Cut(v.Time, "T"); day != "" {
			parts = append(parts, day)
		}
		if v.Dirty {
			parts = append(parts, "dirty")
		}
	}
	if len(parts) == 0 {
		return out
	}
	return out + " (" + strings.Join(parts, ", ") + ")"
}
