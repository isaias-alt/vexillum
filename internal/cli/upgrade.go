package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/scaffold"
)

const upgradeUsage = `Refresh an already-initialized project's vexillum scaffold
(.claude/rules/vexillum.md, the sentinel Stop hook) to match this binary's
latest version, without deleting and re-running '` + cmdname.Name + ` init' from
scratch.

Usage:
  ` + cmdname.Name + ` upgrade [--force] [--global]

By default, .claude/rules/vexillum.md is only refreshed when vexillum can
tell it wasn't hand-edited since it last wrote it (tracked by a stored
content hash) - anything else is left untouched and reported instead.

--force overwrites .claude/rules/vexillum.md to the latest template
regardless, including a project from before this hash tracking existed
whose content vexillum can't otherwise vouch for. Only pass it once
you've confirmed there's nothing local worth keeping in that file - it
discards it.

--global refreshes the global scaffold (~/.claude/rules/vexillum.md,
written by '` + cmdname.Name + ` init --global') instead of the current project's.
`

// Upgrade runs the "vx upgrade" command.
func Upgrade(args []string) int {
	force := false
	global := false
	for _, a := range args {
		switch a {
		case "-h", "--help":
			fmt.Print(upgradeUsage)
			return 0
		case "--force":
			force = true
		case "--global":
			global = true
		default:
			fmt.Fprintf(os.Stderr, cmdname.Name+": unknown upgrade flag %q\n", a)
			fmt.Fprint(os.Stderr, upgradeUsage)
			return 1
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, cmdname.Name+": cannot determine home directory: %v\n", err)
		return 1
	}
	vexillumHome := filepath.Join(home, ".vexillum")

	if global {
		return runUpgradeGlobal(vexillumHome, home, force, os.Stdout, os.Stderr)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, cmdname.Name+": cannot determine current directory: %v\n", err)
		return 1
	}

	return runUpgrade(cwd, vexillumHome, force, os.Stdout, os.Stderr)
}

func runUpgrade(projectDir, vexillumHome string, force bool, stdout, stderr io.Writer) int {
	if err := refuseInsideVexillumHome(projectDir, vexillumHome); err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}

	if !scaffold.ProjectInitialized(projectDir) {
		fmt.Fprintln(stderr, cmdname.Name+": project not initialized here (no .vexillum/config.json)")
		fmt.Fprintln(stderr, "run '"+cmdname.Name+" init' first.")
		return 1
	}

	if _, err := scaffold.EnsureDir(vexillumHome); err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": cannot create %s: %v\n", vexillumHome, err)
		return 1
	}

	configDir := filepath.Join(projectDir, ".vexillum")
	cfg, err := scaffold.ReadConfig(configDir)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": cannot read .vexillum/config.json: %v\n", err)
		return 1
	}

	ruleDir := filepath.Join(projectDir, ".claude", "rules")
	if err := os.MkdirAll(ruleDir, 0o755); err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": cannot create %s: %v\n", ruleDir, err)
		return 1
	}
	ruleResult, err := scaffold.UpgradeFile(ruleDir, "vexillum.md", productVexillumRule, cfg.VexillumRuleHash, force)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": cannot upgrade .claude/rules/vexillum.md: %v\n", err)
		return 1
	}

	if err := scaffold.RecordHash(configDir, ruleResult.Changed); err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": cannot update .vexillum/config.json: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, ".claude/rules/vexillum.md: %s\n", ruleResult.Status)

	hookAdded, hookErr := ensureSentinelHook(projectDir)
	if hookErr != nil {
		fmt.Fprintf(stderr, cmdname.Name+": warning: could not add the sentinel Stop hook: %v\n", hookErr)
	} else if hookAdded {
		fmt.Fprintln(stdout, "Added "+cmdname.Name+" sentinel Stop hook to .claude/settings.json")
	} else {
		fmt.Fprintln(stdout, "Sentinel Stop hook already present, left untouched")
	}

	fmt.Fprintln(stdout, cmdname.Name+" upgrade complete.")
	return 0
}

// runUpgradeGlobal refreshes the global scaffold (~/.claude/rules/vexillum.md,
// written by 'vx init --global') instead of a project's - same
// hash-based drift detection and --force escape hatch as runUpgrade, just
// against vexillumHome/config.json instead of a project's .vexillum/.
func runUpgradeGlobal(vexillumHome, home string, force bool, stdout, stderr io.Writer) int {
	if !scaffold.GlobalInitialized(vexillumHome) {
		fmt.Fprintf(stderr, cmdname.Name+": global scaffold not initialized (no %s)\n", filepath.Join(vexillumHome, "config.json"))
		fmt.Fprintln(stderr, "run '"+cmdname.Name+" init --global' first.")
		return 1
	}

	if _, err := scaffold.EnsureDir(vexillumHome); err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": cannot create %s: %v\n", vexillumHome, err)
		return 1
	}

	cfg, err := scaffold.ReadConfig(vexillumHome)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": cannot read %s: %v\n", filepath.Join(vexillumHome, "config.json"), err)
		return 1
	}

	ruleDir := filepath.Join(home, ".claude", "rules")
	if err := os.MkdirAll(ruleDir, 0o755); err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": cannot create %s: %v\n", ruleDir, err)
		return 1
	}
	ruleResult, err := scaffold.UpgradeFile(ruleDir, "vexillum.md", productVexillumRule, cfg.VexillumRuleHash, force)
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": cannot upgrade ~/.claude/rules/vexillum.md: %v\n", err)
		return 1
	}

	if err := scaffold.RecordHash(vexillumHome, ruleResult.Changed); err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": cannot update %s: %v\n", filepath.Join(vexillumHome, "config.json"), err)
		return 1
	}

	fmt.Fprintf(stdout, "~/.claude/rules/vexillum.md: %s\n", ruleResult.Status)
	fmt.Fprintln(stdout, cmdname.Name+" upgrade complete (global).")
	return 0
}
