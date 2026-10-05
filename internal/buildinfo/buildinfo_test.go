package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestFormatVersion(t *testing.T) {
	full := VCS{Commit: "a1b2c3d", Time: "2026-10-05T12:34:56Z", Dirty: true}
	tests := []struct {
		name    string
		version string
		vcs     VCS
		want    string
	}{
		{"dev full", "dev", full, "vx dev (a1b2c3d, 2026-10-05, dirty)"},
		{"dev clean", "dev", VCS{Commit: "a1b2c3d", Time: "2026-10-05T12:34:56Z"}, "vx dev (a1b2c3d, 2026-10-05)"},
		{"dev no vcs", "dev", VCS{}, "vx dev"},
		{"release with commit", "1.2.3", full, "vx 1.2.3 (a1b2c3d)"},
		{"release no vcs", "1.2.3", VCS{}, "vx 1.2.3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatVersion("vx", tt.version, tt.vcs); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVCSFromSettings(t *testing.T) {
	got := vcsFromSettings([]debug.BuildSetting{
		{Key: "vcs", Value: "git"},
		{Key: "vcs.revision", Value: "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"},
		{Key: "vcs.time", Value: "2026-10-05T12:34:56Z"},
		{Key: "vcs.modified", Value: "true"},
	})
	want := VCS{Commit: "a1b2c3d", Time: "2026-10-05T12:34:56Z", Dirty: true}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if got := vcsFromSettings(nil); got != (VCS{}) {
		t.Errorf("empty settings: got %+v, want zero", got)
	}
}
