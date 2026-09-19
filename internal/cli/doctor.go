package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const doctorUsage = `Report on the health of the vexillum environment. Read-only.

Usage:
  vexillum doctor
`

// checkResult is the outcome of a single doctor check. Required checks that
// fail make the environment not ready (non-zero exit); optional ones only
// produce a warning.
type checkResult struct {
	Name     string
	OK       bool
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

	return runDoctor(cwd, filepath.Join(home, ".vexillum"), os.Stdout)
}

func runDoctor(projectDir, vexillumHome string, out io.Writer) int {
	checks := []checkResult{
		checkBinary("Claude Code", "claude", true),
		checkBinary("herdr", "herdr", true),
		checkBinary("tmux", "tmux", false),
		checkVexillumHome(vexillumHome),
		checkProjectInitialized(projectDir),
		checkGitRepo(projectDir),
	}

	ready := true
	var optionalMissing []string
	for _, c := range checks {
		status := "ok"
		if !c.OK {
			status = "missing"
		}
		line := fmt.Sprintf("[%s] %s", status, c.Name)
		if !c.OK && c.Detail != "" {
			line += " - " + c.Detail
		}
		fmt.Fprintln(out, line)

		if !c.OK {
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
	if len(optionalMissing) > 0 {
		fmt.Fprintf(out, "Environment ready (optional: %s missing).\n", strings.Join(optionalMissing, ", "))
	} else {
		fmt.Fprintln(out, "Environment ready.")
	}
	return 0
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
