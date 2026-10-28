package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestNewWakeSampleSplitsTheRunAtItsInstants(t *testing.T) {
	t0 := time.Unix(1000, 0)
	s := newWakeSample(t0, t0.Add(2400*time.Millisecond), t0.Add(3450*time.Millisecond), t0.Add(5000*time.Millisecond))
	if s.sentinelPoll != 2400*time.Millisecond || s.stateWrite != 1050*time.Millisecond || s.awaitPoll != 1550*time.Millisecond {
		t.Errorf("unexpected phases: %+v", s)
	}
	if s.total() != 5*time.Second {
		t.Errorf("the phases add up to the whole run, got %v", s.total())
	}
}

// File times can be a millisecond coarse, so an instant can land just before
// the one it must follow; a phase is never negative.
func TestNewWakeSampleNeverGoesNegative(t *testing.T) {
	t0 := time.Unix(1000, 0)
	s := newWakeSample(t0, t0.Add(-time.Millisecond), t0.Add(500*time.Millisecond), t0.Add(400*time.Millisecond))
	if s.sentinelPoll != 0 || s.awaitPoll != 0 || s.stateWrite < 0 {
		t.Errorf("negative phase: %+v", s)
	}
}

func TestJitterCoversTheWholePollIntervalEvenly(t *testing.T) {
	const runs = 10
	var buckets [5]int
	for i := 0; i < runs; i++ {
		j := jitter(i)
		if j < 0 || j >= wakePollInterval {
			t.Fatalf("jitter(%d) = %v is outside [0, %v)", i, j, wakePollInterval)
		}
		buckets[int(j*5/wakePollInterval)]++
	}
	for i, n := range buckets {
		if n == 0 {
			t.Errorf("no run lands in the %d-th fifth of the interval: %v", i, buckets)
		}
	}
	if jitter(0) == jitter(1) {
		t.Error("consecutive runs must not share a phase")
	}
}

func TestHasWakeSeesOnlyPendingWakeFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "wakes")
	if hasWake(dir) {
		t.Error("a missing directory holds no wake")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A wake being delivered is renamed away from .json by the drain.
	if err := os.WriteFile(filepath.Join(dir, "abc.json.claimed-1-1"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if hasWake(dir) {
		t.Error("a claimed wake is not pending")
	}
	if err := os.WriteFile(filepath.Join(dir, "abc.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !hasWake(dir) {
		t.Error("a .json file is a pending wake")
	}
}

func TestWatchForWakeReportsWhenTheFileAppears(t *testing.T) {
	dir := t.TempDir()
	found := make(chan time.Time, 1)
	stop := make(chan struct{})
	defer close(stop)
	go watchForWake(dir, found, stop)

	before := time.Now()
	if err := os.WriteFile(filepath.Join(dir, "task.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case at := <-found:
		if at.Before(before) {
			t.Errorf("seen at %v, before the file was written at %v", at, before)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the watcher never saw the wake file")
	}
}

func TestWatchForWakeStopsWhenAsked(t *testing.T) {
	stop := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		watchForWake(t.TempDir(), make(chan time.Time, 1), stop)
		close(finished)
	}()
	close(stop)
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("the watcher kept running after stop")
	}
}

func TestWriteFileAtomicReplacesTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status")
	for _, content := range []string{"working\n", "idle\n"} {
		if err := writeFileAtomic(path, content); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != content {
			t.Errorf("got %q, %v; want %q", got, err, content)
		}
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("the temporary file must be gone")
	}
}

func TestWaitForFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registered")
	go func() {
		time.Sleep(30 * time.Millisecond)
		_ = os.WriteFile(path, nil, 0o644)
	}()
	if err := waitForFile(path, 10*time.Second); err != nil {
		t.Errorf("the file appeared: %v", err)
	}
	if err := waitForFile(filepath.Join(t.TempDir(), "never"), 30*time.Millisecond); err == nil {
		t.Error("a file that never appears is an error")
	}
}

func TestHermeticPathHoldsTheFakesAndNoOtherTool(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	fakes := t.TempDir()
	for _, name := range []string{"pi", "herdr"} {
		if err := os.WriteFile(filepath.Join(fakes, name), []byte("#!/bin/sh\necho fake\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(fakes, "notes.txt"), []byte("not a tool"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := &bench{root: t.TempDir(), fakes: fakes}
	dir := b.hermeticPath()
	if again := b.hermeticPath(); again != dir {
		t.Error("the directory is made once")
	}

	for _, want := range []string{"pi", "herdr", "git"} {
		info, err := os.Stat(filepath.Join(dir, want))
		if err != nil || info.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s must be in the hermetic PATH and runnable: %v", want, err)
		}
	}
	for _, absent := range []string{"notes.txt", "opencode", "gh", "claude"} {
		if _, err := os.Stat(filepath.Join(dir, absent)); err == nil {
			t.Errorf("%s must not be in the hermetic PATH", absent)
		}
	}
	out, err := exec.Command(filepath.Join(dir, "pi")).Output()
	if err != nil || string(out) != "fake\n" {
		t.Errorf("the fake must run through its link: %q, %v", out, err)
	}
	if err := exec.Command(filepath.Join(dir, "git"), "--version").Run(); err != nil {
		t.Errorf("git must run through its link: %v", err)
	}
}

func TestWorldEnvUsesTheHermeticPathAndExtraEnv(t *testing.T) {
	fakes := t.TempDir()
	b := &bench{root: t.TempDir(), fakes: fakes}
	w := &world{b: b, home: "/h", extraEnv: []string{"VXBENCH_STATUS_FILE=/x"}}

	env := w.env(spec{fake: true, hermetic: true, env: []string{"VXBENCH_PI_DELAY=0.1"}})
	for _, want := range []string{"PATH=" + b.hermeticPath(), "HERDR_WORKSPACE_ID=w1", "VXBENCH_STATUS_FILE=/x", "VXBENCH_PI_DELAY=0.1"} {
		if !contains(env, want) {
			t.Errorf("%q is missing from %v", want, env)
		}
	}
	if plain := w.env(spec{}); contains(plain, "HERDR_WORKSPACE_ID=w1") {
		t.Errorf("a plain command gets no fake workspace: %v", plain)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
