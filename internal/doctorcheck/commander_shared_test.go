package doctorcheck

import (
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/commanderreg"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/project"
)

// registerCommanders registers one commander per agent in a fresh project and
// returns the project directory. The agents are the ones herdr (the fake) knows.
func registerCommanders(t *testing.T, home string, agents func(dir string) []herdr.AgentInfo) (string, fakeLocator) {
	t.Helper()
	dir := t.TempDir()
	h := fakeLocator{agents: agents(dir)}
	root, err := project.Root(home, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range h.agents {
		if _, err := commanderreg.Register(root, dir, a.PaneID, h, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	return dir, h
}

func TestSharedWakeQueue_WarnsWithTwoLiveCommanders(t *testing.T) {
	home := t.TempDir()
	dir, h := registerCommanders(t, home, func(dir string) []herdr.AgentInfo {
		return []herdr.AgentInfo{
			{Kind: "claude", PaneID: "w1:p1", Cwd: dir},
			{Kind: "pi", PaneID: "w1:p2", Cwd: dir},
		}
	})

	got := sharedWakeQueue(dir, home, h, nil)
	if len(got) != 1 || !got[0].Warn {
		t.Fatalf("expected one warning, got %+v", got)
	}
	if !strings.Contains(got[0].Detail, "claude, pi") || !strings.Contains(got[0].Detail, "2 commanders") {
		t.Errorf("the warning should name both harnesses, got: %s", got[0].Detail)
	}
}

func TestSharedWakeQueue_QuietWithOneCommander(t *testing.T) {
	home := t.TempDir()
	dir, h := registerCommanders(t, home, func(dir string) []herdr.AgentInfo {
		return []herdr.AgentInfo{{Kind: "claude", PaneID: "w1:p1", Cwd: dir}}
	})
	if got := sharedWakeQueue(dir, home, h, nil); len(got) != 0 {
		t.Errorf("one commander should not warn, got %+v", got)
	}
}

// A registry entry whose pane is gone is not a commander: the registry never
// deletes entries, so counting them would warn forever after a pane closed.
func TestSharedWakeQueue_QuietWhenTheOtherCommanderIsGone(t *testing.T) {
	home := t.TempDir()
	dir, h := registerCommanders(t, home, func(dir string) []herdr.AgentInfo {
		return []herdr.AgentInfo{
			{Kind: "claude", PaneID: "w1:p1", Cwd: dir},
			{Kind: "pi", PaneID: "w1:p2", Cwd: dir},
		}
	})
	h.agents = h.agents[:1]
	if got := sharedWakeQueue(dir, home, h, nil); len(got) != 0 {
		t.Errorf("a closed pane should not count, got %+v", got)
	}
}

func TestSharedWakeQueue_QuietWithoutARegistry(t *testing.T) {
	if got := sharedWakeQueue(t.TempDir(), t.TempDir(), fakeLocator{}, nil); len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}
