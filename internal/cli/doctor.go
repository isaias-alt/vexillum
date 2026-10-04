package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/doctorcheck"
	"github.com/isaias-alt/vexillum/internal/scaffold"
)

const doctorUsage = `Report on the health of the vexillum environment. Read-only.

Besides the tools vexillum drives, doctor reports, for an initialized project,
the state of the vexillum block in AGENTS.md or CLAUDE.md (absent, current,
stale, drifted or malformed), whether CLAUDE.md imports AGENTS.md when the
block is in AGENTS.md, each first-party skill (installed and current, stale,
edited by hand, or missing), whether the model profiles in models.json are
valid, and whether yolo mode is on (the commander lands a finished mission
without asking).

It also checks the sentinel Stop hook, the one that wakes the commander when a
soldier finishes: whether it is registered in .claude/settings.json, whether
its command finds a ` + cmdname.Name + ` binary when run from a bare environment (only HOME and
PATH=/usr/bin:/bin, the way a hook shell that never read your profile has it),
and whether a sentinel is running. A hook that cannot find ` + cmdname.Name + ` is a warning
that names the fix; it never fails doctor.

The sentinel line warns, and never fails, when more than one sentinel process is
alive (they would race over the same tasks), when the live sentinel is a
different version than this binary, or when the binary it started from has been
replaced on disk since (a rebuild reports the same version, "dev"). A sentinel
that predates version tracking is reported the same way. Each warning names the
pid to stop; the next ` + cmdname.Name + ` dispatch, prompt or Stop hook await then starts a
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
	checks := []doctorcheck.Result{
		doctorcheck.Binary("Claude Code", "claude", true),
		doctorcheck.Binary("herdr", "herdr", true),
		doctorcheck.HerdrVersion(),
		doctorcheck.Binary("tmux", "tmux", false),
		doctorcheck.GitHubCLI(),
		doctorcheck.VXShadow(),
		doctorcheck.VexillumHome(vexillumHome),
		doctorcheck.ProjectInitialized(projectDir),
		doctorcheck.GitRepo(projectDir),
	}
	if scaffold.ProjectInitialized(projectDir) {
		checks = append(checks, doctorcheck.ProjectChecks(projectDir, vexillumHome, homeDir)...)
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
