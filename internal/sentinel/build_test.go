package sentinel_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/sentinel"
	"github.com/isaias-alt/vexillum/internal/state"
)

// fakeExecutable writes a stand-in binary and makes CurrentBuild treat it as
// the running executable for the rest of the test.
func fakeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("binary one"), 0o755); err != nil {
		t.Fatalf("writing fake executable: %v", err)
	}
	t.Cleanup(sentinel.SetExecutablePath(func() (string, error) { return path, nil }))
}

// liveOtherPID starts a process of our own that stays alive for the test and
// returns its pid.
func liveOtherPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting helper process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd.Process.Pid
}

func TestBuild_NotStaleWhileTheExecutableIsUntouched(t *testing.T) {
	fakeExecutable(t, filepath.Join(t.TempDir(), "vx"))
	b, err := sentinel.CurrentBuild("v1.0.0")
	if err != nil {
		t.Fatalf("CurrentBuild: %v", err)
	}
	if b.Version != "v1.0.0" {
		t.Errorf("Version = %q, want v1.0.0", b.Version)
	}
	if reason := b.Stale(); reason != "" {
		t.Errorf("Stale = %q for an untouched executable", reason)
	}
}

func TestBuild_StaleWhenTheExecutableIsReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vx")
	fakeExecutable(t, path)
	b, err := sentinel.CurrentBuild("dev")
	if err != nil {
		t.Fatalf("CurrentBuild: %v", err)
	}

	// A rebuild: new contents, same path. The version string still says "dev".
	if err := os.WriteFile(path, []byte("binary two, longer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if reason := b.Stale(); !strings.Contains(reason, "replaced") {
		t.Errorf("Stale = %q after rewriting the file, want a replaced reason", reason)
	}
}

func TestBuild_StaleWhenOnlyTheModTimeChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vx")
	fakeExecutable(t, path)
	b, err := sentinel.CurrentBuild("dev")
	if err != nil {
		t.Fatalf("CurrentBuild: %v", err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if b.Stale() == "" {
		t.Error("a newer modification time on the same bytes was not noticed")
	}
}

// brew upgrade moves a symlink to a new versioned directory; the path the
// sentinel started from never changes, what it resolves to does.
func TestBuild_StaleWhenTheSymlinkIsRetargeted(t *testing.T) {
	dir := t.TempDir()
	oldBin := filepath.Join(dir, "vx-1")
	newBin := filepath.Join(dir, "vx-2")
	link := filepath.Join(dir, "vx")
	for _, p := range []string{oldBin, newBin} {
		if err := os.WriteFile(p, []byte("binary"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(oldBin, link); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sentinel.SetExecutablePath(func() (string, error) { return link, nil }))
	b, err := sentinel.CurrentBuild("v1")
	if err != nil {
		t.Fatalf("CurrentBuild: %v", err)
	}

	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(newBin, link); err != nil {
		t.Fatal(err)
	}
	if reason := b.Stale(); !strings.Contains(reason, "now points to") {
		t.Errorf("Stale = %q after retargeting the symlink", reason)
	}
}

func TestBuild_StaleWhenTheExecutableIsGone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vx")
	fakeExecutable(t, path)
	b, err := sentinel.CurrentBuild("dev")
	if err != nil {
		t.Fatalf("CurrentBuild: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if reason := b.Stale(); !strings.Contains(reason, "gone") {
		t.Errorf("Stale = %q after removing the file, want a gone reason", reason)
	}
}

// Run keeps ticking until retire gives a reason, and does not stop in the
// middle of a pass: the task that settled during the last tick is persisted
// and its wake recorded before Run returns.
func TestRun_RetiresAfterTheCurrentPass(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	task := newRunningTask(t, proj, "vx-do-the-thing")
	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "done"}}

	var out strings.Builder
	checks := 0
	reason := sentinel.Run(home, client, time.Millisecond, &out, func() string {
		checks++
		if checks < 3 {
			return ""
		}
		return "the binary changed"
	})

	if reason != "the binary changed" {
		t.Errorf("Run returned %q, want the retire reason", reason)
	}
	if checks != 3 {
		t.Errorf("retire was consulted %d times, want 3 (once per completed pass)", checks)
	}
	persisted, err := state.Load(proj, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Status != state.StatusDone {
		t.Errorf("status = %s: the pass in flight when retiring was cut short", persisted.Status)
	}
	if wakes, err := sentinel.Drain(proj); err != nil || len(wakes) != 1 {
		t.Errorf("Drain = %v, %v, want the wake of the finished pass", wakes, err)
	}
}

// The incident, end to end through the sentinel: a task file carrying fields
// this build does not know is settled by a pass and keeps them.
func TestTick_KeepsUnknownTaskFieldsAcrossATransition(t *testing.T) {
	home := t.TempDir()
	proj := projectRoot(home, "proj1")
	task := newRunningTask(t, proj, "vx-do-the-thing")

	path := filepath.Join(proj, "tasks", task.ID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	patched := strings.Replace(string(data), "{", `{"added_by_a_newer_build": {"keep": "me"},`, 1)
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}

	client := &fakeHerdr{statuses: map[string]string{"vx-do-the-thing": "done"}}
	if woke, err := sentinel.Tick(home, client); err != nil || woke != 1 {
		t.Fatalf("Tick = %d, %v, want 1 wake", woke, err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), `"added_by_a_newer_build"`) || !strings.Contains(string(after), `"keep": "me"`) {
		t.Errorf("unknown field erased by the sentinel's save:\n%s", after)
	}
	if persisted, err := state.Load(proj, task.ID); err != nil || persisted.Status != state.StatusDone {
		t.Errorf("Load = %+v, %v, want the done status persisted", persisted.Status, err)
	}
}

func TestAcquireLock_WritesPidAndReleaseCleansUp(t *testing.T) {
	home := t.TempDir()
	release, err := sentinel.AcquireLock(home)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(home, "sentinel.pid"))
	if err != nil || strings.TrimSpace(string(data)) != strconv.Itoa(os.Getpid()) {
		t.Errorf("pid file = %q, %v, want our pid", data, err)
	}
	if err := sentinel.PublishInfo(home, sentinel.Build{Version: "v1"}); err != nil {
		t.Fatalf("PublishInfo: %v", err)
	}

	release()
	release() // idempotent

	for _, name := range []string{"sentinel.pid", "sentinel.json"} {
		if _, err := os.Stat(filepath.Join(home, name)); !os.IsNotExist(err) {
			t.Errorf("%s left behind after release (err %v)", name, err)
		}
	}
}

// A release that runs after someone else took over must not delete what the
// new holder wrote - the old pid-file lock did exactly that.
func TestAcquireLock_ReleaseLeavesAnotherHoldersFilesAlone(t *testing.T) {
	home := t.TempDir()
	release, err := sentinel.AcquireLock(home)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	other := filepath.Join(home, "sentinel.pid")
	if err := os.WriteFile(other, []byte("424242"), 0o644); err != nil {
		t.Fatal(err)
	}
	release()
	if data, err := os.ReadFile(other); err != nil || string(data) != "424242" {
		t.Errorf("pid file = %q, %v after release, want the other holder's untouched", data, err)
	}
}

// A pid file naming a live process that is not a sentinel (the pid was
// recycled) is stale.
func TestAcquireLock_IgnoresARecycledPid(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "sentinel.pid"), []byte(strconv.Itoa(liveOtherPID(t))), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := sentinel.SetInspectProcess(func(pid int) (int, string, error) {
		return 1, "/usr/bin/some-editor notes.md", nil
	})
	defer restore()

	if sentinel.IsRunning(home) {
		t.Error("IsRunning = true for a recycled pid")
	}
	release, err := sentinel.AcquireLock(home)
	if err != nil {
		t.Fatalf("AcquireLock over a recycled pid: %v", err)
	}
	release()
}

// Whoever loses the race for the lock learns who won, from the pid file.
func TestAcquireLock_ErrRunningNamesTheHolder(t *testing.T) {
	home := t.TempDir()
	release, err := sentinel.AcquireLock(home)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	_, err = sentinel.AcquireLock(home)
	var running *sentinel.ErrRunning
	if !errors.As(err, &running) || running.PID != os.Getpid() {
		t.Errorf("AcquireLock = %v, want ErrRunning naming pid %d", err, os.Getpid())
	}
}

func TestLiveInfo_ReportsTheRunningSentinelsBuild(t *testing.T) {
	home := t.TempDir()
	if _, ok := sentinel.LiveInfo(home); ok {
		t.Fatal("LiveInfo reported a sentinel with none running")
	}
	release, err := sentinel.AcquireLock(home)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := sentinel.LiveInfo(home); ok {
		t.Error("LiveInfo reported a build before one was published")
	}
	want := sentinel.Build{Version: "v9.9.9", Executable: "/x/vx", Resolved: "/x/vx", Size: 3}
	if err := sentinel.PublishInfo(home, want); err != nil {
		t.Fatal(err)
	}
	info, ok := sentinel.LiveInfo(home)
	if !ok || info.PID != os.Getpid() || !reflect.DeepEqual(info.Build, want) {
		t.Errorf("LiveInfo = %+v, %v, want our pid and the published build", info, ok)
	}

	release()
	if _, ok := sentinel.LiveInfo(home); ok {
		t.Error("LiveInfo still reports a sentinel after release")
	}
}

func TestHolderStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vx")
	fakeExecutable(t, path)
	home := t.TempDir()
	build, err := sentinel.CurrentBuild("dev")
	if err != nil {
		t.Fatal(err)
	}
	release, err := sentinel.AcquireLock(home)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := sentinel.PublishInfo(home, build); err != nil {
		t.Fatal(err)
	}

	if sentinel.HolderStale(home) {
		t.Error("HolderStale = true for an untouched executable")
	}
	if err := os.WriteFile(path, []byte("a different build entirely"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !sentinel.HolderStale(home) {
		t.Error("HolderStale = false after the executable was replaced")
	}
}

// A replacement for a stale holder waits for it to retire rather than giving
// up, and takes the lock as soon as it is free.
func TestAcquireLockRetiring_WaitsForAStaleHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vx")
	fakeExecutable(t, path)
	home := t.TempDir()
	build, err := sentinel.CurrentBuild("dev")
	if err != nil {
		t.Fatal(err)
	}
	releaseOld, err := sentinel.AcquireLock(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := sentinel.PublishInfo(home, build); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("the new build"), 0o755); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(400 * time.Millisecond)
		releaseOld()
	}()

	release, err := sentinel.AcquireLockRetiring(home, 10*time.Second)
	wg.Wait()
	if err != nil {
		t.Fatalf("AcquireLockRetiring: %v", err)
	}
	release()
}

// The holder is caught mid-release: its build record is already gone while it
// still holds the flock, and it only drops the flock after a further refusal.
// A holder seen stale once must keep being waited for.
func TestAcquireLockRetiring_SurvivesTheHolderExitingAfterARefusal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vx")
	fakeExecutable(t, path)
	home := t.TempDir()
	build, err := sentinel.CurrentBuild("dev")
	if err != nil {
		t.Fatal(err)
	}
	releaseOld, err := sentinel.AcquireLock(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := sentinel.PublishInfo(home, build); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("the new build"), 0o755); err != nil {
		t.Fatal(err)
	}

	attempts := 0
	t.Cleanup(sentinel.SetTryAcquireLock(func(h string) (func(), error) {
		release, err := sentinel.AcquireLock(h)
		attempts++
		switch attempts {
		case 1:
			if err := os.Remove(filepath.Join(h, "sentinel.json")); err != nil {
				t.Errorf("removing the build record: %v", err)
			}
		case 2:
			releaseOld()
		}
		return release, err
	}))

	release, err := sentinel.AcquireLockRetiring(home, 10*time.Second)
	if err != nil {
		t.Fatalf("AcquireLockRetiring: %v", err)
	}
	release()
}

// A holder that is not stale is not waited for: two live sentinels of the same
// build never queue up behind each other.
func TestAcquireLockRetiring_RefusesAtOnceWhenTheHolderIsCurrent(t *testing.T) {
	fakeExecutable(t, filepath.Join(t.TempDir(), "vx"))
	home := t.TempDir()
	build, err := sentinel.CurrentBuild("dev")
	if err != nil {
		t.Fatal(err)
	}
	release, err := sentinel.AcquireLock(home)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := sentinel.PublishInfo(home, build); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	_, err = sentinel.AcquireLockRetiring(home, 10*time.Second)
	var running *sentinel.ErrRunning
	if !errors.As(err, &running) {
		t.Fatalf("AcquireLockRetiring = %v, want ErrRunning", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("waited %s for a holder that is not stale", time.Since(start))
	}
}

func TestLivePIDs_FindsOnlyTheSentinelProcesses(t *testing.T) {
	restore := sentinel.SetListProcesses(func() (string, error) {
		return strings.Join([]string{
			"  101 /opt/homebrew/bin/vx sentinel",
			"  102 /Users/me/go/bin/vx sentinel await",
			" 103 /Users/me/code/vexillum/vx sentinel",
			"  104 vx sentinel drain",
			"  105 /usr/bin/vim sentinel",
			"  106 /Users/me/My Tools/vx sentinel",
			"  107 vx dispatch sentinel",
			"  garbage",
		}, "\n"), nil
	})
	defer restore()

	pids, err := sentinel.LivePIDs()
	if err != nil {
		t.Fatalf("LivePIDs: %v", err)
	}
	if want := []int{101, 103, 106}; !reflect.DeepEqual(pids, want) {
		t.Errorf("LivePIDs = %v, want %v", pids, want)
	}
}

func TestLivePIDs_ReportsAListingFailure(t *testing.T) {
	restore := sentinel.SetListProcesses(func() (string, error) { return "", errors.New("no ps") })
	defer restore()
	if _, err := sentinel.LivePIDs(); err == nil {
		t.Error("LivePIDs hid a failure to list processes")
	}
}
