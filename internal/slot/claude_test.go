package slot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImportsAgents(t *testing.T) {
	tests := []struct {
		name, in string
		want     bool
	}{
		{"empty", "", false},
		{"plain line", "@AGENTS.md\n", true},
		{"dot slash", "@./AGENTS.md\n", true},
		{"no trailing newline", "# Hi\n@AGENTS.md", true},
		{"CRLF", "# Hi\r\n@AGENTS.md\r\n", true},
		{"inline in a sentence", "See @AGENTS.md for rules.\n", true},
		{"other file", "@docs/AGENTS.md\n", false},
		{"longer name", "@AGENTS.md.bak\n", false},
		{"email-like", "mail me@AGENTS.md\n", false},
		{"in code fence", "```\n@AGENTS.md\n```\n", false},
		{"in inline code", "use `@AGENTS.md` to import\n", false},
		{"after a closed fence", "```\nx\n```\n@AGENTS.md\n", true},
		{"unrelated content", "# Claude\nBe nice.\n", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ImportsAgents(tc.in); got != tc.want {
				t.Errorf("ImportsAgents(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestAppendImport(t *testing.T) {
	for in, want := range map[string]string{
		"":       "@AGENTS.md\n",
		"# Hi\n": "# Hi\n@AGENTS.md\n",
		"# Hi":   "# Hi\n@AGENTS.md\n",
		"a\r\n":  "a\r\n@AGENTS.md\r\n",
		"a\r\nb": "a\r\nb\r\n@AGENTS.md\r\n",
	} {
		if got := AppendImport(in); got != want {
			t.Errorf("AppendImport(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEnsureClaudeImport(t *testing.T) {
	write := func(t *testing.T, dir, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("missing", func(t *testing.T) {
		dir := t.TempDir()
		got, err := EnsureClaudeImport(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got.Exists || got.Imports || got.Action != ClaudeCreate || got.NewContent != "@AGENTS.md\n" {
			t.Errorf("%+v", got)
		}
		if got.Path != filepath.Join(dir, "CLAUDE.md") {
			t.Errorf("path = %q", got.Path)
		}
	})
	t.Run("imports already", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "CLAUDE.md", "@./AGENTS.md\n")
		got, err := EnsureClaudeImport(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Exists || !got.Imports || got.Action != ClaudeOK || got.NewContent != "" {
			t.Errorf("%+v", got)
		}
	})
	t.Run("exists without import", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "CLAUDE.md", "# Mine")
		got, err := EnsureClaudeImport(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Exists || got.Imports || got.Action != ClaudeAppend || got.NewContent != "# Mine\n@AGENTS.md\n" {
			t.Errorf("%+v", got)
		}
	})
	t.Run("symlink to AGENTS.md", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "AGENTS.md", "rules\n")
		if err := os.Symlink("AGENTS.md", filepath.Join(dir, "CLAUDE.md")); err != nil {
			t.Skip("symlinks unavailable:", err)
		}
		got, err := EnsureClaudeImport(dir)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Imports || got.Action != ClaudeOK {
			t.Errorf("%+v", got)
		}
	})
	t.Run("unreadable CLAUDE.md is an error", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "CLAUDE.md"), 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := EnsureClaudeImport(dir); err == nil {
			t.Error("want error when CLAUDE.md is a directory")
		}
	})
}
