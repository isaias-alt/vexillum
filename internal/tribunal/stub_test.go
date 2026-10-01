package tribunal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// claudeStub is a scripted fake "claude" on a minimal PATH (real git, cat,
// sleep and nothing else). Invocation N records its argv to args.N and its
// prompt (the -p value) to prompt.N, then runs hook.N inside the camp (so a
// "fixer" can edit files) if present, optionally sleeps instead of
// answering (sleep.N), and prints out.N if present (else out.last).
type claudeStub struct {
	t   *testing.T
	dir string
}

func newClaudeStub(t *testing.T) *claudeStub {
	t.Helper()
	dir := gitOnlyPath(t)
	for _, tool := range []string{"cat", "sleep"} {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("%s not found on PATH: %v", tool, err)
		}
		if err := os.Symlink(real, filepath.Join(dir, tool)); err != nil {
			t.Fatalf("linking real %s: %v", tool, err)
		}
	}
	data := t.TempDir()
	script := `#!/bin/sh
d='` + data + `'
n=$(cat "$d/count" 2>/dev/null || echo 0)
n=$((n+1))
echo $n > "$d/count"
printf '%s\n' "$@" > "$d/args.$n"
printf '%s' "$2" > "$d/prompt.$n"
[ -f "$d/sleep.$n" ] && exec sleep 30
[ -f "$d/hook.$n" ] && . "$d/hook.$n"
if [ -f "$d/out.$n" ]; then cat "$d/out.$n"; elif [ -f "$d/out.last" ]; then cat "$d/out.last"; fi
exit 0
`
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatalf("writing claude stub: %v", err)
	}
	// The fixer's commit needs an identity.
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	t.Setenv("PATH", dir)
	return &claudeStub{t: t, dir: data}
}

func (c *claudeStub) write(name, content string) {
	c.t.Helper()
	if err := os.WriteFile(filepath.Join(c.dir, name), []byte(content), 0o644); err != nil {
		c.t.Fatalf("writing %s: %v", name, err)
	}
}

// out sets what invocation n prints; n=0 sets the default for every call.
func (c *claudeStub) out(n int, output string) {
	if n == 0 {
		c.write("out.last", output)
		return
	}
	c.write("out."+strconv.Itoa(n), output)
}

// hook sets a shell snippet invocation n runs in its cwd (the camp).
func (c *claudeStub) hook(n int, snippet string) { c.write("hook."+strconv.Itoa(n), snippet) }

// sleepOn makes invocation n hang instead of answering.
func (c *claudeStub) sleepOn(n int) { c.write("sleep."+strconv.Itoa(n), "") }

func (c *claudeStub) calls() int {
	b, err := os.ReadFile(filepath.Join(c.dir, "count"))
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

func (c *claudeStub) read(prefix string, n int) string {
	c.t.Helper()
	b, err := os.ReadFile(filepath.Join(c.dir, prefix+"."+strconv.Itoa(n)))
	if err != nil {
		c.t.Fatalf("no %s recorded for call %d: %v", prefix, n, err)
	}
	return string(b)
}

func (c *claudeStub) prompt(n int) string { return c.read("prompt", n) }
func (c *claudeStub) args(n int) string   { return c.read("args", n) }

// reportJSON renders a reviewer answer: prose, then the JSON object.
func reportJSON(findings string, paths ...string) string {
	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = strconv.Quote(p)
	}
	return `Reviewed the change.
{"findings": [` + findings + `], "reviewed_paths": [` + strings.Join(quoted, ",") + `], "risk_level": "low", "risk_rationale": "small change"}`
}

// finding renders one finding object.
func finding(severity, action, file string) string {
	return `{"file": "` + file + `", "line": 3, "severity": "` + severity + `", "action": "` + action +
		`", "description": "a defect", "failure_scenario": "input X yields Y", "sibling_sites": []}`
}
