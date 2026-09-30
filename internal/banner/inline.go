package banner

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// InlineLocalAssets reads the HTML file at path and returns it with every
// local asset reference (images, stylesheets, scripts, and a handful of
// other same-directory-tree references reachable via src/href/poster/
// inline style) rewritten as a data: URI, so the page is fully
// self-contained once published to a remote host that can't see the
// author's filesystem. Remote references (an absolute URL, a
// protocol-relative "//...", or already a data: URI) are left untouched -
// they resolve in the visitor's own browser, same as forum-tool's own
// export/share packaging.
//
// A local stylesheet's own url(...) references (fonts, background
// images) are inlined too, since a data: URI has no location of its own
// for a browser to resolve a *relative* url() against - left alone,
// those would silently break once served from ht-ml.app's domain instead
// of the author's local directory. @import chains inside CSS are not
// followed; a stylesheet that @imports another local file is a known
// limitation.
//
// A reference that can't be resolved (missing file, or one that escapes
// the directory tree rooted at path's own directory) is left untouched
// rather than failing the whole publish, and reported back as a warning.
func InlineLocalAssets(path string) (out string, warnings []string, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("reading %s: %w", path, err)
	}
	root := filepath.Dir(path)
	if root, err = filepath.Abs(root); err != nil {
		return "", nil, fmt.Errorf("resolving %s: %w", path, err)
	}

	doc, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return "", nil, fmt.Errorf("parsing %s as HTML: %w", path, err)
	}

	in := &inliner{root: root}
	in.walk(doc)

	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		return "", nil, fmt.Errorf("rendering inlined HTML: %w", err)
	}
	return buf.String(), in.warnings, nil
}

type inliner struct {
	root     string
	warnings []string
}

func (in *inliner) warnf(format string, args ...any) {
	in.warnings = append(in.warnings, fmt.Sprintf(format, args...))
}

func (in *inliner) walk(n *html.Node) {
	if n.Type == html.ElementNode {
		in.visitElement(n)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		in.walk(c)
	}
}

func (in *inliner) visitElement(n *html.Node) {
	switch n.DataAtom {
	case atom.Img, atom.Script, atom.Source, atom.Track, atom.Audio, atom.Video, atom.Embed:
		in.inlineAttrAsData(n, "src", in.root)
	case atom.Link:
		if linkIsInlinable(n) {
			in.inlineStylesheetOrIconLink(n)
		}
	case atom.Style:
		if n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
			n.FirstChild.Data = in.inlineCSSURLs(n.FirstChild.Data, in.root)
		}
	}
	if hasAttr(n, "poster") {
		in.inlineAttrAsData(n, "poster", in.root)
	}
	if style, ok := getAttr(n, "style"); ok && style != "" {
		setAttr(n, "style", in.inlineCSSURLs(style, in.root))
	}
}

// linkIsInlinable reports whether a <link> element's rel is one worth
// inlining - a stylesheet or an icon. Other rel values (canonical,
// alternate, manifest, preconnect, dns-prefetch, ...) aren't local
// assets in the sense this command cares about, so they're left as-is.
func linkIsInlinable(n *html.Node) bool {
	rel, _ := getAttr(n, "rel")
	for _, token := range strings.Fields(strings.ToLower(rel)) {
		if token == "stylesheet" || token == "icon" {
			return true
		}
	}
	return false
}

func (in *inliner) inlineStylesheetOrIconLink(n *html.Node) {
	href, ok := getAttr(n, "href")
	if !ok || isRemoteRef(href) {
		return
	}
	full, ok := in.resolveLocalPath(in.root, href)
	if !ok {
		in.warnf("skipped %q: escapes the artifact's local directory tree", href)
		return
	}

	if strings.EqualFold(filepath.Ext(full), ".css") {
		raw, err := os.ReadFile(full)
		if err != nil {
			in.warnf("skipped %q: %v", href, err)
			return
		}
		css := in.inlineCSSURLs(string(raw), filepath.Dir(full))
		encoded := base64.StdEncoding.EncodeToString([]byte(css))
		setAttr(n, "href", "data:text/css;base64,"+encoded)
		return
	}

	in.inlineAttrAsData(n, "href", in.root)
}

func (in *inliner) inlineAttrAsData(n *html.Node, key, dir string) {
	ref, ok := getAttr(n, key)
	if !ok || isRemoteRef(ref) {
		return
	}
	dataURI, warning, ok := in.readAsDataURI(dir, ref)
	if !ok {
		in.warnf("%s", warning)
		return
	}
	setAttr(n, key, dataURI)
}

// cssURLPattern matches CSS url(...) references, double-quoted,
// single-quoted, or bare. RE2 (unlike PCRE) has no backreferences, so
// quoted and unquoted forms are three separate alternatives rather than
// one pattern with a matched closing quote.
var cssURLPattern = regexp.MustCompile(`url\(\s*(?:"([^"]*)"|'([^']*)'|([^'")\s]*))\s*\)`)

func (in *inliner) inlineCSSURLs(css, dir string) string {
	return cssURLPattern.ReplaceAllStringFunc(css, func(match string) string {
		sub := cssURLPattern.FindStringSubmatch(match)
		ref := strings.TrimSpace(firstNonEmpty(sub[1], sub[2], sub[3]))
		if isRemoteRef(ref) {
			return match
		}
		dataURI, warning, ok := in.readAsDataURI(dir, ref)
		if !ok {
			in.warnf("%s", warning)
			return match
		}
		return "url(" + dataURI + ")"
	})
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func (in *inliner) readAsDataURI(dir, ref string) (dataURI, warning string, ok bool) {
	full, ok := in.resolveLocalPath(dir, ref)
	if !ok {
		return "", fmt.Sprintf("skipped %q: escapes the artifact's local directory tree", ref), false
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Sprintf("skipped %q: %v", ref, err), false
	}
	mimeType := mime.TypeByExtension(filepath.Ext(full))
	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	return "data:" + mimeType + ";base64," + encoded, "", true
}

// resolveLocalPath resolves ref relative to dir, then requires the result
// to still fall within in.root - the original HTML file's own directory
// tree. dir may itself be a subdirectory reached by an earlier inlining
// step (e.g. a stylesheet's own directory), so relative resolution
// happens against dir, but containment is always checked against the one
// root for the whole run.
func (in *inliner) resolveLocalPath(dir, ref string) (string, bool) {
	u, err := url.Parse(ref)
	if err != nil || u.Path == "" {
		return "", false
	}
	p := u.Path
	if decoded, err := url.PathUnescape(p); err == nil {
		p = decoded
	}
	p = strings.TrimPrefix(p, "/")

	full := filepath.Clean(filepath.Join(dir, filepath.FromSlash(p)))
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", false
	}
	if absFull != in.root && !strings.HasPrefix(absFull, in.root+string(filepath.Separator)) {
		return "", false
	}
	return absFull, true
}

// isRemoteRef reports whether ref points somewhere other than the local
// filesystem: it already has a scheme (http, https, data, mailto, ...),
// it's protocol-relative ("//fonts.example.com/..."), it's an in-page
// anchor ("#section"), or it's empty.
func isRemoteRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "#") || strings.HasPrefix(ref, "//") {
		return true
	}
	u, err := url.Parse(ref)
	if err != nil {
		return true
	}
	return u.IsAbs()
}

func getAttr(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val, true
		}
	}
	return "", false
}

func hasAttr(n *html.Node, key string) bool {
	_, ok := getAttr(n, key)
	return ok
}

func setAttr(n *html.Node, key, val string) {
	for i, a := range n.Attr {
		if a.Key == key {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: key, Val: val})
}
