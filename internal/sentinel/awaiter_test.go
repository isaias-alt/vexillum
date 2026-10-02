package sentinel_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/sentinel"
)

const awaitCmd = "/opt/homebrew/bin/vx sentinel await"

func awaiterRecordPath(home string, pid int) string {
	return filepath.Join(home, "sentinel-awaiters", strconv.Itoa(pid)+".json")
}

func writeAwaiterRecord(t *testing.T, home string, rec sentinel.AwaiterRecord) {
	t.Helper()
	dir := filepath.Join(home, "sentinel-awaiters")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(rec)
	if err := os.WriteFile(awaiterRecordPath(home, rec.PID), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// victim starts a harmless child process the test owns, so signaling it is
// safe, and reports when it has exited.
func victim(t *testing.T) (pid int, exited <-chan struct{}) {
	t.Helper()
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting victim: %v", err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return cmd.Process.Pid, done
}

func waitExited(t *testing.T, exited <-chan struct{}) bool {
	t.Helper()
	select {
	case <-exited:
		return true
	case <-time.After(3 * time.Second):
		return false
	}
}

// inspectAs makes the inspector report every pid as the given process.
func inspectAs(t *testing.T, ppid int, command string) {
	t.Helper()
	t.Cleanup(sentinel.SetInspectProcess(func(int) (int, string, error) { return ppid, command, nil }))
}

func TestRegisterAwaiter_WritesAndReleasesItsRecord(t *testing.T) {
	home := t.TempDir()
	release, err := sentinel.RegisterAwaiter(home, "/proj", 4242)
	if err != nil {
		t.Fatalf("RegisterAwaiter: %v", err)
	}

	data, err := os.ReadFile(awaiterRecordPath(home, os.Getpid()))
	if err != nil {
		t.Fatalf("expected a record for this process: %v", err)
	}
	var rec sentinel.AwaiterRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.PID != os.Getpid() || rec.OwnerPID != 4242 || rec.ProjectRoot != "/proj" {
		t.Errorf("unexpected record %+v", rec)
	}

	release()
	if _, err := os.Stat(awaiterRecordPath(home, os.Getpid())); !os.IsNotExist(err) {
		t.Errorf("expected the record removed on release, stat err = %v", err)
	}
}

// The previous turn's await of the same session and project is redundant
// once a new one starts: it is told to exit.
func TestRegisterAwaiter_SupersedesSameOwnerAndProject(t *testing.T) {
	home := t.TempDir()
	pid, exited := victim(t)
	writeAwaiterRecord(t, home, sentinel.AwaiterRecord{PID: pid, OwnerPID: 4242, ProjectRoot: "/proj"})
	inspectAs(t, 4242, awaitCmd)

	release, err := sentinel.RegisterAwaiter(home, "/proj", 4242)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if !waitExited(t, exited) {
		t.Fatal("expected the older await of the same owner and project to be signaled")
	}
	if _, err := os.Stat(awaiterRecordPath(home, pid)); !os.IsNotExist(err) {
		t.Errorf("expected the superseded record removed, stat err = %v", err)
	}
}

// Another session's await (different owner) or another project's must be
// left alone.
func TestRegisterAwaiter_LeavesOtherOwnersAndProjectsAlone(t *testing.T) {
	home := t.TempDir()
	otherOwner, otherOwnerExited := victim(t)
	otherProject, otherProjectExited := victim(t)
	writeAwaiterRecord(t, home, sentinel.AwaiterRecord{PID: otherOwner, OwnerPID: 1111, ProjectRoot: "/proj"})
	writeAwaiterRecord(t, home, sentinel.AwaiterRecord{PID: otherProject, OwnerPID: 4242, ProjectRoot: "/elsewhere"})
	t.Cleanup(sentinel.SetInspectProcess(func(pid int) (int, string, error) {
		if pid == otherOwner {
			return 1111, awaitCmd, nil
		}
		return 4242, awaitCmd, nil
	}))

	release, err := sentinel.RegisterAwaiter(home, "/proj", 4242)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if waitExitedBriefly(otherOwnerExited) || waitExitedBriefly(otherProjectExited) {
		t.Fatal("an unrelated await must not be signaled")
	}
}

func waitExitedBriefly(exited <-chan struct{}) bool {
	select {
	case <-exited:
		return true
	case <-time.After(300 * time.Millisecond):
		return false
	}
}

// An await whose real parent is no longer the one it recorded (it was
// reparented when its hook died) is an orphan and is told to exit.
func TestRegisterAwaiter_ReapsOrphans(t *testing.T) {
	home := t.TempDir()
	pid, exited := victim(t)
	writeAwaiterRecord(t, home, sentinel.AwaiterRecord{PID: pid, OwnerPID: 1111, ProjectRoot: "/other"})
	inspectAs(t, 1, awaitCmd) // reparented to init

	release, err := sentinel.RegisterAwaiter(home, "/proj", 4242)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if !waitExited(t, exited) {
		t.Fatal("expected the orphaned await to be signaled")
	}
}

func TestReapAwaiters_SignalsOnlyOrphansAndCountsThem(t *testing.T) {
	home := t.TempDir()
	orphan, orphanExited := victim(t)
	live, liveExited := victim(t)
	writeAwaiterRecord(t, home, sentinel.AwaiterRecord{PID: orphan, OwnerPID: 1111, ProjectRoot: "/a"})
	writeAwaiterRecord(t, home, sentinel.AwaiterRecord{PID: live, OwnerPID: 2222, ProjectRoot: "/b"})
	t.Cleanup(sentinel.SetInspectProcess(func(pid int) (int, string, error) {
		if pid == orphan {
			return 1, awaitCmd, nil
		}
		return 2222, awaitCmd, nil
	}))

	if n := sentinel.ReapAwaiters(home); n != 1 {
		t.Errorf("expected 1 process signaled, got %d", n)
	}
	if !waitExited(t, orphanExited) {
		t.Error("expected the orphan to exit")
	}
	if waitExitedBriefly(liveExited) {
		t.Error("an await with a live owner must be left alone")
	}
}

// A recycled pid now belongs to something else: its record is dropped but
// the process is never signaled.
func TestSweep_NeverSignalsAPidThatIsNotAnAwait(t *testing.T) {
	home := t.TempDir()
	pid, exited := victim(t)
	writeAwaiterRecord(t, home, sentinel.AwaiterRecord{PID: pid, OwnerPID: 1111, ProjectRoot: "/proj"})
	inspectAs(t, 1, "/usr/bin/some-other-program --flag") // would be an orphan if it were ours

	release, err := sentinel.RegisterAwaiter(home, "/proj", 1111)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if waitExitedBriefly(exited) {
		t.Fatal("a process that is not a vx sentinel await must never be signaled")
	}
	if _, err := os.Stat(awaiterRecordPath(home, pid)); !os.IsNotExist(err) {
		t.Errorf("expected the stale record removed, stat err = %v", err)
	}
}

// The main polling sentinel is "vx sentinel", not an await: a record that
// somehow points at it must not get it signaled.
func TestSweep_NeverSignalsThePollingSentinel(t *testing.T) {
	home := t.TempDir()
	pid, exited := victim(t)
	writeAwaiterRecord(t, home, sentinel.AwaiterRecord{PID: pid, OwnerPID: 1111, ProjectRoot: "/proj"})
	inspectAs(t, 1, "/opt/homebrew/bin/vexillum sentinel")

	sentinel.ReapAwaiters(home)
	if waitExitedBriefly(exited) {
		t.Fatal("the polling sentinel must never be signaled")
	}
}

func TestSweep_DropsRecordsOfDeadProcessesAndGarbage(t *testing.T) {
	home := t.TempDir()
	writeAwaiterRecord(t, home, sentinel.AwaiterRecord{PID: 987654, OwnerPID: 1111, ProjectRoot: "/proj"})
	garbage := filepath.Join(home, "sentinel-awaiters", "555.json")
	if err := os.WriteFile(garbage, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	mismatched := filepath.Join(home, "sentinel-awaiters", "777.json")
	data, _ := json.Marshal(sentinel.AwaiterRecord{PID: 1, OwnerPID: 1111})
	if err := os.WriteFile(mismatched, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sentinel.SetInspectProcess(func(int) (int, string, error) { return 0, "", sentinel.ErrProcessGone }))

	if n := sentinel.ReapAwaiters(home); n != 0 {
		t.Errorf("expected nothing signaled, got %d", n)
	}
	for _, p := range []string{awaiterRecordPath(home, 987654), garbage, mismatched} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("expected %s removed, stat err = %v", p, err)
		}
	}
}

func TestReapAwaiters_NoDirectoryIsFine(t *testing.T) {
	if n := sentinel.ReapAwaiters(t.TempDir()); n != 0 {
		t.Errorf("expected 0, got %d", n)
	}
}

func TestIsAwaitCommand(t *testing.T) {
	cases := map[string]bool{
		"vx sentinel await":                                    true,
		"/Users/me/.local/bin/vx sentinel await":               true,
		"/opt/homebrew/bin/vexillum sentinel await":            true,
		"/Users/me/My Tools/vx sentinel await":                 true,
		"/opt/homebrew/bin/vexillum sentinel":                  false,
		"vx sentinel drain":                                    false,
		"vx sentinel await --extra":                            false,
		"/usr/bin/vim sentinel await":                          false,
		"vx dispatch sentinel await":                           false,
		`herdr agent prompt x "see /opt/bin/vx sentinel await`: false,
		"sleep 60":                            false,
		"":                                    false,
		"/some/path/notvx sentinel await":     false,
		"/some/path/vx-other sentinel await":  false,
		"herdr agent prompt x sentinel await": false,
	}
	for cmd, want := range cases {
		if got := sentinel.IsAwaitCommand(cmd); got != want {
			t.Errorf("isAwaitCommand(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestParsePSLine(t *testing.T) {
	ppid, cmd, err := sentinel.ParsePSLine("  1234 /opt/homebrew/bin/vx sentinel await\n")
	if err != nil || ppid != 1234 || cmd != "/opt/homebrew/bin/vx sentinel await" {
		t.Errorf("got %d %q %v", ppid, cmd, err)
	}
	if _, _, err := sentinel.ParsePSLine(""); err == nil {
		t.Error("expected an error for empty output")
	}
	if _, _, err := sentinel.ParsePSLine("abc def"); err == nil {
		t.Error("expected an error for a non-numeric ppid")
	}
}

// The real inspector, against a process we know: ourselves.
func TestInspectProcess_RealPS(t *testing.T) {
	ppid, cmd, err := sentinel.InspectProcess(os.Getpid())
	if err != nil {
		t.Fatalf("inspecting own pid: %v", err)
	}
	if ppid != os.Getppid() {
		t.Errorf("ppid = %d, want %d", ppid, os.Getppid())
	}
	if filepath.Base(os.Args[0]) == "" || cmd == "" {
		t.Errorf("expected a command line, got %q", cmd)
	}
	if _, _, err := sentinel.InspectProcess(987654); err == nil {
		t.Error("expected an error for a pid that does not exist")
	}
}

func TestOwnerGone(t *testing.T) {
	if sentinel.OwnerGone(os.Getppid()) {
		t.Error("the real parent must not read as gone")
	}
	if !sentinel.OwnerGone(os.Getppid() + 1) {
		t.Error("a different recorded owner must read as gone (reparented)")
	}
}

// Two awaits registering at the same instant must not both die: only an
// older await is superseded, a newer one of the same owner and project
// (which started a moment before this one finished registering) is left.
func TestRegisterAwaiter_NeverSupersedesANewerAwait(t *testing.T) {
	home := t.TempDir()
	pid, exited := victim(t)
	writeAwaiterRecord(t, home, sentinel.AwaiterRecord{
		PID: pid, OwnerPID: 4242, ProjectRoot: "/proj", StartedAt: time.Now().Add(time.Hour),
	})
	inspectAs(t, 4242, awaitCmd)

	release, err := sentinel.RegisterAwaiter(home, "/proj", 4242)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	if waitExitedBriefly(exited) {
		t.Fatal("a newer await of the same owner must not be signaled by an older one")
	}
}

func TestLiveAwaiters_CountsOnlyVerifiedAwaitsAndTouchesNothing(t *testing.T) {
	home := t.TempDir()
	writeAwaiterRecord(t, home, sentinel.AwaiterRecord{PID: 4001, OwnerPID: 1})
	writeAwaiterRecord(t, home, sentinel.AwaiterRecord{PID: 4002, OwnerPID: 1})
	writeAwaiterRecord(t, home, sentinel.AwaiterRecord{PID: 4003, OwnerPID: 1})
	t.Cleanup(sentinel.SetInspectProcess(func(pid int) (int, string, error) {
		switch pid {
		case 4001:
			return 1, awaitCmd, nil
		case 4002:
			return 1, "/usr/bin/vim notes.txt", nil // a recycled pid
		}
		return 0, "", sentinel.ErrProcessGone
	}))

	if got := sentinel.LiveAwaiters(home); got != 1 {
		t.Errorf("LiveAwaiters = %d, want 1", got)
	}
	for _, pid := range []int{4001, 4002, 4003} {
		if _, err := os.Stat(awaiterRecordPath(home, pid)); err != nil {
			t.Errorf("LiveAwaiters must not delete record %d: %v", pid, err)
		}
	}
	if got := sentinel.LiveAwaiters(t.TempDir()); got != 0 {
		t.Errorf("no registry: LiveAwaiters = %d, want 0", got)
	}
}
