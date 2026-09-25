package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/isaias-alt/vexillum/internal/doctorcheck"
)

const doctorUsage = `Report on the health of the vexillum environment. Read-only.

Usage:
  vexillum doctor
`

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

func runDoctor(projectDir, vexillumHome, homeDir string, out io.Writer) int {
	checks := []doctorcheck.Result{
		doctorcheck.Binary("Claude Code", "claude", true),
		doctorcheck.Binary("herdr", "herdr", true),
		doctorcheck.HerdrVersion(),
		doctorcheck.Binary("tmux", "tmux", false),
		doctorcheck.NoMistakes(projectDir),
		doctorcheck.GitHubCLI(),
		doctorcheck.VexillumHome(vexillumHome),
		doctorcheck.ProjectInitialized(projectDir),
		doctorcheck.GitRepo(projectDir),
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
	fmt.Fprintln(out, "AXIs and first-party skills (on-demand, installed as Agent Skills - not required):")
	for _, a := range doctorcheck.KnownAXIs {
		fmt.Fprintln(out, doctorcheck.AXIStatusLine(a, projectDir, homeDir))
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
