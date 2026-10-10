package forum_test

import (
	"encoding/json"
	"testing"

	"github.com/isaias-alt/vexillum/internal/forum"
)

func TestExtractMermaidSources_Empty(t *testing.T) {
	sources := forum.ExtractMermaidSources(`<html><body><p>No diagrams here.</p></body></html>`)
	if len(sources) != 0 {
		t.Fatalf("expected no sources, got %d", len(sources))
	}
}

func TestExtractMermaidSources_SingleDiagram(t *testing.T) {
	html := `<html><body><div class="mermaid">flowchart TD
  A --> B</div></body></html>`
	sources := forum.ExtractMermaidSources(html)
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if sources[0].Index != 0 {
		t.Errorf("Index = %d, want 0", sources[0].Index)
	}
	want := "flowchart TD\n  A --> B"
	if sources[0].Source != want {
		t.Errorf("Source = %q, want %q", sources[0].Source, want)
	}
	if sources[0].Hash == "" {
		t.Error("expected a non-empty hash")
	}
}

func TestExtractMermaidSources_MultipleDiagramsInDocumentOrder(t *testing.T) {
	html := `<html><body>
<div class="mermaid">graph TD; A-->B</div>
<p>some prose</p>
<div class="mermaid">sequenceDiagram
  Alice->>Bob: Hello</div>
</body></html>`
	sources := forum.ExtractMermaidSources(html)
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}
	if sources[0].Index != 0 || sources[1].Index != 1 {
		t.Errorf("indices = %d, %d, want 0, 1", sources[0].Index, sources[1].Index)
	}
	if sources[0].Source != "graph TD; A-->B" {
		t.Errorf("sources[0].Source = %q", sources[0].Source)
	}
}

func TestExtractMermaidSources_ClassAttributeWithMultipleClasses(t *testing.T) {
	html := `<div class="artifact-diagram mermaid rounded">graph TD; A-->B</div>`
	sources := forum.ExtractMermaidSources(html)
	if len(sources) != 1 {
		t.Fatalf("expected 1 source when .mermaid is combined with other classes, got %d", len(sources))
	}
}

func TestExtractMermaidSources_IgnoresNonMermaidDivs(t *testing.T) {
	html := `<div class="not-mermaid-at-all">graph TD; A-->B</div>`
	sources := forum.ExtractMermaidSources(html)
	if len(sources) != 0 {
		t.Fatalf("expected 0 sources for a div whose class only contains 'mermaid' as a substring of another word, got %d", len(sources))
	}
}

func TestExtractMermaidSources_UnescapesHTMLEntities(t *testing.T) {
	html := `<div class="mermaid">graph TD; A--&gt;B</div>`
	sources := forum.ExtractMermaidSources(html)
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if sources[0].Source != "graph TD; A-->B" {
		t.Errorf("Source = %q, want unescaped %q", sources[0].Source, "graph TD; A-->B")
	}
}

func TestExtractMermaidSources_SameSourceSameHash(t *testing.T) {
	html := `<div class="mermaid">graph TD; A-->B</div><div class="mermaid">graph TD; A-->B</div>`
	sources := forum.ExtractMermaidSources(html)
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}
	if sources[0].Hash != sources[1].Hash {
		t.Errorf("identical sources produced different hashes: %q != %q", sources[0].Hash, sources[1].Hash)
	}
}

func TestExtractMermaidSources_DifferentSourceDifferentHash(t *testing.T) {
	html := `<div class="mermaid">graph TD; A-->B</div><div class="mermaid">graph TD; A-->C</div>`
	sources := forum.ExtractMermaidSources(html)
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}
	if sources[0].Hash == sources[1].Hash {
		t.Errorf("different sources produced the same hash: %q", sources[0].Hash)
	}
}

// The listing is the whiteboard's wire format: ordinal, text, digest.
func TestExtractMermaidSources_JSONShape(t *testing.T) {
	sources := forum.ExtractMermaidSources(`<div class="mermaid">graph TD; A--&gt;B</div>`)
	raw, err := json.Marshal(sources)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil || len(decoded) != 1 {
		t.Fatalf("decode %s: %v", raw, err)
	}
	if len(decoded[0]) != 3 || decoded[0]["ordinal"] != float64(0) || decoded[0]["text"] != "graph TD; A-->B" || decoded[0]["digest"] != forum.HashMermaidSource("graph TD; A-->B") {
		t.Errorf("listing entry = %v, want exactly ordinal, text and digest", decoded[0])
	}
}

// The digest is stored in records on disk, so its value must not drift.
func TestHashMermaidSource_IsStable(t *testing.T) {
	if got := forum.HashMermaidSource("graph TD; A-->B"); got != "c18237e0a535bdb7" {
		t.Errorf("digest = %q", got)
	}
	first := forum.HashMermaidSource("x")
	second := forum.HashMermaidSource("x")
	if first != second || len(first) != 16 {
		t.Error("digest must be deterministic and 16 hex characters")
	}
}
