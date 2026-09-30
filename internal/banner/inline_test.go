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
