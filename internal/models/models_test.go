package models

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeModels(t *testing.T, dir, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// layout returns a project dir and a vexillum home, both empty.
func layout(t *testing.T) (project, home string) {
	root := t.TempDir()
	return filepath.Join(root, "project"), filepath.Join(root, "home")
}

func projectModels(project string) string { return filepath.Join(project, ".vexillum") }

func names(t Table) string {
	var out []string
	for _, p := range t.Profiles {
		out = append(out, p.Name)
	}
	return strings.Join(out, ",")
}

func TestDefaultsMirrorTheSixRules(t *testing.T) {
	project, home := layout(t)
	tbl, err := Load(project, home)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ name, model, effort string }{
		{"scout-facts", "haiku", "low"},
		{"scout-judgment", "sonnet", "medium"},
		{"mission-mechanical", "haiku", "low"},
		{"mission-big", "sonnet", "high"},
		{"opus-on-request", "opus", "high"},
	}
	if len(tbl.Profiles) != len(want) {
		t.Fatalf("got profiles %s", names(tbl))
	}
	for i, w := range want {
		p := tbl.Profiles[i]
		if p.Name != w.name || p.Model != w.model || p.Effort != w.effort || p.When == "" {
			t.Errorf("profile %d = %+v, want %+v with a when", i, p, w)
		}
	}
	if tbl.Default != (Choice{Model: "sonnet", Effort: "medium"}) {
		t.Errorf("default = %+v", tbl.Default)
	}
	if len(tbl.Sources) != 0 {
		t.Errorf("sources = %v, want none", tbl.Sources)
	}
}

func TestDefaultJSONIsAValidModelsFile(t *testing.T) {
	// What init writes must load back to the same table as the defaults.
	project, home := layout(t)
	writeModels(t, projectModels(project), string(DefaultJSON()))
	tbl, err := Load(project, home)
	if err != nil {
		t.Fatal(err)
	}
	if names(tbl) != "scout-facts,scout-judgment,mission-mechanical,mission-big,opus-on-request" {
		t.Errorf("got %s", names(tbl))
	}
	// DefaultJSON hands out a copy.
	b := DefaultJSON()
	b[0] = 'X'
	if DefaultJSON()[0] == 'X' {
		t.Error("DefaultJSON exposes its backing array")
	}
}

func TestMergeProjectOverGlobalOverDefaults(t *testing.T) {
	project, home := layout(t)
	globalPath := writeModels(t, home, `{"profiles": {"mission-big": {"effort": "max"}, "scout-facts": {"model": "sonnet"}}, "default": {"effort": "high"}}`)
	projectPath := writeModels(t, projectModels(project), `{"profiles": {"mission-big": {"model": "opus"}, "docs-writer": {"model": "sonnet", "effort": "low", "when": "docs only"}}}`)

	tbl, err := Load(project, home)
	if err != nil {
		t.Fatal(err)
	}
	big, _ := tbl.Lookup("mission-big")
	if big != (Choice{Model: "opus", Effort: "max"}) {
		t.Errorf("mission-big = %+v, want model from project and effort from global", big)
	}
	facts, _ := tbl.Lookup("scout-facts")
	if facts != (Choice{Model: "sonnet", Effort: "low"}) {
		t.Errorf("scout-facts = %+v", facts)
	}
	if tbl.Default != (Choice{Model: "sonnet", Effort: "high"}) {
		t.Errorf("default = %+v", tbl.Default)
	}
	if got := names(tbl); !strings.HasSuffix(got, "opus-on-request,docs-writer") {
		t.Errorf("order = %s, user profiles must come after the built-ins", got)
	}
	if len(tbl.Sources) != 2 || tbl.Sources[0] != globalPath || tbl.Sources[1] != projectPath {
		t.Errorf("sources = %v", tbl.Sources)
	}
}

func TestLoadExampleTestdata(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "example.json"))
	if err != nil {
		t.Fatal(err)
	}
	project, home := layout(t)
	writeModels(t, projectModels(project), string(data))
	tbl, err := Load(project, home)
	if err != nil {
		t.Fatal(err)
	}
	big, _ := tbl.Lookup("mission-big")
	if big != (Choice{Model: "sonnet", Effort: "xhigh"}) {
		t.Errorf("mission-big = %+v", big)
	}
	if _, err := tbl.Lookup("docs-writer"); err != nil {
		t.Error(err)
	}
}

func TestLookup(t *testing.T) {
	project, home := layout(t)
	tbl, err := Load(project, home)
	if err != nil {
		t.Fatal(err)
	}
	c, err := tbl.Lookup("default")
	if err != nil || c != (Choice{Model: "sonnet", Effort: "medium"}) {
		t.Errorf("default lookup = %+v, %v", c, err)
	}
	_, err = tbl.Lookup("nope")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{`"nope"`, "scout-facts", "opus-on-request", "default"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestInvalidFiles(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string // substrings the error must contain
	}{
		{"bad json", `{"profiles": `, []string{"invalid JSON"}},
		{"trailing data", `{} {}`, []string{"unexpected data"}},
		{"unknown top-level field", `{"profile": {}}`, []string{"invalid JSON", "profile"}},
		{"unknown profile field", `{"profiles": {"mission-big": {"efort": "low"}}}`, []string{"efort"}},
		{"bad model", `{"profiles": {"mission-big": {"model": "gpt"}}}`, []string{`profiles["mission-big"].model`, `"gpt"`, "haiku, sonnet, opus, fable"}},
		{"bad effort", `{"profiles": {"mission-big": {"effort": "ultra"}}}`, []string{`profiles["mission-big"].effort`, `"ultra"`, "low, medium, high, xhigh, max"}},
		{"bad default model", `{"default": {"model": "x"}}`, []string{"default.model"}},
		{"bad default effort", `{"default": {"effort": "x"}}`, []string{"default.effort"}},
		{"empty model", `{"profiles": {"mission-big": {"model": ""}}}`, []string{"model: must not be empty"}},
		{"empty when", `{"profiles": {"mission-big": {"when": " "}}}`, []string{"when: must not be empty"}},
		{"new profile incomplete", `{"profiles": {"fresh": {"model": "haiku"}}}`, []string{`"fresh"`, "missing effort, when"}},
		{"reserved name", `{"profiles": {"default": {"model": "haiku", "effort": "low", "when": "x"}}}`, []string{"reserved"}},
		{"whitespace in name", `{"profiles": {"my profile": {"model": "haiku", "effort": "low", "when": "x"}}}`, []string{"whitespace"}},
		{"null profile", `{"profiles": {"mission-big": null}}`, []string{"must be an object"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			project, home := layout(t)
			path := writeModels(t, projectModels(project), c.content)
			_, err := Load(project, home)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error %q does not name the file %s", err, path)
			}
			for _, w := range c.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not contain %q", err, w)
				}
			}
		})
	}
}

func TestInvalidGlobalFileNamesGlobalPath(t *testing.T) {
	project, home := layout(t)
	path := writeModels(t, home, `{"default": {"model": "nope"}}`)
	_, err := Load(project, home)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("error %v should name %s", err, path)
	}
}

func TestUnreadableFileIsAnError(t *testing.T) {
	project, home := layout(t)
	// A directory where the file should be: exists, but cannot be read.
	if err := os.MkdirAll(filepath.Join(projectModels(project), FileName), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(project, home); err == nil || !strings.Contains(err.Error(), "reading") {
		t.Errorf("got %v", err)
	}
}
