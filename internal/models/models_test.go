package models

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/soldier"
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

const allBuiltIn = "scout-facts,scout-judgment,mission-mechanical,mission-big,opus-on-request," +
	"haiku-4-5,haiku-5-5,sonnet-4-6,sonnet-5,sonnet-5-5,opus-4-6,opus-4-7,opus-4-8,opus-5,opus-5-5,fable-5,fable-5-1"

func TestBuiltInsHaveSeventeenProfilesInOrder(t *testing.T) {
	project, home := layout(t)
	tbl, err := Load(project, home)
	if err != nil {
		t.Fatal(err)
	}
	if names(tbl) != allBuiltIn {
		t.Errorf("got %s", names(tbl))
	}
	if len(tbl.Profiles) != 17 {
		t.Errorf("got %d profiles, want 17", len(tbl.Profiles))
	}
}

func TestPinnedProfilesPassClaudeValidation(t *testing.T) {
	project, home := layout(t)
	tbl, err := Load(project, home)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range tbl.Profiles[5:] {
		if p.Harness != "" {
			t.Errorf("%s: harness = %q, want the claude default", p.Name, p.Harness)
		}
		if err := soldier.ValidateModelEffort("", p.Model, p.Effort); err != nil {
			t.Errorf("%s: %v", p.Name, err)
		}
		if !strings.Contains(p.When, "explicitly asks") {
			t.Errorf("%s: when = %q, want it to say it is used only on explicit request", p.Name, p.When)
		}
	}
}

func TestFableProfilesRequireApproval(t *testing.T) {
	project, home := layout(t)
	tbl, err := Load(project, home)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"fable-5", "fable-5-1"} {
		for _, p := range tbl.Profiles {
			if p.Name != name {
				continue
			}
			for _, want := range []string{"expensive", "explicit OK", "do not use it"} {
				if !strings.Contains(p.When, want) {
					t.Errorf("%s: when = %q, missing %q", name, p.When, want)
				}
			}
		}
	}
}

func TestProjectOverridesPinnedEffortKeepsModelAndWhen(t *testing.T) {
	project, home := layout(t)
	writeModels(t, projectModels(project), `{"profiles": {"opus-5-5": {"effort": "max"}}}`)
	tbl, err := Load(project, home)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range tbl.Profiles {
		if p.Name == "opus-5-5" {
			if p.Model != "claude-opus-5-5" || p.Effort != "max" || !strings.Contains(p.When, "Opus 5.5") {
				t.Errorf("opus-5-5 = %+v", p)
			}
			return
		}
	}
	t.Error("opus-5-5 missing")
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
	if len(tbl.Profiles) < len(want) {
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
	if names(tbl) != allBuiltIn {
		t.Errorf("got %s", names(tbl))
	}
	// DefaultJSON hands out a copy.
	b := DefaultJSON()
	b[0] = 'X'
	if DefaultJSON()[0] == 'X' {
		t.Error("DefaultJSON exposes its backing array")
	}
}

func TestDefaultJSONWritesAllSeventeenProfilesAndDefault(t *testing.T) {
	var f struct {
		Profiles map[string]json.RawMessage `json:"profiles"`
		Default  Choice                     `json:"default"`
	}
	if err := json.Unmarshal(DefaultJSON(), &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Profiles) != 17 {
		t.Errorf("DefaultJSON has %d profiles, want 17", len(f.Profiles))
	}
	if f.Default != (Choice{Model: "sonnet", Effort: "medium"}) {
		t.Errorf("default = %+v", f.Default)
	}
	// The written file lists the profiles in the documented order, so a
	// project that adds its own after them keeps the built-in ordering.
	of, err := orderedLayer(DefaultJSON())
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, p := range of.profiles {
		order = append(order, p.name)
	}
	if got := strings.Join(order, ","); got != allBuiltIn {
		t.Errorf("order = %s", got)
	}
	// Written as-is, the file must merge to exactly the built-in table.
	project, home := layout(t)
	want, err := Load(project, home)
	if err != nil {
		t.Fatal(err)
	}
	writeModels(t, projectModels(project), string(DefaultJSON()))
	got, err := Load(project, home)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Profiles, want.Profiles) || got.Default != want.Default {
		t.Error("table from the written file differs from the built-in table")
	}
}

func TestShortProjectFileStillInheritsEveryBuiltIn(t *testing.T) {
	project, home := layout(t)
	writeModels(t, projectModels(project), `{"profiles": {"opus-5": {"effort": "max"}}}`)
	tbl, err := Load(project, home)
	if err != nil {
		t.Fatal(err)
	}
	if names(tbl) != allBuiltIn {
		t.Errorf("got %s", names(tbl))
	}
	if c, _ := tbl.Lookup("opus-5"); c.Effort != "max" || c.Model != "claude-opus-5" {
		t.Errorf("opus-5 = %+v", c)
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
	if got := names(tbl); !strings.HasSuffix(got, "fable-5-1,docs-writer") {
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

// The example files under docs/examples are what the "Models for other
// harnesses" guide shows, so they must load and keep naming the harness they
// are an example for.
func TestLoadDocsExamples(t *testing.T) {
	for _, harness := range []string{"opencode", "pi"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "docs", "examples", "models."+harness+".json"))
		if err != nil {
			t.Fatal(err)
		}
		project, home := layout(t)
		writeModels(t, projectModels(project), string(data))
		tbl, err := Load(project, home)
		if err != nil {
			t.Fatalf("%s example: %v", harness, err)
		}
		for _, name := range []string{"scout-facts", "scout-judgment", "mission-big"} {
			c, err := tbl.Lookup(name)
			if err != nil {
				t.Fatalf("%s example: %v", harness, err)
			}
			if c.Harness != harness {
				t.Errorf("%s example: %s runs on %q", harness, name, c.Harness)
			}
		}
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

func TestMergeFullModelIDOverGlobalAlias(t *testing.T) {
	project, home := layout(t)
	writeModels(t, home, `{"profiles": {"mission-big": {"model": "opus", "effort": "max"}}}`)
	writeModels(t, projectModels(project), `{"profiles": {"mission-big": {"model": "claude-opus-4-8"}, "pinned": {"model": "claude-sonnet-5-5", "effort": "low", "when": "pinned"}}, "default": {"model": "claude-haiku-4-5"}}`)

	tbl, err := Load(project, home)
	if err != nil {
		t.Fatal(err)
	}
	big, _ := tbl.Lookup("mission-big")
	if big != (Choice{Model: "claude-opus-4-8", Effort: "max"}) {
		t.Errorf("mission-big = %+v, want full id from project and effort from global", big)
	}
	pinned, _ := tbl.Lookup("pinned")
	if pinned != (Choice{Model: "claude-sonnet-5-5", Effort: "low"}) {
		t.Errorf("pinned = %+v", pinned)
	}
	if tbl.Default.Model != "claude-haiku-4-5" {
		t.Errorf("default = %+v", tbl.Default)
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
		{"uppercase full id", `{"profiles": {"mission-big": {"model": "Claude-Sonnet-5-5"}}}`, []string{`profiles["mission-big"].model`, "full model id"}},
		{"pi style id on claude", `{"profiles": {"mission-big": {"model": "anthropic/claude-sonnet-5"}}}`, []string{`profiles["mission-big"].model`}},
		{"full id with space", `{"default": {"model": "claude-sonnet 5"}}`, []string{"default.model"}},
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

// A profile without a harness field resolves to the empty harness name,
// which means claude; none of the built-in profiles sets one.
func TestHarness_AbsentMeansClaude(t *testing.T) {
	project, home := layout(t)
	tbl, err := Load(project, home)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range tbl.Profiles {
		if p.Harness != "" {
			t.Errorf("built-in profile %s has harness %q, want none", p.Name, p.Harness)
		}
	}
	c, err := tbl.Lookup("mission-big")
	if err != nil || c.Harness != "" {
		t.Errorf("mission-big = %+v, %v; want no harness", c, err)
	}
	if tbl.Default.Harness != "" {
		t.Errorf("default harness = %q, want none", tbl.Default.Harness)
	}
}

func TestHarness_ExplicitClaudeAndOverrideAcrossLayers(t *testing.T) {
	project, home := layout(t)
	writeModels(t, home, `{"profiles": {"mission-big": {"harness": "claude"}}}`)
	tbl, err := Load(project, home)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := tbl.Lookup("mission-big")
	if c != (Choice{Model: "sonnet", Effort: "high", Harness: "claude"}) {
		t.Errorf("mission-big = %+v, want the explicit claude harness with model and effort inherited", c)
	}

	// A later layer overrides the harness. The model it inherits is checked
	// against the new harness, so switching to opencode needs a model of its
	// own shape; the effort is only a label there and is inherited as is.
	writeModels(t, projectModels(project), `{"profiles": {"mission-big": {"harness": "opencode", "model": "opencode/glm-5.3-flash-free"}}}`)
	tbl, err = Load(project, home)
	if err != nil {
		t.Fatal(err)
	}
	c, _ = tbl.Lookup("mission-big")
	if c.Harness != "opencode" || c.Model != "opencode/glm-5.3-flash-free" || c.Effort != "high" {
		t.Errorf("mission-big = %+v, want harness opencode with the new model and the inherited effort", c)
	}

	// Without a model of its own, the inherited claude alias is rejected.
	writeModels(t, projectModels(project), `{"profiles": {"mission-big": {"harness": "opencode"}}}`)
	if _, err = Load(project, home); err == nil || !strings.Contains(err.Error(), "provider/model") {
		t.Errorf("Load = %v, want the inherited claude alias rejected for opencode", err)
	}
}

// An opencode profile takes provider/model with an optional #variant, and any
// plain label as its effort: the effort is recorded, never mapped to a variant.
func TestHarness_OpenCodeProfile(t *testing.T) {
	project, home := layout(t)
	writeModels(t, projectModels(project), `{"profiles": {"oc-scout": {"model": "opencode/deepseek-v4.1-flash-free#high", "effort": "medium", "when": "x", "harness": "opencode"}}}`)
	tbl, err := Load(project, home)
	if err != nil {
		t.Fatal(err)
	}
	c, err := tbl.Lookup("oc-scout")
	if err != nil {
		t.Fatal(err)
	}
	if c != (Choice{Model: "opencode/deepseek-v4.1-flash-free#high", Effort: "medium", Harness: "opencode"}) {
		t.Errorf("oc-scout = %+v", c)
	}
}

// A pi profile takes a provider/id model, including an id that ends in
// OpenRouter's ":free", and a thinking level as its effort.
func TestHarness_PiProfile(t *testing.T) {
	project, home := layout(t)
	writeModels(t, projectModels(project), `{"profiles": {"pi-scout": {"model": "openrouter/some/model:free", "effort": "minimal", "when": "x", "harness": "pi"}}}`)
	tbl, err := Load(project, home)
	if err != nil {
		t.Fatal(err)
	}
	c, err := tbl.Lookup("pi-scout")
	if err != nil {
		t.Fatal(err)
	}
	if c != (Choice{Model: "openrouter/some/model:free", Effort: "minimal", Harness: "pi"}) {
		t.Errorf("pi-scout = %+v", c)
	}
}

// A later layer's model or effort for a profile is checked against the
// harness the profile ends up with, not the claude default, so a project file
// can retune a pi profile the global base defined; and switching a profile to
// pi without giving it a provider/id model is refused.
func TestHarness_ValuesAreCheckedAgainstTheMergedHarness(t *testing.T) {
	project, home := layout(t)
	writeModels(t, home, `{"profiles": {"pi-scout": {"model": "anthropic/claude-haiku-4-5", "effort": "low", "when": "x", "harness": "pi"}}}`)
	writeModels(t, projectModels(project), `{"profiles": {"pi-scout": {"model": "openai/gpt-5", "effort": "minimal"}}}`)
	tbl, err := Load(project, home)
	if err != nil {
		t.Fatalf("a project layer retuning a pi profile: %v", err)
	}
	if c, _ := tbl.Lookup("pi-scout"); c != (Choice{Model: "openai/gpt-5", Effort: "minimal", Harness: "pi"}) {
		t.Errorf("pi-scout = %+v", c)
	}

	project, home = layout(t)
	path := writeModels(t, projectModels(project), `{"profiles": {"mission-big": {"harness": "pi"}}}`)
	_, err = Load(project, home)
	if err == nil {
		t.Fatal("switching mission-big to pi kept its claude alias model, expected an error")
	}
	for _, w := range []string{path, `profiles["mission-big"].model`, `"sonnet"`, "provider/id"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q does not contain %q", err, w)
		}
	}

	project, home = layout(t)
	// Its inherited effort "high" is a thinking level too, so giving the model
	// alongside the harness is enough.
	writeModels(t, projectModels(project), `{"profiles": {"mission-big": {"harness": "pi", "model": "anthropic/claude-sonnet-5"}}}`)
	if _, err := Load(project, home); err != nil {
		t.Errorf("switching to pi with a provider/id model: %v", err)
	}
}

func TestHarness_InvalidValues(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{"unknown harness", `{"profiles": {"mission-big": {"harness": "gemini"}}}`, []string{`profiles["mission-big"].harness`, `unknown harness "gemini"`, "supported: claude, opencode, pi"}},
		{"empty harness", `{"profiles": {"mission-big": {"harness": ""}}}`, []string{`profiles["mission-big"].harness: must not be empty`}},
		{"case matters", `{"profiles": {"mission-big": {"harness": "Claude"}}}`, []string{`unknown harness "Claude"`}},
		{"default has no harness", `{"default": {"harness": "claude"}}`, []string{"invalid JSON", "harness"}},
		{"pi model must be provider/id", `{"profiles": {"mission-big": {"harness": "pi", "model": "sonnet", "effort": "high"}}}`, []string{`profiles["mission-big"].model`, `"sonnet"`, "provider/id"}},
		{"pi effort is a thinking level", `{"profiles": {"mission-big": {"harness": "pi", "model": "a/b", "effort": "extreme"}}}`, []string{`profiles["mission-big"].effort`, `"extreme"`, "xhigh"}},
		{"opencode model must be provider/model", `{"profiles": {"mission-big": {"harness": "opencode", "model": "sonnet", "effort": "high"}}}`, []string{`profiles["mission-big"].model`, `"sonnet"`, "provider/model"}},
		{"opencode variant must be a plain token", `{"profiles": {"mission-big": {"harness": "opencode", "model": "opencode/x#hi gh", "effort": "high"}}}`, []string{`profiles["mission-big"].model`, "#variant"}},
		{"claude values still checked", `{"profiles": {"mission-big": {"harness": "claude", "model": "gpt"}}}`, []string{`profiles["mission-big"].model`, `"gpt"`}},
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
