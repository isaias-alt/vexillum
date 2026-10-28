package project

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkRoot runs at the start of every project-scoped command: Abs,
// EvalSymlinks and a sha256 of the path.
func BenchmarkRoot(b *testing.B) {
	home := b.TempDir()
	dir := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Root(home, dir); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAllRoots is the project listing the sentinel does on every tick.
func BenchmarkAllRoots(b *testing.B) {
	for _, n := range []int{1, 10, 100} {
		b.Run(fmt.Sprintf("projects=%d", n), func(b *testing.B) {
			home := b.TempDir()
			for i := 0; i < n; i++ {
				if err := os.MkdirAll(filepath.Join(home, projectsDirName, fmt.Sprintf("repo-%08d", i)), 0o755); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				roots, err := AllRoots(home)
				if err != nil || len(roots) != n {
					b.Fatalf("AllRoots: %d roots, err %v", len(roots), err)
				}
			}
		})
	}
}
