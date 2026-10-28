package doctorcheck

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The results come back in the order of the checks, however the checks finish.
func TestRunAll_KeepsTheOrderOfTheChecks(t *testing.T) {
	// Check 0 finishes last, check 2 first.
	release := []chan struct{}{make(chan struct{}), make(chan struct{}), make(chan struct{})}
	checks := []Check{
		One(func() Result { <-release[0]; return Result{Name: "a"} }),
		Fixed(Result{Name: "b1"}, Result{Name: "b2"}),
		One(func() Result { <-release[2]; close(release[1]); return Result{Name: "c"} }),
		func() []Result { return nil },
		One(func() Result { return Result{Name: "e"} }),
	}
	// Open the gates in the reverse order of the checks.
	go func() {
		release[2] <- struct{}{}
		time.Sleep(10 * time.Millisecond)
		release[0] <- struct{}{}
	}()

	if got, want := names(RunAll(len(checks), checks)), "a, b1, b2, c, e"; got != want {
		t.Errorf("order = %s, want %s", got, want)
	}
}

// With room for every check, they all run at the same time: each waits for
// the others to have started, which a one-at-a-time run could never satisfy.
func TestRunAll_RunsChecksSideBySide(t *testing.T) {
	const n = 4
	var started sync.WaitGroup
	started.Add(n)
	allStarted := make(chan struct{})
	go func() { started.Wait(); close(allStarted) }()

	var checks []Check
	for i := range n {
		checks = append(checks, One(func() Result {
			started.Done()
			select {
			case <-allStarted:
				return Result{Name: fmt.Sprint(i), OK: true}
			case <-time.After(10 * time.Second):
				return Result{Name: fmt.Sprint(i)}
			}
		}))
	}
	for _, r := range RunAll(n, checks) {
		if !r.OK {
			t.Errorf("check %s never saw the others start, so they did not overlap", r.Name)
		}
	}
}

// No more than limit checks run at once, and a limit below 1 means one.
func TestRunAll_RespectsTheLimit(t *testing.T) {
	for _, limit := range []int{-3, 0, 1, 2, 3} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			var running, peak atomic.Int32
			var checks []Check
			for range 12 {
				checks = append(checks, One(func() Result {
					n := running.Add(1)
					for {
						p := peak.Load()
						if n <= p || peak.CompareAndSwap(p, n) {
							break
						}
					}
					time.Sleep(2 * time.Millisecond)
					running.Add(-1)
					return Result{Name: "x"}
				}))
			}
			if got := RunAll(limit, checks); len(got) != 12 {
				t.Fatalf("got %d results, want 12", len(got))
			}
			if want := int32(max(limit, 1)); peak.Load() > want {
				t.Errorf("up to %d checks ran at once, limit %d", peak.Load(), want)
			}
		})
	}
}

func TestRunAll_NoChecks(t *testing.T) {
	if got := RunAll(4, nil); len(got) != 0 {
		t.Errorf("got %+v", got)
	}
}
