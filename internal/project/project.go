// Package project computes vexillum's per-project namespace under
// ~/.vexillum: every project's tasks, wakes and camps live under their
// own ~/.vexillum/projects/<key> root, so two projects open at once
// never see each other's state (before this package existed, tasks/ and
// wakes/ were flat and global-per-machine, and a commander's Stop hook in
// one project could drain a wake that belonged to a soldier in another).
package project

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

const projectsDirName = "projects"

// Key returns vexillum's stable per-project identifier -
// "<repo>-<shortHash>" derived from the project's absolute path - used to
// namespace a project's tasks, wakes and camps under
// ~/.vexillum/projects/<key>.
//
// Renaming or moving the project changes absProjectDir and so changes
// this key: whatever was already recorded under the old key (pending
// tasks, wakes, camps) becomes orphaned - invisible to any command run
// from the project's new location. Accepted risk, not handled: no
// migration, no detection of an orphaned old key.
func Key(absProjectDir string) string {
	repoName := filepath.Base(absProjectDir)
	return fmt.Sprintf("%s-%s", repoName, shortHash(absProjectDir))
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}

// Root resolves projectDir to its namespaced root under vexillumHome:
// <vexillumHome>/projects/<Key>. projectDir need not be absolute or
// symlink-free yet - Root normalizes it first.
//
// Normalization matters because two different call paths resolve the
// same project from two different starting points: dispatch resolves it
// from a plain os.Getwd() (shell-normalized, symlink-preserving), while
// "vexillum sentinel drain" resolves it from "git rev-parse
// --show-toplevel" (git-normalized). On a machine where the project's
// path involves a symlink (e.g. macOS's $TMPDIR under /var, itself a
// symlink to /private/var), those two forms can disagree on the same
// directory - which would otherwise compute two different keys for what
// is really one project. Resolving through EvalSymlinks here makes every
// caller converge on the same key regardless of which path form it
// started from. If EvalSymlinks fails (e.g. a filesystem that doesn't
// support it), Root falls back to the plain absolute path rather than
// failing outright.
func Root(vexillumHome, projectDir string) (string, error) {
	absProject, err := filepath.Abs(projectDir)
	if err != nil {
		return "", fmt.Errorf("resolving project path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(absProject); err == nil {
		absProject = resolved
	}
	return filepath.Join(vexillumHome, projectsDirName, Key(absProject)), nil
}

// AllRoots lists every project root currently namespaced under
// vexillumHome - vexillumHome/projects/*, one per project vexillum has
// ever dispatched into. Used by the sentinel, which runs as a single
// process per machine and must sweep every project's tasks in one Tick.
// A missing projects/ directory (nothing dispatched yet, anywhere)
// yields nil, not an error.
func AllRoots(vexillumHome string) ([]string, error) {
	dir := filepath.Join(vexillumHome, projectsDirName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing projects: %w", err)
	}

	var roots []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		roots = append(roots, filepath.Join(dir, entry.Name()))
	}
	return roots, nil
}
