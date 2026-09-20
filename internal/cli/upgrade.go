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
  vexillum upgrade [--force]

By default, AGENTS.md/CLAUDE.md are only refreshed when vexillum can tell
they weren't hand-edited since it last wrote them (tracked by a stored
content hash) - anything else is left untouched and reported instead.

--force overwrites AGENTS.md/CLAUDE.md to the latest template
regardless, including a project from before this hash tracking existed
whose content vexillum can't otherwise vouch for. Only pass it once
you've confirmed there's nothing local worth keeping in those files -
it discards it.
`

// Upgrade runs the "vexillum upgrade" command.
func Upgrade(args []string) int {
	force := false
	for _, a := range args {
		switch a {
		case "-h", "--help":
			fmt.Print(upgradeUsage)
			return 0
		case "--force":
			force = true
		default:
			fmt.Fprintf(os.Stderr, "vexillum: unknown upgrade flag %q\n", a)
			fmt.Fprint(os.Stderr, upgradeUsage)
			return 1
		}
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

	return runUpgrade(cwd, filepath.Join(home, ".vexillum"), force, os.Stdout, os.Stderr)
}

func runUpgrade(projectDir, vexillumHome string, force bool, stdout, stderr io.Writer) int {
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

	agentsResult, err := upgradeScaffoldFile(projectDir, "AGENTS.md", productAgentsMD, cfg.AgentsMDHash, force)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot upgrade AGENTS.md: %v\n", err)
		return 1
	}
	claudeResult, err := upgradeScaffoldFile(projectDir, "CLAUDE.md", productClaudeMD, cfg.ClaudeMDHash, force)
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
//     the general may have edited it - left untouched and reported
//     instead, unless force is set (see the --force flag's own doc in
//     upgradeUsage), which overwrites regardless.
func upgradeScaffoldFile(projectDir, name, latest, storedHash string, force bool) (scaffoldResult, error) {
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

	safeToRefresh := storedHash != "" && storedHash == hashContent(current)
	if !safeToRefresh && !force {
		return scaffoldResult{status: "has local changes, left untouched (compare manually, or rerun with --force to overwrite)"}, nil
	}

	if err := os.WriteFile(path, []byte(latest), 0o644); err != nil {
		return scaffoldResult{}, err
	}
	status := "upgraded to the latest template"
	if !safeToRefresh {
		status = "force-upgraded to the latest template (local changes discarded)"
	}
	return scaffoldResult{changed: true, status: status}, nil
}
