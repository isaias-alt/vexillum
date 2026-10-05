package banner

import (
	"strings"

	"golang.org/x/net/html"
)

// NoSurfaceWarning is the advisory printed when a page gives no sign of
// painting its own surface. It is a single line on purpose: it is printed
// next to the other publish warnings.
const NoSurfaceWarning = "Nothing in this page sets a background for the page itself. " +
	"The host draws pages over a color you did not choose, so text can lose contrast or " +
	"vanish, whether it is light text on a light host or dark text on a dark one. " +
	"Give the page its own background and a text color that contrasts with it, then publish again."

const (
	reasonOpaqueParse = "unparseable"
	reasonStylesheet  = "stylesheet-link"
	reasonScript      = "script-with-src"
	reasonMetaScheme  = "meta-color-scheme"
	reasonThemeAttr   = "theme-attribute"
	reasonLegacyAttr  = "legacy-background-attribute"
	reasonBgClass     = "background-class"
)

// DefinesOwnSurface reports whether the page (the final HTML that would be
// published) defines its own surface, and a short machine-readable reason
// naming the first thing that convinced the check. The reason is empty when
// nothing did.
//
// The check is deliberately quiet. A false alarm repeats on every publish and
// teaches authors to ignore the warning; a missed one costs a single look at
// the hosted page. So anything that might paint the page, or that the check
// cannot read (an external stylesheet, a script that styles at runtime, a
// declared color scheme), counts as defining a surface. Only a page where
// none of that shows up is reported as bare.
func DefinesOwnSurface(page string) (bool, string) {
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		return true, reasonOpaqueParse
	}
	for n := doc; n != nil; n = nextNode(n) {
		if n.Type != html.ElementNode {
			continue
		}
		if reason := elementSignal(n); reason != "" {
			return true, reason
		}
	}
	return false, ""
}

// nextNode steps through the tree in document order without recursion.
func nextNode(n *html.Node) *html.Node {
	if n.FirstChild != nil {
		return n.FirstChild
	}
	for ; n != nil; n = n.Parent {
		if n.NextSibling != nil {
			return n.NextSibling
		}
	}
	return nil
}

// elementSignal returns the reason a single element settles the question.
func elementSignal(n *html.Node) string {
	switch n.Data {
	case "html", "body":
		return rootElementSignal(n)
	case "link":
		for _, token := range strings.Fields(strings.ToLower(relOf(n))) {
			if token == "stylesheet" {
				return reasonStylesheet
			}
		}
	case "script":
		if hasAttr(n, "src") {
			return reasonScript
		}
	case "meta":
		if strings.EqualFold(strings.TrimSpace(nameOf(n)), "color-scheme") {
			return reasonMetaScheme
		}
	case "style":
		return cssSignal(textContent(n))
	}
	return ""
}

// rootElementSignal looks at the attributes of html and body.
func rootElementSignal(n *html.Node) string {
	for _, a := range n.Attr {
		key := strings.ToLower(a.Key)
		switch {
		case key == "style":
			if r := inlineStyleSignal(a.Val); r != "" {
				return r
			}
		case key == "bgcolor" || key == "background":
			return reasonLegacyAttr
		case key == "class":
			for _, token := range strings.Fields(a.Val) {
				if isBackgroundUtility(token) {
					return reasonBgClass
				}
			}
		case isThemeAttribute(key):
			return reasonThemeAttr
		}
	}
	return ""
}

// isThemeAttribute matches attributes that hand theming to a framework:
// data-theme, data-bs-theme, data-color-mode, data-color-scheme and the like.
func isThemeAttribute(key string) bool {
	return strings.HasSuffix(key, "theme") ||
		strings.HasSuffix(key, "color-mode") ||
		strings.HasSuffix(key, "color-scheme")
}

// isBackgroundUtility reports whether a class token is a background utility:
// the prefix "bg-" followed by a name, possibly behind variants ("dark:",
// "md:") or a leading important/negative marker. A class that merely contains
// the prefix inside a longer word ("nobg-x") is not one.
func isBackgroundUtility(token string) bool {
	start, depth := 0, 0
	for i := 0; i < len(token); i++ {
		switch token[i] {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		case ':':
			if depth == 0 {
				start = i + 1
			}
		}
	}
	name := strings.TrimLeft(token[start:], "!-")
	return strings.HasPrefix(name, "bg-") && len(name) > len("bg-")
}

func relOf(n *html.Node) string {
	v, _ := getAttr(n, "rel")
	return v
}

func nameOf(n *html.Node) string {
	v, _ := getAttr(n, "name")
	return v
}

// textContent concatenates the text nodes directly below n.
func textContent(n *html.Node) string {
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			sb.WriteString(c.Data)
		}
	}
	return sb.String()
}
