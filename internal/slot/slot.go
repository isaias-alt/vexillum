// Package slot manages the block vexillum owns inside a user's AGENTS.md:
//
//	<!-- BEGIN VEXILLUM v:1 hash:ab12cd34 -->
//	...commander core...
//	<!-- END VEXILLUM -->
//
// Everything here is pure string in / string out; callers do the file I/O
// (WriteFile in this package is the atomic, mode- and symlink-preserving
// helper for that). The hash in the BEGIN marker is the first 8 hex chars of
// the SHA-256 of the block body, so a body that no longer matches it was
// edited by the user and is reported as drifted instead of overwritten.
//
// Marker detection is anchored to line starts and ignores markers inside
// fenced code blocks, so documentation that quotes the markers is safe.
// Repair is inspired by gentle-ai's InjectMarkdownSection and
// stripOrphanMarkers (MIT, github.com/Gentleman-Programming/gentle-ai); see
// THIRD-PARTY-NOTICES.md.
package slot

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Version is the marker format version written as "v:<Version>".
const Version = 1

// State is the condition of the slot in a given AGENTS.md.
type State int

const (
	// StateAbsent: no vexillum markers in the file.
	StateAbsent State = iota
	// StateCurrent: the block matches the template exactly, marker included.
	StateCurrent
	// StateStale: the block is untouched by the user (its hash matches the
	// body, or the body is the template) but differs from the template.
	StateStale
	// StateDrifted: the body does not match the marker's hash and is not the
	// template, i.e. the user edited inside the block.
	StateDrifted
	// StateMalformed: BEGIN without END, END before BEGIN, or more than one
	// pair. Only Repair, after confirmation, can fix these.
	StateMalformed
)

func (s State) String() string {
	switch s {
	case StateAbsent:
		return "absent"
	case StateCurrent:
		return "current"
	case StateStale:
		return "stale"
	case StateDrifted:
		return "drifted"
	case StateMalformed:
		return "malformed"
	}
	return fmt.Sprintf("State(%d)", int(s))
}

var (
	// ErrMalformed is wrapped by Upsert and Remove when the markers are
	// unbalanced, misordered or duplicated.
	ErrMalformed = errors.New("malformed vexillum block")
	// ErrDrifted is wrapped by Upsert when the body was edited by the user
	// and force is false.
	ErrDrifted = errors.New("vexillum block was edited")
)

// Inspection is the result of Inspect.
type Inspection struct {
	State State
	// Body is the canonical body found between the markers (empty when
	// absent or malformed).
	Body string
	// MarkerHash and MarkerVersion are the metadata read from BEGIN; empty
	// when the marker does not carry them.
	MarkerHash    string
	MarkerVersion string
	// BodyHash is Hash(Body).
	BodyHash string
	// Reason explains a malformed state.
	Reason string
}

// canonical normalises line endings to LF and drops trailing newlines, so
// the same content hashes and compares equal whatever the file's EOL style.
func canonical(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	return strings.TrimRight(body, "\n")
}

// Hash returns the 8-hex-char digest recorded in the BEGIN marker: SHA-256
// of the canonical body.
func Hash(body string) string {
	sum := sha256.Sum256([]byte(canonical(body)))
	return hex.EncodeToString(sum[:])[:8]
}

func beginMarker(body string) string {
	return fmt.Sprintf("%s v:%d hash:%s -->", beginPrefix, Version, Hash(body))
}

// Render returns the full block for body with LF line endings and a final
// newline.
func Render(body string) string {
	return render(body, "\n", true)
}

func render(body, eol string, finalEOL bool) string {
	body = canonical(body)
	var b strings.Builder
	b.WriteString(beginMarker(body))
	b.WriteString(eol)
	if body != "" {
		b.WriteString(strings.ReplaceAll(body, "\n", eol))
		b.WriteString(eol)
	}
	b.WriteString(endLine)
	if finalEOL {
		b.WriteString(eol)
	}
	return b.String()
}

// Inspect classifies the slot in content against the template body.
func Inspect(content, template string) Inspection {
	d := parse(content)
	return inspect(d, template)
}

func inspect(d doc, template string) Inspection {
	if len(d.markers) == 0 {
		return Inspection{State: StateAbsent}
	}
	pairs, orphans := d.pairs()
	if len(pairs) != 1 || len(orphans) != 0 {
		return Inspection{State: StateMalformed, Reason: malformedReason(d, pairs, orphans)}
	}
	p := pairs[0]
	body := canonical(d.body(p))
	ins := Inspection{Body: body, BodyHash: Hash(body)}
	ins.MarkerVersion, ins.MarkerHash = parseBegin(d.text(p.begin.line))

	tmpl := canonical(template)
	bodyIsTemplate := body == tmpl
	markerOK := strings.TrimRight(d.text(p.begin.line), " \t") == beginMarker(tmpl)
	switch {
	case bodyIsTemplate && markerOK:
		ins.State = StateCurrent
	case bodyIsTemplate || ins.MarkerHash == ins.BodyHash:
		// Untouched by the user: either it is the template under an old
		// marker, or an older template whose hash still vouches for it.
		ins.State = StateStale
	default:
		ins.State = StateDrifted
	}
	return ins
}

func parseBegin(t string) (version, hash string) {
	inner := strings.TrimSuffix(strings.TrimRight(t, " \t"), "-->")
	inner = strings.TrimPrefix(inner, beginPrefix)
	for _, f := range strings.Fields(inner) {
		switch {
		case strings.HasPrefix(f, "v:"):
			version = f[2:]
		case strings.HasPrefix(f, "hash:"):
			hash = f[5:]
		}
	}
	return version, hash
}

func malformedReason(d doc, pairs []span, orphans []marker) string {
	if len(orphans) > 0 {
		o := orphans[0]
		n := o.line + 1
		if o.kind == kindEnd {
			return fmt.Sprintf("END marker at line %d has no matching BEGIN before it", n)
		}
		return fmt.Sprintf("BEGIN marker at line %d has no matching END", n)
	}
	return fmt.Sprintf("found %d vexillum blocks, expected exactly one", len(pairs))
}

func eolOf(content string) string {
	if i := strings.IndexByte(content, '\n'); i > 0 && content[i-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

// Upsert returns content with the slot set to body.
//
//   - No markers: the block is appended after one blank line (or becomes
//     the whole file when content is empty or blank). User content is never
//     reordered.
//   - One well-formed pair: what is between the markers is replaced; when
//     the block is already current the input is returned unchanged.
//   - Malformed markers: error wrapping ErrMalformed, whatever force says.
//   - Edited (drifted) body: error wrapping ErrDrifted unless force is true.
//
// The block uses the file's own line-ending style.
func Upsert(content, body string, force bool) (string, error) {
	d := parse(content)
	ins := inspect(d, body)
	switch ins.State {
	case StateMalformed:
		return "", fmt.Errorf("%w: %s", ErrMalformed, ins.Reason)
	case StateCurrent:
		return content, nil
	case StateDrifted:
		if !force {
			return "", fmt.Errorf("%w: body does not match marker hash %q", ErrDrifted, ins.MarkerHash)
		}
	case StateAbsent:
		return appendBlock(content, body), nil
	}
	pairs, _ := d.pairs()
	p := pairs[0]
	start, end := d.bounds(p)
	eol := eolOf(content)
	if l := d.lines[p.begin.line]; l.next > l.textEnd {
		// textEnd excludes the \r, so this is "\r\n" for CRLF lines.
		eol = d.src[l.textEnd:l.next]
	}
	finalEOL := end > d.lines[p.end.line].textEnd
	return content[:start] + render(body, eol, finalEOL) + content[end:], nil
}

func appendBlock(content, body string) string {
	if strings.TrimSpace(content) == "" {
		return render(body, eolOf(content), true)
	}
	eol := eolOf(content)
	sep := ""
	switch {
	case !strings.HasSuffix(content, "\n"):
		sep = eol + eol
	case strings.HasSuffix(content, "\n\n"), strings.HasSuffix(content, "\r\n\r\n"):
		sep = ""
	default:
		sep = eol
	}
	return content + sep + render(body, eol, true)
}

// Remove returns content without the vexillum block: the marker lines, the
// body, the END line's terminator and one adjacent blank separator line (the
// one Upsert put before an appended block; after a block at the very top,
// the one following it). Absent markers return content unchanged; malformed
// ones return an error wrapping ErrMalformed. The caller decides whether to
// delete the file when the result is blank and vexillum created it.
func Remove(content string) (string, error) {
	d := parse(content)
	if len(d.markers) == 0 {
		return content, nil
	}
	pairs, orphans := d.pairs()
	if len(pairs) != 1 || len(orphans) != 0 {
		return "", fmt.Errorf("%w: %s", ErrMalformed, malformedReason(d, pairs, orphans))
	}
	start, end := d.bounds(pairs[0])
	start, end = tidyRange(content, start, end)
	return content[:start] + content[end:], nil
}

// tidyRange widens [start,end) to also take one adjacent blank line so no
// double blank is left behind: the one before it, or, at the very top of the
// file, the one after it.
func tidyRange(content string, start, end int) (int, int) {
	prefix := content[:start]
	if prefix == "" {
		return start, end + len(leadingBlank(content[end:]))
	}
	switch {
	case strings.HasSuffix(prefix, "\r\n\r\n"):
		return start - 2, end
	case strings.HasSuffix(prefix, "\n\n"):
		return start - 1, end
	}
	return start, end
}

// applyCuts removes the given ranges (any order, overlaps merged).
func applyCuts(content string, ranges [][2]int) string {
	sort.Slice(ranges, func(i, j int) bool { return ranges[i][0] < ranges[j][0] })
	var b strings.Builder
	pos := 0
	for _, r := range ranges {
		if r[0] > pos {
			b.WriteString(content[pos:r[0]])
		}
		if r[1] > pos {
			pos = r[1]
		}
	}
	b.WriteString(content[pos:])
	return b.String()
}

// leadingBlank returns the blank line (terminator included) s starts with.
func leadingBlank(s string) string {
	switch {
	case strings.HasPrefix(s, "\r\n"):
		return s[:2]
	case strings.HasPrefix(s, "\n"):
		return s[:1]
	}
	return ""
}

// Repair fixes orphan markers and duplicate pairs and describes each action
// so the caller can ask for confirmation before writing. It keeps the first
// well-formed pair, deletes every other pair (including its contents) and
// deletes orphan marker lines (only the marker line, never the text around
// it). With no pair left the file simply has no slot, and Upsert will append
// a fresh one. It returns content unchanged and no actions when the markers
// are already fine. The result may still be drifted; Repair never judges
// the body.
func Repair(content string) (string, []string) {
	d := parse(content)
	pairs, orphans := d.pairs()
	if len(d.markers) == 0 || (len(pairs) == 1 && len(orphans) == 0) {
		return content, nil
	}

	type cutRange struct {
		start, end int
		first      int // line number for ordering
		desc       string
	}
	var cuts []cutRange
	for i, p := range pairs {
		if i == 0 {
			continue
		}
		s, e := d.bounds(p)
		s, e = tidyRange(content, s, e)
		cuts = append(cuts, cutRange{s, e, p.begin.line,
			fmt.Sprintf("remove duplicate block at lines %d-%d", p.begin.line+1, p.end.line+1)})
	}
	for _, o := range orphans {
		l := d.lines[o.line]
		name := "BEGIN"
		if o.kind == kindEnd {
			name = "END"
		}
		cuts = append(cuts, cutRange{l.start, l.next, o.line,
			fmt.Sprintf("remove orphan %s marker at line %d", name, o.line+1)})
	}

	sort.Slice(cuts, func(i, j int) bool { return cuts[i].first < cuts[j].first })
	actions := make([]string, len(cuts))
	ranges := make([][2]int, len(cuts))
	for i, c := range cuts {
		actions[i] = c.desc
		ranges[i] = [2]int{c.start, c.end}
	}
	return applyCuts(content, ranges), actions
}
