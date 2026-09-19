package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const initUsage = `Prepare the current project to be orchestrated by vexillum.

Usage:
  vexillum init
`

const productAgentsMD = `# AGENTS.md - Commander

This file governs how the vexillum commander behaves in this project. Claude
Code reads it when vexillum dispatches work here.

## Vocabulary

- **general**: the human you report to.
- **commander**: you, the orchestrator.
- **soldiers**: subagents you dispatch to do work.
- **mission**: a task that changes code and delivers a PR.
- **scout**: a task that only investigates and leaves a report.
- **camp**: the isolated git worktree a soldier works in.
- **sentinel**: watches soldiers and wakes you only when something needs
  attention.

## Status

This file was scaffolded by ` + "`vexillum init`" + `. Dispatch logic isn't wired up
yet - vexillum is still on its CLI base layer. Edit this file freely;
vexillum will not overwrite it once it exists.
`

type localConfig struct {
	Version       int       `json:"version"`
	InitializedAt time.Time `json:"initialized_at"`
}

// Init runs the "vexillum init" command.
func Init(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(initUsage)
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

	return runInit(cwd, filepath.Join(home, ".vexillum"), os.Stdout, os.Stderr)
}

func runInit(projectDir, vexillumHome string, stdout, stderr io.Writer) int {
	if !isGitRepo(projectDir) {
		fmt.Fprintln(stderr, "vexillum: current directory is not a git repository")
		fmt.Fprintln(stderr, "vexillum requires git; run 'git init' first.")
		return 1
	}

	created, err := ensureDir(vexillumHome)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot create %s: %v\n", vexillumHome, err)
		return 1
	}
	if created {
		fmt.Fprintf(stdout, "Created %s\n", vexillumHome)
	}

	if projectAlreadyInitialized(projectDir) {
		fmt.Fprintln(stdout, "Project already initialized (found .vexillum/config.json). Nothing to do.")
		return 0
	}

	if err := writeLocalConfig(projectDir); err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot write .vexillum/config.json: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "Created .vexillum/config.json")

	agentsCreated, err := writeAgentsMDIfMissing(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot write AGENTS.md: %v\n", err)
		return 1
	}
	if agentsCreated {
		fmt.Fprintln(stdout, "Created AGENTS.md")
	} else {
		fmt.Fprintln(stdout, "AGENTS.md already exists, left untouched")
	}

	fmt.Fprintln(stdout, "vexillum initialized.")
	return 0
}

func isGitRepo(dir string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

// ensureDir creates path if it doesn't exist. Returns whether it created it.
func ensureDir(path string) (created bool, err error) {
	if _, statErr := os.Stat(path); statErr == nil {
		return false, nil
	} else if !os.IsNotExist(statErr) {
		return false, statErr
	}

	if err := os.MkdirAll(path, 0o755); err != nil {
		return false, err
	}
	return true, nil
}

func projectAlreadyInitialized(projectDir string) bool {
	_, err := os.Stat(filepath.Join(projectDir, ".vexillum", "config.json"))
	return err == nil
}

func writeLocalConfig(projectDir string) error {
	dir := filepath.Join(projectDir, ".vexillum")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	cfg := localConfig{Version: 1, InitializedAt: time.Now().UTC()}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	return os.WriteFile(filepath.Join(dir, "config.json"), data, 0o644)
}

func writeAgentsMDIfMissing(projectDir string) (created bool, err error) {
	path := filepath.Join(projectDir, "AGENTS.md")

	if _, statErr := os.Stat(path); statErr == nil {
		return false, nil
	} else if !os.IsNotExist(statErr) {
		return false, statErr
	}

	if err := os.WriteFile(path, []byte(productAgentsMD), 0o644); err != nil {
		return false, err
	}
	return true, nil
}
