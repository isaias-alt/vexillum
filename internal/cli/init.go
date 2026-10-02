package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/scaffold"
)

const initUsage = `Prepare the current project to be orchestrated by vexillum.

Usage:
  ` + cmdname.Name + ` init [--global] [--yes] [--lang en|es] [--skills | --no-skills]

init gives the project's agent the commander rules and the tools to use
vexillum well. Before it writes anything it lists the files of yours it will
change and asks for consent (Continue? [Y/n]):

  AGENTS.md              gets a block managed by vexillum with the always-on
                         commander core (created if missing)
  CLAUDE.md              gets the line @AGENTS.md so Claude Code sees that
                         block (created if missing; an existing one is edited
                         only after a second question)
  .claude/settings.json  gets the sentinel Stop hook, a one-line command that
                         finds ` + cmdname.Name + ` without relying on the PATH of the hook's shell
                         (an older ` + cmdname.Name + ` sentinel await hook is migrated)

The block is written in the language it detects in AGENTS.md (English when it
cannot tell); you confirm or override it. Then init asks "Install the
vexillum, forum and muster skills?" and, if you accept, writes them to
.claude/skills/. It also writes vexillum's own files under .vexillum/
(config.json, models.json, .gitignore); an existing models.json is never
overwritten. Running init again changes nothing that is already in place.

Without a terminal (scripts, CI) init asks nothing and does nothing unless
--yes is given; without it, it prints what it would change and exits with an
error.

Flags:
  --yes, -y       Accept every question, including editing an existing
                  CLAUDE.md and installing the skills.
  --lang en|es    Language of the AGENTS.md block, instead of detecting it.
  --skills        Install the skills without asking.
  --no-skills     Do not install the skills, without asking.
  --global        Scaffold for every project on this machine instead of the
                  current one (see below).

--global scaffolds the commander rules once for every project on this
machine. There is no project AGENTS.md in that case, so the always-on core is
written to ~/.claude/rules/vexillum.md (which Claude Code loads in every
session) and the skills go to ~/.claude/skills/, after the same consent flow.
Opt-in only - it makes the commander persona apply to every Claude Code
session on this machine, not just vexillum projects. Without it, init only
ever touches the current project.
`

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

// Init runs the "vx init" command.
func Init(args []string) int {
	return runSetupCommand(setupInit, initUsage, args)
}

// runSetupCommand is the shared entry of "vx init" and "vx upgrade":
// parse the flags, resolve the directories and run the project or global
// flow against the real process streams.
func runSetupCommand(kind setupKind, usage string, args []string) int {
	opts, help, err := parseSetupArgs(kind, args)
	if help {
		fmt.Print(usage)
		return 0
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, cmdname.Name+": %v\n", err)
		fmt.Fprint(os.Stderr, usage)
		return 1
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, cmdname.Name+": cannot determine home directory: %v\n", err)
		return 1
	}
	vexillumHome := filepath.Join(home, ".vexillum")
	env := osSetupEnv(opts)

	if opts.Global {
		if kind == setupInit {
			return runInitGlobal(env, vexillumHome, home)
		}
		return runUpgradeGlobal(env, vexillumHome, home)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, cmdname.Name+": cannot determine current directory: %v\n", err)
		return 1
	}
	if kind == setupInit {
		return runInit(env, cwd, vexillumHome)
	}
	return runUpgrade(env, cwd, vexillumHome)
}
