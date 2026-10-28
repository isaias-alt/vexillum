package cli

import (
	"fmt"
	"io"
	"testing"

	"github.com/isaias-alt/vexillum/internal/pending"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/state"
)

// seedBenchTasks writes n tasks into the project's state and returns the
// project directory and the vexillum home to run a command against.
func seedBenchTasks(b *testing.B, n int) (projectDir, home string) {
	b.Helper()
	projectDir = b.TempDir()
	home = b.TempDir()
	root, err := project.Root(home, projectDir)
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < n; i++ {
		task, err := state.New(state.KindMission, fmt.Sprintf("task %d: refactor the parser and keep the tests green", i))
		if err != nil {
			b.Fatal(err)
		}
		task.Status = state.StatusRunning
		task.CampBranch = "vexillum/" + task.ID
		if err := state.Save(root, task); err != nil {
			b.Fatal(err)
		}
	}
	return projectDir, home
}

// BenchmarkRunStatus is "vx status" without the process: project root,
// task listing, sort and print.
func BenchmarkRunStatus(b *testing.B) {
	for _, n := range []int{0, 50, 500} {
		for _, asJSON := range []bool{false, true} {
			name := fmt.Sprintf("tasks=%d/json=%t", n, asJSON)
			b.Run(name, func(b *testing.B) {
				projectDir, home := seedBenchTasks(b, n)
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if code := runStatus(projectDir, home, asJSON, io.Discard, io.Discard); code != 0 {
						b.Fatalf("runStatus exited %d", code)
					}
				}
			})
		}
	}
}

func BenchmarkRunModels(b *testing.B) {
	projectDir := b.TempDir()
	home := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if code := runModels(projectDir, home, io.Discard, io.Discard); code != 0 {
			b.Fatalf("runModels exited %d", code)
		}
	}
}

func BenchmarkRunPendingList(b *testing.B) {
	for _, n := range []int{0, 50} {
		b.Run(fmt.Sprintf("items=%d", n), func(b *testing.B) {
			projectDir := b.TempDir()
			home := b.TempDir()
			root, err := project.Root(home, projectDir)
			if err != nil {
				b.Fatal(err)
			}
			for i := 0; i < n; i++ {
				if _, err := pending.Add(root, fmt.Sprintf("decision %d", i), []string{"yes", "no"}, 1); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if code := runPending(projectDir, home, []string{"list"}, io.Discard, io.Discard); code != 0 {
					b.Fatalf("runPending exited %d", code)
				}
			}
		})
	}
}
