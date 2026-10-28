package atomicfile

import (
	"path/filepath"
	"strings"
	"testing"
)

// benchDoc is about the size of a persisted task record.
type benchDoc struct {
	ID     string   `json:"id"`
	Prompt string   `json:"prompt"`
	Status string   `json:"status"`
	Notes  []string `json:"notes"`
}

func newBenchDoc() benchDoc {
	return benchDoc{
		ID:     "0123456789abcdef",
		Prompt: strings.Repeat("investigate the slow path and report back. ", 20),
		Status: "running",
		Notes:  []string{"one", "two", "three"},
	}
}

// BenchmarkWriteJSON is the write every state record goes through: marshal,
// temp file, rename, with no fsync.
func BenchmarkWriteJSON(b *testing.B) {
	path := filepath.Join(b.TempDir(), "doc.json")
	doc := newBenchDoc()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := WriteJSON(path, doc); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWrite isolates the temp file and rename from the JSON encoding.
func BenchmarkWrite(b *testing.B) {
	path := filepath.Join(b.TempDir(), "blob")
	data := []byte(strings.Repeat("x", 4096))
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := Write(path, data); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkWriteMode is Write plus the chmod and fsync the executable
// replacement path adds, so the price of the fsync shows next to Write.
func BenchmarkWriteMode(b *testing.B) {
	path := filepath.Join(b.TempDir(), "blob")
	data := []byte(strings.Repeat("x", 4096))
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := WriteMode(path, data, 0o755); err != nil {
			b.Fatal(err)
		}
	}
}
