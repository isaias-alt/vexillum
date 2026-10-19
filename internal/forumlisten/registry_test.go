package forumlisten_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/commanderreg"
	"github.com/isaias-alt/vexillum/internal/forum"
	"github.com/isaias-alt/vexillum/internal/forumlisten"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/sentinel"
)

// countingHerdr is a herdr that knows a fixed set of agents and counts how
// often the listener asked it anything.
type countingHerdr struct {
	mu     sync.Mutex
	agents []herdr.AgentInfo
	calls  int
}

func (c *countingHerdr) AgentGet(target string) (herdr.AgentInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	for _, a := range c.agents {
		if a.PaneID == target {
			return a, nil
		}
	}
	return herdr.AgentInfo{}, &herdr.APIError{Code: "agent_not_found", Message: "not found"}
}

func (c *countingHerdr) AgentList() ([]herdr.AgentInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.agents, nil
}

func (c *countingHerdr) set(agents ...herdr.AgentInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.agents = agents
}

func (c *countingHerdr) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// TestMain lets the test binary stand in for a long-running "vx sentinel
// await" (see liveAwait): started with VX_FAKE_AWAIT set it only waits.
func TestMain(m *testing.M) {
	if os.Getenv("VX_FAKE_AWAIT") != "" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// liveAwait records a real "vx sentinel await" process of the project, owned by
// this test, so sentinel.HasLiveAwaiter verifies it through ps exactly as it
// does for a Stop hook. The process is the test binary under the name vx.
func liveAwait(t *testing.T, home, root string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	vx := filepath.Join(t.TempDir(), cmdname.Name)
	if err := os.Symlink(exe, vx); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(vx, "sentinel", "await")
	cmd.Env = append(os.Environ(), "VX_FAKE_AWAIT=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	dir := filepath.Join(home, "sentinel-awaiters")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	data, _ := json.Marshal(sentinel.AwaiterRecord{PID: pid, OwnerPID: os.Getpid(), ProjectRoot: root})
	if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(pid)+".json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if !sentinel.HasLiveAwaiter(home, root) {
		t.Skip("this platform's ps does not show the fake await as a vx process")
	}
}

// relayedAs runs the listener on a fresh session with the default commander
// check (opts.Commander left nil) and returns what the server was told about
// the commander, with the notice the browser shows.
func relayedAs(t *testing.T, e *env, opts forumlisten.Options) string {
	t.Helper()
	key, _ := e.session(e.root)
	e.send(key, "anyone there?", false)
	ctx, cancel := context.WithCancel(context.Background())
	done := e.run(ctx, opts)
	e.eventually("the relayed state", func() bool { return e.snap(key).Relayed != nil })
	snap := e.snap(key)
	var notices []string
	for _, m := range snap.Transcript {
		if m.Kind == forum.MessageKindNotice {
			notices = append(notices, m.Text)
		}
	}
	cancel()
	if err := e.wait(done); err != nil {
		t.Fatal(err)
	}
	return snap.Relayed.Commander + "|" + strings.Join(notices, "|")
}

func defaultOptions(e *env, h herdr.Locator) forumlisten.Options {
	opts := e.options()
	opts.Commander = nil
	opts.Herdr = h
	return opts
}

// A Claude commander is its live await. The default check must answer exactly
// what sentinel.HasLiveAwaiter alone answered before the registry existed,
// ask herdr nothing, and leave the relayed state and notice byte-identical.
func TestDefaultCommander_ClaudeOnlyBehaviorIsUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name      string
		withAwait bool
		want      string
	}{
		{"live await", true, forum.CommanderConnected + "|" + forumlisten.Notice},
		{"no await", false, forum.CommanderNone + "|" + forumlisten.Notice},
	} {
		t.Run(tc.name, func(t *testing.T) {
			legacy := newEnv(t)
			current := newEnv(t)
			if tc.withAwait {
				liveAwait(t, legacy.home, legacy.root)
				liveAwait(t, current.home, current.root)
			}

			legacyOpts := legacy.options()
			legacyOpts.Commander = func(root string) bool { return sentinel.HasLiveAwaiter(legacy.home, root) }
			want := relayedAs(t, legacy, legacyOpts)

			herdrSpy := &countingHerdr{agents: []herdr.AgentInfo{{Kind: "claude", PaneID: "w1:p1", Cwd: current.root}}}
			got := relayedAs(t, current, defaultOptions(current, herdrSpy))

			if got != want || got != tc.want {
				t.Errorf("relayed = %q, legacy = %q, want %q", got, want, tc.want)
			}
			if n := herdrSpy.count(); n != 0 {
				t.Errorf("herdr was asked %d times for a project with no registry", n)
			}
		})
	}
}

func TestDefaultCommander_ALiveAwaitSettlesItWithoutAskingHerdr(t *testing.T) {
	e := newEnv(t)
	proj := t.TempDir()
	h := &countingHerdr{agents: []herdr.AgentInfo{{Kind: "opencode", PaneID: "w1:p2", Cwd: proj}}}
	if _, err := commanderreg.Register(e.root, proj, "w1:p2", h, time.Now()); err != nil {
		t.Fatal(err)
	}
	liveAwait(t, e.home, e.root)
	before := h.count()

	if got := relayedAs(t, e, defaultOptions(e, h)); !strings.HasPrefix(got, forum.CommanderConnected+"|") {
		t.Errorf("relayed = %q, want connected", got)
	}
	if h.count() != before {
		t.Errorf("herdr was asked although an await was alive (%d calls, %d before)", h.count(), before)
	}
}

// A Pi or OpenCode commander has no await to find: a live registry entry is
// what tells the browser a commander is there.
func TestDefaultCommander_ALiveRegistryEntryCounts(t *testing.T) {
	e := newEnv(t)
	proj := t.TempDir()
	h := &countingHerdr{agents: []herdr.AgentInfo{{Kind: "opencode", PaneID: "w1:p2", Cwd: proj}}}
	if _, err := commanderreg.Register(e.root, proj, "w1:p2", h, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := relayedAs(t, e, defaultOptions(e, h)); !strings.HasPrefix(got, forum.CommanderConnected+"|") {
		t.Errorf("relayed = %q, want connected for a live registered commander", got)
	}
}

func TestDefaultCommander_AStaleRegistryEntryIsNoCommander(t *testing.T) {
	e := newEnv(t)
	proj := t.TempDir()
	h := &countingHerdr{agents: []herdr.AgentInfo{{Kind: "pi", PaneID: "w1:p2", Cwd: proj}}}
	if _, err := commanderreg.Register(e.root, proj, "w1:p2", h, time.Now()); err != nil {
		t.Fatal(err)
	}
	h.set() // the pane is gone and nothing replaced it

	if got := relayedAs(t, e, defaultOptions(e, h)); !strings.HasPrefix(got, forum.CommanderNone+"|") {
		t.Errorf("relayed = %q, want none for a dead registered commander", got)
	}
}

// The registry is read at every refresh, so a commander that registers (or
// dies) while a round is open flips the browser's status without a new prompt.
func TestDefaultCommander_FollowsTheRegistryWhileARoundIsOpen(t *testing.T) {
	e := newEnv(t)
	proj := t.TempDir()
	h := &countingHerdr{agents: []herdr.AgentInfo{{Kind: "opencode", PaneID: "w1:p2", Cwd: proj}}}
	key, _ := e.session(e.root)
	e.send(key, "hello?", false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := e.run(ctx, defaultOptions(e, h))
	status := func() string {
		if r := e.snap(key).Relayed; r != nil {
			return r.Commander
		}
		return ""
	}
	e.eventually("relayed with no commander", func() bool { return status() == forum.CommanderNone })
	if _, err := commanderreg.Register(e.root, proj, "w1:p2", h, time.Now()); err != nil {
		t.Fatal(err)
	}
	e.eventually("the commander registering", func() bool { return status() == forum.CommanderConnected })
	h.set()
	e.eventually("the commander going away", func() bool { return status() == forum.CommanderNone })
	cancel()
	if err := e.wait(done); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
