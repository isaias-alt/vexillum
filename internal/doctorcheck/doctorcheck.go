// Package doctorcheck implements the individual environment checks
// "vx doctor" reports on - required and optional binaries, herdr's
// version, the project's own scaffold - plus the first-party skills
// doctor lists informationally. Each check is a pure function returning
// a Result; "vx doctor" itself only composes and prints them, and never
// affects the exit code beyond what Result.Required/OK say.
package doctorcheck

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/scaffold"
)

// Result is the outcome of a single doctor check. Required checks that
// fail (OK false) make the environment not ready (non-zero exit); a Warn
// result is neither ok nor missing - it flags something worth the
// general's attention (e.g. an unverified herdr version) without ever
// affecting the exit code, the same way an optional check never does.
type Result struct {
	Name     string
	OK       bool
	Warn     bool
	Detail   string
	Required bool
}

// Binary checks whether binaryName is on PATH.
func Binary(label, binaryName string, required bool) Result {
	if _, err := exec.LookPath(binaryName); err != nil {
		detail := fmt.Sprintf("%s not found in PATH", binaryName)
		if !required {
			detail += " (optional control backend)"
		}
		return Result{Name: label, OK: false, Detail: detail, Required: required}
	}
	return Result{Name: label, OK: true, Required: required}
}

// GitHubCLI reports whether the "gh" binary is installed - the official
// GitHub CLI, called directly by "vx ship" (internal/ghpr.Create)
// to open a mission's pull request once its tribunal pipeline passes,
// and by "vx land" (internal/ghpr.MergeShipped) to later merge it -
// not the separate "gh-tool" agent-facing AXI. Optional and purely
// informational: a project that never ships never needs it.
func GitHubCLI() Result {
	const name = "GitHub CLI (gh)"
	if _, err := exec.LookPath("gh"); err != nil {
		return Result{Name: name, Detail: "not found in PATH (optional - only needed for '" + cmdname.Name + " ship'/'" + cmdname.Name + " land' on a shipped mission)"}
	}
	return Result{Name: name, OK: true}
}

// HerdrVersion anchors the implicit assumption internal/herdr's
// error-code parsing has always run under - herdr 0.9.x, verified live -
// against a real, executable check (PRD v2, A.1). herdr not being on
// PATH at all is already reported by the "herdr" Binary check; this
// returns a zero-value Result (an empty Name, skipped by the caller)
// rather than a second line about the same absence.
func HerdrVersion() Result {
	if _, err := exec.LookPath("herdr"); err != nil {
		return Result{}
	}

	const name = "herdr version"

	version, err := herdr.Version()
	if err != nil {
		return Result{Name: name, Warn: true, Detail: "could not determine herdr version: " + err.Error()}
	}
	if !herdr.IsSupportedVersion(version) {
		return Result{Name: name, Warn: true, Detail: fmt.Sprintf("%s is not a %s version - vexillum's herdr integration was verified against %sx, this version may behave differently", version, herdr.SupportedVersionPrefix, herdr.SupportedVersionPrefix)}
	}
	return Result{Name: name, OK: true, Detail: version}
}

// VXShadow checks that typing the command name runs the executable that
// is running right now. A different "vx" earlier in PATH (an unrelated
// tool that happens to share the name) would silently answer instead -
// including from the sentinel Stop hook, whose command tries the first
// vx on PATH before the known install locations - so this warns and names
// the one that wins.
// It only ever warns, never fails: not finding any vx on PATH (the binary
// was run by its full path) is not a conflict.
func VXShadow() Result {
	exe, err := os.Executable()
	if err != nil {
		return Result{Name: vxShadowName, Warn: true, Detail: "could not determine the running executable: " + err.Error()}
	}
	return vxShadow(exe, os.Getenv("PATH"))
}

const vxShadowName = "another " + cmdname.Name + " earlier in PATH"

// vxShadow is VXShadow with the running executable and PATH injected, so
// tests control both.
func vxShadow(exe, pathEnv string) Result {
	self := realPath(exe)

	first := firstOnPath(cmdname.Name, pathEnv)
	if first == "" {
		return Result{Name: vxShadowName, OK: true, Detail: "no, " + cmdname.Name + " is not on PATH (running " + self + ")"}
	}
	if realPath(first) == self {
		return Result{Name: vxShadowName, OK: true, Detail: "no, " + cmdname.Name + " on PATH is " + first}
	}
	return Result{
		Name: vxShadowName,
		Warn: true,
		Detail: fmt.Sprintf("%s comes first in PATH, so running '%s' does not run this one (%s) - put this one's directory first, or remove the other",
			first, cmdname.Name, self),
	}
}

// firstOnPath returns the first executable file called name in pathEnv's
// directories, in order, the way a shell resolves a bare command, or ""
// if there is none. Empty and relative entries are skipped, as
// exec.LookPath refuses them.
func firstOnPath(name, pathEnv string) string {
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		return candidate
	}
	return ""
}

// realPath resolves symlinks (a brew-linked vx points into its Cellar),
// falling back to the cleaned input if that fails.
func realPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

// VexillumHome checks that path (~/.vexillum/) exists, is a directory,
// and is writable.
func VexillumHome(path string) Result {
	const name = "~/.vexillum/"

	info, err := os.Stat(path)
	if err != nil {
		return Result{Name: name, OK: false, Detail: "does not exist, run '" + cmdname.Name + " init'", Required: true}
	}
	if !info.IsDir() {
		return Result{Name: name, OK: false, Detail: "exists but is not a directory", Required: true}
	}
	if info.Mode().Perm()&0o200 == 0 {
		return Result{Name: name, OK: false, Detail: "not writable", Required: true}
	}
	return Result{Name: name, OK: true, Required: true}
}

// ProjectInitialized checks that projectDir has been scaffolded by
// 'vx init'.
func ProjectInitialized(projectDir string) Result {
	const name = "project initialized"

	if scaffold.ProjectInitialized(projectDir) {
		return Result{Name: name, OK: true, Required: true}
	}
	return Result{Name: name, OK: false, Detail: "run '" + cmdname.Name + " init'", Required: true}
}

// GitRepo checks that projectDir is a git repository.
func GitRepo(projectDir string) Result {
	const name = "git repository"

	if scaffold.IsGitRepo(projectDir) {
		return Result{Name: name, OK: true, Required: true}
	}
	return Result{Name: name, OK: false, Detail: "current directory is not a git repository", Required: true}
}
