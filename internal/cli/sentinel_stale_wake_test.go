package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/sentinel"
	"github.com/isaias-alt/vexillum/internal/state"
)

// queueWake records a Done scout task in root and a wake announcing it,
// detected at the given time, its file last written 20 minutes ago.
func queueWake(t *testing.T, root string, detected time.Time) sentinel.Wake {
	t.Helper()
	task, err := state.New(state.KindScout, "look into it")
	if err != nil {
		t.Fatal(err)
	}
	task.Status = state.StatusDone
	if err := state.Save(root, task); err != nil {
		t.Fatal(err)
	}
	w := sentinel.Wake{TaskID: task.ID, Kind: task.Kind, OldStatus: state.StatusRunning, NewStatus: state.StatusDone, DetectedAt: detected.UTC()}
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "wakes"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "wakes", task.ID+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-20 * time.Minute)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
	return w
}

// A project nobody uses any more keeps a wake that is older than the TTL:
// "sentinel drain" deletes it and says nothing, and "doctor" does not warn
// about it. Everything runs under a temporary HOME.
func TestStaleWake_DrainDiscardsItAndDoctorStaysQuiet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	vexillumHome := filepath.Join(home, ".vexillum")
	proj := initDispatchTestProject(t)
	t.Chdir(proj)

	root, err := resolveDrainTarget(proj, vexillumHome)
	if err != nil || root == "" {
		t.Fatalf("resolveDrainTarget = %q, %v", root, err)
	}
	stale := queueWake(t, root, time.Now().Add(-sentinel.StaleWakeAfter-time.Hour))

	var out bytes.Buffer
	runDoctor(proj, vexillumHome, home, &out)
	if strings.Contains(out.String(), "pending wakes") {
		t.Errorf("doctor warns about a stale wake:\n%s", out.String())
	}

	out.Reset()
	if code := runSentinelDrain(root, &out, &out); code != 0 {
		t.Fatalf("drain exit %d: %s", code, out.String())
	}
	if strings.TrimSpace(out.String()) != "{}" {
		t.Errorf("drain printed %q, want {} for a stale wake", out.String())
	}
	if _, err := os.Stat(filepath.Join(root, "wakes", stale.TaskID+".json")); !os.IsNotExist(err) {
		t.Errorf("the stale wake file is still queued: %v", err)
	}
}

// A wake within the TTL behaves as before: doctor warns while nobody listens,
// and drain delivers it once.
func TestFreshWake_DoctorWarnsAndDrainDeliversIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	vexillumHome := filepath.Join(home, ".vexillum")
	proj := initDispatchTestProject(t)
	t.Chdir(proj)

	root, err := resolveDrainTarget(proj, vexillumHome)
	if err != nil || root == "" {
		t.Fatalf("resolveDrainTarget = %q, %v", root, err)
	}
	fresh := queueWake(t, root, time.Now().Add(-sentinel.StaleWakeAfter+time.Hour))

	var out bytes.Buffer
	runDoctor(proj, vexillumHome, home, &out)
	if !strings.Contains(out.String(), "pending wakes") {
		t.Errorf("doctor should warn about a fresh wake nobody listens to:\n%s", out.String())
	}

	out.Reset()
	if code := runSentinelDrain(root, &out, &out); code != 0 {
		t.Fatalf("drain exit %d: %s", code, out.String())
	}
	var decision map[string]string
	if err := json.Unmarshal(out.Bytes(), &decision); err != nil {
		t.Fatalf("drain output %q: %v", out.String(), err)
	}
	if decision["decision"] != "block" || !strings.Contains(decision["reason"], fresh.TaskID) {
		t.Errorf("drain printed %v, want a block naming %s", decision, fresh.TaskID)
	}
}
