package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/isaias-alt/vexillum/internal/scaffold"
)

const initUsage = `Prepare the current project to be orchestrated by vexillum.

Usage:
  vexillum init [--global]

--global scaffolds the commander rules once for every project on this
machine (~/.claude/rules/vexillum.md) instead of the current project. Opt-in
only - it makes the commander persona apply to every Claude Code session on
this machine, not just vexillum projects. Without it, init only ever touches
the current project.
`

// productVexillumRule is the commander rules content vexillum writes to
// .claude/rules/vexillum.md. It lives in internal/scaffold since
// "vexillum upgrade" (upgrade.go) needs it too; kept as a package-level
// name here for this package's own tests.
var productVexillumRule = scaffold.VexillumCommanderRules

const (
	sentinelHookCommand       = scaffold.SentinelHookCommand
	legacySentinelHookCommand = scaffold.LegacySentinelHookCommand
)

// writeLocalConfig creates a fresh config.json directly inside configDir -
// the project's .vexillum/ for local scaffolds, or vexillumHome itself for
// the global scaffold (see runInitGlobal).
func writeLocalConfig(configDir string) error {
	return scaffold.WriteConfig(configDir)
}

// ensureSentinelHook merges the async sentinel Stop hook into the
// project's .claude/settings.json - see scaffold.EnsureSentinelHook.
func ensureSentinelHook(projectDir string) (bool, error) {
	return scaffold.EnsureSentinelHook(projectDir)
}

// Init runs the "vexillum init" command.
func Init(args []string) int {
	global := false
	for _, a := range args {
		switch a {
		case "-h", "--help":
			fmt.Print(initUsage)
			return 0
		case "--global":
			global = true
		default:
			fmt.Fprintf(os.Stderr, "vexillum: unknown init flag %q\n", a)
			fmt.Fprint(os.Stderr, initUsage)
			return 1
		}
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "vexillum: cannot determine home directory: %v\n", err)
		return 1
	}
	vexillumHome := filepath.Join(home, ".vexillum")

	if global {
		return runInitGlobal(vexillumHome, home, os.Stdout, os.Stderr)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "vexillum: cannot determine current directory: %v\n", err)
		return 1
	}

	return runInit(cwd, vexillumHome, os.Stdout, os.Stderr)
}

func runInit(projectDir, vexillumHome string, stdout, stderr io.Writer) int {
	if err := refuseInsideVexillumHome(projectDir, vexillumHome); err != nil {
		fmt.Fprintln(stderr, "vexillum:", err)
		return 1
	}

	if !scaffold.IsGitRepo(projectDir) {
		fmt.Fprintln(stderr, "vexillum: current directory is not a git repository")
		fmt.Fprintln(stderr, "vexillum requires git; run 'git init' first.")
		return 1
	}

	created, err := scaffold.EnsureDir(vexillumHome)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot create %s: %v\n", vexillumHome, err)
		return 1
	}
	if created {
		fmt.Fprintf(stdout, "Created %s\n", vexillumHome)
	}

	configDir := filepath.Join(projectDir, ".vexillum")
	alreadyInitialized := scaffold.ProjectInitialized(projectDir)
	if !alreadyInitialized {
		if err := writeLocalConfig(configDir); err != nil {
			fmt.Fprintf(stderr, "vexillum: cannot write .vexillum/config.json: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "Created .vexillum/config.json")
	}

	ruleDir := filepath.Join(projectDir, ".claude", "rules")
	if err := os.MkdirAll(ruleDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot create %s: %v\n", ruleDir, err)
		return 1
	}
	ruleCreated, err := scaffold.WriteFileIfMissing(ruleDir, "vexillum.md", productVexillumRule)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot write .claude/rules/vexillum.md: %v\n", err)
		return 1
	}

	// .claude/settings.json isn't vexillum's file - it's the user's own
	// Claude Code config. Merge our Stop hook in carefully; a malformed
	// existing file is a pre-existing problem, warn but don't fail init
	// over it.
	hookAdded, hookErr := ensureSentinelHook(projectDir)
	if hookErr != nil {
		fmt.Fprintf(stderr, "vexillum: warning: could not add the sentinel Stop hook: %v\n", hookErr)
	} else if hookAdded {
		fmt.Fprintln(stdout, "Added vexillum sentinel Stop hook to .claude/settings.json")
	}

	if alreadyInitialized {
		if !ruleCreated && !hookAdded {
			fmt.Fprintln(stdout, "Project already initialized (found .vexillum/config.json). Nothing to do.")
			return 0
		}
		// Healing an older init that predates one of these: the scaffold
		// itself isn't new, but restore what's missing.
		if ruleCreated {
			fmt.Fprintln(stdout, "Created missing .claude/rules/vexillum.md")
			if err := scaffold.RecordHash(configDir, ruleCreated); err != nil {
				fmt.Fprintf(stderr, "vexillum: warning: could not record scaffold hash: %v\n", err)
			}
		}
		fmt.Fprintln(stdout, "Project already initialized (found .vexillum/config.json); restored the missing piece(s) above.")
		return 0
	}

	if ruleCreated {
		fmt.Fprintln(stdout, "Created .claude/rules/vexillum.md")
	} else {
		fmt.Fprintln(stdout, ".claude/rules/vexillum.md already exists, left untouched")
	}
	if err := scaffold.RecordHash(configDir, ruleCreated); err != nil {
		fmt.Fprintf(stderr, "vexillum: warning: could not record scaffold hash: %v\n", err)
	}

	fmt.Fprintln(stdout, "vexillum initialized.")
	return 0
}

// runInitGlobal scaffolds the commander rules once for every project on
// this machine (~/.claude/rules/vexillum.md) instead of the current
// project - opt-in via 'vexillum init --global'. Applies to every Claude
// Code session on this machine, not just vexillum projects; see initUsage
// for why this isn't the default.
func runInitGlobal(vexillumHome, home string, stdout, stderr io.Writer) int {
	created, err := scaffold.EnsureDir(vexillumHome)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot create %s: %v\n", vexillumHome, err)
		return 1
	}
	if created {
		fmt.Fprintf(stdout, "Created %s\n", vexillumHome)
	}

	alreadyInitialized := scaffold.GlobalInitialized(vexillumHome)
	if !alreadyInitialized {
		if err := writeLocalConfig(vexillumHome); err != nil {
			fmt.Fprintf(stderr, "vexillum: cannot write %s: %v\n", filepath.Join(vexillumHome, "config.json"), err)
			return 1
		}
		fmt.Fprintf(stdout, "Created %s\n", filepath.Join(vexillumHome, "config.json"))
	}

	ruleDir := filepath.Join(home, ".claude", "rules")
	if err := os.MkdirAll(ruleDir, 0o755); err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot create %s: %v\n", ruleDir, err)
		return 1
	}
	ruleCreated, err := scaffold.WriteFileIfMissing(ruleDir, "vexillum.md", productVexillumRule)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot write %s: %v\n", filepath.Join(ruleDir, "vexillum.md"), err)
		return 1
	}

	if alreadyInitialized {
		if !ruleCreated {
			fmt.Fprintln(stdout, "Already initialized globally (found "+filepath.Join(vexillumHome, "config.json")+"). Nothing to do.")
			return 0
		}
		fmt.Fprintln(stdout, "Created missing ~/.claude/rules/vexillum.md")
		if err := scaffold.RecordHash(vexillumHome, ruleCreated); err != nil {
			fmt.Fprintf(stderr, "vexillum: warning: could not record scaffold hash: %v\n", err)
		}
		fmt.Fprintln(stdout, "Already initialized globally; restored the missing piece above.")
		return 0
	}

	if ruleCreated {
		fmt.Fprintln(stdout, "Created ~/.claude/rules/vexillum.md")
	} else {
		fmt.Fprintln(stdout, "~/.claude/rules/vexillum.md already exists, left untouched")
	}
	if err := scaffold.RecordHash(vexillumHome, ruleCreated); err != nil {
		fmt.Fprintf(stderr, "vexillum: warning: could not record scaffold hash: %v\n", err)
	}

	fmt.Fprintln(stdout, "vexillum initialized globally - this applies to every Claude Code session on this machine.")
	return 0
}
