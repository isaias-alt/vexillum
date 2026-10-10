package forumlisten

import (
	"errors"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

const listenCmd = "/usr/local/bin/vx forum listen"

// fakeInspect makes every pid look like the given command (or a missing
// process), restoring the real inspector afterwards.
func fakeInspect(t *testing.T, f func(pid int) (string, error)) {
	t.Helper()
	old := inspectProcess
	inspectProcess = f
	t.Cleanup(func() { inspectProcess = old })
}

func TestIsListenCommand(t *testing.T) {
	for cmd, want := range map[string]bool{
		"/usr/local/bin/vx forum listen":     true,
		"vx forum listen":                    true,
		"/opt/my tools/vx forum listen":      true,
		"/usr/local/bin/vx forum serve":      false,
		"/usr/local/bin/vx forum listen --x": false,
		"/usr/bin/vim forum listen":          false,
		"vx sentinel await":                  false,
		"my tools/vx forum listen":           false,
	} {
		if got := isListenCommand(cmd); got != want {
			t.Errorf("isListenCommand(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestAcquireLock_IsASingleton(t *testing.T) {
	home := t.TempDir()
	fakeInspect(t, func(int) (string, error) { return listenCmd, nil })
	release, err := AcquireLock(home)
	if err != nil {
		t.Fatal(err)
	}
	_, err = AcquireLock(home)
	var running *ErrRunning
	if !errors.As(err, &running) || running.PID != os.Getpid() {
		t.Fatalf("second AcquireLock = %v, want ErrRunning for our pid", err)
	}
	if !Running(home) {
		t.Error("Running = false with a live verified listener")
	}
	release()
	if Running(home) {
		t.Error("Running = true after release")
	}
	again, err := AcquireLock(home)
	if err != nil {
		t.Fatalf("AcquireLock after release = %v", err)
	}
	again()
}

func TestAcquireLock_ReclaimsALockWhosePidIsNotAListener(t *testing.T) {
	home := t.TempDir()
	// The pid in the lock is alive (ours) but now belongs to something else.
	fakeInspect(t, func(int) (string, error) { return "/usr/bin/vim notes.txt", nil })
	if err := os.MkdirAll(dir(home), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath(home), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	release, err := AcquireLock(home)
	if err != nil {
		t.Fatalf("a recycled pid must not hold the lock: %v", err)
	}
	release()
}

func TestAcquireLock_ReclaimsGarbageAndDeadPids(t *testing.T) {
	home := t.TempDir()
	fakeInspect(t, func(int) (string, error) { return "", errProcessGone })
	if err := os.MkdirAll(dir(home), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"not a pid", "999999999", ""} {
		if err := os.WriteFile(lockPath(home), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		release, err := AcquireLock(home)
		if err != nil {
			t.Fatalf("lock holding %q must be reclaimed: %v", content, err)
		}
		release()
	}
}

func TestAcquireLock_RacingStartersLeaveExactlyOneWinner(t *testing.T) {
	home := t.TempDir()
	fakeInspect(t, func(int) (string, error) { return listenCmd, nil })
	var winners atomic.Int32
	done := make(chan func(), 16)
	for i := 0; i < 16; i++ {
		go func() {
			release, err := AcquireLock(home)
			if err == nil {
				winners.Add(1)
				done <- release
				return
			}
			done <- func() {}
		}()
	}
	var releases []func()
	for i := 0; i < 16; i++ {
		releases = append(releases, <-done)
	}
	if winners.Load() != 1 {
		t.Errorf("%d starters won the lock, want 1", winners.Load())
	}
	for _, r := range releases {
		r()
	}
}

func TestStop_NeverSignalsAnUnverifiedProcess(t *testing.T) {
	home := t.TempDir()
	// A lock naming a live process (ours!) that is not a listener: Stop must
	// leave it alone. If it signaled it, this test process would be terminated.
	fakeInspect(t, func(int) (string, error) { return "/usr/bin/vim notes.txt", nil })
	if err := os.MkdirAll(dir(home), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath(home), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	stopped, err := Stop(home)
	if err != nil || stopped {
		t.Fatalf("Stop = %v %v, want no listener found", stopped, err)
	}
}

func TestStop_WithNoListenerIsANoOp(t *testing.T) {
	if stopped, err := Stop(t.TempDir()); err != nil || stopped {
		t.Fatalf("Stop = %v %v", stopped, err)
	}
}

func TestEnsure_StartsOnceAndWaitsForTheLock(t *testing.T) {
	home := t.TempDir()
	fakeInspect(t, func(int) (string, error) { return listenCmd, nil })
	var spawns atomic.Int32
	spawn := func() error {
		spawns.Add(1)
		go func() {
			time.Sleep(100 * time.Millisecond)
			_, _ = AcquireLock(home) // the spawned listener claims the lock
		}()
		return nil
	}
	if err := Ensure(home, spawn, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if spawns.Load() != 1 {
		t.Errorf("spawned %d times, want 1", spawns.Load())
	}
	// Already running: no spawn at all.
	if err := Ensure(home, spawn, time.Second); err != nil || spawns.Load() != 1 {
		t.Errorf("Ensure with a running listener spawned again (%d) or failed: %v", spawns.Load(), err)
	}
}

func TestEnsure_ReportsAListenerThatNeverStarts(t *testing.T) {
	home := t.TempDir()
	fakeInspect(t, func(int) (string, error) { return "", errProcessGone })
	err := Ensure(home, func() error { return nil }, 300*time.Millisecond)
	if err == nil {
		t.Fatal("Ensure succeeded with no listener")
	}
}
