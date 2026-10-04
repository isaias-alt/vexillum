// Package forum implements "vx forum". It serves a single local HTML
// artifact file so its Mermaid diagrams (each in a
// `<div class="mermaid">...</div>` container) render as editable Excalidraw
// whiteboards in the browser.
//
// Beyond the whiteboards it carries the review loop: sessions keyed by the
// artifact's path, a prompt queue the user fills from the browser (typed
// messages, decision forms through window.forum, and annotations of elements
// or selected text, all the same Prompt), a long-poll the agent drains, and
// replies back into the browser. Annotation capture lives in the artifact
// script (assets/chrome/forum-sdk.js) and its note card in the chrome
// (forum-chrome.js); everything reaches the queue through the same
// same-origin, session-token API. Images pasted or dropped into the
// conversation are stored per session and reach the agent as local file paths
// (attachments.go); every open tab of a session stays in step over a
// server-sent-events feed (server_browser.go); the transcript is bounded (500
// messages, 5 MB) without ever dropping anything not yet delivered (hub.go);
// and the browser's passive layout audit fills a per-session inbox the user
// triages, which only becomes a prompt when the user queues it (layout.go).
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
// first 16 hex characters of sha256(absolute path). Two `vx forum`
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

// ValidDiagramIndex reports whether index is in the range vx forum
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
