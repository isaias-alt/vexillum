package cli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/commanderreg"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/project"
)

// herdrWithAgents is a fake herdr that answers "agent get <pane>" for the
// panes of agents (kind by pane id) and "agent not found" for any other pane.
func herdrWithAgents(t *testing.T, dir, cwd string, kinds map[string]string) {
	t.Helper()
	var cases strings.Builder
	for pane, kind := range kinds {
		fmt.Fprintf(&cases, "  %s) echo '{\"id\":\"1\",\"result\":{\"agent\":{\"agent\":\"%s\",\"pane_id\":\"%s\",\"cwd\":\"%s\"}}}';;\n", pane, kind, pane, cwd)
	}
	writeStub(t, dir, "herdr", `case "$1" in
--version) echo "herdr 0.9.5";;
agent)
case "$3" in
`+cases.String()+`  *) echo '{"id":"1","error":{"code":"agent_not_found","message":"no such agent"}}' >&2; exit 1;;
esac;;
esac`)
}

// Two commanders registered and live in one project share one wake queue:
// doctor says so and stays ready (a warning, never a failure).
func TestDoctor_WarnsWhenTwoCommandersShareTheWakeQueue(t *testing.T) {
	bin := fakeBinDir(t, "claude")
	project1 := initializedProject(t)
	herdrWithAgents(t, bin, project1, map[string]string{"w1:p1": "claude", "w1:p2": "pi"})
	t.Setenv("PATH", bin)

	vexillumHome := t.TempDir()
	root, err := project.Root(vexillumHome, project1)
	if err != nil {
		t.Fatal(err)
	}
	for _, pane := range []string{"w1:p1", "w1:p2"} {
		if _, err := commanderreg.Register(root, project1, pane, herdr.CLI{}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}

	var out bytes.Buffer
	if code := runDoctor(project1, vexillumHome, t.TempDir(), &out); code != 0 {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "[warn] commanders share one wake queue - 2 commanders are live in this project (claude, pi)") {
		t.Errorf("expected the shared wake queue warning, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Environment ready") || !strings.Contains(out.String(), "warnings: sentinel Stop hook, commanders share one wake queue") {
		t.Errorf("expected a ready summary naming the warning, got:\n%s", out.String())
	}
}

// One live commander prints no line about the queue.
func TestDoctor_NoSharedWakeQueueLineForOneCommander(t *testing.T) {
	bin := fakeBinDir(t, "claude")
	project1 := initializedProject(t)
	herdrWithAgents(t, bin, project1, map[string]string{"w1:p1": "claude"})
	t.Setenv("PATH", bin)

	vexillumHome := t.TempDir()
	root, err := project.Root(vexillumHome, project1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := commanderreg.Register(root, project1, "w1:p1", herdr.CLI{}, time.Now()); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if code := runDoctor(project1, vexillumHome, t.TempDir(), &out); code != 0 {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
	if strings.Contains(out.String(), "wake queue") {
		t.Errorf("one commander should print no wake queue line, got:\n%s", out.String())
	}
}
