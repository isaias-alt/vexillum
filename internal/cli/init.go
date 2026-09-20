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
Code reads it when vexillum dispatches work here. It was scaffolded by
` + "`vexillum init`" + ` and will not be overwritten once it exists - edit it freely
as you learn what works for this project.

## Vocabulary

- **general**: the human you report to.
- **commander**: you, the orchestrator.
- **soldiers**: subagents you dispatch to do work.
- **mission**: a task that changes code and delivers something to land.
- **scout**: a task that only investigates and reports back - never
  commits or pushes anything.
- **camp**: the isolated git worktree a soldier works in.
- **sentinel**: watches soldiers and wakes you only when something needs
  attention. Not built yet (v1 is still sequential) - for now, you find
  out a soldier is done by waiting on the backgrounded dispatch command.

## Dispatching a soldier

**Do NOT use your own Agent/Task tool for this.** That spawns your own
internal subagent - it never touches vexillum's camp/herdr machinery,
never creates an isolated git worktree, never opens a herdr pane, and the
general can't see or attach to it. It is not a vexillum soldier, even
though the word is similar. If you dispatch a "soldier" with your Agent
tool, you have not done what was asked - use ` + "`vexillum dispatch`" + ` instead,
via your Bash tool:

` + "```" + `
vexillum dispatch "<prompt>" [--kind mission|scout]
` + "```" + `

Kind defaults to mission. Use scout for investigation, diagnosis, or
research that shouldn't change anything - never dispatch a scout for work
that's meant to change code.

This creates an isolated camp (git worktree, own branch) and starts a
real, interactive Claude Code session inside a herdr pane in the current
workspace (requires ` + "`$HERDR_WORKSPACE_ID`" + `, set automatically when you're
running inside a herdr-managed pane), with ` + "`--dangerously-skip-permissions`" + `.
That's deliberate: the camp's worktree isolation bounds what the soldier
can affect, and the real safety control is the landing approval below -
nothing a soldier does reaches this project's real history until you
explicitly approve landing it. A soldier can still come back blocked if
Claude Code asks a genuine clarifying question, just rarely.

**Run it in the background, not inline.** The command blocks until the
soldier settles (there's no sentinel yet to watch it for you), so running
it in the foreground makes you go silent and unresponsive to the general
for that whole time - which defeats the point of dispatching a soldier
instead of doing the work yourself. Use your Bash tool's
background-execution option (not ` + "`&`" + `/` + "`nohup`" + ` by hand), tell the general
it's underway, and report back when you're notified it's done.

**Report the outcome, not the plumbing.** When a soldier finishes, tell
the general what got done the way you'd report work you did yourself -
"listo, creé X con Y" - not a technical appendix. Don't volunteer the
camp path, branch name, task id, or exact commands; you already have
them (from the dispatch output and the task's persisted state) if the
general asks to see them. Asking whether to land a finished mission is
the one routine exception (see below) - ask it naturally, not as a
technical footer.

## Landing a finished mission

A scout has nothing to land - it should never have committed anything.
If a scout's camp somehow has commits, don't land it silently; tell the
general and ask what they want done. Once you've reported a scout's
findings, just release its camp (below) - no landing step.

A mission's work lands with a fast-forward merge into this project's base
branch, done by you with ` + "`vexillum land <task-id>`" + `, not by running raw
` + "`git merge`" + ` on your own judgment.

Default posture (unless the general has told you otherwise for this
project): ask before landing. Once a mission finishes done, briefly tell
the general what it did and ask if you should land it - don't merge
unreviewed work on your own. On approval:

` + "```" + `
vexillum land <task-id>
` + "```" + `

This refuses and leaves everything untouched if this project's own
checkout is dirty, or if the mission's branch has diverged from the base
(not a clean fast-forward) - it never forces or rebases anything. If it
refuses, tell the general instead of retrying blindly.

A dirty checkout here is often just ` + "`vexillum init`" + `'s own scaffold
(` + "`AGENTS.md`" + `, ` + "`CLAUDE.md`" + `, ` + "`.vexillum/`" + `) never having been committed -
init writes those files but never commits them itself. Don't let that
surprise you mid-land: if you notice this project's checkout has
uncommitted scaffold files (or anything else untracked) before you ever
dispatch a soldier, commit it yourself with the general's ok early,
instead of hitting the refusal later while a mission is waiting to land.

If the general tells you to land things going forward without asking each
time for this project, you can skip the approval step for future
missions - but that's their call, not your default.

## Releasing a camp

A camp's worktree and pane are never cleaned up automatically. Once a
mission is landed (or a scout's findings are reported), release its camp
so the worktree returns to the pool and the herdr pane closes:

` + "```" + `
vexillum release <task-id>
` + "```" + `

This refuses (leaving the camp and pane untouched) if the worktree still
has uncommitted changes or unlanded commits. Don't call this until the
soldier's work is actually landed (or, for a scout, reported) - it's not
a "give up on this soldier" command.
`

// productClaudeMD makes Claude Code actually load the product AGENTS.md:
// Claude Code auto-loads CLAUDE.md, not a bare AGENTS.md, so without this
// shim the commander never sees AGENTS.md's instructions at all.
const productClaudeMD = "@AGENTS.md\n"

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

	alreadyInitialized := projectAlreadyInitialized(projectDir)
	if !alreadyInitialized {
		if err := writeLocalConfig(projectDir); err != nil {
			fmt.Fprintf(stderr, "vexillum: cannot write .vexillum/config.json: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "Created .vexillum/config.json")
	}

	agentsCreated, err := writeFileIfMissing(projectDir, "AGENTS.md", productAgentsMD)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot write AGENTS.md: %v\n", err)
		return 1
	}

	// Claude Code auto-loads CLAUDE.md, not a bare AGENTS.md - without
	// this, the commander never actually reads AGENTS.md's instructions.
	claudeCreated, err := writeFileIfMissing(projectDir, "CLAUDE.md", productClaudeMD)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot write CLAUDE.md: %v\n", err)
		return 1
	}

	if alreadyInitialized {
		if !agentsCreated && !claudeCreated {
			fmt.Fprintln(stdout, "Project already initialized (found .vexillum/config.json). Nothing to do.")
			return 0
		}
		// Healing an older init that predates one of these files: the
		// scaffold itself isn't new, but restore what's missing.
		if agentsCreated {
			fmt.Fprintln(stdout, "Created missing AGENTS.md")
		}
		if claudeCreated {
			fmt.Fprintln(stdout, "Created missing CLAUDE.md")
		}
		fmt.Fprintln(stdout, "Project already initialized (found .vexillum/config.json); restored the missing file(s) above.")
		return 0
	}

	if agentsCreated {
		fmt.Fprintln(stdout, "Created AGENTS.md")
	} else {
		fmt.Fprintln(stdout, "AGENTS.md already exists, left untouched")
	}
	if claudeCreated {
		fmt.Fprintln(stdout, "Created CLAUDE.md")
	} else {
		fmt.Fprintln(stdout, "CLAUDE.md already exists, left untouched")
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

func writeFileIfMissing(projectDir, name, content string) (created bool, err error) {
	path := filepath.Join(projectDir, name)

	if _, statErr := os.Stat(path); statErr == nil {
		return false, nil
	} else if !os.IsNotExist(statErr) {
		return false, statErr
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return false, err
	}
	return true, nil
}
