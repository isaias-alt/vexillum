package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/commanderreg"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/scaffold"
)

// stubLocator answers "herdr agent get" for a fixed set of panes.
type stubLocator struct {
	agents map[string]herdr.AgentInfo
	err    error
	gets   int
}

func (s *stubLocator) AgentGet(target string) (herdr.AgentInfo, error) {
	s.gets++
	if s.err != nil {
		return herdr.AgentInfo{}, s.err
	}
	if a, ok := s.agents[target]; ok {
		return a, nil
	}
	return herdr.AgentInfo{}, &herdr.APIError{Code: "agent_not_found", Message: "not found"}
}

func (s *stubLocator) AgentList() ([]herdr.AgentInfo, error) {
	var out []herdr.AgentInfo
	for _, a := range s.agents {
		out = append(out, a)
	}
	return out, nil
}

// useLocator installs a locator for the test and the pane the command runs in.
func useLocator(t *testing.T, pane string, l herdr.Locator) {
	t.Helper()
	prev := commanderLocator
	commanderLocator = l
	t.Cleanup(func() { commanderLocator = prev })
	t.Setenv("HERDR_PANE_ID", pane)
}

func TestRegisterCommander_RecordsThePaneOfTheCommand(t *testing.T) {
	home := t.TempDir()
	root := chdirProject(t, home)
	cwd, _ := os.Getwd()
	loc := &stubLocator{agents: map[string]herdr.AgentInfo{"w1:p1": {Kind: "pi", PaneID: "w1:p1", Cwd: cwd}}}
	useLocator(t, "w1:p1", loc)

	var errb bytes.Buffer
	registerCommander(root, cwd, &errb)

	entries, err := commanderreg.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if e := entries["pi"]; e.PaneID != "w1:p1" || e.Dir == "" {
		t.Errorf("entries = %+v", entries)
	}
	if errb.Len() != 0 {
		t.Errorf("a registration prints nothing, got %q", errb.String())
	}
}

func TestRegisterCommander_RegistersTheRepositoryRootFromASubdirectory(t *testing.T) {
	home := t.TempDir()
	root := chdirProject(t, home)
	top, _ := os.Getwd()
	sub := filepath.Join(top, "pkg", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	loc := &stubLocator{agents: map[string]herdr.AgentInfo{"w1:p1": {Kind: "claude", PaneID: "w1:p1", Cwd: top}}}
	useLocator(t, "w1:p1", loc)

	registerCommander(root, sub, &bytes.Buffer{})

	entries, _ := commanderreg.Load(root)
	resolvedTop, _ := filepath.EvalSymlinks(top)
	if got, _ := filepath.EvalSymlinks(entries["claude"].Dir); got != resolvedTop {
		t.Errorf("registered dir = %q, want the repository root %q", entries["claude"].Dir, resolvedTop)
	}
	if live, _ := commanderreg.Resolve(root, home, loc); len(live) != 1 {
		t.Errorf("a commander started at the root and registered from a subdirectory must stay live: %v", live)
	}
}

func TestRegisterCommander_SilentWhenThereIsNothingToRegister(t *testing.T) {
	home := t.TempDir()
	root := chdirProject(t, home)
	cwd, _ := os.Getwd()

	for name, setup := range map[string]func(){
		"outside herdr":        func() { useLocator(t, "", &stubLocator{}) },
		"a pane with no agent": func() { useLocator(t, "w1:p9", &stubLocator{}) },
	} {
		setup()
		var errb bytes.Buffer
		registerCommander(root, cwd, &errb)
		if errb.Len() != 0 {
			t.Errorf("%s: stderr %q, want silence", name, errb.String())
		}
		if _, err := os.Stat(commanderreg.Path(root)); !os.IsNotExist(err) {
			t.Errorf("%s: a registry was written (stat: %v)", name, err)
		}
	}
}

func TestRegisterCommander_AHerdrFailureIsAWarning(t *testing.T) {
	home := t.TempDir()
	root := chdirProject(t, home)
	cwd, _ := os.Getwd()
	useLocator(t, "w1:p1", &stubLocator{err: errors.New("herdr is down")})

	var errb bytes.Buffer
	registerCommander(root, cwd, &errb)
	if !strings.Contains(errb.String(), "warning: could not register this commander") || !strings.Contains(errb.String(), "herdr is down") {
		t.Errorf("stderr = %q", errb.String())
	}
}

func TestRegisterDispatchCommander_UsesTheDispatchProjectRootAndSkipsUninitializedProjects(t *testing.T) {
	home := t.TempDir()
	vhome := filepath.Join(home, ".vexillum")
	chdirProject(t, vhome)
	cwd, _ := os.Getwd()
	root, err := project.Root(vhome, cwd)
	if err != nil {
		t.Fatal(err)
	}
	loc := &stubLocator{agents: map[string]herdr.AgentInfo{"w1:p1": {Kind: "opencode", PaneID: "w1:p1", Cwd: cwd}}}
	useLocator(t, "w1:p1", loc)

	registerDispatchCommander(cwd, vhome)
	if loc.gets != 0 {
		t.Fatal("an uninitialized project must register nothing")
	}

	if err := scaffold.WriteConfig(filepath.Join(cwd, ".vexillum")); err != nil {
		t.Fatal(err)
	}
	registerDispatchCommander(cwd, vhome)
	if entries, _ := commanderreg.Load(root); entries["opencode"].PaneID != "w1:p1" {
		t.Errorf("dispatch did not register the commander under %s: %+v", root, entries)
	}
}

func TestForumOpen_RegistersTheCommanderBeforeTheListenerStarts(t *testing.T) {
	inProcessSpawn(t)
	home := t.TempDir()
	root := chdirProject(t, home)
	cwd, _ := os.Getwd()
	loc := &stubLocator{agents: map[string]herdr.AgentInfo{"w1:p4": {Kind: "opencode", PaneID: "w1:p4", Cwd: cwd}}}
	useLocator(t, "w1:p4", loc)
	var seenAtListenerStart bool
	ensureForumListener = func(string) error {
		entries, _ := commanderreg.Load(root)
		seenAtListenerStart = entries["opencode"].PaneID == "w1:p4"
		return nil
	}

	var out, errb bytes.Buffer
	if code := runForumOpen(context.Background(), home, []string{writeArtifact(t), "--no-open"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !seenAtListenerStart {
		t.Error("the commander was not registered by the time the listener was ensured")
	}
}

func TestForumOpen_OutsideAProjectRegistersNothing(t *testing.T) {
	inProcessSpawn(t)
	t.Chdir(t.TempDir())
	loc := &stubLocator{agents: map[string]herdr.AgentInfo{"w1:p4": {Kind: "pi", PaneID: "w1:p4"}}}
	useLocator(t, "w1:p4", loc)

	var out, errb bytes.Buffer
	if code := runForumOpen(context.Background(), t.TempDir(), []string{writeArtifact(t), "--no-open"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if loc.gets != 0 {
		t.Error("a session with no project has no registry to write")
	}
}
