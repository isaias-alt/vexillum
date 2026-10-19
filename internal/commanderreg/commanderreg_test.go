package commanderreg_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/commanderreg"
	"github.com/isaias-alt/vexillum/internal/herdr"
)

// fakeLocator is a herdr that knows a fixed set of agents and counts its calls.
type fakeLocator struct {
	mu       sync.Mutex
	agents   []herdr.AgentInfo
	getErr   error
	listErr  error
	gets     []string
	listings int
}

func (f *fakeLocator) AgentGet(target string) (herdr.AgentInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets = append(f.gets, target)
	if f.getErr != nil {
		return herdr.AgentInfo{}, f.getErr
	}
	for _, a := range f.agents {
		if a.PaneID == target || (a.Name != "" && a.Name == target) {
			return a, nil
		}
	}
	return herdr.AgentInfo{}, &herdr.APIError{Code: "agent_not_found", Message: "agent target " + target + " not found"}
}

func (f *fakeLocator) AgentList() ([]herdr.AgentInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listings++
	return f.agents, f.listErr
}

var now = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func agent(kind, pane, cwd string) herdr.AgentInfo {
	return herdr.AgentInfo{Kind: kind, PaneID: pane, Cwd: cwd}
}

func panes(live []commanderreg.Commander) string {
	var out []string
	for _, c := range live {
		s := c.Harness + "=" + c.PaneID
		if c.Recomputed {
			s += "*"
		}
		out = append(out, s)
	}
	return strings.Join(out, ",")
}

func TestRegister_RecordsThePaneUnderTheHarnessHerdrReports(t *testing.T) {
	root, proj := t.TempDir(), t.TempDir()
	h := &fakeLocator{agents: []herdr.AgentInfo{agent("pi", "w1:p3", proj)}}

	kind, err := commanderreg.Register(root, proj, "w1:p3", h, now)
	if err != nil || kind != "pi" {
		t.Fatalf("Register = (%q, %v), want (pi, nil)", kind, err)
	}
	entries, err := commanderreg.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	got := entries["pi"]
	if got.PaneID != "w1:p3" || got.Dir != proj || !got.RegisteredAt.Equal(now) {
		t.Errorf("entry = %+v", got)
	}
	raw, _ := os.ReadFile(commanderreg.Path(root))
	if !strings.Contains(string(raw), `"schema_version": 0`) {
		t.Errorf("registry file lacks its schema version:\n%s", raw)
	}
	if left, _ := filepath.Glob(filepath.Join(root, "*.tmp")); len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func TestRegister_SeveralKindsCoexistAndReregisteringOverwritesOnlyItsKind(t *testing.T) {
	root, proj := t.TempDir(), t.TempDir()
	h := &fakeLocator{agents: []herdr.AgentInfo{
		agent("claude", "w1:p1", proj),
		agent("pi", "w1:p2", proj),
		agent("pi", "w1:p9", proj),
	}}
	for _, pane := range []string{"w1:p1", "w1:p2"} {
		if _, err := commanderreg.Register(root, proj, pane, h, now); err != nil {
			t.Fatal(err)
		}
	}
	later := now.Add(time.Hour)
	if _, err := commanderreg.Register(root, proj, "w1:p9", h, later); err != nil {
		t.Fatal(err)
	}
	entries, _ := commanderreg.Load(root)
	if len(entries) != 2 {
		t.Fatalf("want one entry per kind, got %+v", entries)
	}
	if entries["pi"].PaneID != "w1:p9" || !entries["pi"].RegisteredAt.Equal(later) {
		t.Errorf("pi was not overwritten: %+v", entries["pi"])
	}
	if entries["claude"].PaneID != "w1:p1" || !entries["claude"].RegisteredAt.Equal(now) {
		t.Errorf("claude changed when pi re-registered: %+v", entries["claude"])
	}
}

func TestRegister_RefusesWhatIsNotARegistrableCommander(t *testing.T) {
	root, proj := t.TempDir(), t.TempDir()
	h := &fakeLocator{agents: []herdr.AgentInfo{agent("", "w1:p5", proj)}}

	if _, err := commanderreg.Register(root, proj, "", h, now); !errors.Is(err, commanderreg.ErrNoPane) {
		t.Errorf("empty pane: %v, want ErrNoPane", err)
	}
	if _, err := commanderreg.Register(root, proj, "w1:p404", h, now); !errors.Is(err, commanderreg.ErrNotAnAgent) {
		t.Errorf("unknown pane: %v, want ErrNotAnAgent", err)
	}
	if _, err := commanderreg.Register(root, proj, "w1:p5", h, now); !errors.Is(err, commanderreg.ErrNotAnAgent) {
		t.Errorf("agent without a kind: %v, want ErrNotAnAgent", err)
	}
	h.getErr = errors.New("herdr is down")
	if _, err := commanderreg.Register(root, proj, "w1:p5", h, now); err == nil || errors.Is(err, commanderreg.ErrNotAnAgent) {
		t.Errorf("a herdr failure must stay a plain error, got %v", err)
	}
	if _, err := os.Stat(commanderreg.Path(root)); !os.IsNotExist(err) {
		t.Errorf("a refused registration must not write the registry (stat: %v)", err)
	}
}

func TestRegister_ConcurrentKindsLoseNothing(t *testing.T) {
	root, proj := t.TempDir(), t.TempDir()
	h := &fakeLocator{agents: []herdr.AgentInfo{
		agent("claude", "w1:p1", proj), agent("pi", "w1:p2", proj), agent("opencode", "w1:p3", proj),
	}}
	var wg sync.WaitGroup
	for _, pane := range []string{"w1:p1", "w1:p2", "w1:p3"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := commanderreg.Register(root, proj, pane, h, now); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if entries, _ := commanderreg.Load(root); len(entries) != 3 {
		t.Errorf("a concurrent registration was lost: %+v", entries)
	}
}

func TestLoad_AFutureSchemaIsRefused(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(commanderreg.Path(root), []byte(`{"schema_version": 7, "commanders": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := commanderreg.Load(root); err == nil || !strings.Contains(err.Error(), "schema version 7") {
		t.Errorf("Load = %v, want a schema version error", err)
	}
}

func TestResolve_NoRegistryCostsNoHerdrCall(t *testing.T) {
	h := &fakeLocator{}
	live, err := commanderreg.Resolve(t.TempDir(), "/home/.vexillum", h)
	if err != nil || len(live) != 0 {
		t.Errorf("Resolve = (%v, %v)", live, err)
	}
	if len(h.gets) != 0 || h.listings != 0 {
		t.Errorf("herdr was asked without a registry: gets=%v listings=%d", h.gets, h.listings)
	}
}

func TestResolve_ALivePaneIsReturnedAsRegistered(t *testing.T) {
	root, proj := t.TempDir(), t.TempDir()
	h := &fakeLocator{agents: []herdr.AgentInfo{agent("claude", "w1:p1", filepath.Join(proj, "sub")), agent("opencode", "w1:p2", proj)}}
	for _, pane := range []string{"w1:p1", "w1:p2"} {
		if _, err := commanderreg.Register(root, proj, pane, h, now); err != nil {
			t.Fatal(err)
		}
	}
	live, err := commanderreg.Resolve(root, "/home/.vexillum", h)
	if err != nil {
		t.Fatal(err)
	}
	if got := panes(live); got != "claude=w1:p1,opencode=w1:p2" {
		t.Errorf("live = %s", got)
	}
	if h.listings != 0 {
		t.Errorf("a live registry must not list agents (%d listings)", h.listings)
	}
}

func TestResolve_AStalePaneFallsBackToTheAgentListByDirectory(t *testing.T) {
	root, proj, other := t.TempDir(), t.TempDir(), t.TempDir()
	h := &fakeLocator{agents: []herdr.AgentInfo{agent("pi", "w1:p2", proj)}}
	if _, err := commanderreg.Register(root, proj, "w1:p2", h, now); err != nil {
		t.Fatal(err)
	}

	// herdr restarted: the pane got a new id, and the old id now names another
	// project's agent.
	focused := agent("pi", "w2:p8", proj)
	focused.Focused = true
	h.agents = []herdr.AgentInfo{agent("pi", "w2:p4", other), agent("pi", "w2:p7", proj), focused}
	renumbered := &renumbered{fakeLocator: h, stale: "w1:p2", reusedBy: agent("pi", "w1:p2", other)}

	live, err := commanderreg.Resolve(root, "/home/.vexillum", renumbered)
	if err != nil {
		t.Fatal(err)
	}
	if got := panes(live); got != "pi=w2:p8*" {
		t.Errorf("live = %s, want the focused pi working in the project, recomputed", got)
	}
	entries, _ := commanderreg.Load(root)
	if entries["pi"].PaneID != "w1:p2" {
		t.Errorf("Resolve must not rewrite the registry, entry = %+v", entries["pi"])
	}
}

// renumbered answers the stale pane id with another project's agent, the way
// herdr does once it reuses an id.
type renumbered struct {
	*fakeLocator
	stale    string
	reusedBy herdr.AgentInfo
}

func (r *renumbered) AgentGet(target string) (herdr.AgentInfo, error) {
	if target == r.stale {
		return r.reusedBy, nil
	}
	return r.fakeLocator.AgentGet(target)
}

func TestResolve_AStalePaneWithNoReplacementIsAbsent(t *testing.T) {
	root, proj, home := t.TempDir(), t.TempDir(), t.TempDir()
	h := &fakeLocator{agents: []herdr.AgentInfo{agent("opencode", "w1:p2", proj)}}
	if _, err := commanderreg.Register(root, proj, "w1:p2", h, now); err != nil {
		t.Fatal(err)
	}
	camp := filepath.Join(home, "projects", "x", "camps", "1", "repo")
	h.agents = []herdr.AgentInfo{
		agent("opencode", "w3:p1", camp),               // a soldier's camp
		agent("claude", "w3:p2", proj),                 // another harness
		agent("opencode", "w3:p3", filepath.Dir(proj)), // the parent directory
		agent("opencode", "w3:p4", t.TempDir()),        // another project
		{Kind: "opencode", PaneID: "w3:p5"},            // no directory at all
	}
	if commanderreg.Live(root, home, h) {
		t.Error("an entry whose pane is gone and that no agent replaces must be absent")
	}
}

func TestResolve_ASoldierCampUnderTheHomeIsNeverTheFallback(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	proj := filepath.Dir(home) // the project contains the home: the worst case
	h := &fakeLocator{agents: []herdr.AgentInfo{agent("pi", "w1:p2", proj)}}
	if _, err := commanderreg.Register(root, proj, "w1:p2", h, now); err != nil {
		t.Fatal(err)
	}
	h.agents = []herdr.AgentInfo{agent("pi", "w3:p1", filepath.Join(home, "projects", "x", "camps", "1"))}
	if commanderreg.Live(root, home, h) {
		t.Error("a soldier's camp was taken for the commander")
	}
}

func TestResolve_HerdrFailingIsReportedAndTheCommanderIsAbsent(t *testing.T) {
	root, proj := t.TempDir(), t.TempDir()
	h := &fakeLocator{agents: []herdr.AgentInfo{agent("claude", "w1:p1", proj)}}
	if _, err := commanderreg.Register(root, proj, "w1:p1", h, now); err != nil {
		t.Fatal(err)
	}
	h.getErr = errors.New("herdr is down")
	h.listErr = errors.New("herdr is down")
	live, err := commanderreg.Resolve(root, "/home/.vexillum", h)
	if len(live) != 0 || err == nil {
		t.Errorf("Resolve = (%v, %v), want no commander and an error", live, err)
	}
	if entries, _ := commanderreg.Load(root); entries["claude"].PaneID != "w1:p1" {
		t.Error("a herdr failure must not drop the entry")
	}
}
