package sentinel_test

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/sentinel"
	"github.com/isaias-alt/vexillum/internal/state"
)

// seedWatchedProjects fills home with projects of tasksPer running tasks each
// and returns the fake herdr that reports every one of them as still
// working: the steady state the sentinel spends nearly all of its life in.
func seedWatchedProjects(b *testing.B, home string, projects, tasksPer int) *fakeHerdr {
	b.Helper()
	fake := &fakeHerdr{statuses: map[string]string{}}
	for p := 0; p < projects; p++ {
		root := projectRoot(home, fmt.Sprintf("repo-%08d", p))
		for t := 0; t < tasksPer; t++ {
			task, err := state.New(state.KindMission, "do the thing")
			if err != nil {
				b.Fatal(err)
			}
			task.Status = state.StatusRunning
			task.HerdrAgentName = fmt.Sprintf("agent-%d-%d", p, t)
			task.UpdatedAt = time.Now().Add(-time.Minute)
			if err := state.Save(root, task); err != nil {
				b.Fatal(err)
			}
			fake.statuses[task.HerdrAgentName] = "working"
		}
	}
	return fake
}

// BenchmarkTickSteadyState is one sentinel pass over tasks that are all still
// working, so nothing is written. The herdr client is in memory: in real use
// each task adds one "herdr agent get" process to the pass, which this
// benchmark deliberately leaves out (see bench/ for those).
func BenchmarkTickSteadyState(b *testing.B) {
	for _, shape := range []struct{ projects, tasksPer int }{{1, 1}, {1, 10}, {5, 10}, {20, 10}} {
		b.Run(fmt.Sprintf("projects=%d/tasks=%d", shape.projects, shape.tasksPer), func(b *testing.B) {
			home := b.TempDir()
			fake := seedWatchedProjects(b, home, shape.projects, shape.tasksPer)
			sweeper := sentinel.NewSweeper(io.Discard)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := sweeper.Tick(home, fake); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkTickSettledLeased is a pass over finished tasks whose camp lease is
// still held, the case reopenActiveTasks probes on every tick: one pool read
// and one herdr status call per task.
func BenchmarkTickSettledLeased(b *testing.B) {
	for _, n := range []int{1, 10, 50} {
		b.Run(fmt.Sprintf("tasks=%d", n), func(b *testing.B) {
			home := b.TempDir()
			root := projectRoot(home, "repo-00000000")
			fake := &fakeHerdr{statuses: map[string]string{}}
			slots := ""
			for i := 0; i < n; i++ {
				task, err := state.New(state.KindMission, "do the thing")
				if err != nil {
					b.Fatal(err)
				}
				task.Status = state.StatusDone
				task.HerdrAgentName = fmt.Sprintf("agent-%d", i)
				if err := state.Save(root, task); err != nil {
					b.Fatal(err)
				}
				fake.statuses[task.HerdrAgentName] = "idle"
				if i > 0 {
					slots += ","
				}
				slots += fmt.Sprintf(`{"number":%d,"branch":"vexillum/%s","leased_by":%q}`, i+1, task.ID, task.ID)
			}
			poolDir := filepath.Join(root, "camps")
			if err := os.MkdirAll(poolDir, 0o755); err != nil {
				b.Fatal(err)
			}
			pool := `{"schema_version":0,"slots":[` + slots + `]}`
			if err := os.WriteFile(filepath.Join(poolDir, "pool.json"), []byte(pool), 0o644); err != nil {
				b.Fatal(err)
			}
			sweeper := sentinel.NewSweeper(io.Discard)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := sweeper.Tick(home, fake); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkDrainEmpty is what the Stop hook's drain pays when nothing is
// pending: a directory read that finds no wakes.
func BenchmarkDrainEmpty(b *testing.B) {
	root := projectRoot(b.TempDir(), "repo-00000000")
	if err := os.MkdirAll(filepath.Join(root, "wakes"), 0o755); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := sentinel.Drain(root); err != nil {
			b.Fatal(err)
		}
	}
}
