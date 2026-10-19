package soldier_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

func ocTask(t *testing.T, major int, model, effort string) state.Task {
	t.Helper()
	task := newTask(t)
	task.Harness = "opencode"
	task.HarnessMajor = major
	task.Model = model
	task.Effort = effort
	return task
}

func TestHarnessFor_OpenCode(t *testing.T) {
	h, err := soldier.HarnessFor("opencode")
	if err != nil {
		t.Fatalf("HarnessFor(opencode): %v", err)
	}
	if _, ok := h.(soldier.OpenCodeHarness); !ok {
		t.Fatalf("HarnessFor(opencode) = %T, want OpenCodeHarness", h)
	}
	if h.Binary() != "opencode" || h.HerdrAgentKind() != "opencode" {
		t.Errorf("binary %q, kind %q, want opencode and opencode", h.Binary(), h.HerdrAgentKind())
	}
	if h.QuestionModal() {
		t.Error("OpenCode's question form is not Claude's selector, QuestionModal must be false")
	}
	if d := h.TrustDialog(); d.Marker != "" || len(d.Keys) != 0 {
		t.Errorf("TrustDialog = %+v, want none", d)
	}
	if got := h.HerdrExtraArgs(); !slices.Equal(got, []string{"--auto"}) {
		t.Errorf("HerdrExtraArgs = %v, want [--auto]", got)
	}
}

func TestHarnessForTask_CarriesTheRecordedMajor(t *testing.T) {
	h, err := soldier.HarnessForTask(ocTask(t, 1, "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if oc, ok := h.(soldier.OpenCodeHarness); !ok || oc.Major != 1 {
		t.Errorf("HarnessForTask = %#v, want OpenCodeHarness{Major: 1}", h)
	}
	// Harnesses without generations are returned as registered.
	if h, _ := soldier.HarnessForTask(piTask(t, "", "")); h != (soldier.PiHarness{}) {
		t.Errorf("HarnessForTask(pi) = %#v", h)
	}
}

func TestOpenCodeValidateModelEffort(t *testing.T) {
	cases := []struct {
		name          string
		model, effort string
		wantErr       string
	}{
		{name: "both empty"},
		{name: "free zen model", model: "opencode/glm-5.3-flash-free"},
		{name: "model with a variant", model: "opencode/deepseek-v4.1-flash-free#high"},
		{name: "gateway id with slashes and a colon", model: "openrouter/anthropic/claude-3.5-sonnet:free#max"},
		{name: "effort is only a label", model: "opencode/glm-5.3-flash-free", effort: "medium"},
		{name: "effort xhigh is not mapped to anything", effort: "xhigh"},
		{name: "claude alias is not a model", model: "sonnet", wantErr: "provider/model"},
		{name: "no model after the slash", model: "opencode/", wantErr: "provider/model"},
		{name: "empty variant", model: "opencode/x#", wantErr: "#variant"},
		{name: "two variants", model: "opencode/x#a#b", wantErr: "#variant"},
		{name: "spaces in the variant", model: "opencode/x#a b", wantErr: "#variant"},
		{name: "effort with spaces", effort: "very high", wantErr: "never passed to opencode"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := soldier.OpenCodeHarness{}.ValidateModelEffort(c.model, c.effort)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("error = %v, want one mentioning %q", err, c.wantErr)
			}
		})
	}
}

func TestParseOpenCodeVersion(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "opencode v2.0.32\n", want: "2.0.32"},
		{in: "1.19.3\n", want: "1.19.3"},
		{in: "opencode 1.18.29 (build abc)", want: "1.18.29"},
		{in: "2.1.0-beta.4", want: "2.1.0"},
		{in: "dev", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, c := range cases {
		v, err := soldier.ParseOpenCodeVersion(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseOpenCodeVersion(%q): expected an error", c.in)
			}
			continue
		}
		if err != nil || v.String() != c.want {
			t.Errorf("ParseOpenCodeVersion(%q) = %v, %v; want %s", c.in, v, err, c.want)
		}
	}
}

func TestOpenCodeVersionSupported(t *testing.T) {
	for _, ok := range []string{"2.0.32", "2.5.1", "1.19.3", "1.18.29", "1.18.30", "1.20.0"} {
		v, _ := soldier.ParseOpenCodeVersion(ok)
		if err := v.Supported(); err != nil {
			t.Errorf("%s: unexpected error %v", ok, err)
		}
	}
	for _, bad := range []string{"1.18.28", "1.17.6", "1.0.0", "0.9.0", "3.0.0"} {
		v, _ := soldier.ParseOpenCodeVersion(bad)
		if err := v.Supported(); err == nil {
			t.Errorf("%s: expected it to be unsupported", bad)
		}
	}
}

func TestOpenCodeLaunch_V2_WritesAgentAndConfigWithTheVariant(t *testing.T) {
	spec, err := soldier.OpenCodeHarness{Major: 2}.Launch(ocTask(t, 2, "opencode/deepseek-v4.1-flash-free#max", "medium"))
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Args) != 0 {
		t.Errorf("V2 TUI has no model flag, Args = %v", spec.Args)
	}
	if spec.Env["OPENCODE_DISABLE_AUTOUPDATE"] != "1" {
		t.Errorf("Env = %v, want OPENCODE_DISABLE_AUTOUPDATE=1", spec.Env)
	}
	if _, ok := spec.Env["OPENCODE_CONFIG_CONTENT"]; ok {
		t.Error("OPENCODE_CONFIG_CONTENT is ignored by the V2 TUI and must not be used")
	}
	files := map[string]string{}
	for _, f := range spec.Files {
		files[f.Path] = string(f.Content)
	}
	agent := files[".opencode/agents/vx-soldier.md"]
	for _, want := range []string{"---\n", "mode: primary", `model: "opencode/deepseek-v4.1-flash-free#max"`} {
		if !strings.Contains(agent, want) {
			t.Errorf("agent file lacks %q:\n%s", want, agent)
		}
	}
	cfg := files["opencode.json"]
	for _, want := range []string{`"default_agent": "vx-soldier"`, `"model": "opencode/deepseek-v4.1-flash-free"`} {
		if !strings.Contains(cfg, want) {
			t.Errorf("opencode.json lacks %q:\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "#") {
		t.Errorf("the root model keeps no variant, got:\n%s", cfg)
	}
	if strings.Contains(agent, "permission") || strings.Contains(cfg, "permission") {
		t.Error("no permission rules are written: --auto keeps the general's own denies, a wildcard rule would override them")
	}
	if !slices.Equal(spec.GitExclude, []string{"/.opencode/", "/opencode.json"}) {
		t.Errorf("GitExclude = %v", spec.GitExclude)
	}
	if !slices.Contains(spec.RefuseIfPresent, "opencode.jsonc") {
		t.Errorf("RefuseIfPresent = %v, want opencode.jsonc", spec.RefuseIfPresent)
	}
}

// Without a variant in the model string none is written: the effort label is
// recorded on the task and never turned into one.
func TestOpenCodeLaunch_V2_NoVariantWhenTheProfileHasNone(t *testing.T) {
	spec, err := soldier.OpenCodeHarness{Major: 2}.Launch(ocTask(t, 2, "opencode/glm-5.3-flash-free", "high"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range spec.Files {
		if strings.Contains(string(f.Content), "high") || strings.Contains(string(f.Content), "#") {
			t.Errorf("%s carries a variant the profile did not supply:\n%s", f.Path, f.Content)
		}
	}
}

func TestOpenCodeLaunch_V2_NoModelLeavesTheDefault(t *testing.T) {
	spec, err := soldier.OpenCodeHarness{Major: 2}.Launch(ocTask(t, 2, "", ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range spec.Files {
		if strings.Contains(string(f.Content), "model") {
			t.Errorf("%s names a model although the task has none:\n%s", f.Path, f.Content)
		}
	}
}

func TestOpenCodeLaunch_V1_ModelFlagAndVariantThroughTheConfigEnv(t *testing.T) {
	spec, err := soldier.OpenCodeHarness{Major: 1}.Launch(ocTask(t, 1, "anthropic/claude-sonnet-5#high", "medium"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(spec.Args, []string{"--model", "anthropic/claude-sonnet-5"}) {
		t.Errorf("Args = %v, want --model anthropic/claude-sonnet-5", spec.Args)
	}
	if len(spec.Files) != 0 {
		t.Errorf("V1 writes no files into the camp, got %d", len(spec.Files))
	}
	want := `{"agent":{"build":{"model":"anthropic/claude-sonnet-5","variant":"high"}}}`
	if spec.Env["OPENCODE_CONFIG_CONTENT"] != want {
		t.Errorf("OPENCODE_CONFIG_CONTENT = %q, want %q", spec.Env["OPENCODE_CONFIG_CONTENT"], want)
	}
	if spec.Env["OPENCODE_DISABLE_AUTOUPDATE"] != "1" {
		t.Errorf("Env = %v", spec.Env)
	}
}

func TestOpenCodeLaunch_V1_NoVariantNoConfigEnv(t *testing.T) {
	spec, err := soldier.OpenCodeHarness{Major: 1}.Launch(ocTask(t, 1, "opencode/glm-5.3-flash-free", "medium"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := spec.Env["OPENCODE_CONFIG_CONTENT"]; ok {
		t.Errorf("no variant, so no config env, got %v", spec.Env)
	}
}

func TestOpenCodeLaunch_NeedsARecordedMajor(t *testing.T) {
	for _, major := range []int{0, 3} {
		if _, err := (soldier.OpenCodeHarness{Major: major}).Launch(ocTask(t, major, "", "")); err == nil {
			t.Errorf("Major %d: expected an error", major)
		}
	}
}

func TestOpenCodeCommand(t *testing.T) {
	cases := []struct {
		name  string
		major int
		model string
		want  []string
	}{
		{"v2 with a variant", 2, "opencode/deepseek-v4.1-flash-free#high",
			[]string{"run", "--auto", "--format", "json", "--model", "opencode/deepseek-v4.1-flash-free#high", "--", "the prompt"}},
		{"v2 without", 2, "opencode/glm-5.3-flash-free",
			[]string{"run", "--auto", "--format", "json", "--model", "opencode/glm-5.3-flash-free", "--", "the prompt"}},
		{"v1 splits the variant into its flag", 1, "anthropic/claude-sonnet-5#high",
			[]string{"run", "--auto", "--format", "json", "--model", "anthropic/claude-sonnet-5", "--variant", "high", "--", "the prompt"}},
		{"no model", 2, "",
			[]string{"run", "--auto", "--format", "json", "--", "the prompt"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			task := ocTask(t, c.major, c.model, "")
			task.Prompt = "the prompt"
			spec, err := soldier.HarnessCommand("opencode", task)
			if err != nil {
				t.Fatal(err)
			}
			if spec.Command != "opencode" || !slices.Equal(spec.Args, c.want) {
				t.Errorf("Command = %s %v, want opencode %v", spec.Command, spec.Args, c.want)
			}
			if !slices.Contains(spec.Env, "OPENCODE_DISABLE_AUTOUPDATE=1") {
				t.Errorf("Env = %v", spec.Env)
			}
			if spec.ParseOutput == nil {
				t.Error("the run prints JSON events, ParseOutput must be set")
			}
		})
	}
}

func TestParseOpenCodeRunOutput(t *testing.T) {
	t.Run("text events are joined, a missing step_finish is fine", func(t *testing.T) {
		out := `{"type":"step_start","timestamp":1,"sessionID":"ses_1","part":{"type":"step-start"}}
{"type":"text","timestamp":2,"sessionID":"ses_1","part":{"type":"text","text":"first"}}
{"type":"text","timestamp":3,"sessionID":"ses_1","part":{"type":"text","text":"{\"findings\": []}"}}
`
		got, err := soldier.ParseOpenCodeRunOutput(out)
		if err != nil {
			t.Fatal(err)
		}
		if got != "first\n{\"findings\": []}" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("an error event is a failure with its message", func(t *testing.T) {
		out := `{"type":"error","timestamp":1,"sessionID":"ses_1","error":{"type":"provider.no-route","message":"Variant unavailable for opencode/longcat-2.5-preview-free: high"}}` + "\n"
		_, err := soldier.ParseOpenCodeRunOutput(out)
		if err == nil || !strings.Contains(err.Error(), "Variant unavailable") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("an error carried only as a type or as data.message", func(t *testing.T) {
		for _, line := range []string{
			`{"type":"error","error":{"type":"provider.no-route"}}`,
			`{"type":"error","error":{"name":"APIError","data":{"message":"quota"}}}`,
		} {
			if _, err := soldier.ParseOpenCodeRunOutput(line); err == nil {
				t.Errorf("%s: expected an error", line)
			}
		}
	})
	t.Run("junk lines are ignored", func(t *testing.T) {
		got, err := soldier.ParseOpenCodeRunOutput("starting...\n{not json\n{\"type\":\"text\",\"part\":{\"text\":\"ok\"}}\n")
		if err != nil || got != "ok" {
			t.Errorf("got %q, %v", got, err)
		}
	})
	t.Run("nothing at all is empty, not an error", func(t *testing.T) {
		got, err := soldier.ParseOpenCodeRunOutput("")
		if err != nil || got != "" {
			t.Errorf("got %q, %v", got, err)
		}
	})
}

// The two files are the stdout of `opencode run --auto --format json` on a
// real OpenCode 2.0.32, run against free Zen models with trivial prompts:
// a successful run, which ends after its text event with no step_finish, and a
// run asked for a variant the model does not have, which exits 1 with an error
// event.
func TestParseOpenCodeRunOutput_RealOutput(t *testing.T) {
	ok, err := os.ReadFile("testdata/opencode-2.0.32-run-ok.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	got, err := soldier.ParseOpenCodeRunOutput(string(ok))
	if err != nil || got != "ping" {
		t.Errorf("success: got %q, %v; want ping", got, err)
	}

	bad, err := os.ReadFile("testdata/opencode-2.0.32-run-error.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := soldier.ParseOpenCodeRunOutput(string(bad)); err == nil || !strings.Contains(err.Error(), "Variant unavailable for opencode/longcat-2.5-preview-free: high") {
		t.Errorf("error run: got %v", err)
	}
}
