package sentinel_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/sentinel"
)

// wakeDetectedAt records a task that is Done, without a camp, and a pending
// wake announcing it that was detected at the given time: a wake that is news
// in every way but its age.
func wakeDetectedAt(t *testing.T, root string, at time.Time) sentinel.Wake {
	t.Helper()
	w := currentWake(t, root)
	w.DetectedAt = at.UTC()
	writeWakeFile(t, root, w)
	return w
}

func TestWakeIsStale_Boundary(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		at   time.Time
		want bool
	}{
		{"just under the TTL", now.Add(-sentinel.StaleWakeAfter + time.Second), false},
		{"exactly the TTL", now.Add(-sentinel.StaleWakeAfter), false},
		{"just over the TTL", now.Add(-sentinel.StaleWakeAfter - time.Second), true},
		{"fresh", now, false},
		{"no detection time", time.Time{}, false},
	} {
		if got := (sentinel.Wake{DetectedAt: tc.at}).IsStale(now); got != tc.want {
			t.Errorf("%s: IsStale = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestClaimAt_DiscardsAStaleWakeAndDeliversAFreshOneInTheSameRun(t *testing.T) {
	root := projectRoot(t.TempDir(), "proj1")
	now := time.Now()
	stale := wakeDetectedAt(t, root, now.Add(-sentinel.StaleWakeAfter-time.Hour))
	fresh := wakeDetectedAt(t, root, now.Add(-sentinel.StaleWakeAfter+time.Hour))

	claim, err := sentinel.ClaimAt(root, now)
	if err != nil {
		t.Fatal(err)
	}
	if claim.Len() != 1 || claim.Wakes[0].TaskID != fresh.TaskID {
		t.Fatalf("claimed %+v, want only the fresh wake %s", claim.Wakes, fresh.TaskID)
	}
	if strings.Contains(claim.Reason(), stale.TaskID) {
		t.Errorf("the reason %q tells the commander about the stale wake", claim.Reason())
	}
	if err := claim.Commit(); err != nil {
		t.Fatal(err)
	}
	if files := wakeFiles(t, root); len(files) != 0 {
		t.Errorf("files left = %v, want none: the stale wake is deleted, not kept queued", files)
	}
}

func TestDrain_DropsAWakeNobodyListenedToForLongerThanTheTTL(t *testing.T) {
	root := projectRoot(t.TempDir(), "proj1")
	wakeDetectedAt(t, root, time.Now().Add(-sentinel.StaleWakeAfter-time.Minute))

	got, err := sentinel.Drain(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("drained %+v, want nothing for a stale wake", got)
	}
	if files := wakeFiles(t, root); len(files) != 0 {
		t.Errorf("files left = %v, want none", files)
	}
}

func TestDrain_AFreshWakeIsStillDeliveredOnce(t *testing.T) {
	root := projectRoot(t.TempDir(), "proj1")
	w := wakeDetectedAt(t, root, time.Now().Add(-sentinel.StaleWakeAfter+time.Hour))

	got, err := sentinel.Drain(root)
	if err != nil || len(got) != 1 || got[0].TaskID != w.TaskID {
		t.Fatalf("drained %+v, %v, want the wake of %s", got, err, w.TaskID)
	}
}

func TestPurgeStaleWakes_RemovesOnlyTheStaleOnes(t *testing.T) {
	root := projectRoot(t.TempDir(), "proj1")
	now := time.Now()
	wakeDetectedAt(t, root, now.Add(-sentinel.StaleWakeAfter-time.Second))
	wakeDetectedAt(t, root, now.Add(-2*sentinel.StaleWakeAfter))
	fresh := wakeDetectedAt(t, root, now.Add(-sentinel.StaleWakeAfter+time.Second))

	n, err := sentinel.PurgeStaleWakes(root, now)
	if err != nil || n != 2 {
		t.Fatalf("PurgeStaleWakes = %d, %v, want 2", n, err)
	}
	files := wakeFiles(t, root)
	if len(files) != 1 || files[0] != fresh.TaskID+".json" {
		t.Errorf("files left = %v, want only the fresh wake", files)
	}
	if n, _ := sentinel.PurgeStaleWakes(root, now); n != 0 {
		t.Errorf("a second purge removed %d, want 0", n)
	}
}

func TestPurgeStaleWakes_NothingToPurgeInAMissingQueue(t *testing.T) {
	if n, err := sentinel.PurgeStaleWakes(projectRoot(t.TempDir(), "none"), time.Now()); n != 0 || err != nil {
		t.Errorf("PurgeStaleWakes = %d, %v, want nothing", n, err)
	}
}

// The sentinel's own sweep removes a stale wake even when nobody drains.
func TestSweeperTick_DropsStaleWakesOfEveryProject(t *testing.T) {
	home := t.TempDir()
	a, b := projectRoot(home, "a-1"), projectRoot(home, "b-2")
	now := time.Now()
	wakeDetectedAt(t, a, now.Add(-sentinel.StaleWakeAfter-time.Hour))
	fresh := wakeDetectedAt(t, b, now)
	wakeDetectedAt(t, b, now.Add(-sentinel.StaleWakeAfter-time.Hour))

	var log bytes.Buffer
	s := sentinel.NewSweeper(&log)
	s.Now = func() time.Time { return now }
	if _, err := s.Tick(home, &fakeHerdr{}); err != nil {
		t.Fatal(err)
	}

	if files := wakeFiles(t, a); len(files) != 0 {
		t.Errorf("project a files = %v, want none", files)
	}
	if files := wakeFiles(t, b); len(files) != 1 || files[0] != fresh.TaskID+".json" {
		t.Errorf("project b files = %v, want only the fresh wake", files)
	}
	if !strings.Contains(log.String(), "dropped 1 stale wake(s)") {
		t.Errorf("log = %q, want the drops reported", log.String())
	}
	// A sweep that finds nothing stale says nothing.
	log.Reset()
	if _, err := s.Tick(home, &fakeHerdr{}); err != nil {
		t.Fatal(err)
	}
	if log.Len() != 0 {
		t.Errorf("second sweep logged %q, want nothing", log.String())
	}
}

func TestPendingQueues_SkipsStaleTaskWakes(t *testing.T) {
	home := t.TempDir()
	root := projectRoot(home, "old-1")
	now := time.Now()
	wakeDetectedAt(t, root, now.Add(-sentinel.StaleWakeAfter-time.Second))

	if queues := sentinel.PendingQueues(home, now); len(queues) != 0 {
		t.Errorf("queues = %+v, want none for a stale wake", queues)
	}
	if queues := sentinel.PendingQueues(home, now.Add(-time.Hour)); len(queues) != 1 || queues[0].Count != 1 {
		t.Errorf("queues = %+v, want the wake counted while it is under the TTL", queues)
	}
}
