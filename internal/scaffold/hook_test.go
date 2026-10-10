package scaffold

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/cmdname"
)

func readSettings(t *testing.T, projectDir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(projectDir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("reading settings.json: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("parsing settings.json: %v", err)
	}
	return settings
}

func TestEnsureSentinelHook_CreatesFresh(t *testing.T) {
	dir := t.TempDir()

	added, err := EnsureSentinelHook(dir)
	if err != nil {
		t.Fatalf("EnsureSentinelHook: %v", err)
	}
	if !added {
		t.Error("expected added=true when settings.json didn't exist yet")
	}

	settings := readSettings(t, dir)
	hooks := settings["hooks"].(map[string]any)
	stop := hooks["Stop"].([]any)
	if len(stop) != 1 {
		t.Fatalf("expected exactly one Stop hook group, got %d", len(stop))
	}
}

func TestEnsureSentinelHook_IdempotentOnSecondCall(t *testing.T) {
	dir := t.TempDir()

	if _, err := EnsureSentinelHook(dir); err != nil {
		t.Fatalf("EnsureSentinelHook: %v", err)
	}
	added, err := EnsureSentinelHook(dir)
	if err != nil {
		t.Fatalf("EnsureSentinelHook (second call): %v", err)
	}
	if added {
		t.Error("expected added=false once the hook is already present")
	}
}

// runHookCommand runs the hook command through sh with exactly the given
// environment, the way a hook runner with a bare shell would, and returns
// its stdout, stderr and exit code.
func runHookCommand(t *testing.T, env ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command("/usr/bin/env", append([]string{"-i"}, append(env, "sh", "-c", SentinelHookCommand)...)...)
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	default:
		t.Fatalf("running the hook command: %v", err)
	}
	return out.String(), errOut.String(), code
}

// writeFakeVX writes a vx stub into dir that prints its own path and
// arguments, so a test can tell which binary the hook command ran.
func writeFakeVX(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, cmdname.Name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho \"$0 $*\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSentinelHookCommand_ResolvesWithoutPATH(t *testing.T) {
	// vx is only in $HOME/.local/bin, and PATH has nothing useful: the
	// "command not found" case that left the commander unwoken.
	home := t.TempDir()
	vx := writeFakeVX(t, filepath.Join(home, ".local", "bin"))

	stdout, stderr, code := runHookCommand(t, "HOME="+home, "PATH=/usr/bin:/bin")
	if code != 0 || stderr != "" {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}
	if want := vx + " sentinel await"; strings.TrimSpace(stdout) != want {
		t.Errorf("ran %q, want %q", strings.TrimSpace(stdout), want)
	}
}

func TestSentinelHookCommand_PrefersVXOnPATH(t *testing.T) {
	home := t.TempDir()
	writeFakeVX(t, filepath.Join(home, ".local", "bin"))
	onPath := writeFakeVX(t, t.TempDir())

	stdout, _, code := runHookCommand(t, "HOME="+home, "PATH="+filepath.Dir(onPath)+":/usr/bin:/bin")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if want := onPath + " sentinel await"; strings.TrimSpace(stdout) != want {
		t.Errorf("ran %q, want the one on PATH, %q", strings.TrimSpace(stdout), want)
	}
}

func TestSentinelHookCommand_NotFoundIsLoudAndNonBlocking(t *testing.T) {
	for _, known := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/home/linuxbrew/.linuxbrew/bin"} {
		if _, err := os.Stat(filepath.Join(known, cmdname.Name)); err == nil {
			t.Skipf("a real %s is installed in %s on this machine", cmdname.Name, known)
		}
	}
	home := t.TempDir()

	stdout, stderr, code := runHookCommand(t, "HOME="+home, "PATH=/usr/bin:/bin")
	if code != 0 {
		t.Errorf("a missing vx must not block Claude Code, got exit %d", code)
	}
	if stdout != "" {
		t.Errorf("unexpected stdout %q", stdout)
	}
	if strings.TrimSpace(stderr) != SentinelHookNotFoundMessage {
		t.Errorf("stderr = %q, want the not-found message", stderr)
	}
	for _, want := range []string{"not found", "PATH", cmdname.Name + " doctor"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("not-found message %q should mention %q", stderr, want)
		}
	}
}

func TestSentinelHookCommand_BakesNoAbsolutePath(t *testing.T) {
	// settings.json is committed and shared across machines.
	home, _ := os.UserHomeDir()
	if home != "" && strings.Contains(SentinelHookCommand, home) {
		t.Errorf("the command must not contain this machine's home directory: %s", SentinelHookCommand)
	}
	for _, want := range []string{"command -v " + cmdname.Name, "$HOME/.local/bin/" + cmdname.Name, "/opt/homebrew/bin/" + cmdname.Name, "/usr/local/bin/" + cmdname.Name, "/home/linuxbrew/.linuxbrew/bin/" + cmdname.Name} {
		if !strings.Contains(SentinelHookCommand, want) {
			t.Errorf("the command should try %q", want)
		}
	}
	if strings.ContainsAny(SentinelHookNotFoundMessage, "'\"`$") {
		t.Errorf("the not-found message must be safe inside the shell wrapper: %q", SentinelHookNotFoundMessage)
	}
}

func writeSettings(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".claude", "settings.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// stopCommands lists every Stop hook command in settings, in order.
func stopCommands(settings map[string]any) []string {
	var cmds []string
	stop, _ := settings["hooks"].(map[string]any)["Stop"].([]any)
	for _, g := range stop {
		entries, _ := g.(map[string]any)["hooks"].([]any)
		for _, e := range entries {
			cmd, _ := e.(map[string]any)["command"].(string)
			cmds = append(cmds, cmd)
		}
	}
	return cmds
}

func TestEnsureSentinelHook_KeepsForeignHooks(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, dir, `{
  "model": "sonnet",
  "hooks": {
    "PostToolUse": [{"matcher": "Write", "hooks": [{"type": "command", "command": "prettier --write"}]}],
    "Stop": [
      {"hooks": [{"type": "command", "command": "notify-me stopped"}, {"type": "command", "command": "`+strings.ReplaceAll(SentinelHookCommand, `"`, `\"`)+`"}]},
      {"hooks": [{"type": "command", "command": "echo another stop hook"}]}
    ]
  }
}`)

	if added, err := EnsureSentinelHook(dir); err != nil || !added {
		t.Fatalf("EnsureSentinelHook = %v, %v", added, err)
	}
	settings := readSettings(t, dir)
	want := []string{"notify-me stopped", SentinelHookCommand, "echo another stop hook"}
	got := stopCommands(settings)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("Stop commands = %q, want %q", got, want)
	}
	if settings["model"] != "sonnet" {
		t.Errorf("model setting lost: %v", settings["model"])
	}
	if post, _ := settings["hooks"].(map[string]any)["PostToolUse"].([]any); len(post) != 1 {
		t.Errorf("PostToolUse hooks lost: %v", settings["hooks"])
	}
}

func TestEnsureSentinelHook_AddsAlongsideForeignStopHook(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, dir, `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"notify-me stopped"}]}]}}`)

	if added, err := EnsureSentinelHook(dir); err != nil || !added {
		t.Fatalf("EnsureSentinelHook = %v, %v", added, err)
	}
	got := stopCommands(readSettings(t, dir))
	if len(got) != 2 || got[0] != "notify-me stopped" || got[1] != SentinelHookCommand {
		t.Errorf("Stop commands = %q", got)
	}
}

func TestEnsureSentinelHook_WritesShellCharactersUnescaped(t *testing.T) {
	dir := t.TempDir()
	if _, err := EnsureSentinelHook(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `\u00`) {
		t.Errorf("settings.json should hold \">&\" and \"&&\" as written, not as unicode escapes:\n%s", data)
	}
	if !strings.HasSuffix(string(data), "}\n") {
		t.Errorf("settings.json should end with a single newline: %q", data[len(data)-3:])
	}
}

func TestInspectSentinelHook(t *testing.T) {
	dir := t.TempDir()
	if state, err := InspectSentinelHook(dir); err != nil || state.Present {
		t.Errorf("no settings file: %+v, %v", state, err)
	}
	writeSettings(t, dir, `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"notify-me stopped"}]}]}}`)
	if state, err := InspectSentinelHook(dir); err != nil || state.Present {
		t.Errorf("only a foreign hook: %+v, %v", state, err)
	}
	writeSettings(t, dir, `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"`+strings.ReplaceAll(SentinelHookCommand, `"`, `\"`)+`"}]}]}}`)
	if state, err := InspectSentinelHook(dir); err != nil || !state.Present || state.Current {
		t.Errorf("current command without asyncRewake must not count as current: %+v, %v", state, err)
	}
	writeSettings(t, dir, "{not json")
	if _, err := InspectSentinelHook(dir); err == nil {
		t.Error("expected an error for invalid JSON")
	}
}

func TestEnsureSentinelHook_RefusesInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	settingsDir := filepath.Join(dir, ".claude")
	os.MkdirAll(settingsDir, 0o755)
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("writing invalid settings.json: %v", err)
	}

	if _, err := EnsureSentinelHook(dir); err == nil {
		t.Error("expected an error for invalid existing JSON")
	}
}

func TestSentinelHookNeeded(t *testing.T) {
	dir := t.TempDir()
	need, err := SentinelHookNeeded(dir)
	if err != nil || !need {
		t.Fatalf("fresh project: need=%v err=%v", need, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude")); !os.IsNotExist(err) {
		t.Error("SentinelHookNeeded must not write anything")
	}
	if _, err := EnsureSentinelHook(dir); err != nil {
		t.Fatal(err)
	}
	if need, err := SentinelHookNeeded(dir); err != nil || need {
		t.Errorf("after ensure: need=%v err=%v", need, err)
	}
}
