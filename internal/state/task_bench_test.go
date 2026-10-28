package state

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// benchTask returns a task shaped like a running mission, with a prompt and
// an output transcript of a realistic size.
func benchTask(b *testing.B) Task {
	b.Helper()
	task, err := New(KindMission, strings.Repeat("refactor the parser and keep the tests green. ", 12))
	if err != nil {
		b.Fatal(err)
	}
	task.Status = StatusRunning
	task.Harness = "claude"
	task.Model = "sonnet"
	task.Effort = "medium"
	task.CampSlot = 1
	task.CampPath = "/home/user/.vexillum/projects/repo-1a2b3c4d/camps/1/repo"
	task.CampBranch = "vexillum/" + task.ID
	task.CampBase = "main"
	task.HerdrAgentName = "refactor-the-parser"
	task.Output = strings.Repeat("transcript line from the soldier pane\n", 60)
	task.UpdatedAt = time.Now().UTC()
	return task
}

func seedTasks(b *testing.B, root string, n int) []Task {
	b.Helper()
	tasks := make([]Task, 0, n)
	for i := 0; i < n; i++ {
		task := benchTask(b)
		if err := Save(root, task); err != nil {
			b.Fatal(err)
		}
		tasks = append(tasks, task)
	}
	return tasks
}

// BenchmarkSaveNew is the first write of a task: no file to read unknown
// fields from.
func BenchmarkSaveNew(b *testing.B) {
	root := b.TempDir()
	tasks := make([]Task, b.N)
	for i := range tasks {
		tasks[i] = benchTask(b)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := Save(root, tasks[i]); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSaveExisting is every later write of a task: Save also reads and
// parses the file on disk to carry unknown top-level keys over.
func BenchmarkSaveExisting(b *testing.B) {
	root := b.TempDir()
	task := benchTask(b)
	if err := Save(root, task); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := Save(root, task); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLoad(b *testing.B) {
	root := b.TempDir()
	task := seedTasks(b, root, 1)[0]
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Load(root, task.ID); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkUpdate is the locked read-modify-write the sentinel, land and
// dispatch all use: flock, Load, fn, Save.
func BenchmarkUpdate(b *testing.B) {
	root := b.TempDir()
	task := seedTasks(b, root, 1)[0]
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Update(root, task.ID, func(t *Task) (bool, error) {
			t.UpdatedAt = time.Now().UTC()
			return true, nil
		}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkList is what status, the sentinel's every tick and the muster
// board pay: read and decode every task file of a project.
func BenchmarkList(b *testing.B) {
	for _, n := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("tasks=%d", n), func(b *testing.B) {
			root := b.TempDir()
			seedTasks(b, root, n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tasks, skipped, err := List(root)
				if err != nil || len(skipped) != 0 || len(tasks) != n {
					b.Fatalf("List: %d tasks, %d skipped, err %v", len(tasks), len(skipped), err)
				}
			}
		})
	}
}
