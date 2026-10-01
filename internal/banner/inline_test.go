package banner

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func TestInlineLocalAssetsImgAndScript(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "logo.png"), "fake-png-bytes")
	writeFile(t, filepath.Join(dir, "app.js"), "console.log('hi')")
	htmlPath := filepath.Join(dir, "index.html")
	writeFile(t, htmlPath, `<!doctype html><html><head>
<script src="app.js"></script>
</head><body>
<img src="logo.png">
<img src="https://cdn.example.com/remote.png">
</body></html>`)

	out, warnings, err := InlineLocalAssets(htmlPath)
	if err != nil {
		t.Fatalf("InlineLocalAssets: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if !strings.Contains(out, `src="data:image/png;base64,`) {
		t.Errorf("expected the local image to be inlined as a data URI, got:\n%s", out)
	}
	if !strings.Contains(out, `src="data:`) || !strings.Contains(out, `javascript`) || !strings.Contains(out, `;base64,`) {
		t.Errorf("expected the local script to be inlined as a data URI, got:\n%s", out)
	}
	if !strings.Contains(out, `src="https://cdn.example.com/remote.png"`) {
		t.Errorf("expected the remote image reference to be left untouched, got:\n%s", out)
	}
}

func TestInlineLocalAssetsStylesheetLink(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "style.css"), "body { color: red; }")
	htmlPath := filepath.Join(dir, "index.html")
	writeFile(t, htmlPath, `<!doctype html><html><head>
<link rel="stylesheet" href="style.css">
<link rel="preconnect" href="https://fonts.gstatic.com">
</head><body></body></html>`)

	out, warnings, err := InlineLocalAssets(htmlPath)
	if err != nil {
		t.Fatalf("InlineLocalAssets: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if !strings.Contains(out, `href="data:text/css;base64,`) {
		t.Errorf("expected the local stylesheet to be inlined as a data URI, got:\n%s", out)
	}
	if !strings.Contains(out, `href="https://fonts.gstatic.com"`) {
		t.Errorf("expected the non-stylesheet remote link to be left untouched, got:\n%s", out)
	}
}

func TestInlineLocalAssetsStylesheetOwnURLReferences(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "bg.png"), "fake-png-bytes")
	writeFile(t, filepath.Join(dir, "css", "style.css"), `body { background: url("../bg.png"); }`)
	htmlPath := filepath.Join(dir, "index.html")
	writeFile(t, htmlPath, `<!doctype html><html><head>
<link rel="stylesheet" href="css/style.css">
</head><body></body></html>`)

	out, warnings, err := InlineLocalAssets(htmlPath)
	if err != nil {
		t.Fatalf("InlineLocalAssets: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	// Decode the base64 CSS payload back out to check its own
	// url(...) reference was itself inlined - a plain substring check
	// on the outer HTML wouldn't tell base64'd CSS apart from anything
	// else.
	start := strings.Index(out, "data:text/css;base64,")
	if start == -1 {
		t.Fatalf("expected an inlined stylesheet, got:\n%s", out)
	}
	end := strings.IndexAny(out[start:], `"'`)
	if end == -1 {
		t.Fatalf("could not find the end of the data URI in:\n%s", out)
	}
	b64 := out[start+len("data:text/css;base64,") : start+end]
	css := mustBase64Decode(t, b64)
	if !strings.Contains(css, "data:image/png;base64,") {
		t.Errorf("expected the stylesheet's own background-image to be inlined, got css:\n%s", css)
	}
}

func TestInlineLocalAssetsInlineStyleTagAndAttribute(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "bg.png"), "fake-png-bytes")
	htmlPath := filepath.Join(dir, "index.html")
	writeFile(t, htmlPath, `<!doctype html><html><head>
<style>body { background: url(bg.png); }</style>
</head><body>
<div style="background-image: url('bg.png')"></div>
</body></html>`)

	out, warnings, err := InlineLocalAssets(htmlPath)
	if err != nil {
		t.Fatalf("InlineLocalAssets: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if strings.Count(out, "data:image/png;base64,") != 2 {
		t.Errorf("expected both the <style> block and the style attribute to inline bg.png, got:\n%s", out)
	}
}

func TestInlineLocalAssetsMissingFileIsAWarningNotAnError(t *testing.T) {
	dir := t.TempDir()
	htmlPath := filepath.Join(dir, "index.html")
	writeFile(t, htmlPath, `<img src="missing.png">`)

	out, warnings, err := InlineLocalAssets(htmlPath)
	if err != nil {
		t.Fatalf("InlineLocalAssets: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected exactly one warning, got %v", warnings)
	}
	if !strings.Contains(out, `src="missing.png"`) {
		t.Errorf("expected the unresolvable reference to be left untouched, got:\n%s", out)
	}
}

func TestInlineLocalAssetsEscapingDirectoryTreeIsAWarningNotAnError(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "site")
	writeFile(t, filepath.Join(dir, "secret.png"), "outside the tree")
	htmlPath := filepath.Join(sub, "index.html")
	writeFile(t, htmlPath, `<img src="../secret.png">`)

	out, warnings, err := InlineLocalAssets(htmlPath)
	if err != nil {
		t.Fatalf("InlineLocalAssets: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected exactly one warning about escaping the tree, got %v", warnings)
	}
	if !strings.Contains(out, `src="../secret.png"`) {
		t.Errorf("expected the out-of-tree reference to be left untouched, got:\n%s", out)
	}
}

func mustBase64Decode(t *testing.T, s string) string {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("decoding base64: %v", err)
	}
	return string(data)
}

func decodeDataURIs(t *testing.T, s string) string {
	t.Helper()
	var out strings.Builder
	rest := s
	for {
		i := strings.Index(rest, ";base64,")
		if i < 0 {
			break
		}
		rest = rest[i+len(";base64,"):]
		end := strings.IndexAny(rest, `")' `)
		if end < 0 {
			end = len(rest)
		}
		b, err := base64.StdEncoding.DecodeString(rest[:end])
		if err != nil {
			t.Fatalf("decoding data URI: %v", err)
		}
		out.Write(b)
		out.WriteString("\n")
		rest = rest[end:]
	}
	return out.String()
}

// B3
func TestInlineLocalAssetsPerAssetCap(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "big.png"), strings.Repeat("x", 100))
	writeFile(t, filepath.Join(dir, "small.png"), "ok")
	htmlPath := filepath.Join(dir, "index.html")
	writeFile(t, htmlPath, `<img src="big.png"><img src="small.png">`)
	t.Setenv(EnvMaxAssetBytes, "50")

	out, warnings, err := InlineLocalAssets(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `src="big.png"`) {
		t.Errorf("oversized asset should stay a reference, got:\n%s", out)
	}
	if !strings.Contains(out, `src="data:image/png;base64,`) {
		t.Errorf("small asset should still be inlined, got:\n%s", out)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "per-asset cap") || !strings.Contains(warnings[0], EnvMaxAssetBytes) {
		t.Errorf("expected one per-asset cap warning, got %v", warnings)
	}
}

func TestInlineLocalAssetsPerBundleCap(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.png"), strings.Repeat("a", 60))
	writeFile(t, filepath.Join(dir, "b.png"), strings.Repeat("b", 60))
	htmlPath := filepath.Join(dir, "index.html")
	writeFile(t, htmlPath, `<img src="a.png"><img src="b.png">`)
	t.Setenv(EnvMaxBundleBytes, "100")

	out, warnings, err := InlineLocalAssets(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `src="b.png"`) || strings.Contains(out, `src="a.png"`) {
		t.Errorf("first asset inlined, second left as reference, got:\n%s", out)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "per-bundle cap") {
		t.Errorf("expected one per-bundle cap warning, got %v", warnings)
	}
}

func TestBytesFromEnv(t *testing.T) {
	t.Setenv("VEXILLUM_BANNER_TEST", "")
	if got := bytesFromEnv("VEXILLUM_BANNER_TEST", 7); got != 7 {
		t.Errorf("unset: got %d", got)
	}
	t.Setenv("VEXILLUM_BANNER_TEST", "nope")
	if got := bytesFromEnv("VEXILLUM_BANNER_TEST", 7); got != 7 {
		t.Errorf("garbage: got %d", got)
	}
	t.Setenv("VEXILLUM_BANNER_TEST", "-3")
	if got := bytesFromEnv("VEXILLUM_BANNER_TEST", 7); got != 7 {
		t.Errorf("negative: got %d", got)
	}
	t.Setenv("VEXILLUM_BANNER_TEST", "1234")
	if got := bytesFromEnv("VEXILLUM_BANNER_TEST", 7); got != 1234 {
		t.Errorf("valid: got %d", got)
	}
}

// B4
func TestInlineLocalAssetsSrcset(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "foo-1x.png"), "one-x")
	writeFile(t, filepath.Join(dir, "foo-2x.png"), "two-x")
	writeFile(t, filepath.Join(dir, "wide.webp"), "wide")
	htmlPath := filepath.Join(dir, "index.html")
	writeFile(t, htmlPath, `<img srcset="foo-1x.png 1x, foo-2x.png 2x, https://cdn.example.com/r.png 3x">
<picture><source srcset="wide.webp 480w" type="image/webp"><img src="foo-1x.png"></picture>`)

	out, warnings, err := InlineLocalAssets(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if strings.Contains(out, "foo-1x.png ") || strings.Contains(out, "foo-2x.png") || strings.Contains(out, "wide.webp") {
		t.Errorf("srcset candidates should all be inlined, got:\n%s", out)
	}
	for _, want := range []string{"one-x", "two-x", "wide"} {
		if !strings.Contains(decodeDataURIs(t, out), want) {
			t.Errorf("missing inlined payload %q", want)
		}
	}
	if !strings.Contains(out, " 1x,") || !strings.Contains(out, " 2x,") || !strings.Contains(out, " 480w") {
		t.Errorf("descriptors must be preserved, got:\n%s", out)
	}
	if !strings.Contains(out, "https://cdn.example.com/r.png 3x") {
		t.Errorf("remote candidate must be untouched, got:\n%s", out)
	}
}

func TestParseSrcsetCandidatesKeepsDataURIPayloadComma(t *testing.T) {
	v := "data:image/png;base64,AAAA 1x, b.png 2x"
	got := parseSrcsetCandidates(v)
	if len(got) != 2 || v[got[0].start:got[0].end] != "data:image/png;base64,AAAA" || v[got[1].start:got[1].end] != "b.png" {
		t.Errorf("unexpected candidates: %+v", got)
	}
}

// B5
func TestInlineLocalAssetsRedactsFileRefs(t *testing.T) {
	dir := t.TempDir()
	htmlPath := filepath.Join(dir, "index.html")
	writeFile(t, htmlPath, `<style>body{background:url(file:///Users/me/bg.png)}</style>
<img src="file:///Users/me/secret.png">
<img srcset="FILE:///Users/me/a.png 1x">
<link rel="stylesheet" href=" file:///Users/me/s.css">`)

	out, warnings, err := InlineLocalAssets(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(out), "file:") || strings.Contains(out, "/Users/me") {
		t.Errorf("file: refs must not survive, got:\n%s", out)
	}
	if strings.Count(out, "about:blank") < 4 {
		t.Errorf("expected about:blank in every redacted spot, got:\n%s", out)
	}
	if len(warnings) != 4 {
		t.Errorf("expected one warning per redaction, got %v", warnings)
	}
}

// B6
func TestInlineLocalAssetsFollowsCSSImports(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "css", "main.css"), `@import "base.css"; @import url('print.css') print; p{color:red}`)
	writeFile(t, filepath.Join(dir, "css", "base.css"), `@import url(fonts/f.css); body{background:url(../bg.png)}`)
	writeFile(t, filepath.Join(dir, "css", "fonts", "f.css"), `h1{font-family:x}`)
	writeFile(t, filepath.Join(dir, "css", "print.css"), `div{display:none}`)
	writeFile(t, filepath.Join(dir, "bg.png"), "BGBYTES")
	htmlPath := filepath.Join(dir, "index.html")
	writeFile(t, htmlPath, `<link rel="stylesheet" href="css/main.css">`)

	out, warnings, err := InlineLocalAssets(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	css := decodeDataURIs(t, out)
	for _, want := range []string{"h1{font-family:x}", "body{background:url(data:", "@media print{div{display:none}}", "p{color:red}", base64.StdEncoding.EncodeToString([]byte("BGBYTES"))} {
		if !strings.Contains(css, want) {
			t.Errorf("inlined CSS missing %q, got:\n%s", want, css)
		}
	}
	if strings.Contains(css, "@import") {
		t.Errorf("no @import should remain, got:\n%s", css)
	}
}

func TestInlineLocalAssetsImportDepthAndCycleGuard(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "loop.css"), `@import "loop.css"; a{b:c}`)
	for i := 0; i < 12; i++ {
		next := ""
		if i < 11 {
			next = `@import "d` + string(rune('a'+i+1)) + `.css";`
		}
		writeFile(t, filepath.Join(dir, "d"+string(rune('a'+i))+".css"), next+`.x`+string(rune('a'+i))+`{y:z}`)
	}
	htmlPath := filepath.Join(dir, "index.html")
	writeFile(t, htmlPath, `<link rel="stylesheet" href="loop.css"><style>@import "da.css";</style>`)

	_, warnings, err := InlineLocalAssets(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	var cycle, deep bool
	for _, w := range warnings {
		cycle = cycle || strings.Contains(w, "cycle")
		deep = deep || strings.Contains(w, "levels deep")
	}
	if !cycle || !deep {
		t.Errorf("expected cycle and depth warnings, got %v", warnings)
	}
}

func TestInlineLocalAssetsLeavesRemoteImportAlone(t *testing.T) {
	dir := t.TempDir()
	htmlPath := filepath.Join(dir, "index.html")
	writeFile(t, htmlPath, `<style>@import url(https://fonts.googleapis.com/css?family=X);</style>`)
	out, warnings, err := InlineLocalAssets(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "@import url(https://fonts.googleapis.com/css?family=X);") || len(warnings) != 0 {
		t.Errorf("remote import must be untouched, got:\n%s\nwarnings: %v", out, warnings)
	}
}
