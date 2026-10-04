package selfupdate

import (
	"errors"
	"fmt"
	"strings"
)

// Channel says which releases an upgrade follows.
type Channel string

const (
	// Stable follows the newest full release (not a pre-release).
	Stable Channel = "stable"
	// Canary follows the newest pre-release whose tag carries "-canary.".
	Canary Channel = "canary"
)

// canaryMarker is what a canary pre-release's tag contains, as in
// v0.4.0-canary.2.
const canaryMarker = "-canary."

// ParseChannel reads a --channel value; "" is the default, stable.
func ParseChannel(s string) (Channel, error) {
	switch Channel(s) {
	case "", Stable:
		return Stable, nil
	case Canary:
		return Canary, nil
	}
	return "", fmt.Errorf("unknown channel %q (use stable or canary)", s)
}

// ErrNoRelease means the channel has no usable release.
var ErrNoRelease = errors.New("no release found")

// Latest picks the newest release of the channel by version order, not by the
// order the source listed them in. Drafts are never candidates, and neither
// is a tag that is not a version.
func Latest(releases []Release, ch Channel) (Release, error) {
	var best Release
	var bestVer version
	found := false
	for _, r := range releases {
		if r.Draft || !inChannel(r, ch) {
			continue
		}
		v, err := parseVersion(r.Tag)
		if err != nil {
			continue
		}
		if !found || compare(v, bestVer) > 0 {
			best, bestVer, found = r, v, true
		}
	}
	if !found {
		return Release{}, fmt.Errorf("%w on the %s channel", ErrNoRelease, ch)
	}
	return best, nil
}

func inChannel(r Release, ch Channel) bool {
	if ch == Canary {
		return r.Prerelease && strings.Contains(r.Tag, canaryMarker)
	}
	return !r.Prerelease
}
