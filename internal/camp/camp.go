// Package camp manages a pool of reusable, isolated git worktrees per
// project - vexillum's "camp" concept, one worktree per task. Inspired by
// treehouse (see docs/references.md): a task acquires a clean, landed slot
// from the pool if one is available, or a fresh worktree otherwise; a slot
// is returned to the pool only when it's safe to reuse - never dirty,
// never holding unlanded commits, and only by the task that actually
// leases it.
package camp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
)

// poolSchemaVersion is the pool state file's schema version.
const poolSchemaVersion = 1

// Camp is one acquired worktree: an isolated checkout of the project on
// its own branch, ready for a soldier to work in.
type Camp struct {
	ProjectDir string `json:"project_dir"`
	PoolRoot   string `json:"pool_root"`
	Path       string `json:"path"`
	Branch     string `json:"branch"`
	Slot       int    `json:"slot"`
}

type poolSlot struct {
	Number   int    `json:"number"`
	Branch   string `json:"branch"`
	LeasedBy string `json:"leased_by,omitempty"` // task id; empty means idle
}

type poolState struct {
	SchemaVersion int        `json:"schema_version"`
	Slots         []poolSlot `json:"slots"`
}

// Acquire returns a camp for taskID: a clean, already-landed slot reused
// from the pool if one exists, or a freshly created worktree otherwise.
// The project's own working tree is never touched.
func Acquire(projectDir, vexillumHome, taskID string) (Camp, error) {
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		return Camp{}, fmt.Errorf("resolving project path: %w", err)
	}
	repoName := filepath.Base(absProject)
	poolRoot := filepath.Join(vexillumHome, fmt.Sprintf("%s-%s", repoName, shortHash(absProject)))

	if err := os.MkdirAll(poolRoot, 0o755); err != nil {
		return Camp{}, fmt.Errorf("creating camp pool directory: %w", err)
	}

	// N soldiers dispatched at once means N concurrent Acquire calls (each
	// a separate `vexillum dispatch` process) racing against the same
	// pool.json - without serializing the read-modify-write below, two
	// could both read the pool before either writes back, both compute
	// the same slot number, and both run `git worktree add` at the
	// identical path (confirmed live: reproduced 100% of the time with 8
	// concurrent Acquire calls before this lock existed).
	unlock, err := lockPool(poolRoot)
	if err != nil {
		return Camp{}, err
	}
	defer unlock()

	pool, err := loadPool(poolRoot)
	if err != nil {
		return Camp{}, err
	}

	base, err := currentBranch(absProject)
	if err != nil {
		return Camp{}, fmt.Errorf("resolving base branch: %w", err)
	}

	branch := "vexillum/" + taskID

	for i := range pool.Slots {
		slot := &pool.Slots[i]
		if slot.LeasedBy != "" {
			continue
		}
		worktreePath := filepath.Join(poolRoot, strconv.Itoa(slot.Number), repoName)

		dirty, err := isDirty(worktreePath)
		if err != nil {
			// Slot's worktree is missing or broken; skip it rather than
			// fail the whole acquire.
			continue
		}
		if dirty {
			continue
		}
		if _, err := runGit(worktreePath, "checkout", "--detach", base); err != nil {
			continue
		}
		if _, err := runGit(worktreePath, "checkout", "-b", branch); err != nil {
			continue
		}

		slot.LeasedBy = taskID
		slot.Branch = branch
		if err := savePool(poolRoot, pool); err != nil {
			return Camp{}, err
		}
		return Camp{ProjectDir: absProject, PoolRoot: poolRoot, Path: worktreePath, Branch: branch, Slot: slot.Number}, nil
	}

	number := len(pool.Slots) + 1
	worktreePath := filepath.Join(poolRoot, strconv.Itoa(number), repoName)
	if _, err := runGit(absProject, "worktree", "add", "-b", branch, worktreePath, base); err != nil {
		return Camp{}, fmt.Errorf("creating camp worktree: %w", err)
	}

	pool.Slots = append(pool.Slots, poolSlot{Number: number, Branch: branch, LeasedBy: taskID})
	if err := savePool(poolRoot, pool); err != nil {
		return Camp{}, err
	}

	return Camp{ProjectDir: absProject, PoolRoot: poolRoot, Path: worktreePath, Branch: branch, Slot: number}, nil
}

// Resolve reconstructs the Camp for an already-acquired slot from the
// pool's own bookkeeping. Useful for a caller that only kept the slot
// number (e.g. persisted on a state.Task) and needs the rest of Camp back
// to call Release.
func Resolve(projectDir, vexillumHome string, slot int) (Camp, error) {
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		return Camp{}, fmt.Errorf("resolving project path: %w", err)
	}
	repoName := filepath.Base(absProject)
	poolRoot := filepath.Join(vexillumHome, fmt.Sprintf("%s-%s", repoName, shortHash(absProject)))

	pool, err := loadPool(poolRoot)
	if err != nil {
		return Camp{}, err
	}
	for _, s := range pool.Slots {
		if s.Number == slot {
			worktreePath := filepath.Join(poolRoot, strconv.Itoa(slot), repoName)
			return Camp{ProjectDir: absProject, PoolRoot: poolRoot, Path: worktreePath, Branch: s.Branch, Slot: slot}, nil
		}
	}
	return Camp{}, fmt.Errorf("camp slot %d not found in pool state for %s", slot, absProject)
}

// Land fast-forwards the project's own checkout to c's branch - the
// "local-only" delivery mode from firstmate (bin/fm-merge-local.sh),
// which vexillum mirrors here rather than leaving the commander to run
// raw git commands on its own judgment. It never forces anything: the
// project's checkout must already be clean, and the merge must be a
// clean fast-forward (c's branch has no divergent history from the base -
// only commits ahead of it). If the base moved since the camp was
// created, Land refuses and says so, instead of rebasing or forcing a
// merge - that's a decision for a human or the soldier that owns the
// branch, not this function.
func Land(c Camp) error {
	base, err := currentBranch(c.ProjectDir)
	if err != nil {
		return fmt.Errorf("resolving base branch: %w", err)
	}

	dirty, err := isDirty(c.ProjectDir)
	if err != nil {
		return fmt.Errorf("checking project checkout for uncommitted changes: %w", err)
	}
	if dirty {
		return fmt.Errorf("project checkout %s has uncommitted changes, refusing to land %s", c.ProjectDir, c.Branch)
	}

	fastForward, err := isAncestor(c.ProjectDir, base, c.Branch)
	if err != nil {
		return fmt.Errorf("checking whether %s is a fast-forward of %s: %w", c.Branch, base, err)
	}
	if !fastForward {
		// firstmate's own fm-merge-local.sh hits this exact case landing
		// sibling missions dispatched from the same base one at a time:
		// once the first lands, base has moved, so every other still-open
		// mission stops being a fast-forward - not a defect, an inherent
		// property of fast-forward-only landing with more than one
		// branch sharing an ancestor. Its own message ("Have the crewmate
		// rebase $BRANCH onto $DEFAULT, then retry") names who should do
		// it - the soldier, with full context of its own change, not
		// this function or the commander guessing at a rebase from
		// outside.
		return fmt.Errorf("%s is not a fast-forward of %s (it has diverged) - have the soldier rebase %s onto %s, then retry", c.Branch, base, c.Branch, base)
	}

	if _, err := runGit(c.ProjectDir, "merge", "--ff-only", c.Branch); err != nil {
		return fmt.Errorf("landing %s into %s: %w", c.Branch, base, err)
	}
	return nil
}

// Release returns c's slot to the pool for reuse, but only when it's safe:
// taskID must be the slot's recorded owner, the worktree must have no
// uncommitted changes, and its branch must already be landed (merged) on
// the project's base branch. The worktree itself is never destroyed - a
// released slot stays on disk, ready for the next Acquire to reset and
// reuse.
func Release(c Camp, taskID string) error {
	unlock, err := lockPool(c.PoolRoot)
	if err != nil {
		return err
	}
	defer unlock()

	pool, err := loadPool(c.PoolRoot)
	if err != nil {
		return err
	}

	idx := -1
	for i := range pool.Slots {
		if pool.Slots[i].Number == c.Slot {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("camp slot %d not found in pool state", c.Slot)
	}

	slot := pool.Slots[idx]
	if slot.LeasedBy == "" {
		return fmt.Errorf("camp slot %d is not leased, nothing to release", c.Slot)
	}
	if slot.LeasedBy != taskID {
		return fmt.Errorf("camp slot %d is leased by task %s, not %s, refusing to release", c.Slot, slot.LeasedBy, taskID)
	}

	dirty, err := isDirty(c.Path)
	if err != nil {
		return fmt.Errorf("checking camp for uncommitted changes: %w", err)
	}
	if dirty {
		return fmt.Errorf("camp %s has uncommitted changes, refusing to release", c.Path)
	}

	base, err := currentBranch(c.ProjectDir)
	if err != nil {
		return fmt.Errorf("resolving base branch: %w", err)
	}
	landed, err := isAncestor(c.Path, "HEAD", base)
	if err != nil {
		return fmt.Errorf("checking whether camp branch landed: %w", err)
	}
	if !landed {
		// A plain ancestor check only proves a "vexillum land"
		// fast-forward. A shipped mission whose PR merged via squash or
		// rebase on GitHub never satisfies it, even after a real local
		// pull - the merge commit's parent is the pre-merge base, not
		// this camp's tip. Fall back to a content check: does merging
		// this branch into the current base introduce anything base
		// doesn't already have? If not, the work already landed, just
		// under different commit SHAs. Same technique
		// github.com/kunchenguid/firstmate uses for this exact case.
		landed, err = contentAlreadyInBase(c.Path, base)
		if err != nil {
			return fmt.Errorf("checking whether camp content already landed: %w", err)
		}
	}
	if !landed {
		return fmt.Errorf("camp %s branch %s has commits not yet landed on %s, refusing to release", c.Path, c.Branch, base)
	}

	pool.Slots[idx].LeasedBy = ""
	return savePool(c.PoolRoot, pool)
}

// Discard is Release's deliberately destructive counterpart (PRD v2,
// A.2): where Release refuses anything but a clean, landed camp, Discard
// throws away whatever's there - a dead soldier's half-finished working
// tree and any commits it never landed - and returns the slot to the pool
// clean, ready for immediate reuse (typically by a re-dispatch of the
// same task). taskID must still be the slot's recorded owner: that guard
// isn't relaxed just because this path is destructive - never discard
// another task's camp.
func Discard(c Camp, taskID string) error {
	unlock, err := lockPool(c.PoolRoot)
	if err != nil {
		return err
	}
	defer unlock()

	pool, err := loadPool(c.PoolRoot)
	if err != nil {
		return err
	}

	idx := -1
	for i := range pool.Slots {
		if pool.Slots[i].Number == c.Slot {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("camp slot %d not found in pool state", c.Slot)
	}

	slot := pool.Slots[idx]
	if slot.LeasedBy == "" {
		return fmt.Errorf("camp slot %d is not leased, nothing to discard", c.Slot)
	}
	if slot.LeasedBy != taskID {
		return fmt.Errorf("camp slot %d is leased by task %s, not %s, refusing to discard", c.Slot, slot.LeasedBy, taskID)
	}

	base, err := currentBranch(c.ProjectDir)
	if err != nil {
		return fmt.Errorf("resolving base branch: %w", err)
	}

	if _, err := runGit(c.Path, "reset", "--hard"); err != nil {
		return fmt.Errorf("resetting camp worktree: %w", err)
	}
	if _, err := runGit(c.Path, "clean", "-fd"); err != nil {
		return fmt.Errorf("cleaning camp worktree: %w", err)
	}
	if _, err := runGit(c.Path, "checkout", "--detach", base); err != nil {
		return fmt.Errorf("detaching camp worktree from %s: %w", c.Branch, err)
	}
	if _, err := runGit(c.Path, "branch", "-D", c.Branch); err != nil {
		return fmt.Errorf("deleting camp branch %s: %w", c.Branch, err)
	}

	pool.Slots[idx].LeasedBy = ""
	pool.Slots[idx].Branch = ""
	return savePool(c.PoolRoot, pool)
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}

func poolStatePath(poolRoot string) string {
	return filepath.Join(poolRoot, "pool.json")
}

func poolLockPath(poolRoot string) string {
	return filepath.Join(poolRoot, "pool.lock")
}

// lockPool acquires an exclusive, blocking file lock scoped to poolRoot,
// serializing Acquire/Release's read-modify-write over pool.json across
// every process touching this same pool - this is a short critical
// section held per call, not a long-lived singleton (contrast
// internal/sentinel.AcquireLock, which guards one whole process's
// lifetime, not a brief section). flock releases itself automatically if
// the holding process dies mid-section, so a crash never leaves other
// callers blocked on a stale lock the way a pid-file convention could.
func lockPool(poolRoot string) (unlock func(), err error) {
	f, err := os.OpenFile(poolLockPath(poolRoot), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening camp pool lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("locking camp pool: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

func loadPool(poolRoot string) (poolState, error) {
	data, err := os.ReadFile(poolStatePath(poolRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return poolState{SchemaVersion: poolSchemaVersion}, nil
		}
		return poolState{}, fmt.Errorf("reading camp pool state: %w", err)
	}
	var p poolState
	if err := json.Unmarshal(data, &p); err != nil {
		return poolState{}, fmt.Errorf("parsing camp pool state: %w", err)
	}
	if p.SchemaVersion != poolSchemaVersion {
		return poolState{}, fmt.Errorf("unsupported camp pool schema version %d (expected %d)", p.SchemaVersion, poolSchemaVersion)
	}
	return p, nil
}

func savePool(poolRoot string, p poolState) error {
	p.SchemaVersion = poolSchemaVersion
	return atomicfile.WriteJSON(poolStatePath(poolRoot), p)
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s (in %s): %w: %s", strings.Join(args, " "), dir, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func currentBranch(dir string) (string, error) {
	out, err := runGit(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func isDirty(dir string) (bool, error) {
	out, err := runGit(dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// isAncestor reports whether ancestor is reachable from descendant (i.e.
// descendant already contains ancestor's commits).
func isAncestor(dir, ancestor, descendant string) (bool, error) {
	cmd := exec.Command("git", "merge-base", "--is-ancestor", ancestor, descendant)
	cmd.Dir = dir
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor %s %s (in %s): %w", ancestor, descendant, dir, err)
}

// contentAlreadyInBase reports whether campDir's current HEAD introduces
// nothing that base doesn't already contain - the squash/rebase-merge
// case isAncestor can't see. campDir is a worktree of the same repository
// as base, so base resolves there directly; no fetch needed, since the
// general's own "git pull" on the project's checkout is what brings a
// real GitHub merge in locally. Merges base into HEAD in memory
// (git merge-tree, nothing written to disk) and compares the resulting
// tree to base's own tree: if they match, HEAD added nothing base
// lacked, so the content already landed even though the commit graphs
// never touch. A real conflict returns (false, nil), not an error -
// that's still-diverged, unlanded work, not a landed match.
func contentAlreadyInBase(campDir, base string) (bool, error) {
	baseTree, err := runGit(campDir, "rev-parse", base+"^{tree}")
	if err != nil {
		return false, fmt.Errorf("resolving %s's tree: %w", base, err)
	}
	mergedTree, err := runGit(campDir, "merge-tree", "--write-tree", base, "HEAD")
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(mergedTree) == strings.TrimSpace(baseTree), nil
}
