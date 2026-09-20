package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const upgradeUsage = `Refresh an already-initialized project's vexillum scaffold (AGENTS.md,
CLAUDE.md, the sentinel Stop hook) to match this binary's latest version,
without deleting and re-running 'vexillum init' from scratch.

Usage:
  vexillum upgrade
`

// Upgrade runs the "vexillum upgrade" command.
func Upgrade(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(upgradeUsage)
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

	return runUpgrade(cwd, filepath.Join(home, ".vexillum"), os.Stdout, os.Stderr)
}

func runUpgrade(projectDir, vexillumHome string, stdout, stderr io.Writer) int {
	if !projectAlreadyInitialized(projectDir) {
		fmt.Fprintln(stderr, "vexillum: project not initialized here (no .vexillum/config.json)")
		fmt.Fprintln(stderr, "run 'vexillum init' first.")
		return 1
	}

	if _, err := ensureDir(vexillumHome); err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot create %s: %v\n", vexillumHome, err)
		return 1
	}

	cfg, err := readLocalConfig(projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot read .vexillum/config.json: %v\n", err)
		return 1
	}

	agentsResult, err := upgradeScaffoldFile(projectDir, "AGENTS.md", productAgentsMD, cfg.AgentsMDHash)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot upgrade AGENTS.md: %v\n", err)
		return 1
	}
	claudeResult, err := upgradeScaffoldFile(projectDir, "CLAUDE.md", productClaudeMD, cfg.ClaudeMDHash)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot upgrade CLAUDE.md: %v\n", err)
		return 1
	}

	if agentsResult.changed || claudeResult.changed {
		if err := recordScaffoldHashes(projectDir, agentsResult.changed, claudeResult.changed); err != nil {
			fmt.Fprintf(stderr, "vexillum: cannot update .vexillum/config.json: %v\n", err)
			return 1
		}
	}

	fmt.Fprintf(stdout, "AGENTS.md: %s\n", agentsResult.status)
	fmt.Fprintf(stdout, "CLAUDE.md: %s\n", claudeResult.status)

	hookAdded, hookErr := ensureSentinelHook(projectDir)
	if hookErr != nil {
		fmt.Fprintf(stderr, "vexillum: warning: could not add the sentinel Stop hook: %v\n", hookErr)
	} else if hookAdded {
		fmt.Fprintln(stdout, "Added vexillum sentinel Stop hook to .claude/settings.json")
	} else {
		fmt.Fprintln(stdout, "Sentinel Stop hook already present, left untouched")
	}

	fmt.Fprintln(stdout, "vexillum upgrade complete.")
	return 0
}

type scaffoldResult struct {
	changed bool
	status  string
}

// upgradeScaffoldFile brings a single scaffold file up to date with the
// given latest template content:
//   - missing entirely: created fresh.
//   - already matches latest: nothing to do.
//   - matches the hash vexillum stored when it last wrote this file: safe
//     to refresh, since nothing has touched it since.
//   - anything else (no stored hash, or content diverged from that hash):
//     the general may have edited it - left untouched, reported instead.
func upgradeScaffoldFile(projectDir, name, latest, storedHash string) (scaffoldResult, error) {
	path := filepath.Join(projectDir, name)

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if writeErr := os.WriteFile(path, []byte(latest), 0o644); writeErr != nil {
			return scaffoldResult{}, writeErr
		}
		return scaffoldResult{changed: true, status: "created (was missing)"}, nil
	}
	if err != nil {
		return scaffoldResult{}, err
	}

	current := string(data)
	if current == latest {
		return scaffoldResult{status: "already up to date"}, nil
	}

	if storedHash == "" || storedHash != hashContent(current) {
		return scaffoldResult{status: "has local changes, left untouched (compare manually for the latest template)"}, nil
	}

	if err := os.WriteFile(path, []byte(latest), 0o644); err != nil {
		return scaffoldResult{}, err
	}
	return scaffoldResult{changed: true, status: "upgraded to the latest template"}, nil
}
