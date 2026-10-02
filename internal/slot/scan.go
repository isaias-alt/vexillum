package slot

import "strings"

// line is one physical line of a document: [start, textEnd) is the text
// without its line terminator (a trailing "\r" is excluded too), next is
// the offset of the following line.
type line struct{ start, textEnd, next int }

type markerKind int

const (
	kindBegin markerKind = iota + 1
	kindEnd
)

// marker is a BEGIN or END marker line found outside fenced code.
type marker struct {
	kind markerKind
	line int // index into doc.lines
}

// doc is a parsed document: its lines and every marker line, in order.
type doc struct {
	src     string
	lines   []line
	markers []marker
}

// span is a well-formed BEGIN..END pair.
type span struct{ begin, end marker }

const (
	beginPrefix = "<!-- BEGIN VEXILLUM"
	endLine     = "<!-- END VEXILLUM -->"
)

func splitLines(s string) []line {
	var out []line
	for start := 0; start < len(s); {
		i := strings.IndexByte(s[start:], '\n')
		if i < 0 {
			out = append(out, line{start, len(s), len(s)})
			break
		}
		textEnd := start + i
		next := textEnd + 1
		if textEnd > start && s[textEnd-1] == '\r' {
			textEnd--
		}
		out = append(out, line{start, textEnd, next})
		start = next
	}
	return out
}

func parse(src string) doc {
	d := doc{src: src, lines: splitLines(src)}
	var fenceCh byte
	fenceN := 0
	for i := range d.lines {
		t := d.text(i)
		if fenceN > 0 {
			if closesFence(t, fenceCh, fenceN) {
				fenceN = 0
			}
			continue
		}
		if ch, n, ok := opensFence(t); ok {
			fenceCh, fenceN = ch, n
			continue
		}
		switch {
		case isBegin(t):
			d.markers = append(d.markers, marker{kindBegin, i})
		case isEnd(t):
			d.markers = append(d.markers, marker{kindEnd, i})
		}
	}
	return d
}

func (d doc) text(i int) string {
	l := d.lines[i]
	return d.src[l.start:l.textEnd]
}

// pairs groups the markers into BEGIN..END pairs. A BEGIN directly followed
// by another BEGIN is an orphan (its END never came); an END with no open
// BEGIN is an orphan too.
func (d doc) pairs() (pairs []span, orphans []marker) {
	for i := 0; i < len(d.markers); i++ {
		m := d.markers[i]
		if m.kind == kindBegin && i+1 < len(d.markers) && d.markers[i+1].kind == kindEnd {
			pairs = append(pairs, span{m, d.markers[i+1]})
			i++
			continue
		}
		orphans = append(orphans, m)
	}
	return pairs, orphans
}

// bounds returns the byte range of a pair, BEGIN line start to the offset
// after the END line's terminator (or EOF).
func (d doc) bounds(p span) (start, end int) {
	return d.lines[p.begin.line].start, d.lines[p.end.line].next
}

// body returns the raw text between the markers (without the terminator of
// the BEGIN line, up to the start of the END line).
func (d doc) body(p span) string {
	return d.src[d.lines[p.begin.line].next:d.lines[p.end.line].start]
}

func isBegin(t string) bool {
	if !strings.HasPrefix(t, beginPrefix) {
		return false
	}
	rest := t[len(beginPrefix):]
	if rest == "" || (rest[0] != ' ' && rest[0] != '-') {
		return false
	}
	return strings.HasSuffix(strings.TrimRight(t, " \t"), "-->")
}

func isEnd(t string) bool {
	return strings.TrimRight(t, " \t") == endLine
}

// opensFence reports whether t opens a fenced code block (CommonMark: up to
// three spaces of indent, then three or more backticks or tildes; a
// backtick fence's info string cannot contain backticks).
func opensFence(t string) (ch byte, n int, ok bool) {
	rest := trimIndent(t)
	if rest == "" || (rest[0] != '`' && rest[0] != '~') {
		return 0, 0, false
	}
	ch = rest[0]
	for n < len(rest) && rest[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0, false
	}
	if ch == '`' && strings.Contains(rest[n:], "`") {
		return 0, 0, false
	}
	return ch, n, true
}

func closesFence(t string, ch byte, minN int) bool {
	rest := trimIndent(t)
	n := 0
	for n < len(rest) && rest[n] == ch {
		n++
	}
	return n >= minN && strings.TrimSpace(rest[n:]) == ""
}

// trimIndent strips up to three leading spaces.
func trimIndent(t string) string {
	i := 0
	for i < len(t) && i < 3 && t[i] == ' ' {
		i++
	}
	return t[i:]
}
