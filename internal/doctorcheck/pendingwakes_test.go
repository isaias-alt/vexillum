package doctorcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/commanderreg"
	"github.com/isaias-alt/vexillum/internal/herdr"
)

// waitingWake puts a wake file in the queue of a project under home, last
// written age ago, and returns the project's root.
func waitingWake(t *testing.T, home, name string, age time.Duration) string {
	t.Helper()
	root := filepath.Join(home, "projects", name)
	dir := filepath.Join(root, "wakes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "abc123.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-age)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
	return root
}

func noAwaiter(string, string) bool { return false }

func TestPendingWakes_WarnsAboutAQueueNobodyListensTo(t *testing.T) {
	home := t.TempDir()
	waitingWake(t, home, "calculadolar-1", 20*time.Minute)

	got := pendingWakes(home, fakeLocator{}, time.Now(), noAwaiter)
	if len(got) != 1 || !got[0].Warn {
		t.Fatalf("got %+v, want one warning", got)
	}
	if !strings.Contains(got[0].Name, "calculadolar-1") || !strings.Contains(got[0].Detail, "1 wake waiting") {
		t.Errorf("the warning should name the project and count the wake: %+v", got[0])
	}
}

func TestPendingWakes_QuietWhenAnAwaitIsListening(t *testing.T) {
	home := t.TempDir()
	root := waitingWake(t, home, "ideaverse-2", 20*time.Minute)

	asked := ""
	got := pendingWakes(home, fakeLocator{}, time.Now(), func(_, r string) bool { asked = r; return true })
	if len(got) != 0 {
		t.Errorf("got %+v, want nothing for a queue with a live await", got)
	}
	if asked != root {
		t.Errorf("asked about %q, want %q", asked, root)
	}
}

func TestPendingWakes_QuietWhenARegisteredCommanderIsLive(t *testing.T) {
	home := t.TempDir()
	root := waitingWake(t, home, "oc-3", 20*time.Minute)
	dir := t.TempDir()
	h := fakeLocator{agents: []herdr.AgentInfo{{Kind: "opencode", PaneID: "w1:p1", Cwd: dir}}}
	if _, err := commanderreg.Register(root, dir, "w1:p1", h, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := pendingWakes(home, h, time.Now(), noAwaiter); len(got) != 0 {
		t.Errorf("got %+v, want nothing: the sentinel delivers to a live OpenCode commander", got)
	}
}

func TestPendingWakes_AFreshWakeIsJustInFlight(t *testing.T) {
	home := t.TempDir()
	waitingWake(t, home, "proj-4", 5*time.Second)
	if got := pendingWakes(home, fakeLocator{}, time.Now(), noAwaiter); len(got) != 0 {
		t.Errorf("got %+v, want nothing for a wake seconds old", got)
	}
}

func TestPendingWakes_NothingPendingStartsNothing(t *testing.T) {
	home := t.TempDir()
	asked := false
	got := pendingWakes(home, fakeLocator{}, time.Now(), func(string, string) bool { asked = true; return false })
	if len(got) != 0 || asked {
		t.Errorf("got %+v, asked %v, want no result and no lookup", got, asked)
	}
}
