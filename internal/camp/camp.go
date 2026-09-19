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

// Release returns c's slot to the pool for reuse, but only when it's safe:
// taskID must be the slot's recorded owner, the worktree must have no
// uncommitted changes, and its branch must already be landed (merged) on
// the project's base branch. The worktree itself is never destroyed - a
// released slot stays on disk, ready for the next Acquire to reset and
// reuse.
func Release(c Camp, taskID string) error {
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
		return fmt.Errorf("camp %s branch %s has commits not yet landed on %s, refusing to release", c.Path, c.Branch, base)
	}

	pool.Slots[idx].LeasedBy = ""
	return savePool(c.PoolRoot, pool)
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}

func poolStatePath(poolRoot string) string {
	return filepath.Join(poolRoot, "pool.json")
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
