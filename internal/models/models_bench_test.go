package models

import (
	"os"
	"path/filepath"
	"testing"
)

const benchGlobalJSON = `{
  "profiles": {
    "scout-facts": {"model": "sonnet", "effort": "low"},
    "pi-big": {"harness": "pi", "model": "anthropic/claude-sonnet-5", "effort": "high", "when": "A big change that should run on Pi."}
  }
}`

const benchProjectJSON = `{
  "profiles": {
    "mission-big": {"model": "opus", "effort": "high"},
    "oc-small": {"harness": "opencode", "model": "anthropic/claude-haiku-5#fast", "effort": "low", "when": "A small edit on OpenCode."}
  },
  "default": {"model": "sonnet", "effort": "medium"}
}`

// BenchmarkLoadDefaults is the models table with no file on disk: parse and
// validate the embedded defaults.
func BenchmarkLoadDefaults(b *testing.B) {
	home := b.TempDir()
	project := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Load(project, home); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLoadLayered adds a global and a project models.json on top of the
// defaults, the usual shape of a configured install.
func BenchmarkLoadLayered(b *testing.B) {
	home := b.TempDir()
	project := b.TempDir()
	if err := os.WriteFile(filepath.Join(home, FileName), []byte(benchGlobalJSON), 0o644); err != nil {
		b.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(project, ".vexillum"), 0o755); err != nil {
		b.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".vexillum", FileName), []byte(benchProjectJSON), 0o644); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Load(project, home); err != nil {
			b.Fatal(err)
		}
	}
}
