package camp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func runGitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// initProjectRepo creates a git repo with a single commit on a branch
// named "main", regardless of this machine's init.defaultBranch config.
func initProjectRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGitT(t, dir, "init", "-q")
	runGitT(t, dir, "symbolic-ref", "HEAD", "refs/heads/main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("test project\n"), 0o644); err != nil {
		t.Fatalf("writing README: %v", err)
	}
	runGitT(t, dir, "add", "README.md")
	runGitT(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "initial commit")
	return dir
}

func commitAll(t *testing.T, dir, message string) {
	t.Helper()
	runGitT(t, dir, "add", "-A")
	runGitT(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", message)
}

// L3-01: Acquire creates an isolated worktree on its own branch, without
// touching the project's own working tree.
func TestAcquire_CreatesIsolatedWorktree(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	c, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if info, err := os.Stat(c.Path); err != nil || !info.IsDir() {
		t.Fatalf("expected camp worktree at %s: %v", c.Path, err)
	}
	if c.Branch != "vexillum/task-1" {
		t.Errorf("expected branch vexillum/task-1, got %s", c.Branch)
	}

	branchInCamp := strings.TrimSpace(runGitT(t, c.Path, "rev-parse", "--abbrev-ref", "HEAD"))
	if branchInCamp != c.Branch {
		t.Errorf("expected camp to be on %s, got %s", c.Branch, branchInCamp)
	}

	branchInProject := strings.TrimSpace(runGitT(t, project, "rev-parse", "--abbrev-ref", "HEAD"))
	if branchInProject != "main" {
		t.Errorf("expected project working tree to stay on main, got %s", branchInProject)
	}
}

// L3-07: releasing a clean, landed camp returns its slot to the pool, and
// a later Acquire reuses that same slot/worktree instead of creating a
// new one.
func TestAcquireRelease_ReusesSlot(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	c1, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatalf("Acquire 1: %v", err)
	}

	if err := os.WriteFile(filepath.Join(c1.Path, "change.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("writing change: %v", err)
	}
	commitAll(t, c1.Path, "task 1 change")
	runGitT(t, project, "merge", "--ff-only", c1.Branch)

	if err := Release(c1, "task-1"); err != nil {
		t.Fatalf("Release: %v", err)
	}

	c2, err := Acquire(project, home, "task-2")
	if err != nil {
		t.Fatalf("Acquire 2: %v", err)
	}
	if c2.Slot != c1.Slot {
		t.Errorf("expected slot reuse: slot 1 = %d, slot 2 = %d", c1.Slot, c2.Slot)
	}
	if c2.Path != c1.Path {
		t.Errorf("expected the same worktree path reused, got %s vs %s", c1.Path, c2.Path)
	}
}

// L3-05: Release refuses to return a camp with uncommitted changes.
func TestRelease_RefusesDirtyCamp(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	c, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Path, "untracked.txt"), []byte("oops\n"), 0o644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	if err := Release(c, "task-1"); err == nil {
		t.Fatal("expected Release to refuse a dirty camp")
	}
}

// L3-06: Release refuses a camp whose branch has commits not yet landed
// on the base branch.
func TestRelease_RefusesUnlandedCamp(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	c, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Path, "change.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("writing change: %v", err)
	}
	commitAll(t, c.Path, "unlanded change")

	if err := Release(c, "task-1"); err == nil {
		t.Fatal("expected Release to refuse an unlanded camp")
	}
}

// A squash (or rebase) merge on GitHub never makes the camp branch a git
// ancestor of the base branch - the merge commit's parent is the
// pre-merge base, not the camp's tip. Release must still recognize the
// work as landed once the project's own checkout has the same content,
// via the content-in-base fallback (mirrors github.com/kunchenguid/
// firstmate's content_in_default, used for exactly this case).
func TestRelease_AcceptsSquashMergedContent(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	c, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Path, "change.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("writing change in camp: %v", err)
	}
	commitAll(t, c.Path, "camp change")

	// Simulate GitHub squash-merging the camp's branch: the exact same
	// content lands on the project's base branch, but as a brand new
	// commit with no ancestry link back to the camp's branch tip.
	if err := os.WriteFile(filepath.Join(project, "change.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("writing change in project: %v", err)
	}
	commitAll(t, project, "squash-merge camp change")

	if err := Release(c, "task-1"); err != nil {
		t.Fatalf("expected Release to accept squash-merged content, got: %v", err)
	}
}

// A camp whose branch genuinely diverged - neither an ancestor of base
// nor matching its content - is still refused. The content fallback must
// not paper over real unlanded work.
func TestRelease_RefusesGenuinelyDivergedCamp(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	c, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Path, "change.txt"), []byte("camp-only\n"), 0o644); err != nil {
		t.Fatalf("writing change in camp: %v", err)
	}
	commitAll(t, c.Path, "camp change")

	// The project's base branch also moved, but with unrelated content -
	// the camp's change was never applied anywhere.
	if err := os.WriteFile(filepath.Join(project, "unrelated.txt"), []byte("unrelated\n"), 0o644); err != nil {
		t.Fatalf("writing unrelated change: %v", err)
	}
	commitAll(t, project, "unrelated base change")

	if err := Release(c, "task-1"); err == nil {
		t.Fatal("expected Release to refuse a camp whose content never landed")
	}
}

// L3-09: Release refuses when the caller isn't the slot's recorded owner.
func TestRelease_RefusesWrongOwner(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	c, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if err := Release(c, "someone-else"); err == nil {
		t.Fatal("expected Release to refuse a non-owning caller")
	}
}

// L3-08: a leased slot is never handed out to another task - Acquire
// creates a new one instead.
func TestAcquire_DoesNotReuseLeasedSlot(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	c1, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatalf("Acquire 1: %v", err)
	}

	c2, err := Acquire(project, home, "task-2")
	if err != nil {
		t.Fatalf("Acquire 2: %v", err)
	}

	if c2.Slot == c1.Slot {
		t.Fatalf("expected a distinct slot while task-1's camp is still leased, got %d for both", c1.Slot)
	}
}

// Resolve reconstructs an already-acquired camp from just its slot
// number, matching what Acquire returned.
func TestResolve_ReconstructsAcquiredCamp(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	acquired, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	resolved, err := Resolve(project, home, acquired.Slot)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved != acquired {
		t.Errorf("expected Resolve to reconstruct the same camp:\n got:  %+v\n want: %+v", resolved, acquired)
	}
}

// L3-10: Land fast-forwards the project's checkout to a clean camp
// branch that's ahead of the base with no divergence.
func TestLand_FastForwardsCleanBranch(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	c, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Path, "change.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("writing change: %v", err)
	}
	commitAll(t, c.Path, "task 1 change")

	if err := Land(c); err != nil {
		t.Fatalf("Land: %v", err)
	}

	got := strings.TrimSpace(runGitT(t, project, "log", "-1", "--format=%s"))
	if got != "task 1 change" {
		t.Errorf("expected the project checkout's HEAD to be the landed commit, got %q", got)
	}
	if _, err := os.Stat(filepath.Join(project, "change.txt")); err != nil {
		t.Errorf("expected change.txt to exist in the project checkout after landing: %v", err)
	}
}

// L3-11: Land refuses a branch that has diverged from the base instead
// of forcing or rebasing it.
func TestLand_RefusesDivergedBranch(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	c, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Path, "change.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("writing change: %v", err)
	}
	commitAll(t, c.Path, "task 1 change")

	// Advance the base past the camp's branch point, so the camp's
	// branch is no longer a fast-forward.
	if err := os.WriteFile(filepath.Join(project, "diverge.txt"), []byte("diverge\n"), 0o644); err != nil {
		t.Fatalf("writing diverging change: %v", err)
	}
	commitAll(t, project, "diverging base change")

	if err := Land(c); err == nil {
		t.Fatal("expected Land to refuse a diverged branch")
	}

	got := strings.TrimSpace(runGitT(t, project, "log", "-1", "--format=%s"))
	if got != "diverging base change" {
		t.Errorf("expected the project checkout to be untouched by the refused merge, got HEAD %q", got)
	}
}

// L3-12: Land refuses to touch a dirty project checkout.
func TestLand_RefusesDirtyProjectCheckout(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	c, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Path, "change.txt"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("writing change: %v", err)
	}
	commitAll(t, c.Path, "task 1 change")

	if err := os.WriteFile(filepath.Join(project, "uncommitted.txt"), []byte("oops\n"), 0o644); err != nil {
		t.Fatalf("writing uncommitted file in project: %v", err)
	}

	if err := Land(c); err == nil {
		t.Fatal("expected Land to refuse a dirty project checkout")
	}
}

// Capa 4 paso 3: N soldiers dispatched at once each call Acquire
// concurrently (separate `vexillum dispatch` processes racing against the
// same pool). Acquire's read-modify-write over pool.json (scan for an
// idle slot, or compute len(pool.Slots)+1 for a new one, then save) has
// no mutual exclusion between the read and the write - two concurrent
// calls can both read the same pool state before either writes back,
// both compute the same slot number, and both run `git worktree add` at
// the identical path. Confirms every concurrent Acquire gets a genuinely
// distinct slot and worktree, and the pool's own bookkeeping ends up
// consistent (exactly N slots, all correctly leased).
func TestAcquire_ConcurrentCallsNeverCollideOnASlot(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	const n = 8
	type result struct {
		c   Camp
		err error
	}
	results := make([]result, n)

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := Acquire(project, home, fmt.Sprintf("task-%d", i))
			results[i] = result{c: c, err: err}
		}(i)
	}
	wg.Wait()

	seenSlots := map[int]bool{}
	seenPaths := map[string]bool{}
	for i, r := range results {
		if r.err != nil {
			t.Fatalf("Acquire %d: %v", i, r.err)
		}
		if seenSlots[r.c.Slot] {
			t.Fatalf("slot %d handed out to more than one concurrent Acquire", r.c.Slot)
		}
		seenSlots[r.c.Slot] = true
		if seenPaths[r.c.Path] {
			t.Fatalf("worktree path %s handed out to more than one concurrent Acquire", r.c.Path)
		}
		seenPaths[r.c.Path] = true

		if info, err := os.Stat(r.c.Path); err != nil || !info.IsDir() {
			t.Errorf("expected a real worktree at %s: %v", r.c.Path, err)
		}
	}
	if len(seenSlots) != n {
		t.Errorf("expected %d distinct slots, got %d", n, len(seenSlots))
	}

	pool, err := loadPool(filepath.Dir(filepath.Dir(results[0].c.Path)))
	if err != nil {
		t.Fatalf("loadPool: %v", err)
	}
	if len(pool.Slots) != n {
		t.Fatalf("expected the pool to end up with exactly %d slots, got %d", n, len(pool.Slots))
	}
	leased := map[string]bool{}
	for _, s := range pool.Slots {
		if s.LeasedBy == "" {
			t.Errorf("slot %d ended up unleased after every task acquired one", s.Number)
		}
		if leased[s.LeasedBy] {
			t.Errorf("task %s leased more than one slot", s.LeasedBy)
		}
		leased[s.LeasedBy] = true
	}
}

// Release has the same unprotected read-modify-write shape as Acquire -
// N soldiers finishing around the same time means N concurrent Release
// calls against the same pool.json. Confirms they don't lose each
// other's updates (every slot ends up correctly unleased, not just
// whichever write happened to land last).
func TestRelease_ConcurrentCallsDoNotLoseUpdates(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	const n = 8
	camps := make([]Camp, n)
	for i := 0; i < n; i++ {
		c, err := Acquire(project, home, fmt.Sprintf("task-%d", i))
		if err != nil {
			t.Fatalf("Acquire %d: %v", i, err)
		}
		camps[i] = c
	}

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = Release(camps[i], fmt.Sprintf("task-%d", i))
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("Release %d: %v", i, err)
		}
	}

	pool, err := loadPool(camps[0].PoolRoot)
	if err != nil {
		t.Fatalf("loadPool: %v", err)
	}
	if len(pool.Slots) != n {
		t.Fatalf("expected %d slots, got %d", n, len(pool.Slots))
	}
	for _, s := range pool.Slots {
		if s.LeasedBy != "" {
			t.Errorf("slot %d still shows leased by %q after every task released", s.Number, s.LeasedBy)
		}
	}
}

// A2-01: Discard throws away a dirty, unlanded camp - the worktree comes
// back clean, the branch is gone, and a later Acquire for the same task
// reuses the freed slot instead of creating a new one.
func TestDiscard_ClearsDirtyUnlandedCamp(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	c, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if err := os.WriteFile(filepath.Join(c.Path, "wip.txt"), []byte("half-finished\n"), 0o644); err != nil {
		t.Fatalf("writing wip file: %v", err)
	}
	commitAll(t, c.Path, "unlanded work from a dead soldier")
	if err := os.WriteFile(filepath.Join(c.Path, "untracked.txt"), []byte("stray\n"), 0o644); err != nil {
		t.Fatalf("writing untracked file: %v", err)
	}

	if err := Discard(c, "task-1"); err != nil {
		t.Fatalf("Discard: %v", err)
	}

	if status := strings.TrimSpace(runGitT(t, c.Path, "status", "--porcelain")); status != "" {
		t.Errorf("expected a clean worktree after Discard, got status:\n%s", status)
	}
	branches := runGitT(t, c.Path, "branch", "--list", c.Branch)
	if strings.TrimSpace(branches) != "" {
		t.Errorf("expected branch %s to be deleted, still present: %s", c.Branch, branches)
	}

	c2, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatalf("Acquire after Discard: %v", err)
	}
	if c2.Slot != c.Slot {
		t.Errorf("expected Acquire to reuse discarded slot %d, got slot %d", c.Slot, c2.Slot)
	}
	if _, err := os.Stat(filepath.Join(c2.Path, "wip.txt")); err == nil {
		t.Errorf("expected the discarded commit's file to be gone from the reused slot")
	}
}

// A2-02: Discard refuses a slot leased by another task, exactly like
// Release does.
func TestDiscard_RefusesWrongOwner(t *testing.T) {
	project := initProjectRepo(t)
	home := t.TempDir()

	c, err := Acquire(project, home, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	if err := Discard(c, "task-2"); err == nil {
		t.Fatal("expected Discard to refuse a slot leased by a different task")
	}

	pool, err := loadPool(c.PoolRoot)
	if err != nil {
		t.Fatalf("loadPool: %v", err)
	}
	if pool.Slots[0].LeasedBy != "task-1" {
		t.Errorf("expected slot to remain leased by task-1, got %q", pool.Slots[0].LeasedBy)
	}
}
