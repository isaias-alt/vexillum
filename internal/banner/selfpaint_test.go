package banner

import (
	"math/rand"
	"strings"
	"testing"
)

func TestDefinesOwnSurface(t *testing.T) {
	cases := []struct {
		name string
		page string
		want bool
	}{
		// Pages that give no sign of a surface.
		{"bare paragraph", `<p>hello</p>`, false},
		{"full document, no styling", `<!doctype html><html><head><title>t</title></head><body><h1>hi</h1></body></html>`, false},
		{"empty input", ``, false},
		{"plain text", `just some words, not markup`, false},
		{"only a nested class is painted", `<style>.card{background:#fff} p{color:red}</style><body><div class="card">x</div></body>`, false},
		{"class contains the prefix inside a word", `<body class="nobg-x wrapper">x</body>`, false},
		{"bare prefix without a name", `<body class="bg- x">x</body>`, false},
		{"background only inside a css comment", `<style>/* body{background:red} */ p{margin:0}</style><body>x</body>`, false},
		{"css text inside a script string", `<body><script>var s = "body { background: red }"; var t = 'html{background:#000}';</script></body>`, false},
		{"css text inside an html comment", `<body><!-- <style>body{background:red}</style> --><p>x</p></body>`, false},
		{"selection color is not a page paint", `<style>::selection{background:yellow} body::before{background:red} *::after{background:blue}</style><body>x</body>`, false},
		{"descendant of the body only", `<style>body .card{background:#fff} body > p{background:#eee}</style><body>x</body>`, false},
		{"custom property named like a background", `<style>:root{--background:#000;--bg: white} p{color:var(--bg)}</style><body>x</body>`, false},
		{"an image and a canvas", `<body><img src="a.png"><canvas></canvas></body>`, false},
		{"script without a source", `<body><script>console.log(1)</script></body>`, false},
		{"unrelated link", `<head><link rel="icon" href="x.ico"><link rel="canonical" href="https://a.b/"></head><body>x</body>`, false},
		{"unrelated meta", `<head><meta charset="utf-8"><meta name="viewport" content="width=device-width"></head><body>x</body>`, false},
		{"unterminated comment hides the rest", `<style>p{color:red} /* body{background:red}</style><body>x</body>`, false},
		{"unbalanced braces", `<style>p{color:red; } } } body{ </style><body>x</body>`, false},

		// External or unreadable sources.
		{"stylesheet link", `<head><link rel="stylesheet" href="https://cdn.example.com/a.css"></head><body>x</body>`, true},
		{"stylesheet link among rel tokens", `<head><link rel="alternate STYLESHEET" href="a.css"></head><body>x</body>`, true},
		{"css import with url()", `<style>@import url("https://cdn.example.com/a.css"); p{color:red}</style><body>x</body>`, true},
		{"css import with a bare string", `<style>@import 'a.css';</style><body>x</body>`, true},
		{"runtime styling script", `<head><script src="https://cdn.tailwindcss.com"></script></head><body>x</body>`, true},

		// Declared color schemes.
		{"color-scheme meta", `<head><meta name="color-scheme" content="dark light"></head><body>x</body>`, true},
		{"color-scheme meta, loud", `<head><META NAME="Color-Scheme" CONTENT="dark"></head><body>x</body>`, true},
		{"color-scheme in a style block", `<style>:root{color-scheme:dark}</style><body>x</body>`, true},
		{"color-scheme inline on html", `<html style="color-scheme: light dark"><body>x</body></html>`, true},
		{"color-scheme inline on body", `<body style="color-scheme:dark">x</body>`, true},

		// Attributes on the roots.
		{"data-theme on html", `<html data-theme="dark"><body>x</body></html>`, true},
		{"data-theme on body", `<body data-theme=night>x</body>`, true},
		{"bootstrap theme attribute", `<html data-bs-theme="dark"><body>x</body></html>`, true},
		{"bgcolor on body", `<body bgcolor="#111">x</body>`, true},
		{"background attribute on body", `<body background="tile.png">x</body>`, true},
		{"inline background on body", `<body style="background:#111;color:#eee">x</body>`, true},
		{"inline background-color on html", `<html style="color:#000; background-color: white"><body>x</body></html>`, true},
		{"inline background-image on body", `<body style="background-image:url(data:image/png;base64,AAAA)">x</body>`, true},
		{"background utility class", `<body class="min-h-screen bg-slate-900 text-white">x</body>`, true},
		{"background utility behind a variant", `<body class="dark:bg-black">x</body>`, true},
		{"background utility with an arbitrary value", `<body class="bg-[url(a:b)]">x</body>`, true},

		// Style blocks.
		{"body background color", `<style>body{background-color:#111;color:#eee}</style><body>x</body>`, true},
		{"body background shorthand", `<style>body { background: #111 }</style><body>x</body>`, true},
		{"root rule with variables, then html and body", `<style>:root{--a:#111;--b:#eee} html, body{background:var(--a)}</style><body>x</body>`, true},
		{"root pseudo-class", `<style>:root{background:#fff}</style><body>x</body>`, true},
		{"universal selector", `<style>*{background:#111}</style><body>x</body>`, true},
		{"universal selector in a list", `<style>*, *::before{background-color:#111}</style><body>x</body>`, true},
		{"html body descendant combination", `<style>html body{background:#111}</style><body>x</body>`, true},
		{"html child body", `<style>html > body { background: #111 }</style><body>x</body>`, true},
		{"root before body", `<style>:root body{background:#111}</style><body>x</body>`, true},
		{"body with a class", `<style>body.dark{background:#111}</style><body>x</body>`, true},
		{"root with an attribute", `<style>:root[data-theme="dark"]{background:#111}</style><body>x</body>`, true},
		{"body inside is()", `<style>:is(html, body){background:#111}</style><body>x</body>`, true},
		{"uppercase selector and property", `<style>BODY{BACKGROUND:#111}</style><body>x</body>`, true},
		{"nested in a media query", `<style>@media (prefers-color-scheme: dark){body{background:#111}}</style><body>x</body>`, true},
		{"nested in a layer", `<style>@layer base{html{background:#111}}</style><body>x</body>`, true},
		{"nested in supports", `<style>@supports (display:grid){:root{background:#111}}</style><body>x</body>`, true},
		{"nested in a container query", `<style>@container (min-width:1px){body{background:#111}}</style><body>x</body>`, true},
		{"media inside layer inside supports", `<style>@supports (a:b){@layer x{@media screen{body{background:#111}}}}</style><body>x</body>`, true},
		{"nested media in the rule", `<style>body{@media print{background:#111}}</style><body>x</body>`, true},
		{"nested rule under the root", `<style>html{ body{background:#111} }</style><body>x</body>`, true},
		{"after an earlier painted rule that has a url with semicolon", `<style>.a{background:url(data:image/png;base64,AAA)} body{background:#111}</style><body>x</body>`, true},
		{"after a string holding braces", `<style>.a::after{content:"}{;"} body{background:#111}</style><body>x</body>`, true},
		{"after a comment", `<style>/* x */ body /* y */ { /* z */ background: #111 }</style><body>x</body>`, true},

		// Case and quoting.
		{"uppercase tags and attributes", `<HTML><BODY STYLE="BACKGROUND:#111">x</BODY></HTML>`, true},
		{"single-quoted attribute", `<body style='background:#111'>x</body>`, true},
		{"unquoted attribute", `<body bgcolor=#111>x</body>`, true},
		{"uppercase style tag", `<STYLE>body{background:#111}</STYLE><body>x</body>`, true},
		{"uppercase link", `<LINK REL=stylesheet HREF=a.css><body>x</body>`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := DefinesOwnSurface(tc.page)
			if got != tc.want {
				t.Fatalf("DefinesOwnSurface = %v (reason %q), want %v\npage: %s", got, reason, tc.want, tc.page)
			}
			if got && reason == "" {
				t.Errorf("a yes verdict must carry a reason")
			}
			if !got && reason != "" {
				t.Errorf("a no verdict must have an empty reason, got %q", reason)
			}
		})
	}
}

func TestDefinesOwnSurfaceOpaqueCSSDepth(t *testing.T) {
	deep := strings.Repeat("@media screen{", 200) + "p{color:red}" + strings.Repeat("}", 200)
	got, reason := DefinesOwnSurface("<style>" + deep + "</style><body>x</body>")
	if !got || reason == "" {
		t.Errorf("css nested beyond what the reader follows must count as opaque, got %v %q", got, reason)
	}
}

func TestNoSurfaceWarningShape(t *testing.T) {
	if strings.ContainsAny(NoSurfaceWarning, "\r\n") {
		t.Errorf("the warning must be a single line")
	}
	if len(NoSurfaceWarning) >= 450 {
		t.Errorf("the warning is %d chars, want under 450", len(NoSurfaceWarning))
	}
	if strings.Contains(NoSurfaceWarning, "\u2014") {
		t.Errorf("the warning must not contain an em dash")
	}
	for _, fact := range []string{"sets a background for the page itself", "light text on a light host", "dark text on a dark one", "publish again"} {
		if !strings.Contains(NoSurfaceWarning, fact) {
			t.Errorf("the warning should mention %q", fact)
		}
	}
}

// randomMarkup builds a string from fragments that stress the parsers more
// than uniform random bytes would.
func randomMarkup(r *rand.Rand) string {
	pieces := []string{
		"<", ">", "</", "/>", "<style>", "</style>", "<body ", "<html ", "style=", "class=", "bg-x", "\"", "'", "=",
		"{", "}", ";", ":", "(", ")", "[", "]", ",", "/*", "*/", "\\", "@media ", "@import ", "background", "color-scheme",
		"body", "html", ":root", "*", " ", "\n", "\x00", "\xff", "<!--", "-->", "<script>", "</script>", "url(", "data:", "::before",
	}
	var sb strings.Builder
	for n := r.Intn(60); n > 0; n-- {
		if r.Intn(5) == 0 {
			sb.WriteByte(byte(r.Intn(256)))
		} else {
			sb.WriteString(pieces[r.Intn(len(pieces))])
		}
	}
	return sb.String()
}

func TestDefinesOwnSurfaceNeverPanics(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for i := 0; i < 4000; i++ {
		page := randomMarkup(r)
		got, reason := DefinesOwnSurface(page)
		if got == (reason == "") {
			t.Fatalf("verdict %v and reason %q disagree for %q", got, reason, page)
		}
	}
	raw := make([]byte, 512)
	for i := 0; i < 500; i++ {
		r.Read(raw)
		DefinesOwnSurface(string(raw))
	}
}
