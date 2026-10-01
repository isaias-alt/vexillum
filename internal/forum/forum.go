// Package forum implements "vexillum forum". It serves a single local HTML
// artifact file so its Mermaid diagrams (each in a `<div class="mermaid">...</div>` container,
// the same authoring convention forum-tool already uses) render as
// editable Excalidraw whiteboards in the browser.
//
// This package intentionally does not implement the rest of the general
// "vexillum forum" product (element annotation, a comment/feedback prompt
// queue, long-poll session semantics, end/reopen) - see the scout report
// this feature was dispatched from
// (vx-sos-un-scout-en-el-rep-7f53b1-7f53b1b8ae1c4a54.md) for that larger
// design, which explicitly scoped the whiteboard out of its own v1 for lack
// of a concrete need at the time. That larger loop is a separate,
// not-yet-built mission; this package only needs to serve one artifact for
// one process's lifetime, so it deliberately skips the multi-session
// server, lock file, and background-daemon machinery that loop would need.
//
// A Server instance is created per `vexillum forum <file>` invocation and
// serves exactly that one file: there is no session key or artifact id in
// any URL. The session key that does exist (SessionKey, below) is an
// on-disk namespacing detail only - it keys where a diagram's whiteboard
// state lives under ~/.vexillum/<project>/forums/<key>/whiteboards/, so
// that re-running `vexillum forum` against the same file later finds its
// autosaved scenes again.
package forum

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
)

// sessionKeyPattern matches SessionKey's own output: 16 lowercase hex
// characters. Used to validate a key before it reaches filepath.Join, the
// same defensive posture internal/state.ValidateID takes for task ids.
var sessionKeyPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// SessionKey derives the stable on-disk key for a forum session for file: the
// first 16 hex characters of sha256(absolute path). Two `vexillum forum`
// invocations against the same file (any working directory, any relative
// path spelling) converge on the same key, so a diagram's autosaved scene
// survives across runs; a renamed or moved file gets a fresh key and starts
// over, same accepted tradeoff as internal/project.Key for a moved project.
func SessionKey(absFile string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(absFile)))
	return hex.EncodeToString(sum[:])[:16]
}

// ValidSessionKey reports whether key has the shape SessionKey produces.
// Every path built from a caller-influenced key (diagram JSON, feedback
// files) validates it first - see store.go.
func ValidSessionKey(key string) bool {
	return sessionKeyPattern.MatchString(key)
}

// ValidDiagramIndex reports whether index is in the range vexillum forum
// accepts for a diagram's position in the artifact (0-999, matching the
// hard cap encoded in the whiteboard frame's own URL parsing - see
// whiteboard-frame.js's `main()`).
func ValidDiagramIndex(index int) bool {
	return index >= 0 && index <= 999
}

func validateRef(key string, index int) error {
	if !ValidSessionKey(key) {
		return fmt.Errorf("invalid forum session key: %q", key)
	}
	if !ValidDiagramIndex(index) {
		return fmt.Errorf("invalid diagram index: %d", index)
	}
	return nil
}
