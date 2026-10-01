package banner

import "testing"

func TestAnalyzeSelfPaint(t *testing.T) {
	cases := []struct {
		name    string
		html    string
		painted bool
		signal  string
	}{
		{"bare page", `<html><body><p>hi</p></body></html>`, false, ""},
		{"body background rule", `<style>body { background: #111; }</style><body>x</body>`, true, "root-background-rule"},
		{"root var rule", `<style>:root{--a:1} html,body{background-color:#fff}</style>`, true, "root-background-rule"},
		{"non-root background", `<style>.card{background:red}</style><body>x</body>`, false, ""},
		{"commented background", `<style>/* body{background:red} */ p{color:red}</style>`, false, ""},
		{"bg class on body", `<body class="p-4 bg-slate-900">x</body>`, true, "background-class"},
		{"data-theme", `<html data-theme="dark"><body>x</body></html>`, true, "data-theme"},
		{"inline body style", `<body style="background:#000">x</body>`, true, "inline-background"},
		{"stylesheet link", `<link rel="stylesheet" href="a.css"><body>x</body>`, true, "stylesheet-link"},
		{"tailwind runtime", `<script src="https://cdn.tailwindcss.com"></script>`, true, "tailwind-runtime"},
		{"color-scheme meta", `<meta name="color-scheme" content="dark light">`, true, "color-scheme"},
		{"css import", `<style>@import url(x.css);</style>`, true, "css-import"},
		{"class containing bg- inside word is not a bg class", `<body class="nobg-x">x</body>`, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			painted, signal := AnalyzeSelfPaint(tc.html)
			if painted != tc.painted || signal != tc.signal {
				t.Errorf("got (%v, %q), want (%v, %q)", painted, signal, tc.painted, tc.signal)
			}
		})
	}
}
