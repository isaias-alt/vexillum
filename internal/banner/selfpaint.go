package banner

import "regexp"

// SelfPaintWarning is shown when AnalyzeSelfPaint finds no sign that a page
// paints its own background.
const SelfPaintWarning = "this page never paints its own surface: no background on html/body/:root, no bg-* class or data-theme on html/body, and no stylesheet that could set one. The hosted page renders over ht-ml.app's own surface, so text that assumes a dark or light host can be invisible. Set an explicit background and readable text, then republish"

var (
	rootTagRE       = regexp.MustCompile(`(?i)<(?:html|body)\b([^>]*)>`)
	styleBlockRE    = regexp.MustCompile(`(?i)<style\b[^>]*>([\s\S]*?)</style>`)
	cssRuleRE       = regexp.MustCompile(`([^{}]+)\{([^{}]*)\}`)
	cssCommentRE    = regexp.MustCompile(`/\*[\s\S]*?\*/`)
	stylesheetLinkR = regexp.MustCompile(`(?i)<link\b[^>]*\brel\s*=\s*["']?[^"'>]*stylesheet`)
	tailwindScriptR = regexp.MustCompile(`(?i)<script\b[^>]*\bsrc\s*=\s*["']?[^"'>]*tailwind`)
	colorSchemeMeta = regexp.MustCompile(`(?i)<meta\b[^>]*\bname\s*=\s*["']?color-scheme`)
	dataThemeRE     = regexp.MustCompile(`(?i)\bdata-theme\s*=`)
	bgClassRE       = regexp.MustCompile(`(?i)(?:^|[^\w-])bg-`)
	backgroundRE    = regexp.MustCompile(`(?i)background`)
	colorSchemeRE   = regexp.MustCompile(`(?i)color-scheme\s*:`)
	cssImportRE     = regexp.MustCompile(`(?i)@import\b`)
	// RE2 has no lookahead, so "not followed by a word char or dash" is
	// expressed as end-of-string or one non-word, non-dash character.
	rootSelectorRE = regexp.MustCompile(`(?i)(?:^|[\s,>~+])(?:html|body|:root|\*)(?:$|[^\w-])`)
)

// AnalyzeSelfPaint is a render-free, deliberately fail-open check (ported
// from forum-tool's self-paint.js) for an artifact that never paints its own
// page background. Any stylesheet link, @import, Tailwind runtime script,
// color-scheme, or root paint signal counts as painted, because a wrong
// warning is noise on every publish. signal names what suppressed the
// warning, or is empty when painted is false.
func AnalyzeSelfPaint(html string) (painted bool, signal string) {
	switch {
	case stylesheetLinkR.MatchString(html):
		return true, "stylesheet-link"
	case tailwindScriptR.MatchString(html):
		return true, "tailwind-runtime"
	case colorSchemeMeta.MatchString(html):
		return true, "color-scheme"
	}

	for _, m := range rootTagRE.FindAllStringSubmatch(html, -1) {
		attrs := m[1]
		if dataThemeRE.MatchString(attrs) {
			return true, "data-theme"
		}
		if class := attrValue(attrs, "class"); class != "" && bgClassRE.MatchString(class) {
			return true, "background-class"
		}
		if style := attrValue(attrs, "style"); style != "" {
			if backgroundRE.MatchString(style) {
				return true, "inline-background"
			}
			if colorSchemeRE.MatchString(style) {
				return true, "color-scheme"
			}
		}
	}

	for _, m := range styleBlockRE.FindAllStringSubmatch(html, -1) {
		css := cssCommentRE.ReplaceAllString(m[1], "")
		if cssImportRE.MatchString(css) {
			return true, "css-import"
		}
		if colorSchemeRE.MatchString(css) {
			return true, "color-scheme"
		}
		for _, r := range cssRuleRE.FindAllStringSubmatch(css, -1) {
			if backgroundRE.MatchString(r[2]) && rootSelectorRE.MatchString(r[1]) {
				return true, "root-background-rule"
			}
		}
	}
	return false, ""
}

func attrValue(attrs, name string) string {
	re := regexp.MustCompile(`(?i)\b` + name + `\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
	m := re.FindStringSubmatch(attrs)
	if m == nil {
		return ""
	}
	return firstNonEmpty(m[1], m[2], m[3])
}
