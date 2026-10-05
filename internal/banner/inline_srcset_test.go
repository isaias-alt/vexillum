package banner

import (
	"math/rand"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// spanTexts returns the text of every candidate URL in value.
func spanTexts(value string) []string {
	var out []string
	for _, s := range srcsetURLSpans(value) {
		out = append(out, value[s.start:s.end])
	}
	return out
}

func TestSrcsetURLSpans(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  []string
	}{
		{"two candidates with descriptors", "a.png 1x, b.png 2x", []string{"a.png", "b.png"}},
		{"no space after the comma", "a.png 1x,b.png 2x", []string{"a.png", "b.png"}},
		{"no descriptors, comma and space", "a.png, b.png", []string{"a.png", "b.png"}},
		{"comma inside the url token", "a.png,b.png", []string{"a.png,b.png"}},
		{"surrounding whitespace", "  a.png  ", []string{"a.png"}},
		{"width descriptors and a bare last one", "a.png 480w, b.png 800w, c.png", []string{"a.png", "b.png", "c.png"}},
		{"data url with commas in the payload", "data:image/gif;base64,R0lGODlhAQABAAAAACw= 1x, b.png 2x", []string{"data:image/gif;base64,R0lGODlhAQABAAAAACw=", "b.png"}},
		{"data url with an escaped svg", "data:image/svg+xml;utf8,%3Csvg%20a=%271,2%27/%3E 1x, b.png 2x", []string{"data:image/svg+xml;utf8,%3Csvg%20a=%271,2%27/%3E", "b.png"}},
		{"comma inside a parenthesized descriptor", "a.png (1,2) 2x, b.png", []string{"a.png", "b.png"}},
		{"parentheses do not nest, the first close ends the group", "a.png (1,(2,3),4) 2x, b.png", []string{"a.png", "4)", "b.png"}},
		{"unclosed parenthesis swallows the rest", "a.png (1, b.png", []string{"a.png"}},
		{"stray commas around one url", ",,a.png,,", []string{"a.png"}},
		{"stray commas between candidates", "a.png 1x,,, b.png 2x", []string{"a.png", "b.png"}},
		{"empty value", "", nil},
		{"only separators", " , ", nil},
		{"tabs as separators", "a.png\t1x,\tb.png\t2x", []string{"a.png", "b.png"}},
		{"newlines as separators", "a.png\n1x,\nb.png\n2x", []string{"a.png", "b.png"}},
		{"form feeds and carriage returns", "\fa.png\f1x,\r\nb.png", []string{"a.png", "b.png"}},
		{"url closed by a comma then more text", "a.png, 2x", []string{"a.png", "2x"}},
		{"whitespace in a data url splits it", "data:image/svg+xml;utf8,<svg a='1,2'/> 1x, b.png 2x", []string{"data:image/svg+xml;utf8,<svg", "2'/>", "b.png"}},
		{"vertical tab is not whitespace", "a.png\v1x", []string{"a.png\v1x"}},
		{"non-ascii url", "imağen.png 2x, é.png", []string{"imağen.png", "é.png"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := spanTexts(tc.value); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("srcsetURLSpans(%q) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
}

func TestSrcsetURLSpansRandomInput(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	alphabet := []byte("ab.,() \t\n\f\r1x0w\x00\xff:/")
	check := func(value string) {
		t.Helper()
		prevEnd := 0
		for _, s := range srcsetURLSpans(value) {
			if s.start < prevEnd || s.start >= s.end || s.end > len(value) {
				t.Fatalf("bad span %+v (previous end %d) for %q", s, prevEnd, value)
			}
			if strings.ContainsAny(value[s.start:s.end], " \t\n\f\r") || value[s.end-1] == ',' {
				t.Fatalf("span %q holds whitespace or ends in a comma, in %q", value[s.start:s.end], value)
			}
			prevEnd = s.end
		}
	}
	for i := 0; i < 5000; i++ {
		b := make([]byte, r.Intn(40))
		for j := range b {
			if r.Intn(8) == 0 {
				b[j] = byte(r.Intn(256))
			} else {
				b[j] = alphabet[r.Intn(len(alphabet))]
			}
		}
		check(string(b))
	}
}

func TestInlineLocalAssetsSrcsetLeavesOtherCandidatesAlone(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "local.png"), "local-bytes")
	htmlPath := filepath.Join(dir, "index.html")
	dataURL := "data:image/gif;base64,R0lGODlhAQABAAAAACw="
	remote := "https://cdn.example.com/r.png"
	writeFile(t, htmlPath, `<img srcset="local.png 1x, `+remote+` 2x, `+dataURL+` 3x">`)

	out, warnings, err := InlineLocalAssets(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if strings.Contains(out, "local.png") {
		t.Errorf("the local candidate should be inlined, got:\n%s", out)
	}
	if !strings.Contains(decodeDataURIs(t, out), "local-bytes") {
		t.Errorf("the inlined payload is missing, got:\n%s", out)
	}
	for _, keep := range []string{remote + " 2x", dataURL + " 3x"} {
		if !strings.Contains(out, keep) {
			t.Errorf("candidate %q must stay byte-identical, got:\n%s", keep, out)
		}
	}
}
