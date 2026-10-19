package soldier

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/opencodeapi"
	"github.com/isaias-alt/vexillum/internal/state"
)

// OpenCodeHarness is the Harness for the `opencode` CLI, in both generations
// that share the command name: V1 (1.x, from 1.18.29, where the TUI takes
// --model and --agent) and V2 (2.x, where the TUI takes neither and the
// model is chosen by creating the soldier's session through the background
// service's HTTP API). Major says which one the task's soldier runs on; it is
// detected from `opencode --version` when the task is dispatched and recorded
// on the task (state.Task.HarnessMajor), so a re-dispatch and every later
// command drive the same generation. The zero value (Major 0) answers the
// questions that do not depend on the generation (Binary, ValidateModelEffort)
// and cannot Launch.
//
// What was checked, and against what:
//   - V2 (2.0.32): run live with the free zero-retention Zen models, see
//     docs/verification/opencode-soldier-checklist.md for what that covered.
//     The launch creates the soldier's session through the service
//     (POST /api/session with the model, the variant and the camp as its
//     location, no agent and no permissions) and starts the TUI on it with
//     --session and --auto, so nothing is written into the camp or the
//     project, and the project's own config, default agent and permission
//     denies apply untouched. A config layer was rejected: the config is read
//     by the shared service when it starts, so OPENCODE_CONFIG_CONTENT in the
//     TUI's environment is ignored unless the TUI is started with
//     --standalone, and a layer given to a service that starts with it leaks
//     into every other TUI. The variant does reach the wire through a layer
//     (agent.<n>.model with agent.<n>.variant as separate keys), but a layer
//     ranks below a project that sets its own default_agent or model.
//   - V1 (1.19.3): NOT run, no V1 was available. The mechanism comes from the
//     v1.18.29 and v1.19.3 sources and docs (TUI options model, agent, auto,
//     prompt; no variant option) and from the config schema (agent.<name>.model
//     and .variant, which apply only together). Everything V1 is listed as
//     unverified in the checklist.
//
// Neither generation asks a soldier's permission questions with --auto, so a
// soldier blocks only on a question form (the question tool) or a screen
// vexillum does not know. Both are answered as free text, or with a key
// press where the screen is known (see answerOpenCodeBlocked).
type OpenCodeHarness struct {
	Major int
}

const (
	openCodeBinary         = "opencode"
	openCodeHerdrAgentKind = "opencode"

	// openCodeAutoArg approves every permission that is not explicitly
	// denied. An explicit deny in the general's own config still applies,
	// which is why vexillum sets no permission rules of its own, in the
	// session or anywhere else: in V2 the last matching rule wins, so a
	// wildcard rule of ours would override the general's denies. Present on
	// the V1 TUI since 1.18.0 and on the V2 TUI and `run`.
	openCodeAutoArg = "--auto"

	// openCodeSessionArg opens an existing session in the V2 TUI.
	openCodeSessionArg = "--session"

	// The oldest 1.x vexillum drives: the one herdr's OpenCode integration
	// supports, and recent enough to have the TUI's --auto flag.
	openCodeMinV1Minor = 18
	openCodeMinV1Patch = 29
)

// openCodeEnv is set in every soldier's pane and one-shot process. A running
// TUI that updates itself can exit mid-task, and the shared V2 service can be
// restarted by an update under the other soldiers. Whether it is enough to
// prevent that was not observable in a short run (see the checklist).
var openCodeEnv = map[string]string{"OPENCODE_DISABLE_AUTOUPDATE": "1"}

// openCodeToken is what a variant or an effort label may be made of.
var openCodeToken = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func (OpenCodeHarness) Binary() string {
	return openCodeBinary
}

// ValidateModelEffort checks model by shape only, as provider/model with an
// optional #variant: OpenCode's models are whatever its providers offer, a
// catalog that differs per machine and per provider, so there is no list to
// hold a value against here (run 'opencode models'). The variant is part of the
// model string because that is how V2 spells it, and V1 gets it split off.
// What exists is checked later, per generation: V2 asks the service at
// dispatch (prepareOpenCodeSession), and for V1 an unknown model or variant is
// OpenCode's own error when the soldier starts.
//
// Effort is accepted as a label and recorded, never passed to OpenCode:
// vexillum's low..max values have no universal meaning as OpenCode variants,
// whose names differ per model (one model's "high" is another's error). The
// variant comes only from the profile's model string.
func (OpenCodeHarness) ValidateModelEffort(model, effort string) error {
	if model != "" && !validOpenCodeModel(model) {
		return fmt.Errorf("model %q for harness opencode must be provider/model, with an optional #variant, for example opencode/glm-5.3-flash-free or anthropic/claude-sonnet-5#high (run 'opencode models' to see what is available)", model)
	}
	if effort != "" && !openCodeToken.MatchString(effort) {
		return fmt.Errorf("effort %q for harness opencode must be a plain label such as medium: it is recorded on the task, never passed to opencode (put a variant after the model with #, for example provider/model#high)", effort)
	}
	return nil
}

// validOpenCodeModel reports whether m is provider/model with an optional
// "#variant" suffix.
func validOpenCodeModel(m string) bool {
	base, variant, hasVariant := strings.Cut(m, "#")
	if hasVariant && !openCodeToken.MatchString(variant) {
		return false
	}
	return validPiModel(base)
}

// splitOpenCodeModel splits "provider/model#variant" into the model and the
// variant ("" when there is none).
func splitOpenCodeModel(m string) (model, variant string) {
	model, variant, _ = strings.Cut(m, "#")
	return model, variant
}

// Launch builds the interactive start for task's soldier on its recorded
// generation of the CLI.
func (h OpenCodeHarness) Launch(task state.Task) (LaunchSpec, error) {
	model, variant := splitOpenCodeModel(task.Model)
	switch h.Major {
	case 2:
		return h.launchV2(task, model, variant), nil
	case 1:
		return h.launchV1(model, variant), nil
	}
	return LaunchSpec{}, fmt.Errorf("the task records no usable OpenCode major version (%d), so vexillum cannot tell which launch to use: it is detected from 'opencode --version' at dispatch", h.Major)
}

// launchV2 starts the V2 TUI on a session vexillum creates for the task. The
// V2 TUI has no --model, --agent or variant flag, and the service that runs the
// session reads its config once, when it starts, so the model cannot reach it
// through the TUI's environment or a file without a clash with the project's
// own config. A session carries its own model and variant instead, above the
// project's default and above its agent's own model, and nothing is written
// anywhere. A task with no model starts the TUI plain, on the project's
// defaults.
func (h OpenCodeHarness) launchV2(task state.Task, model, variant string) LaunchSpec {
	spec := LaunchSpec{Env: cloneEnv(openCodeEnv)}
	if model != "" {
		spec.Prepare = func(campPath string) (PreparedLaunch, error) {
			return prepareOpenCodeSession(newOpenCodeSessions(), campPath, task, model, variant)
		}
	}
	return spec
}

// newOpenCodeSessions is how the V2 launch reaches the OpenCode service. A
// variable so tests can replace it.
var newOpenCodeSessions = func() opencodeapi.Sessions {
	return opencodeapi.CLI{Binary: openCodeBinary}
}

// prepareOpenCodeSession gives task's V2 soldier a session in the camp at
// campPath and returns the TUI arguments that open it.
//
// The service is started here, by vexillum, when none runs, from an
// environment without the OPENCODE_CONFIG* variables: `opencode api` would
// start a stopped service itself, with whatever environment this process has,
// and a service keeps the environment it starts with for every TUI that
// connects to it. With the service up before the pane exists, the soldier's TUI
// never gets to be the one that starts it either. The commander's wake path
// (internal/opencodeapi.Client) keeps refusing to start a service.
//
// A session the task already has (a re-dispatch) is reused when the service
// still knows it and it is located in this camp; one the service lost, or one
// located in another camp, is replaced by a new session. The model is checked
// against the models the service lists for the camp before a session is
// created, so a provider, model or variant that does not exist fails the
// dispatch with what does exist, instead of the soldier's first prompt.
func prepareOpenCodeSession(api opencodeapi.Sessions, campPath string, task state.Task, model, variant string) (PreparedLaunch, error) {
	if err := api.EnsureService(); err != nil {
		return PreparedLaunch{}, fmt.Errorf("the OpenCode background service is not available: %w", err)
	}

	if id := task.HarnessSession; id != "" {
		existing, err := api.Session(id)
		switch {
		case err == nil && sameDirectory(existing.Directory, campPath):
			return PreparedLaunch{Args: []string{openCodeSessionArg, id}, SessionID: id}, nil
		case err != nil && !opencodeapi.IsSessionNotFound(err):
			return PreparedLaunch{}, fmt.Errorf("looking up the soldier's OpenCode session %s: %w", id, err)
		}
	}

	providerID, modelID, _ := strings.Cut(model, "/")
	models, err := api.Models(campPath)
	if err != nil {
		return PreparedLaunch{}, fmt.Errorf("listing the models OpenCode can run in the camp: %w", err)
	}
	if err := opencodeapi.CheckModel(models, providerID, modelID, variant); err != nil {
		return PreparedLaunch{}, fmt.Errorf("cannot start this soldier on %s: %w", task.Model, err)
	}
	id, err := api.CreateSession(campPath, "vexillum "+task.ID, opencodeapi.SessionModel{ProviderID: providerID, ID: modelID, Variant: variant})
	if err != nil {
		return PreparedLaunch{}, fmt.Errorf("creating the soldier's OpenCode session: %w", err)
	}
	return PreparedLaunch{Args: []string{openCodeSessionArg, id}, SessionID: id}, nil
}

// sameDirectory reports whether a and b name the same directory, looking
// through symlinks when both exist.
func sameDirectory(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}

// launchV1 starts the V1 TUI with --model, and carries the variant through the
// config JSON of the environment: the V1 TUI has no variant flag, and an
// agent's variant applies only with the agent's own model, so both are set on
// the default agent (build). Unverified on a real V1, see the checklist.
func (OpenCodeHarness) launchV1(model, variant string) LaunchSpec {
	spec := LaunchSpec{Env: cloneEnv(openCodeEnv)}
	if model != "" {
		spec.Args = []string{"--model", model}
	}
	if variant != "" && model != "" {
		cfg, _ := json.Marshal(map[string]any{"agent": map[string]any{"build": map[string]string{"model": model, "variant": variant}}})
		spec.Env["OPENCODE_CONFIG_CONTENT"] = string(cfg)
	}
	return spec
}

// Command builds the one-shot run the tribunal's reviewer and fixer use:
// `opencode run --auto --format json`, whose stdout is one JSON event per
// line. Without --auto a permission request is rejected and the run goes on,
// silently losing the action. The process reads a piped stdin to EOF and
// appends it to the message, so its stdin is the null device (see
// CommandSpec.ParseOutput). V2 takes the variant after the model, V1 as its
// own --variant flag. opts.ReadOnly is ignored, like Claude: nothing here
// narrows the run to reading, so a read-only caller relies on its prompt.
func (h OpenCodeHarness) Command(task state.Task, _ CommandOptions) CommandSpec {
	model, variant := splitOpenCodeModel(task.Model)
	args := []string{"run", openCodeAutoArg, "--format", "json"}
	if model != "" {
		if h.Major == 1 {
			args = append(args, "--model", model)
			if variant != "" {
				args = append(args, "--variant", variant)
			}
		} else if variant != "" {
			args = append(args, "--model", model+"#"+variant)
		} else {
			args = append(args, "--model", model)
		}
	}
	args = append(args, "--", task.Prompt)
	return CommandSpec{
		Command:     openCodeBinary,
		Args:        args,
		Env:         envList(openCodeEnv),
		ParseOutput: ParseOpenCodeRunOutput,
	}
}

func (OpenCodeHarness) HerdrAgentKind() string {
	return openCodeHerdrAgentKind
}

func (OpenCodeHarness) HerdrExtraArgs() []string {
	return []string{openCodeAutoArg}
}

// TrustDialog is the zero value: OpenCode asks nothing on a never-seen
// folder (no trust dialog was found in the V1 or V2 sources, and none showed
// in a fresh git worktree on V2).
func (OpenCodeHarness) TrustDialog() TrustDialog {
	return TrustDialog{}
}

// QuestionModal is false: OpenCode's question tool renders its own form,
// which is not Claude Code's AskUserQuestion selector. A blocked soldier is
// read as prose, and answerOpenCodeBlocked handles the OpenCode screens.
func (OpenCodeHarness) QuestionModal() bool {
	return false
}

func cloneEnv(env map[string]string) map[string]string {
	out := make(map[string]string, len(env)+1)
	for k, v := range env {
		out[k] = v
	}
	return out
}

// envList renders env as sorted "KEY=VALUE" entries.
func envList(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// ParseOpenCodeRunOutput reads the stdout of `opencode run --format json`: one
// JSON event per line. The answer is the text of the "text" events, in order.
// An "error" event is a failure, whatever the exit code: OpenCode reports a
// provider or model error as {"type":"error","error":{...}} on stdout, with
// exit code 1. A trailing "step_finish" event is not required: a plain run
// was seen to end with only step_start and text. Lines that are not JSON are
// ignored.
func ParseOpenCodeRunOutput(stdout string) (string, error) {
	var texts []string
	var failure string
	sc := bufio.NewScanner(strings.NewReader(stdout))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var ev struct {
			Type string `json:"type"`
			Part struct {
				Text string `json:"text"`
			} `json:"part"`
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		switch ev.Type {
		case "text":
			texts = append(texts, ev.Part.Text)
		case "error":
			if failure == "" {
				failure = openCodeErrorMessage(ev.Error)
			}
		}
	}
	if failure != "" {
		return "", fmt.Errorf("opencode reported an error: %s", failure)
	}
	return strings.Join(texts, "\n"), nil
}

// openCodeErrorMessage picks the human message out of an error event's error
// object, which carries it as message, or as data.message, or only as a type.
func openCodeErrorMessage(raw json.RawMessage) string {
	var e struct {
		Type    string `json:"type"`
		Name    string `json:"name"`
		Message string `json:"message"`
		Data    struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &e) == nil {
		switch {
		case e.Message != "":
			return e.Message
		case e.Data.Message != "":
			return e.Data.Message
		case e.Type != "":
			return e.Type
		case e.Name != "":
			return e.Name
		}
	}
	if len(raw) == 0 {
		return "(no detail)"
	}
	return string(raw)
}

// OpenCodeVersion is the version `opencode --version` printed.
type OpenCodeVersion struct {
	Major, Minor, Patch int
}

func (v OpenCodeVersion) String() string {
	return strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor) + "." + strconv.Itoa(v.Patch)
}

var openCodeVersionPattern = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// ParseOpenCodeVersion reads the first x.y.z in out: V2 prints "opencode
// v2.0.32", V1 the bare number.
func ParseOpenCodeVersion(out string) (OpenCodeVersion, error) {
	m := openCodeVersionPattern.FindStringSubmatch(out)
	if m == nil {
		return OpenCodeVersion{}, fmt.Errorf("no version number in %q", strings.TrimSpace(out))
	}
	var v OpenCodeVersion
	v.Major, _ = strconv.Atoi(m[1])
	v.Minor, _ = strconv.Atoi(m[2])
	v.Patch, _ = strconv.Atoi(m[3])
	return v, nil
}

// Supported reports whether vexillum drives this version: V1 from 1.18.29,
// and V2. A major vexillum has not been checked against is refused rather
// than guessed at.
func (v OpenCodeVersion) Supported() error {
	switch v.Major {
	case 2:
		return nil
	case 1:
		if v.Minor > openCodeMinV1Minor || v.Minor == openCodeMinV1Minor && v.Patch >= openCodeMinV1Patch {
			return nil
		}
		return fmt.Errorf("opencode %s is older than 1.18.29, the first 1.x vexillum supports (upgrade it, or install 2.x)", v)
	}
	return fmt.Errorf("opencode %s is not a version vexillum knows how to drive (it supports 1.x from 1.18.29, and 2.x)", v)
}

// OpenCodeVersionAt runs `<binary> --version` and parses what it prints. It
// is run every time and never cached, so an upgrade between two commands is
// seen. OPENCODE_DISABLE_AUTOUPDATE keeps the probe from updating anything.
func OpenCodeVersionAt(binary string) (OpenCodeVersion, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--version")
	cmd.Env = append(os.Environ(), envList(openCodeEnv)...)
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return OpenCodeVersion{}, fmt.Errorf("running %s --version: %w", binary, err)
	}
	return ParseOpenCodeVersion(stdout.String())
}

// DetectOpenCode finds opencode on PATH and reads its version.
func DetectOpenCode() (OpenCodeVersion, error) {
	path, err := exec.LookPath(openCodeBinary)
	if err != nil {
		return OpenCodeVersion{}, fmt.Errorf("opencode is not on PATH: %w", err)
	}
	return OpenCodeVersionAt(path)
}

// DetectHarnessVersion reports the generation of the CLI of the harness
// registered under harness ("" means claude), for a harness whose generations
// launch differently. major is 0 and version empty for one that has none (claude and pi),
// so a caller records nothing for them. A version vexillum cannot drive is an
// error.
func DetectHarnessVersion(harness string) (major int, version string, err error) {
	if harness != HarnessOpenCode {
		return 0, "", nil
	}
	v, err := DetectOpenCode()
	if err != nil {
		return 0, "", err
	}
	if err := v.Supported(); err != nil {
		return 0, "", err
	}
	return v.Major, v.String(), nil
}
