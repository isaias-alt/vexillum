package herdr

import (
	"fmt"
	"os/exec"
	"strings"
)

// SupportedVersionPrefix is the herdr version series internal/herdr's
// error-code classifiers (IsNotFound, IsStalled, IsNotRunning, and the
// rest) were verified against, live, one command at a time - not derived
// from herdr's docs or skill file, which don't promise these codes are
// stable across versions. doctor.go warns (doesn't fail) when the
// installed herdr doesn't match, so a version bump becomes a visible
// signal instead of a silent parsing risk.
const SupportedVersionPrefix = "0.9."

// Version reports the installed herdr binary's version string (e.g.
// "0.9.0"). Unlike run()'s JSON envelope, "herdr --version" prints plain
// text on success ("herdr 0.9.0", verified live) - it's read directly,
// the same way AgentRead reads plain transcript text instead of JSON.
func Version() (string, error) {
	out, err := exec.Command("herdr", "--version").Output()
	if err != nil {
		return "", fmt.Errorf("running herdr --version: %w", err)
	}
	return parseVersion(string(out))
}

// parseVersion extracts the version number from herdr --version's output
// ("herdr 0.9.0" -> "0.9.0"). A pure function so it can be tested without
// shelling out.
func parseVersion(out string) (string, error) {
	trimmed := strings.TrimSpace(out)
	_, version, found := strings.Cut(trimmed, " ")
	if !found || version == "" {
		return "", fmt.Errorf("unexpected herdr --version output: %q", trimmed)
	}
	return version, nil
}

// IsSupportedVersion reports whether v falls within SupportedVersionPrefix.
func IsSupportedVersion(v string) bool {
	return strings.HasPrefix(v, SupportedVersionPrefix)
}
