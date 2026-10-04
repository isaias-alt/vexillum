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
	"strconv"
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
// they resolve in the visitor's own browser.
//
// A local stylesheet's own url(...) references (fonts, background
// images) are inlined too, since a data: URI has no location of its own
// for a browser to resolve a *relative* url() against - left alone,
// those would silently break once served from ht-ml.app's domain instead
// of the author's local directory. @import rules are followed and replaced
// by the imported stylesheet's own (recursively inlined) content, up to
// maxImportDepth levels deep. srcset candidates are inlined one by one.
//
// A reference that can't be resolved (missing file, one that escapes the
// directory tree rooted at path's own directory, or one that would exceed
// the per-asset or per-bundle size cap - see EnvMaxAssetBytes and
// EnvMaxBundleBytes) is left untouched rather than failing the whole
// publish, and reported back as a warning. A file: reference is the
// exception: it is replaced by about:blank, so the author's local
// filesystem path is never published, and a warning says so.
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

	in := &inliner{
		root:     root,
		maxAsset: bytesFromEnv(EnvMaxAssetBytes, DefaultMaxAssetBytes),
		maxTotal: bytesFromEnv(EnvMaxBundleBytes, DefaultMaxBundleBytes),
	}
	in.walk(doc)

	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		return "", nil, fmt.Errorf("rendering inlined HTML: %w", err)
	}
	return buf.String(), in.warnings, nil
}

// Size caps on what gets inlined. Both can
// be overridden (in bytes) through the environment.
const (
	DefaultMaxAssetBytes  int64 = 10 * 1024 * 1024
	DefaultMaxBundleBytes int64 = 25 * 1024 * 1024

	EnvMaxAssetBytes  = "VEXILLUM_BANNER_MAX_ASSET_BYTES"
	EnvMaxBundleBytes = "VEXILLUM_BANNER_MAX_BUNDLE_BYTES"
)

// maxImportDepth bounds nested CSS @import chains.
const maxImportDepth = 8

// redactedFileRef replaces a file: reference so a local path never reaches
// a public page.
const redactedFileRef = "about:blank"

// bytesFromEnv returns the positive integer in env var name, or def when
// it's unset or not a positive integer.
func bytesFromEnv(name string, def int64) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(name)), 10, 64)
	if err != nil || v <= 0 {
		return def
	}
	return v
}

type inliner struct {
	root     string
	warnings []string

	maxAsset int64    // per-asset cap, bytes
	maxTotal int64    // per-bundle cap, bytes
	inlined  int64    // raw bytes inlined so far
	stack    []string // css files currently being imported, for cycle detection
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
		if n.DataAtom == atom.Img || (n.DataAtom == atom.Source && n.Parent != nil && n.Parent.DataAtom == atom.Picture) {
			in.inlineSrcset(n)
		}
	case atom.Link:
		if linkIsInlinable(n) {
			in.inlineStylesheetOrIconLink(n)
		}
	case atom.Style:
		if n.FirstChild != nil && n.FirstChild.Type == html.TextNode {
			n.FirstChild.Data = in.inlineCSS(n.FirstChild.Data, in.root, 0)
		}
	}
	if hasAttr(n, "poster") {
		in.inlineAttrAsData(n, "poster", in.root)
	}
	if style, ok := getAttr(n, "style"); ok && style != "" {
		setAttr(n, "style", in.inlineCSS(style, in.root, 0))
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
	if !ok {
		return
	}
	if isFileRef(href) {
		in.redact(n, "href", href)
		return
	}
	if isRemoteRef(href) {
		return
	}

	if isCSSPath(href) {
		data, full, warning, ok := in.loadLocal(in.root, href)
		if !ok {
			in.warnf("%s", warning)
			return
		}
		css := in.withImportGuard(full, func() string {
			return in.inlineCSS(string(data), filepath.Dir(full), 1)
		})
		encoded := base64.StdEncoding.EncodeToString([]byte(css))
		setAttr(n, "href", "data:text/css;base64,"+encoded)
		return
	}

	in.inlineAttrAsData(n, "href", in.root)
}

// isCSSPath reports whether ref, ignoring any query or fragment, names a
// .css file.
func isCSSPath(ref string) bool {
	u, err := url.Parse(ref)
	return err == nil && strings.EqualFold(filepath.Ext(u.Path), ".css")
}

func (in *inliner) inlineAttrAsData(n *html.Node, key, dir string) {
	ref, ok := getAttr(n, key)
	if !ok {
		return
	}
	if isFileRef(ref) {
		in.redact(n, key, ref)
		return
	}
	if isRemoteRef(ref) {
		return
	}
	dataURI, warning, ok := in.readAsDataURI(dir, ref)
	if !ok {
		in.warnf("%s", warning)
		return
	}
	setAttr(n, key, dataURI)
}

// redact replaces a file: reference with about:blank and says so.
func (in *inliner) redact(n *html.Node, key, ref string) {
	in.warnRedacted(ref)
	setAttr(n, key, redactedFileRef)
}

func (in *inliner) warnRedacted(ref string) {
	in.warnf("redacted %q: a file: reference would leak a local filesystem path to a public page, replaced with %s", ref, redactedFileRef)
}

// inlineSrcset rewrites each candidate URL in n's srcset to its own data:
// URI (or about:blank for a file: ref), leaving descriptors (1x, 480w)
// and remote candidates untouched.
func (in *inliner) inlineSrcset(n *html.Node) {
	value, ok := getAttr(n, "srcset")
	if !ok || value == "" {
		return
	}
	var out strings.Builder
	last := 0
	for _, c := range parseSrcsetCandidates(value) {
		out.WriteString(value[last:c.start])
		ref := value[c.start:c.end]
		switch {
		case isFileRef(ref):
			in.warnRedacted(ref)
			out.WriteString(redactedFileRef)
		case isRemoteRef(ref):
			out.WriteString(ref)
		default:
			dataURI, warning, ok := in.readAsDataURI(in.root, ref)
			if !ok {
				in.warnf("%s", warning)
				out.WriteString(ref)
			} else {
				out.WriteString(dataURI)
			}
		}
		last = c.end
	}
	out.WriteString(value[last:])
	setAttr(n, "srcset", out.String())
}

type srcsetCandidate struct{ start, end int }

// parseSrcsetCandidates returns the byte span of each candidate URL in a
// srcset value. A data: URL's own payload comma doesn't end the candidate
func parseSrcsetCandidates(value string) []srcsetCandidate {
	var out []srcsetCandidate
	i := 0
	for i < len(value) {
		for i < len(value) && (isHTMLSpace(value[i]) || value[i] == ',') {
			i++
		}
		if i >= len(value) {
			break
		}
		start := i
		dataURL := len(value) >= i+5 && strings.EqualFold(value[i:i+5], "data:")
		sawPayloadComma := false
		for i < len(value) {
			ch := value[i]
			if isHTMLSpace(ch) {
				break
			}
			if ch == ',' {
				if !dataURL {
					break
				}
				if !sawPayloadComma {
					sawPayloadComma = true
				} else if isSrcsetSeparator(value, i) {
					break
				}
			}
			i++
		}
		end := i
		for end > start && value[end-1] == ',' {
			end--
		}
		if end > start {
			out = append(out, srcsetCandidate{start, end})
		}
		for i < len(value) && value[i] != ',' {
			i++
		}
		if i < len(value) {
			i++
		}
	}
	return out
}

func isSrcsetSeparator(value string, comma int) bool {
	cur := comma + 1
	for cur < len(value) && isHTMLSpace(value[cur]) {
		cur++
	}
	return cur >= len(value) || cur > comma+1
}

func isHTMLSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\f' || b == '\r'
}

// cssURLPattern matches CSS url(...) references, double-quoted,
// single-quoted, or bare. RE2 (unlike PCRE) has no backreferences, so
// quoted and unquoted forms are three separate alternatives rather than
// one pattern with a matched closing quote.
var cssURLPattern = regexp.MustCompile(`url\(\s*(?:"([^"]*)"|'([^']*)'|([^'")\s]*))\s*\)`)

// cssImportPattern matches an @import rule: its target (url(...) in three
// quoting forms, or a bare string) and whatever conditions (media query,
// supports(), layer) follow before the semicolon.
var cssImportPattern = regexp.MustCompile(`@import\s+(?:url\(\s*(?:"([^"]*)"|'([^']*)'|([^'")\s]*))\s*\)|"([^"]*)"|'([^']*)')\s*([^;]*);`)

// withImportGuard runs fn with full pushed on the import stack, so a
// stylesheet that (indirectly) imports itself is caught.
func (in *inliner) withImportGuard(full string, fn func() string) string {
	in.stack = append(in.stack, full)
	defer func() { in.stack = in.stack[:len(in.stack)-1] }()
	return fn()
}

func (in *inliner) importing(full string) bool {
	for _, f := range in.stack {
		if f == full {
			return true
		}
	}
	return false
}

// inlineCSS inlines @import rules (replacing each with the imported
// stylesheet's own inlined content) and then url(...) references in css,
// resolving relative refs against dir. depth counts nested @import levels.
func (in *inliner) inlineCSS(css, dir string, depth int) string {
	// Imports that stay as-is (remote, failed, unsupported conditions) are
	// parked behind a placeholder so the url() pass below can't turn their
	// url(...) into a data: URI.
	var parked []string
	park := func(text string) string {
		parked = append(parked, text)
		return "\x00import" + strconv.Itoa(len(parked)-1) + "\x00"
	}
	css = cssImportPattern.ReplaceAllStringFunc(css, func(match string) string {
		sub := cssImportPattern.FindStringSubmatch(match)
		ref := strings.TrimSpace(firstNonEmpty(sub[1], sub[2], sub[3], sub[4], sub[5]))
		cond := strings.TrimSpace(sub[6])
		if isFileRef(ref) {
			in.warnRedacted(ref)
			return park("@import url(" + redactedFileRef + ")" + condSuffix(cond) + ";")
		}
		if isRemoteRef(ref) {
			return park(match)
		}
		if depth >= maxImportDepth {
			in.warnf("skipped @import %q: nested more than %d levels deep", ref, maxImportDepth)
			return park(match)
		}
		if !importConditionSupported(cond) {
			in.warnf("skipped @import %q: its %q condition can't be inlined", ref, cond)
			return park(match)
		}
		data, full, warning, ok := in.loadLocal(dir, ref)
		if !ok {
			in.warnf("%s", warning)
			return park(match)
		}
		if in.importing(full) {
			in.warnf("skipped @import %q: it imports a stylesheet that is already being imported (cycle)", ref)
			return park(match)
		}
		inner := in.withImportGuard(full, func() string {
			return in.inlineCSS(string(data), filepath.Dir(full), depth+1)
		})
		if cond != "" {
			return "@media " + cond + "{" + inner + "}"
		}
		return inner
	})

	css = cssURLPattern.ReplaceAllStringFunc(css, func(match string) string {
		sub := cssURLPattern.FindStringSubmatch(match)
		ref := strings.TrimSpace(firstNonEmpty(sub[1], sub[2], sub[3]))
		if isFileRef(ref) {
			in.warnRedacted(ref)
			return "url(" + redactedFileRef + ")"
		}
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

	for i, text := range parked {
		css = strings.Replace(css, "\x00import"+strconv.Itoa(i)+"\x00", text, 1)
	}
	return css
}

func condSuffix(cond string) string {
	if cond == "" {
		return ""
	}
	return " " + cond
}

// importConditionSupported reports whether an @import's trailing
// conditions are empty or a plain media query, the only form that can be
// faithfully rewritten as @media. supports() and layer have no such
// equivalent.
func importConditionSupported(cond string) bool {
	lower := strings.ToLower(cond)
	return !strings.Contains(lower, "supports(") && !strings.HasPrefix(lower, "layer")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// loadLocal resolves ref against dir, enforces the directory-tree
// confinement and the per-asset and per-bundle size caps, then reads the
// file. On success the file's size counts against the bundle cap. On
// failure warning explains why the reference is being left as it was.
func (in *inliner) loadLocal(dir, ref string) (data []byte, full, warning string, ok bool) {
	full, ok = in.resolveLocalPath(dir, ref)
	if !ok {
		return nil, "", fmt.Sprintf("skipped %q: escapes the artifact's local directory tree", ref), false
	}
	info, err := os.Stat(full)
	if err != nil {
		return nil, "", fmt.Sprintf("skipped %q: %v", ref, err), false
	}
	if info.Size() > in.maxAsset {
		return nil, "", fmt.Sprintf("skipped %q: %d bytes exceeds the per-asset cap of %d bytes (raise %s to inline it), left as a reference", ref, info.Size(), in.maxAsset, EnvMaxAssetBytes), false
	}
	if in.inlined+info.Size() > in.maxTotal {
		return nil, "", fmt.Sprintf("skipped %q: inlining it would exceed the per-bundle cap of %d bytes (raise %s to inline it), left as a reference", ref, in.maxTotal, EnvMaxBundleBytes), false
	}
	data, err = os.ReadFile(full)
	if err != nil {
		return nil, "", fmt.Sprintf("skipped %q: %v", ref, err), false
	}
	in.inlined += int64(len(data))
	return data, full, "", true
}

func (in *inliner) readAsDataURI(dir, ref string) (dataURI, warning string, ok bool) {
	data, full, warning, ok := in.loadLocal(dir, ref)
	if !ok {
		return "", warning, false
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

// isFileRef reports whether ref uses the file: scheme. Browsers ignore
// tabs and newlines inside a URL and leading whitespace, so those are
// stripped before checking.
func isFileRef(ref string) bool {
	ref = strings.TrimSpace(strings.NewReplacer("\t", "", "\n", "", "\r", "").Replace(ref))
	return len(ref) >= 5 && strings.EqualFold(ref[:5], "file:")
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
