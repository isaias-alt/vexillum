package forum

import (
	"crypto/sha256"
	"encoding/hex"
	"html"
	"regexp"
	"strings"
)

// MermaidSource is one `<div class="mermaid">...</div>` container found in
// the served artifact, in document order. Index is that document order (the
// board ordinal the frame URL, the routes and the on-disk record in store.go
// all key off), Source is the decoded diagram text and Hash its digest. The
// JSON names are the whiteboard's wire names.
type MermaidSource struct {
	Index  int    `json:"ordinal"`
	Source string `json:"text"`
	Hash   string `json:"digest"`
}

// divWithClassPattern matches any <div class="...">...</div>, capturing its
// raw class attribute and inner HTML. Filtering to the `.mermaid` ones
// happens afterward in Go (hasClass) rather than inside the regex itself,
// since HTML classes are a whitespace-separated token list, not a substring
// match - a class regex like `\bmermaid\b` would wrongly match an unrelated
// class such as "not-mermaid-at-all" (hyphens are non-word characters, so \b
// still finds a boundary around "mermaid" there). It intentionally does not
// handle a `.mermaid` container nested inside another `.mermaid` container
// (not a shape the authoring convention produces) and is not a general HTML
// parser: a malformed document (an unclosed div inside the container) can
// misdetect the closing tag. Good enough for vexillum's own scaffolded
// artifacts; a real HTML parser is out of scope for this feature (see
// internal/forum's package doc on deferred scope).
var divWithClassPattern = regexp.MustCompile(`(?is)<div[^>]*\bclass\s*=\s*["']([^"']*)["'][^>]*>(.*?)</div>`)

// ExtractMermaidSources scans an artifact's HTML for `.mermaid` containers
// and returns their decoded inner text (the Mermaid source each holds) in
// document order, along with a stable hash of that source. The hash is
// what the whiteboard compares against a stored record's digest to decide
// whether to reopen the saved scene, ask the reviewer, or convert again (see
// decideOpening in tools/whiteboard-bundle/src/scene-record.js).
func ExtractMermaidSources(artifactHTML string) []MermaidSource {
	matches := divWithClassPattern.FindAllStringSubmatch(artifactHTML, -1)
	sources := make([]MermaidSource, 0, len(matches))
	for _, m := range matches {
		if !hasClass(m[1], "mermaid") {
			continue
		}
		source := decodeMermaidSource(m[2])
		sources = append(sources, MermaidSource{
			Index:  len(sources),
			Source: source,
			Hash:   HashMermaidSource(source),
		})
	}
	return sources
}

// hasClass reports whether classAttr's whitespace-separated token list
// contains want exactly.
func hasClass(classAttr, want string) bool {
	for _, class := range strings.Fields(classAttr) {
		if class == want {
			return true
		}
	}
	return false
}

// HashMermaidSource returns the stable hex digest ExtractMermaidSources
// stores as a diagram's Hash.
func HashMermaidSource(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])[:16]
}

func decodeMermaidSource(inner string) string {
	return strings.TrimSpace(html.UnescapeString(inner))
}
