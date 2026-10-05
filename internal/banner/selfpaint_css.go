package banner

import "strings"

// A small, tolerant CSS reader. It is not a validator: it only needs to find
// out whether a stylesheet (or an inline style attribute) paints the page
// root, so it understands comments, strings, parentheses, nested blocks and
// at-rules, and shrugs off everything else.

// maxCSSNesting bounds how deep the reader follows nested blocks. Past it the
// stylesheet is treated as opaque, which for this check means "might paint".
const maxCSSNesting = 24

const (
	reasonCSSImport  = "css-import"
	reasonCSSScheme  = "css-color-scheme"
	reasonRootPaint  = "css-root-background"
	reasonCSSTooDeep = "css-too-deeply-nested"
)

// cssBlank fills the spots blankNested hides; it never occurs in real markup.
const cssBlank = 0

// cssItem is one entry of a rule list or a declaration list: the text before
// the first top-level ';' or '{', plus the braced body when there was one.
type cssItem struct {
	head     string
	body     string
	hasBlock bool
}

// stripCSSComments replaces every comment outside a string with one space.
// An unterminated comment swallows the rest of the input, as in a browser.
func stripCSSComments(css string) string {
	if !strings.Contains(css, "/*") {
		return css
	}
	var out strings.Builder
	out.Grow(len(css))
	for i := 0; i < len(css); {
		switch {
		case css[i] == '\\' && i+1 < len(css):
			out.WriteString(css[i : i+2])
			i += 2
		case css[i] == '"' || css[i] == '\'':
			end := cssStringEnd(css, i)
			out.WriteString(css[i:end])
			i = end
		case css[i] == '/' && i+1 < len(css) && css[i+1] == '*':
			out.WriteByte(' ')
			close := strings.Index(css[i+2:], "*/")
			if close < 0 {
				return out.String()
			}
			i += 2 + close + 2
		default:
			out.WriteByte(css[i])
			i++
		}
	}
	return out.String()
}

// cssStringEnd returns the index just past the string that opens at s[i]. A
// raw newline ends a string early, matching how CSS recovers from a bad one.
func cssStringEnd(s string, i int) int {
	quote := s[i]
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case quote:
			return j + 1
		case '\n':
			return j
		}
	}
	return len(s)
}

// skipToStop returns the index of the first byte in stops that sits outside
// strings, parentheses and square brackets, or len(s) when there is none.
func skipToStop(s string, from int, stops string) int {
	depth := 0
	for i := from; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\':
			i++
		case c == '"' || c == '\'':
			i = cssStringEnd(s, i) - 1
		case c == '(' || c == '[':
			depth++
		case (c == ')' || c == ']') && depth > 0:
			depth--
		case depth == 0 && strings.IndexByte(stops, c) >= 0:
			return i
		}
	}
	return len(s)
}

// blockEnd returns the index of the brace that closes the block whose
// opening brace sits at s[open], or len(s) when it never closes.
func blockEnd(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"', '\'':
			i = cssStringEnd(s, i) - 1
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(s)
}

// splitCSSItems cuts comment-free CSS into its top-level items.
func splitCSSItems(css string) []cssItem {
	var items []cssItem
	for i := 0; i < len(css); {
		end := skipToStop(css, i, ";{}")
		head := css[i:end]
		switch {
		case end >= len(css) || css[end] == ';':
			if strings.TrimSpace(head) != "" {
				items = append(items, cssItem{head: head})
			}
			i = end + 1
		case css[end] == '}':
			// A stray closing brace: keep any text before it and move on.
			if strings.TrimSpace(head) != "" {
				items = append(items, cssItem{head: head})
			}
			i = end + 1
		default:
			close := blockEnd(css, end)
			items = append(items, cssItem{head: head, body: css[end+1 : close], hasBlock: true})
			i = close + 1
		}
	}
	return items
}

// cssSignal inspects a stylesheet and returns the reason it settles the
// question (an @import, a color-scheme, a background on the page root), or ""
// when it settles nothing.
func cssSignal(css string) string {
	return itemsSignal(splitCSSItems(stripCSSComments(css)), false, 0)
}

// inlineStyleSignal does the same for the value of a style attribute on an
// element that is itself a page root.
func inlineStyleSignal(style string) string {
	return itemsSignal(splitCSSItems(stripCSSComments(style)), true, 0)
}

// itemsSignal walks a list of items. onRoot says whether the declarations
// found here belong to a rule whose selector reaches the page root. Nested
// blocks recurse: at-rules (media, layer, supports, container, ...) inherit
// the flag, while a nested style rule works it out from its own selector.
func itemsSignal(items []cssItem, onRoot bool, depth int) string {
	if depth > maxCSSNesting {
		return reasonCSSTooDeep
	}
	for _, it := range items {
		head := strings.TrimSpace(it.head)
		if strings.HasPrefix(head, "@") {
			if !it.hasBlock {
				if strings.HasPrefix(strings.ToLower(head), "@import") {
					return reasonCSSImport
				}
				continue
			}
			if r := itemsSignal(splitCSSItems(it.body), onRoot, depth+1); r != "" {
				return r
			}
			continue
		}
		if it.hasBlock {
			if r := itemsSignal(splitCSSItems(it.body), selectorReachesRoot(head), depth+1); r != "" {
				return r
			}
			continue
		}
		colon := strings.IndexByte(head, ':')
		if colon < 0 {
			continue
		}
		prop := strings.ToLower(strings.TrimSpace(head[:colon]))
		switch {
		case prop == "color-scheme":
			return reasonCSSScheme
		case onRoot && (prop == "background" || strings.HasPrefix(prop, "background-")):
			return reasonRootPaint
		}
	}
	return ""
}

// blankNested returns s with everything inside strings, parentheses and
// square brackets overwritten by a filler byte (the brackets themselves are
// kept), so structure can be searched with plain string functions.
func blankNested(s string) string {
	out := []byte(s)
	depth := 0
	for i := 0; i < len(out); i++ {
		c := out[i]
		switch {
		case c == '\\':
			if i+1 < len(out) {
				out[i], out[i+1] = cssBlank, cssBlank
				i++
			}
		case c == '"' || c == '\'':
			end := cssStringEnd(s, i)
			for j := i; j < end; j++ {
				out[j] = cssBlank
			}
			i = end - 1
		case c == '(' || c == '[':
			if depth > 0 {
				out[i] = cssBlank
			}
			depth++
		case (c == ')' || c == ']') && depth > 0:
			depth--
			if depth > 0 {
				out[i] = cssBlank
			}
		case depth > 0:
			out[i] = cssBlank
		}
	}
	return string(out)
}

// selectorReachesRoot reports whether any selector of the list can match the
// page root: html, body, :root or the bare universal selector as the final
// compound, whatever ancestors precede it.
func selectorReachesRoot(list string) bool {
	return selectorListReachesRoot(list, 0)
}

func selectorListReachesRoot(list string, depth int) bool {
	masked := blankNested(list)
	start := 0
	for i := 0; i <= len(masked); i++ {
		if i < len(masked) && masked[i] != ',' {
			continue
		}
		if compoundIsRoot(lastCompound(list[start:i], masked[start:i]), depth) {
			return true
		}
		start = i + 1
	}
	return false
}

// lastCompound returns the final compound selector of a single selector,
// that is the part after the last combinator.
func lastCompound(sel, masked string) string {
	end := len(masked)
	for end > 0 && isCSSSpace(masked[end-1]) {
		end--
	}
	begin := end
	for begin > 0 && !strings.ContainsRune(" \t\n\r\f>+~", rune(masked[begin-1])) {
		begin--
	}
	return sel[begin:end]
}

func isCSSSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f'
}

func isCSSIdentByte(b byte) bool {
	return b == '-' || b == '_' || b >= 0x80 ||
		(b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// compoundIsRoot decides whether one compound selector can be the page root.
func compoundIsRoot(compound string, depth int) bool {
	if compound == "" {
		return false
	}
	masked := asciiLower(blankNested(compound))
	n := 0
	for n < len(masked) && (masked[n] == '*' || isCSSIdentByte(masked[n])) {
		n++
	}
	kind, rest := masked[:n], masked[n:]

	if strings.Contains(rest, "::") {
		return false
	}
	for _, legacy := range []string{"before", "after", "first-line", "first-letter"} {
		if hasPseudo(rest, legacy) {
			return false
		}
	}
	if hasPseudo(rest, "root") {
		return true
	}
	switch kind {
	case "html", "body":
		return true
	case "*":
		return rest == ""
	case "":
		// :is(html, body) and friends.
		for _, fn := range []string{":is(", ":where(", ":matches(", ":-webkit-any(", ":-moz-any("} {
			if strings.HasPrefix(rest, fn) && depth < maxCSSNesting {
				open := len(compound) - len(rest) + len(fn)
				if close := strings.LastIndexByte(compound, ')'); close >= open {
					return selectorListReachesRoot(compound[open:close], depth+1)
				}
			}
		}
	}
	return false
}

// asciiLower lowercases ASCII letters only, so byte offsets survive.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// hasPseudo reports whether masked contains the pseudo-class or legacy
// pseudo-element ":name" as a whole word.
func hasPseudo(masked, name string) bool {
	needle := ":" + name
	for from := 0; ; {
		at := strings.Index(masked[from:], needle)
		if at < 0 {
			return false
		}
		at += from
		after := at + len(needle)
		if after >= len(masked) || !isCSSIdentByte(masked[after]) {
			return true
		}
		from = after
	}
}
