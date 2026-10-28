package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/doctorcheck"
	"github.com/isaias-alt/vexillum/internal/scaffold"
	"github.com/isaias-alt/vexillum/internal/soldier"
)

const doctorUsage = `Report on the health of the vexillum environment. Read-only.

The tool the commander runs in is the one doctor requires. An initialized
project records its commander harnesses in .vexillum/config.json ('` + cmdname.Name + ` init
--commander', claude by default); each one's binary is required, and the
others only warn. For a claude commander doctor also checks the Stop hook and
the CLAUDE.md import described below, which a project with no claude commander
does not need.

Besides the tools vexillum drives, doctor reports, for an initialized project,
the state of the vexillum block in AGENTS.md or CLAUDE.md (absent, current,
stale, drifted or malformed), whether CLAUDE.md imports AGENTS.md when the
block is in AGENTS.md, each first-party skill (installed and current, stale,
edited by hand, or missing), whether the model profiles in models.json are
valid, and whether yolo mode is on (the commander lands a finished mission
without asking).

When a model profile selects the pi harness, or a pi binary is on PATH, doctor
also reports on Pi: the binary, its version, and whether herdr's integration for
pi is installed ('herdr integration install pi'). They only ever warn; a setup
without Pi sees none of these lines.

The same goes for OpenCode, when a profile selects the opencode harness or an
opencode binary is on PATH: the binary, its version (1.x from 1.18.29, or 2.x;
any other is a warning), a warning when an older 1.x install shadows a 2.x one
in PATH (it names both), and whether herdr's integration for opencode is
installed and current ('herdr integration install opencode').

For a pi commander doctor checks, in place of the Stop hook, that
.pi/extensions/vexillum-wake.ts is there and equal to the copy this binary
carries (current, stale or edited by hand), that .pi/settings.json lists
../.claude/skills so Pi sees the skills, whether Pi has been told to trust the
folder (it reads ~/.pi/agent/trust.json and never writes it; with no answer
saved it warns that Pi will ask once), and that the extension's await finds a
` + cmdname.Name + ` binary from the environment doctor runs in. They only warn.

When opencode is one of the project's commanders, doctor also checks how the
sentinel will wake it, from outside, through OpenCode's V2 HTTP API: whether the
commander registered itself (its pane is still live), whether the background
service answers 'GET /api/info' in this environment (asked with 'opencode
service status' first, so doctor never starts one; OpenCode 1.x has no such API
and is a warning), and whether the vexillum block is in a file OpenCode reads
(a block in CLAUDE.md is ignored when an AGENTS.md exists). Doctor does not send
the wake route a prompt. The API checks use this shell's HOME and XDG_*
directories, which the sentinel must share.

It also checks the sentinel Stop hook, the one that wakes the commander when a
soldier finishes: whether it is registered in .claude/settings.json, whether
its command finds a ` + cmdname.Name + ` binary when run from a bare environment (only HOME and
PATH=/usr/bin:/bin, the way a hook shell that never read your profile has it),
and whether a sentinel is running. A hook that cannot find ` + cmdname.Name + ` is a warning
that names the fix; it never fails doctor.

A warning also names the case of two live commanders in one project (the
commander registry knows them, for instance Claude Code and Pi): they share one
wake queue and a wake reaches only whichever asks first, so keep one commander
per project.

The sentinel line warns, and never fails, when more than one sentinel process is
alive (they would race over the same tasks), when the live sentinel is a
different version than this binary, or when the binary it started from has been
replaced on disk since (a rebuild reports the same version, "dev"). Each warning names the
pid to stop; the next ` + cmdname.Name + ` dispatch, prompt or sentinel await then starts a
current one.

Usage:
  ` + cmdname.Name + ` doctor
`

// Doctor runs the "vx doctor" command.
func Doctor(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(doctorUsage)
		return 0
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, cmdname.Name+": cannot determine current directory: %v\n", err)
		return 1
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, cmdname.Name+": cannot determine home directory: %v\n", err)
		return 1
	}

	return runDoctor(cwd, filepath.Join(home, ".vexillum"), home, os.Stdout)
}

func runDoctor(projectDir, vexillumHome, homeDir string, out io.Writer) int {
	commanders, commanderWarns := doctorcheck.Commanders(projectDir)
	isCommander := func(harness string) bool { return slices.Contains(commanders, harness) }

	checks := []doctorcheck.Result{
		doctorcheck.Binary("Claude Code", "claude", isCommander(soldier.HarnessClaude)),
		doctorcheck.Binary("herdr", "herdr", true),
		doctorcheck.HerdrVersion(),
		doctorcheck.GitHubCLI(),
		doctorcheck.VXShadow(),
		doctorcheck.VexillumHome(vexillumHome),
		doctorcheck.ProjectInitialized(projectDir),
		doctorcheck.GitRepo(projectDir),
	}
	checks = append(checks, commanderWarns...)
	checks = append(checks, doctorcheck.PiChecks(projectDir, vexillumHome, isCommander(soldier.HarnessPi))...)
	checks = append(checks, doctorcheck.OpenCodeChecks(projectDir, vexillumHome, isCommander(soldier.HarnessOpenCode))...)
	if isCommander(soldier.HarnessOpenCode) {
		checks = append(checks, doctorcheck.OpenCodeCommanderChecks(projectDir, vexillumHome)...)
	}
	if scaffold.ProjectInitialized(projectDir) {
		checks = append(checks, doctorcheck.ProjectChecks(projectDir, vexillumHome, homeDir)...)
		checks = append(checks, doctorcheck.SharedWakeQueue(projectDir, vexillumHome)...)
	}

	ready := true
	var optionalMissing []string
	var warnings []string
	for _, c := range checks {
		if c.Name == "" {
			// doctorcheck.HerdrVersion skips itself entirely when herdr
			// isn't on PATH - the "[missing] herdr" line above already
			// says so, no need for a second line about it.
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
