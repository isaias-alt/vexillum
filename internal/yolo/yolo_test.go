package yolo

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnabledDefaultsToOff(t *testing.T) {
	on, err := Enabled(t.TempDir())
	if err != nil || on {
		t.Fatalf("Enabled on a project with no file = %v, %v; want false, nil", on, err)
	}
}

func TestSetRoundTrip(t *testing.T) {
	dir := t.TempDir()
	for _, want := range []bool{true, false, true} {
		if err := Set(dir, want); err != nil {
			t.Fatalf("Set(%v): %v", want, err)
		}
		got, err := Enabled(dir)
		if err != nil || got != want {
			t.Fatalf("Enabled after Set(%v) = %v, %v", want, got, err)
		}
	}
	// No temp files are left behind by the atomic write.
	entries, err := os.ReadDir(filepath.Join(dir, ".vexillum"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != FileName {
		t.Errorf("unexpected files in .vexillum: %v", entries)
	}
}

func TestEnabledRejectsInvalidFiles(t *testing.T) {
	for name, content := range map[string]string{
		"not json":      `yes`,
		"unknown field": `{"enable": true}`,
		"wrong type":    `{"enabled": "true"}`,
		"trailing data": `{"enabled": true} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, ".vexillum"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(Path(dir), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			on, err := Enabled(dir)
			if err == nil || on {
				t.Fatalf("Enabled = %v, %v; want an error and false", on, err)
			}
			if !strings.Contains(err.Error(), FileName) {
				t.Errorf("error does not name the file: %v", err)
			}
		})
	}
}
