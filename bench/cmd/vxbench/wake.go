package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/state"
)

// The wake latency scenario times the path that gets a finished soldier's
// result in front of the commander, with the real sentinel and the real
// "vx sentinel await" (the command the Stop hook runs) and only herdr faked:
//
//	t0        the soldier finishes: the fake herdr starts saying "idle"
//	seen      the sentinel's next pass asks herdr and gets "idle"
//	woke      the sentinel has re-read the pane, saved the task and written the wake
//	returned  the await's next poll finds the wake and exits
//
// which gives three phases, "sentinel poll" (t0 to seen), "state write" (seen
// to woke) and "await poll" (woke to returned), and their sum. The two polls
// wait for the next tick of a 5 s timer, so they are spread evenly over 0 to
// 5 s whatever vx does: they are shown and bounded, not compared. The state
// write is the part vx can make slower or faster. It is about a second by
// design: the sentinel reads the pane again a second after the soldier stops
// (settleReadGap) to make sure the final report has finished painting.

const (
	wakeScenarioName = "wake latency (soldier idle to Stop hook)"

	// wakePollInterval is sentinelPollInterval and sentinelAwaitPollInterval in
	// internal/cli/sentinel.go, which are not exported.
	wakePollInterval = 5 * time.Second
	// wakeRunCap is how many timed runs the scenario makes at most: each takes
	// several seconds.
	wakeRunCap = 10
	// wakeRunTimeout is how long one run may take before it is a failure.
	wakeRunTimeout = 60 * time.Second
)

// wakeSample is the phases of one run.
type wakeSample struct {
	sentinelPoll time.Duration
	stateWrite   time.Duration
	awaitPoll    time.Duration
}

func (s wakeSample) total() time.Duration { return s.sentinelPoll + s.stateWrite + s.awaitPoll }

// newWakeSample splits a run at its four instants. Each phase is at least
// zero: the instants come from the file system's timestamps as well as the
// clock, and those can be a millisecond coarse.
func newWakeSample(t0, seen, woke, returned time.Time) wakeSample {
	phase := func(from, to time.Time) time.Duration { return max(to.Sub(from), 0) }
	return wakeSample{
		sentinelPoll: phase(t0, seen),
		stateWrite:   phase(seen, woke),
		awaitPoll:    phase(woke, returned),
	}
}

// jitter spreads the moment the soldier finishes evenly over a poll interval,
// run after run, so the medians of the polling phases do not depend on how the
// runs happened to line up with the timers. It is the golden-ratio sequence:
// any number of runs covers 0 to 1 with no gap larger than about 1/n.
func jitter(run int) time.Duration {
	frac := math.Mod(float64(run+1)*0.6180339887498949, 1)
	return time.Duration(frac * float64(wakePollInterval))
}

func (b *bench) runWake() []result {
	fail := func(format string, args ...any) []result {
		return []result{{Name: wakeScenarioName, Group: "wake", Failure: fmt.Sprintf(format, args...)}}
	}

	w, err := b.newWorld("wake")
	if err != nil {
		return fail("%v", err)
	}
	defer w.teardown()

	statusFile := filepath.Join(w.dir, "agent-status")
	seenFile := filepath.Join(w.dir, "agent-seen-idle")
	w.extraEnv = []string{"VXBENCH_STATUS_FILE=" + statusFile, "VXBENCH_SEEN_FILE=" + seenFile}
	if err := w.startSentinel(); err != nil {
		return fail("starting the sentinel: %v", err)
	}
	root, err := project.Root(w.vexillumHome(), w.project)
	if err != nil {
		return fail("%v", err)
	}

	n, warmup := min(b.cfg.n, wakeRunCap), 1
	var sentinelPoll, stateWrite, awaitPoll, total []time.Duration
	for i := 0; i < warmup+n; i++ {
		fmt.Fprintf(os.Stderr, "vxbench:   wake run %d of %d\n", i+1, warmup+n)
		s, err := b.wakeRun(w, root, statusFile, seenFile, i)
		if err != nil {
			return fail("run %d: %v", i+1, err)
		}
		if i < warmup {
			continue
		}
		sentinelPoll = append(sentinelPoll, s.sentinelPoll)
		stateWrite = append(stateWrite, s.stateWrite)
		awaitPoll = append(awaitPoll, s.awaitPoll)
		total = append(total, s.total())
	}

	// The polls can each take up to an interval; twice that is a poll that
	// has gone wrong, not a slow machine.
	pollCeiling := float64(2 * wakePollInterval / time.Millisecond)
	row := func(name string, ds []time.Duration, informational bool, g guard) result {
		wall := summarize(ds)
		gr := g.evaluate(wall)
		return result{Name: name, Group: "wake", Informational: informational, Wall: wall, Guard: gr}
	}
	return []result{
		row("wake: sentinel poll", sentinelPoll, true, guard{Stat: "max", LimitMS: pollCeiling,
			Why: "the sentinel must notice a finished soldier within a couple of poll intervals (5 s each)"}),
		row("wake: state write", stateWrite, false, guard{Stat: "median", LimitMS: 5000,
			Why: "recording a finished soldier (a second pane read, the task save, the wake file) must take seconds at most"}),
		row("wake: await poll", awaitPoll, true, guard{Stat: "max", LimitMS: pollCeiling,
			Why: "the Stop hook must pick up a wake within a couple of poll intervals (5 s each)"}),
		row("wake: total", total, true, guard{Stat: "max", LimitMS: 3 * pollCeiling,
			Why: "a finished soldier must reach the commander well inside half a minute"}),
	}
}

// wakeRun is one measured run: it makes a finished mission, starts the Stop
// hook's await, lets the soldier "finish" and times what follows.
func (b *bench) wakeRun(w *world, root, statusFile, seenFile string, run int) (wakeSample, error) {
	// The soldier delivered: a mission with a commit in its camp, which is what
	// the sentinel takes as proof the work is done. Its record is made old
	// enough that the sentinel acts on it (it ignores a task for 8 s after
	// dispatch).
	id, _, err := b.dispatch(w)
	if err != nil {
		return wakeSample{}, err
	}
	if err := w.commitInCamp(id); err != nil {
		return wakeSample{}, err
	}
	task, err := state.Load(root, id)
	if err != nil {
		return wakeSample{}, err
	}
	task.UpdatedAt = time.Now().Add(-time.Minute)
	if err := state.Save(root, task); err != nil {
		return wakeSample{}, err
	}
	clearAgentFiles(statusFile, seenFile)

	var stderr strings.Builder
	await := exec.Command(b.cfg.vx, "sentinel", "await")
	await.Dir = w.project
	await.Env = w.env(spec{fake: true})
	await.Stderr = &stderr
	if err := await.Start(); err != nil {
		return wakeSample{}, fmt.Errorf("starting the await: %w", err)
	}
	type exit struct {
		err error
		at  time.Time
	}
	exited := make(chan exit, 1)
	go func() {
		err := await.Wait()
		exited <- exit{err: err, at: time.Now()}
	}()
	kill := func() {
		_ = await.Process.Kill()
		<-exited
	}

	// Registered means it is waiting: it has done its first look for a wake.
	if err := waitForFile(filepath.Join(w.vexillumHome(), "sentinel-awaiters", strconv.Itoa(await.Process.Pid)+".json"), 10*time.Second); err != nil {
		kill()
		return wakeSample{}, fmt.Errorf("the await did not register: %w", err)
	}
	time.Sleep(jitter(run))

	wakes := make(chan time.Time, 1)
	stop := make(chan struct{})
	defer close(stop)
	go watchForWake(filepath.Join(root, "wakes"), wakes, stop)

	t0 := time.Now()
	if err := writeFileAtomic(statusFile, "idle\n"); err != nil {
		kill()
		return wakeSample{}, err
	}

	var done exit
	select {
	case done = <-exited:
	case <-time.After(wakeRunTimeout):
		kill()
		return wakeSample{}, fmt.Errorf("no wake reached the await within %s", wakeRunTimeout)
	}
	var exitCode int
	var exitErr *exec.ExitError
	if errors.As(done.err, &exitErr) {
		exitCode = exitErr.ExitCode()
	} else if done.err != nil {
		return wakeSample{}, fmt.Errorf("waiting for the await: %w", done.err)
	}
	if exitCode != 2 || !strings.Contains(stderr.String(), id) {
		return wakeSample{}, fmt.Errorf("the await exited %d with %q, want exit 2 and a wake for task %s", exitCode, strings.TrimSpace(stderr.String()), id)
	}

	info, err := os.Stat(seenFile)
	if err != nil {
		return wakeSample{}, fmt.Errorf("the sentinel never asked herdr about the finished soldier: %w", err)
	}
	seen := info.ModTime()
	// The watcher polls every millisecond; an await that drained the wake
	// sooner than that leaves no sighting, and its poll phase is then zero.
	woke := done.at
	select {
	case woke = <-wakes:
	default:
	}

	// Strike while herdr still says idle, or the sentinel would reopen the
	// finished task on its next pass; only then let the status go back to working.
	out, err := b.invoke(w, spec{exe: "vx", args: []string{"strike", id, "--discard"}, fake: true})
	if err != nil || out.exit != 0 {
		return wakeSample{}, fmt.Errorf("striking %s: exit %d, err %v\n%s%s", id, out.exit, err, out.stdout, out.stderr)
	}
	clearAgentFiles(statusFile, seenFile)

	return newWakeSample(t0, seen, woke, done.at), nil
}

// clearAgentFiles takes the fake herdr back to its default: a soldier at work,
// and nobody having seen it finish.
func clearAgentFiles(statusFile, seenFile string) {
	_ = os.Remove(statusFile)
	_ = os.Remove(seenFile)
}

// writeFileAtomic writes the file so a reader never sees it half written.
func writeFileAtomic(path, content string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func waitForFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("%s did not appear within %s", path, timeout)
}

// hasWake reports whether dir holds a pending wake file.
func hasWake(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
			return true
		}
	}
	return false
}

// watchForWake reports the moment a wake file shows up in dir, polling every
// millisecond, until stop is closed.
func watchForWake(dir string, found chan<- time.Time, stop <-chan struct{}) {
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if hasWake(dir) {
			found <- time.Now()
			return
		}
		select {
		case <-tick.C:
		case <-stop:
			return
		}
	}
}
