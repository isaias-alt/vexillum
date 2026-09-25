// Package doctorcheck implements the individual environment checks
// "vexillum doctor" reports on - required and optional binaries, the
// no-mistakes gate, herdr's version, the project's own scaffold - plus
// the catalog of AXIs and first-party skills doctor lists
// informationally. Each check is a pure function returning a Result (or,
// for the AXI catalog, a status line); "vexillum doctor" itself only
// composes and prints them, and never affects the exit code beyond what
// Result.Required/OK say.
package doctorcheck

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

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

// AXI is one AXI vexillum knows about, tracked in doctor's
// informational-only AXIs section (PRD v2, "Decisiones de integración de
// AXIs"): doctor verifies skill presence, it never installs anything,
// and absence is never a warning or a failure - a mission that needs it
// installs it on demand.
//
// Each AXI's own README names its own recommended install command, and
// they don't agree on shape: quota-axi's --skill value matches its repo
// name and recommends -g (global), but lavish-axi's --skill value is
// "lavish" (not "lavish-axi") and its own README recommends installing
// project-local, no -g - verified live for both, not assumed from the
// first one.
type AXI struct {
	// Name is the skill's own folder name once installed (also the
	// --skill value) - matches its SKILL.md frontmatter `name`, which
	// isn't guaranteed to match Repo's own name (lavish-axi is the
	// counterexample).
	Name string
	// Repo is "<owner>/<repo>", for "npx skills add <repo> --skill <name>".
	Repo string
	// Global is whether that AXI's own README recommends installing
	// with -g. Detection itself always checks both locations regardless
	// (see AXIInstalled) - this only shapes the install hint shown when
	// the skill is missing.
	Global bool
}

// KnownAXIs is every AXI (and first-party skill) doctor reports on.
var KnownAXIs = []AXI{
	{Name: "quota-axi", Repo: "kunchenguid/quota-axi", Global: true},
	{Name: "lavish", Repo: "kunchenguid/lavish-axi", Global: false},
	{Name: "chrome-devtools-axi", Repo: "kunchenguid/chrome-devtools-axi", Global: true},
	// muster isn't a third-party AXI - it ships in this repo
	// (skills/muster/SKILL.md) and reads this project's own
	// `vexillum status --json`, so it's project-local like lavish, not
	// global. Listed here anyway because the install/detection mechanism
	// ("npx skills add", .claude/skills/<name>/SKILL.md) is identical and
	// this is where the general already looks for on-demand skill status.
	{Name: "muster", Repo: "isaias-alt/vexillum", Global: false},
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

// NoMistakes reports whether the "no-mistakes" binary is installed and,
// if so, whether this project has been gated (PRD v2, B.4): a real,
// standalone CLI (github.com/kunchenguid/no-mistakes, installed via
// curl, not an "npx skills add" AXI) that puts a local git remote in
// front of the real one - "git push no-mistakes <branch>" runs its own
// review/test/lint/docs pipeline and opens the PR itself once every
// check passes. Optional and purely informational, like the AXIs: a
// project that never ships through the gate doesn't need it, and never
// running "no-mistakes init" is not an error.
func NoMistakes(projectDir string) Result {
	const name = "no-mistakes"

	if _, err := exec.LookPath("no-mistakes"); err != nil {
		return Result{Name: name, Detail: "not found in PATH (optional - only needed for 'vexillum ship', see docs/no-mistakes.md)"}
	}
	if !GateConfigured(projectDir) {
		return Result{Name: name, Detail: "installed, but this project hasn't run 'no-mistakes init' yet - 'vexillum ship' will do this automatically the first time"}
	}
	return Result{Name: name, OK: true, Detail: "installed and this project is gated"}
}

// GitHubCLI reports whether the "gh" binary is installed - the official
// GitHub CLI, called directly by "vexillum land" to merge a shipped
// mission's real PR (see internal/ghpr), not the separate "gh-axi"
// agent-facing AXI. Optional and purely informational: a project that
// never ships through the no-mistakes gate never needs it, same posture
// as the no-mistakes check above.
func GitHubCLI() Result {
	const name = "GitHub CLI (gh)"
	if _, err := exec.LookPath("gh"); err != nil {
		return Result{Name: name, Detail: "not found in PATH (optional - only needed for 'vexillum land' on a shipped mission)"}
	}
	return Result{Name: name, OK: true}
}

// GateConfigured reports whether projectDir already has the
// "no-mistakes" git remote that "no-mistakes init" creates - the signal
// "vexillum ship" depends on being there before it can push through it.
func GateConfigured(projectDir string) bool {
	cmd := exec.Command("git", "remote", "get-url", "no-mistakes")
	cmd.Dir = projectDir
	return cmd.Run() == nil
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

// VexillumHome checks that path (~/.vexillum/) exists, is a directory,
// and is writable.
func VexillumHome(path string) Result {
	const name = "~/.vexillum/"

	info, err := os.Stat(path)
	if err != nil {
		return Result{Name: name, OK: false, Detail: "does not exist, run 'vexillum init'", Required: true}
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
// 'vexillum init'.
func ProjectInitialized(projectDir string) Result {
	const name = "project initialized"

	if scaffold.ProjectInitialized(projectDir) {
		return Result{Name: name, OK: true, Required: true}
	}
	return Result{Name: name, OK: false, Detail: "run 'vexillum init'", Required: true}
}

// GitRepo checks that projectDir is a git repository.
func GitRepo(projectDir string) Result {
	const name = "git repository"

	if scaffold.IsGitRepo(projectDir) {
		return Result{Name: name, OK: true, Required: true}
	}
	return Result{Name: name, OK: false, Detail: "current directory is not a git repository", Required: true}
}

// AXIInstalled reports whether a's skill is present project-level
// (<projectDir>/.claude/skills/<name>/SKILL.md) or globally
// (<homeDir>/.claude/skills/<name>/SKILL.md) - "npx skills add" can
// install to either depending on the "-g" flag, and doctor only cares
// that the skill is reachable, not which.
func AXIInstalled(projectDir, homeDir, name string) bool {
	for _, dir := range []string{projectDir, homeDir} {
		path := filepath.Join(dir, ".claude", "skills", name, "SKILL.md")
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

// AXIStatusLine renders a's install status for doctor's informational
// AXIs section.
func AXIStatusLine(a AXI, projectDir, homeDir string) string {
	if AXIInstalled(projectDir, homeDir, a.Name) {
		return fmt.Sprintf("[installed] %s", a.Name)
	}
	cmd := fmt.Sprintf("npx skills add %s --skill %s", a.Repo, a.Name)
	if a.Global {
		cmd += " -g"
	}
	return fmt.Sprintf("[not installed] %s - install with: %s", a.Name, cmd)
}
