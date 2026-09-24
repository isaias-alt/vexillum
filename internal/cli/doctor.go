package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/isaias-alt/vexillum/internal/herdr"
)

const doctorUsage = `Report on the health of the vexillum environment. Read-only.

Usage:
  vexillum doctor
`

// checkResult is the outcome of a single doctor check. Required checks that
// fail (OK false) make the environment not ready (non-zero exit); a Warn
// result is neither ok nor missing - it flags something worth the
// general's attention (e.g. an unverified herdr version) without ever
// affecting the exit code, the same way an optional check never does.
type checkResult struct {
	Name     string
	OK       bool
	Warn     bool
	Detail   string
	Required bool
}

// Doctor runs the "vexillum doctor" command.
func Doctor(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(doctorUsage)
		return 0
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "vexillum: cannot determine current directory: %v\n", err)
		return 1
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "vexillum: cannot determine home directory: %v\n", err)
		return 1
	}

	return runDoctor(cwd, filepath.Join(home, ".vexillum"), home, os.Stdout)
}

// axiSkill is one AXI vexillum knows about, tracked in doctor's
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
type axiSkill struct {
	// Name is the skill's own folder name once installed (also the
	// --skill value) - matches its SKILL.md frontmatter `name`, which
	// isn't guaranteed to match Repo's own name (lavish-axi is the
	// counterexample).
	Name string
	// Repo is "<owner>/<repo>", for "npx skills add <repo> --skill <name>".
	Repo string
	// Global is whether that AXI's own README recommends installing
	// with -g. Detection itself always checks both locations regardless
	// (see axiSkillInstalled) - this only shapes the install hint shown
	// when the skill is missing.
	Global bool
}

var knownAXIs = []axiSkill{
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

func runDoctor(projectDir, vexillumHome, homeDir string, out io.Writer) int {
	checks := []checkResult{
		checkBinary("Claude Code", "claude", true),
		checkBinary("herdr", "herdr", true),
		checkHerdrVersion(),
		checkBinary("tmux", "tmux", false),
		checkNoMistakes(projectDir),
		checkGitHubCLI(),
		checkVexillumHome(vexillumHome),
		checkProjectInitialized(projectDir),
		checkGitRepo(projectDir),
	}

	ready := true
	var optionalMissing []string
	var warnings []string
	for _, c := range checks {
		if c.Name == "" {
			// checkHerdrVersion skips itself entirely when herdr isn't on
			// PATH - checkBinary's own "[missing] herdr" line above
			// already says so, no need for a second line about it.
			continue
		}
		status := "ok"
		switch {
		case c.Warn:
			status = "warn"
		case !c.OK:
			status = "missing"
		}
		line := fmt.Sprintf("[%s] %s", status, c.Name)
		if c.Detail != "" {
			line += " - " + c.Detail
		}
		fmt.Fprintln(out, line)

		switch {
		case c.Warn:
			warnings = append(warnings, c.Name)
		case !c.OK:
			if c.Required {
				ready = false
			} else {
				optionalMissing = append(optionalMissing, c.Name)
			}
		}
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "AXIs and first-party skills (on-demand, installed as Agent Skills - not required):")
	for _, a := range knownAXIs {
		fmt.Fprintln(out, axiStatusLine(a, projectDir, homeDir))
	}
	fmt.Fprintln(out)

	if !ready {
		fmt.Fprintln(out, "Environment not ready, see missing checks above.")
		return 1
	}
	switch {
	case len(optionalMissing) > 0 && len(warnings) > 0:
		fmt.Fprintf(out, "Environment ready (optional: %s missing; warnings: %s).\n", strings.Join(optionalMissing, ", "), strings.Join(warnings, ", "))
	case len(optionalMissing) > 0:
		fmt.Fprintf(out, "Environment ready (optional: %s missing).\n", strings.Join(optionalMissing, ", "))
	case len(warnings) > 0:
		fmt.Fprintf(out, "Environment ready (warnings: %s).\n", strings.Join(warnings, ", "))
	default:
		fmt.Fprintln(out, "Environment ready.")
	}
	return 0
}

// axiSkillInstalled reports whether a's skill is present project-level
// (<projectDir>/.claude/skills/<name>/SKILL.md) or globally
// (<homeDir>/.claude/skills/<name>/SKILL.md) - "npx skills add" can
// install to either depending on the "-g" flag, and doctor only cares
// that the skill is reachable, not which.
func axiSkillInstalled(projectDir, homeDir, name string) bool {
	for _, dir := range []string{projectDir, homeDir} {
		path := filepath.Join(dir, ".claude", "skills", name, "SKILL.md")
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

func axiStatusLine(a axiSkill, projectDir, homeDir string) string {
	if axiSkillInstalled(projectDir, homeDir, a.Name) {
		return fmt.Sprintf("[installed] %s", a.Name)
	}
	cmd := fmt.Sprintf("npx skills add %s --skill %s", a.Repo, a.Name)
	if a.Global {
		cmd += " -g"
	}
	return fmt.Sprintf("[not installed] %s - install with: %s", a.Name, cmd)
}

func checkBinary(label, binaryName string, required bool) checkResult {
	if _, err := exec.LookPath(binaryName); err != nil {
		detail := fmt.Sprintf("%s not found in PATH", binaryName)
		if !required {
			detail += " (optional control backend)"
		}
		return checkResult{Name: label, OK: false, Detail: detail, Required: required}
	}
	return checkResult{Name: label, OK: true, Required: required}
}

// checkNoMistakes reports whether the "no-mistakes" binary is installed
// and, if so, whether this project has been gated (PRD v2, B.4): a real,
// standalone CLI (github.com/kunchenguid/no-mistakes, installed via
// curl, not an "npx skills add" AXI) that puts a local git remote in
// front of the real one - "git push no-mistakes <branch>" runs its own
// review/test/lint/docs pipeline and opens the PR itself once every
// check passes. Optional and purely informational, like the AXIs: a
// project that never ships through the gate doesn't need it, and never
// running "no-mistakes init" is not an error.
func checkNoMistakes(projectDir string) checkResult {
	const name = "no-mistakes"

	if _, err := exec.LookPath("no-mistakes"); err != nil {
		return checkResult{Name: name, Detail: "not found in PATH (optional - only needed for 'vexillum ship', see docs/no-mistakes.md)"}
	}
	if !noMistakesGateConfigured(projectDir) {
		return checkResult{Name: name, Detail: "installed, but this project hasn't run 'no-mistakes init' yet - 'vexillum ship' will do this automatically the first time"}
	}
	return checkResult{Name: name, OK: true, Detail: "installed and this project is gated"}
}

// checkGitHubCLI reports whether the "gh" binary is installed - the
// official GitHub CLI, called directly by "vexillum land" to merge a
// shipped mission's real PR (mergeShippedPR, land_merge.go), not the
// separate "gh-axi" agent-facing AXI. Optional and purely informational:
// a project that never ships through the no-mistakes gate never needs
// it, same posture as the no-mistakes check above.
func checkGitHubCLI() checkResult {
	const name = "GitHub CLI (gh)"
	if _, err := exec.LookPath("gh"); err != nil {
		return checkResult{Name: name, Detail: "not found in PATH (optional - only needed for 'vexillum land' on a shipped mission)"}
	}
	return checkResult{Name: name, OK: true}
}

// noMistakesGateConfigured reports whether projectDir already has the
// "no-mistakes" git remote that "no-mistakes init" creates - the signal
// vexillum ship depends on being there before it can push through it.
func noMistakesGateConfigured(projectDir string) bool {
	cmd := exec.Command("git", "remote", "get-url", "no-mistakes")
	cmd.Dir = projectDir
	return cmd.Run() == nil
}

// checkHerdrVersion anchors the implicit assumption internal/herdr's
// error-code parsing has always run under - herdr 0.9.x, verified live -
// against a real, executable check (PRD v2, A.1). herdr not being on
// PATH at all is already reported by the "herdr" checkBinary line above;
// this returns a zero-value result (an empty Name, skipped by the
// caller) rather than a second line about the same absence.
func checkHerdrVersion() checkResult {
	if _, err := exec.LookPath("herdr"); err != nil {
		return checkResult{}
	}

	const name = "herdr version"

	version, err := herdr.Version()
	if err != nil {
		return checkResult{Name: name, Warn: true, Detail: "could not determine herdr version: " + err.Error()}
	}
	if !herdr.IsSupportedVersion(version) {
		return checkResult{Name: name, Warn: true, Detail: fmt.Sprintf("%s is not a %s version - vexillum's herdr integration was verified against %sx, this version may behave differently", version, herdr.SupportedVersionPrefix, herdr.SupportedVersionPrefix)}
	}
	return checkResult{Name: name, OK: true, Detail: version}
}

func checkVexillumHome(path string) checkResult {
	const name = "~/.vexillum/"

	info, err := os.Stat(path)
	if err != nil {
		return checkResult{Name: name, OK: false, Detail: "does not exist, run 'vexillum init'", Required: true}
	}
	if !info.IsDir() {
		return checkResult{Name: name, OK: false, Detail: "exists but is not a directory", Required: true}
	}
	if info.Mode().Perm()&0o200 == 0 {
		return checkResult{Name: name, OK: false, Detail: "not writable", Required: true}
	}
	return checkResult{Name: name, OK: true, Required: true}
}

func checkProjectInitialized(projectDir string) checkResult {
	const name = "project initialized"

	if projectAlreadyInitialized(projectDir) {
		return checkResult{Name: name, OK: true, Required: true}
	}
	return checkResult{Name: name, OK: false, Detail: "run 'vexillum init'", Required: true}
}

func checkGitRepo(projectDir string) checkResult {
	const name = "git repository"

	if isGitRepo(projectDir) {
		return checkResult{Name: name, OK: true, Required: true}
	}
	return checkResult{Name: name, OK: false, Detail: "current directory is not a git repository", Required: true}
}
