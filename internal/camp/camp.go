// Package camp manages a pool of reusable, isolated git worktrees per
// project - vexillum's "camp" concept, one worktree per task. Inspired by
// poolkeeper (see docs/references.md): a task acquires a clean, landed slot
// from the pool if one is available, or a fresh worktree otherwise; a slot
// is returned to the pool only when it's safe to reuse - never dirty,
// never holding unlanded commits, and only by the task that actually
// leases it.
package camp

import (
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
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/schemaver"
)

// poolSchemaVersion is the pool state file's schema version.
const poolSchemaVersion = schemaver.Current

// Camp is one acquired worktree: an isolated checkout of the project on
// its own branch, ready for a soldier to work in.
type Camp struct {
	ProjectDir string `json:"project_dir"`
	PoolRoot   string `json:"pool_root"`
	Path       string `json:"path"`
	Branch     string `json:"branch"`
	Slot       int    `json:"slot"`

	// Base is the branch this camp was forked from at Acquire time (the
	// project's own current branch back then) - internal/soldier persists
	// it onto the task (state.Task.CampBase) so internal/sentinel can
	// later ask HasNewCommits whether the mission actually produced
	// anything, without needing the project's own checkout directory
	// (which the sentinel, sweeping every project generically, never has -
	// only the camp worktree path itself, see internal/project's package
	// doc on Key being a one-way hash).
	Base string `json:"base"`
}

type poolSlot struct {
	Number   int    `json:"number"`
	Branch   string `json:"branch"`
	Base     string `json:"base,omitempty"`      // the branch Branch was forked from - see Camp.Base
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
	projectRoot, err := project.Root(vexillumHome, absProject)
	if err != nil {
		return Camp{}, err
	}
	poolRoot := filepath.Join(projectRoot, "camps")

	if err := os.MkdirAll(poolRoot, 0o755); err != nil {
		return Camp{}, fmt.Errorf("creating camp pool directory: %w", err)
	}

	// N soldiers dispatched at once means N concurrent Acquire calls (each
	// a separate `vx dispatch` process) racing against the same
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
		slot.Base = base
		if err := savePool(poolRoot, pool); err != nil {
			return Camp{}, err
		}
		return Camp{ProjectDir: absProject, PoolRoot: poolRoot, Path: worktreePath, Branch: branch, Slot: slot.Number, Base: base}, nil
	}

	number := len(pool.Slots) + 1
	worktreePath := filepath.Join(poolRoot, strconv.Itoa(number), repoName)
	if _, err := runGit(absProject, "worktree", "add", "-b", branch, worktreePath, base); err != nil {
		return Camp{}, fmt.Errorf("creating camp worktree: %w", err)
	}

	pool.Slots = append(pool.Slots, poolSlot{Number: number, Branch: branch, Base: base, LeasedBy: taskID})
	if err := savePool(poolRoot, pool); err != nil {
		return Camp{}, err
	}

	return Camp{ProjectDir: absProject, PoolRoot: poolRoot, Path: worktreePath, Branch: branch, Slot: number, Base: base}, nil
}

// Resolve reconstructs the Camp for an already-acquired slot from the
// pool's own bookkeeping. Useful for a caller that only kept the slot
// number (e.g. persisted on a state.Task) and needs the rest of Camp back
// to call Strike.
func Resolve(projectDir, vexillumHome string, slot int) (Camp, error) {
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		return Camp{}, fmt.Errorf("resolving project path: %w", err)
	}
	repoName := filepath.Base(absProject)
	projectRoot, err := project.Root(vexillumHome, absProject)
	if err != nil {
		return Camp{}, err
	}
	poolRoot := filepath.Join(projectRoot, "camps")

	pool, err := loadPool(poolRoot)
	if err != nil {
		return Camp{}, err
	}
	for _, s := range pool.Slots {
		if s.Number == slot {
			worktreePath := filepath.Join(poolRoot, strconv.Itoa(slot), repoName)
			return Camp{ProjectDir: absProject, PoolRoot: poolRoot, Path: worktreePath, Branch: s.Branch, Slot: slot, Base: s.Base}, nil
		}
	}
	return Camp{}, fmt.Errorf("camp slot %d not found in pool state for %s", slot, absProject)
}

// LeasedTasks returns the set of task ids that currently hold a camp lease
// in the pool under projectRoot (see internal/project.Root) - a task whose
// camp was struck no longer does. internal/sentinel uses it to tell a
// finished soldier whose pane is still open (worth watching for a re-prompt)
// from one long since struck. It reads the pool without locking it: the
// pool is only ever replaced atomically (savePool), so a read sees either
// the old or the new version, never a partial one. A project that never
// acquired a camp has no pool file and no leases.
func LeasedTasks(projectRoot string) (map[string]bool, error) {
	pool, err := loadPool(filepath.Join(projectRoot, "camps"))
	if err != nil {
		return nil, err
	}
	leased := map[string]bool{}
	for _, s := range pool.Slots {
		if s.LeasedBy != "" {
			leased[s.LeasedBy] = true
		}
	}
	return leased, nil
}

// Land fast-forwards the project's own checkout to c's branch - the
// "local-only" delivery mode, here rather than leaving the commander to
// run raw git commands on its own judgment. It never forces anything: the
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
		// Landing sibling missions dispatched from the same base one at a
		// time hits this exact case: once the first lands, base has moved,
		// so every other still-open mission stops being a fast-forward -
		// not a defect, an inherent property of fast-forward-only landing
		// with more than one branch sharing an ancestor. The message names
		// who should fix it - the soldier, with full context of its own
		// change, not this function or the commander guessing at a rebase
		// from outside.
		return fmt.Errorf("%s is not a fast-forward of %s (it has diverged) - have the soldier rebase %s onto %s, then retry", c.Branch, base, c.Branch, base)
	}

	if _, err := runGit(c.ProjectDir, "merge", "--ff-only", c.Branch); err != nil {
		return fmt.Errorf("landing %s into %s: %w", c.Branch, base, err)
	}
	return nil
}

// PRMerge is what GitHub reports about a merged pull request, handed to
// StrikeWith so a remote merge counts as landed work.
type PRMerge struct {
	// MergeCommit is the commit the merge created on the base branch. It
	// must be reachable from the local base branch: until the general has
	// pulled, the work is merged on GitHub but not here.
	MergeCommit string
	// HeadCommit is the pull request's head when it merged. The camp's own
	// HEAD must be part of it, or the camp holds work the pull request
	// never carried.
	HeadCommit string
}

// StrikeOptions tunes StrikeWith. The zero value is Strike's behavior.
type StrikeOptions struct {
	// Shipped marks a mission whose work travels as a pull request, so the
	// refusal for commits that are not on the base says to merge it and
	// pull.
	Shipped bool
	// PRMerged, when set, is GitHub's report that the camp's pull request
	// merged. It replaces the content check as the proof of landing, once
	// the merge commit is verified on the local base.
	PRMerged *PRMerge
	// Discard strikes the camp even when it is dirty or holds commits that
	// are not on the base. StrikeReport says exactly what was thrown away.
	// Only for work the general confirmed is already on the base or
	// abandoned.
	Discard bool
}

// StrikeReport lists what a Discard strike threw away.
type StrikeReport struct {
	// DiscardedCommits are the unlanded commits, one "hash subject" each.
	DiscardedCommits []string
	// DiscardedChanges are the uncommitted changes, one "git status
	// --porcelain" line each.
	DiscardedChanges []string
}

// ErrNotLeased is what StrikeWith wraps when the slot is not leased to
// anyone: the strike already released it, so only the task record and the
// herdr pane may be left to clean up.
var ErrNotLeased = errors.New("is not leased")

// Strike strikes the camp: it returns c's slot to the pool for reuse, but
// only when it's safe: taskID must be the slot's recorded owner, the worktree must have no
// uncommitted changes, and its branch must already be landed (merged) on
// the project's base branch. The worktree itself is never destroyed - a
// struck slot stays on disk, ready for the next Acquire to reset and
// reuse.
func Strike(c Camp, taskID string) error {
	_, err := StrikeWith(c, taskID, StrikeOptions{})
	return err
}

// StrikeWith is Strike with options: proof of landing from a merged pull
// request, a refusal that fits a shipped mission, and the explicit Discard
// override.
func StrikeWith(c Camp, taskID string, opts StrikeOptions) (StrikeReport, error) {
	var report StrikeReport

	unlock, err := lockPool(c.PoolRoot)
	if err != nil {
		return report, err
	}
	defer unlock()

	pool, err := loadPool(c.PoolRoot)
	if err != nil {
		return report, err
	}

	idx := -1
	for i := range pool.Slots {
		if pool.Slots[i].Number == c.Slot {
			idx = i
			break
		}
	}
	if idx == -1 {
		return report, fmt.Errorf("camp slot %d not found in pool state", c.Slot)
	}

	slot := pool.Slots[idx]
	if slot.LeasedBy == "" {
		return report, fmt.Errorf("camp slot %d: %w, nothing to strike", c.Slot, ErrNotLeased)
	}
	if slot.LeasedBy != taskID {
		return report, fmt.Errorf("camp slot %d is leased by task %s, not %s, refusing to strike", c.Slot, slot.LeasedBy, taskID)
	}

	dirty, err := isDirty(c.Path)
	if err != nil {
		return report, fmt.Errorf("checking camp for uncommitted changes: %w", err)
	}
	if dirty && !opts.Discard {
		return report, fmt.Errorf("camp %s has uncommitted changes, refusing to strike", c.Path)
	}

	base, err := currentBranch(c.ProjectDir)
	if err != nil {
		return report, fmt.Errorf("resolving base branch: %w", err)
	}
	landed, why, err := campLanded(c, base, opts)
	if err != nil && !opts.Discard {
		return report, err
	}
	if !landed && !opts.Discard {
		msg := fmt.Sprintf("camp %s branch %s has commits not yet landed on %s, refusing to strike", c.Path, c.Branch, base)
		if why != "" {
			msg += ": " + why
		} else if opts.Shipped {
			msg += fmt.Sprintf(": this mission was shipped as a pull request - merge the pull request, run git pull on %s in %s, then retry", base, c.ProjectDir)
		}
		return report, errors.New(msg)
	}

	if !landed {
		out, err := runGit(c.Path, "log", "--format=%h %s", base+"..HEAD")
		if err != nil {
			return report, fmt.Errorf("listing the commits to discard: %w", err)
		}
		report.DiscardedCommits = nonEmptyLines(out)
	}
	if dirty {
		out, err := runGit(c.Path, "status", "--porcelain")
		if err != nil {
			return report, fmt.Errorf("listing the uncommitted changes to discard: %w", err)
		}
		report.DiscardedChanges = nonEmptyLines(out)
		// A dirty slot is never reused by Acquire, so a discarded strike
		// must leave the worktree clean or the slot would leak for good.
		if _, err := runGit(c.Path, "reset", "--hard"); err != nil {
			return report, fmt.Errorf("resetting camp worktree: %w", err)
		}
		if _, err := runGit(c.Path, "clean", "-fd"); err != nil {
			return report, fmt.Errorf("cleaning camp worktree: %w", err)
		}
	}

	if opts.Discard {
		// The slot goes back to the pool like the idle ones, detached,
		// never parked on a branch whose commits were just discarded. The
		// branch itself stays (Prune never force deletes an unmerged one).
		if _, err := runGit(c.Path, "checkout", "--detach", base); err != nil {
			return report, fmt.Errorf("detaching camp worktree from %s: %w", c.Branch, err)
		}
	}

	pool.Slots[idx].LeasedBy = ""
	return report, savePool(c.PoolRoot, pool)
}

// campLanded reports whether c's work is on base. When it is not, why may
// explain what is missing (set only when a merged pull request was
// supplied). A plain ancestor check proves a "vx land" fast-forward; a
// merged pull request is proof of its own; otherwise a content check
// covers a squash or rebase merge that was pulled locally.
func campLanded(c Camp, base string, opts StrikeOptions) (landed bool, why string, err error) {
	landed, err = isAncestor(c.Path, "HEAD", base)
	if err != nil {
		return false, "", fmt.Errorf("checking whether camp branch landed: %w", err)
	}
	if landed {
		return true, "", nil
	}

	if opts.PRMerged != nil {
		// GitHub's word beats the content check, which refuses forever
		// once later commits touched the same lines. The merge commit has
		// to be on the local base first: that is what tells the general
		// has pulled.
		landed, why = verifyMerged(c, base, *opts.PRMerged)
		return landed, why, nil
	}

	// A shipped mission whose PR merged via squash or rebase on GitHub
	// never satisfies the ancestor check, even after a real local pull -
	// the merge commit's parent is the pre-merge base, not this camp's
	// tip. Fall back to a content check: does merging this branch into the
	// current base introduce anything base doesn't already have? If not,
	// the work already landed, just under different commit SHAs.
	landed, err = contentAlreadyInBase(c.Path, base)
	if err != nil {
		return false, "", fmt.Errorf("checking whether camp content already landed: %w", err)
	}
	return landed, "", nil
}

// verifyMerged checks GitHub's report of a merged pull request against the
// local repository, returning why it does not prove the camp landed.
func verifyMerged(c Camp, base string, m PRMerge) (bool, string) {
	if m.MergeCommit == "" {
		return false, "the pull request is merged on GitHub, but GitHub reported no merge commit to check against " + base
	}
	if !commitExists(c.Path, m.MergeCommit) || !reachable(c.Path, m.MergeCommit, base) {
		return false, fmt.Sprintf("the pull request is merged on GitHub, but its merge commit is not on %s yet - run git pull on %s in %s, then retry", base, base, c.ProjectDir)
	}
	if m.HeadCommit != "" && (!commitExists(c.Path, m.HeadCommit) || !reachable(c.Path, "HEAD", m.HeadCommit)) {
		return false, "the pull request is merged, but this camp has commits that were not in it"
	}
	return true, ""
}

func commitExists(dir, rev string) bool {
	cmd := exec.Command("git", "cat-file", "-e", rev+"^{commit}")
	cmd.Dir = dir
	return cmd.Run() == nil
}

// reachable is isAncestor with "cannot tell" read as "no".
func reachable(dir, ancestor, descendant string) bool {
	ok, err := isAncestor(dir, ancestor, descendant)
	return err == nil && ok
}

func nonEmptyLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// Discard is Strike's deliberately destructive counterpart (PRD v2,
// A.2): where Strike refuses anything but a clean, landed camp, Discard
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
	pool.Slots[idx].Base = ""
	return savePool(c.PoolRoot, pool)
}

// HasNewCommits reports whether campPath's current HEAD contains at least
// one commit not already on base - a mission's hard completion proof,
// mirroring internal/report's file-based proof for a scout (internal/
// sentinel uses this as the strong signal it requires before ever
// trusting an idle turn as "done" for a mission). base is resolved from
// within campPath itself, not the project's own checkout directory: a
// git worktree shares every branch/ref with the rest of its repository,
// so this works from the camp alone, which is all the sentinel - sweeping
// every project generically - ever has for a task (see Camp.Base's own
// doc comment). A base ref that can't be resolved (moved, deleted, or the
// camp worktree itself gone) is reported as an error - the caller
// decides how to treat "can't tell", the same way internal/report.Exists
// never guesses either.
func HasNewCommits(campPath, base string) (bool, error) {
	out, err := runGit(campPath, "rev-list", "--count", base+"..HEAD")
	if err != nil {
		return false, fmt.Errorf("counting commits ahead of %s in %s: %w", base, campPath, err)
	}
	count, convErr := strconv.Atoi(strings.TrimSpace(out))
	if convErr != nil {
		return false, fmt.Errorf("parsing commit count %q from %s: %w", out, campPath, convErr)
	}
	return count > 0, nil
}

func poolStatePath(poolRoot string) string {
	return filepath.Join(poolRoot, "pool.json")
}

func poolLockPath(poolRoot string) string {
	return filepath.Join(poolRoot, "pool.lock")
}

// lockPool acquires an exclusive, blocking file lock scoped to poolRoot,
// serializing Acquire/Strike's read-modify-write over pool.json across
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
	if !schemaver.Supported(p.SchemaVersion) {
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
