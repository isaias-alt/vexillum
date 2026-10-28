package camp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// benchProjectRepo is a git repo with one commit on main, like initProjectRepo
// but for a *testing.B.
func benchProjectRepo(b *testing.B) string {
	b.Helper()
	dir := b.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"symbolic-ref", "HEAD", "refs/heads/main"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("bench project\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "README.md"},
		{"-c", "user.name=Bench", "-c", "user.email=bench@example.com", "commit", "-q", "-m", "initial commit"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			b.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

// BenchmarkRunGit is the floor every camp operation is built from: one git
// process that only reads (rev-parse).
func BenchmarkRunGit(b *testing.B) {
	dir := benchProjectRepo(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := currentBranch(dir); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIsDirty is "git status --porcelain", which land, acquire and
// strike each run on the checkout they touch.
func BenchmarkIsDirty(b *testing.B) {
	dir := benchProjectRepo(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := isDirty(dir); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAcquireNew creates a worktree for every iteration (the pool never
// has an idle slot): the cost of the first dispatch of each parallel soldier.
func BenchmarkAcquireNew(b *testing.B) {
	project := benchProjectRepo(b)
	home := b.TempDir()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Acquire(project, home, fmt.Sprintf("task-%d", i)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkAcquireStrikeReuse is the steady-state camp cycle: acquire an idle
// slot (a checkout of the base and a new branch), then strike it back into the
// pool. They are timed together because the strike is what makes the slot
// reusable for the next iteration.
func BenchmarkAcquireStrikeReuse(b *testing.B) {
	project := benchProjectRepo(b)
	home := b.TempDir()
	first, err := Acquire(project, home, "task-warm")
	if err != nil {
		b.Fatal(err)
	}
	if err := Strike(first, "task-warm"); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := fmt.Sprintf("task-%d", i)
		c, err := Acquire(project, home, id)
		if err != nil {
			b.Fatal(err)
		}
		if err := Strike(c, id); err != nil {
			b.Fatal(err)
		}
	}
}
