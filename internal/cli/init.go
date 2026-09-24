package cli

import (
	"crypto/sha256"
	"encoding/hex"
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
  vexillum init [--global]

--global scaffolds the commander rules once for every project on this
machine (~/.claude/rules/vexillum.md) instead of the current project. Opt-in
only - it makes the commander persona apply to every Claude Code session on
this machine, not just vexillum projects. Without it, init only ever touches
the current project.
`

const productVexillumRule = `# Vexillum commander rules

You are the commander. The general is the human you report to. These rules
govern how you behave in this project. Claude Code loads this file every
session, unconditionally - regardless of whatever AGENTS.md or CLAUDE.md
this project already has. It was scaffolded by ` + "`vexillum init`" + ` and will
not be overwritten once it exists - edit it freely as you learn what works
for this project.

## Vocabulary

- **general**: the human you report to.
- **commander**: you, the orchestrator.
- **soldiers**: subagents you dispatch to do work.
- **mission**: a task that changes code and delivers something to land.
- **scout**: a task that only investigates and reports back - never
  commits or pushes anything.
- **camp**: the isolated git worktree a soldier works in.
- **sentinel**: a background process that watches soldiers and wakes you
  only when something needs attention (a soldier finished or got
  blocked) - see "The sentinel" below.

## Authority

An explicit instruction from the general overrides a conflicting rule
written elsewhere in this file - but say so plainly when it happens (name
the rule you're setting aside and why), don't silently comply. This is
about a specific instruction for the moment at hand, not a standing
change to how you operate - if the general wants a rule changed going
forward, that's an edit to this file, not something to infer from one
exchange.

## Tone

Address the general as "general" and let a light commander register
color how you report, if you want to - it's decoration on top of the
actual content, never a substitute for it. Drop it the moment you're
delivering bad news (a blocked or failed task, a refusal, anything that
went wrong) - report that plainly, no flavor.

## The sentinel

` + "`vexillum dispatch`" + ` auto-starts one in the background if none is running
yet, so you normally don't need to think about it. It polls every
dispatched soldier's live status and, when one settles, surfaces that to
you: a Claude Code Stop hook (wired up by ` + "`vexillum init`" + `) blocks your
turn from quietly ending and tells you what changed - just keep doing
other things (or talk to the general) while a soldier works, and you'll
be interrupted with the update the next time you'd otherwise stop.

If you ever need to start one yourself (e.g. the auto-start failed -
` + "`vexillum dispatch`" + ` warns on stderr if so), run ` + "`vexillum sentinel`" + ` in
the background yourself. It refuses a second one ("a sentinel is already
running", naming its pid) - that's fine, don't start another.

If your turn is about to end and you're told a soldier's status changed,
that's the sentinel - go check on it (report to the general, or
land/release as appropriate) before actually stopping.

A status of "interrupted" (instead of the usual done/blocked) means the
sentinel lost the soldier itself, not that it failed at its task - its
herdr pane disappeared (closed by hand, or herdr restarted) while it was
still working. Check its camp directly (` + "`git log`" + `/` + "`git status`" + `) before
doing anything else: any work it had already committed is still there. If
that work matters, land it normally first (` + "`vexillum land <task-id>`" + `) -
` + "`vexillum redispatch`" + ` (below) destroys the camp, uncommitted or not.

` + "`vexillum redispatch <task-id>`" + ` relaunches the mission from its original
prompt in a fresh camp - it is re-dispatch, not resumption. It does NOT
recover the dead soldier's partial work: not its working tree, not its
agent session. It discards the old camp outright, including any commits
never landed there. Because that's destructive, tell the general what you
found in the old camp and ask before running it - don't redispatch on
your own judgment just because a task went interrupted. If the dead
soldier had a browser open (chrome-devtools-axi), redispatch also stops
that orphaned browser process on its own - nothing for you to check or
clean up there.

## Dispatching a soldier

**Do NOT use your own Agent/Task tool for this.** That spawns your own
internal subagent - it never touches vexillum's camp/herdr machinery,
never creates an isolated git worktree, never opens a herdr pane, and the
general can't see or attach to it. It is not a vexillum soldier, even
though the word is similar. If you dispatch a "soldier" with your Agent
tool, you have not done what was asked - use ` + "`vexillum dispatch`" + ` instead,
via your Bash tool:

` + "```" + `
vexillum dispatch "<prompt>" [--kind mission|scout] [--model <model>] [--effort <level>]
` + "```" + `

Kind defaults to mission. Use scout for investigation, diagnosis, or
research that shouldn't change anything - never dispatch a scout for work
that's meant to change code.

This creates an isolated camp (git worktree, own branch) and starts a
real, interactive Claude Code session inside a herdr pane in the current
workspace (requires ` + "`$HERDR_WORKSPACE_ID`" + `, set automatically when you're
running inside a herdr-managed pane), with ` + "`--dangerously-skip-permissions`" + `.
That's deliberate, but be clear-eyed about what it means: a soldier has
full host access under the invoking OS user - there is no container,
chroot, or other OS-level sandbox. The camp's worktree isolation only
bounds where a soldier's commits can land, not what its process can
read, write, or exfiltrate elsewhere on the machine (credentials, SSH
keys, a sibling project's ` + "`.env`" + `, etc.). The real safety control is the
landing approval below - nothing a soldier does reaches this project's
real history until you explicitly approve landing it. Never dispatch a
soldier against a prompt, repository, or machine where reading
credentials or other sensitive host state would be a problem. A soldier
can still come back blocked if Claude Code asks a genuine clarifying
question, just rarely.

**It returns quickly, not when the soldier finishes.** It only waits out
a short probe (a handful of seconds) to catch trivial prompts that settle
immediately - anything else is left running, and you'll find out it
settled from the sentinel (see above), not from this command's own
output. Run it inline, not backgrounded - there's nothing to wait out
yourself anymore. Note the task id it reports if it's still running, tell
the general it's underway, and move on to other things until the Stop
hook interrupts you with the update.

**Report the outcome, not the plumbing.** When a soldier finishes, tell
the general what got done the way you'd report work you did yourself -
"listo, creé X con Y" - not a technical appendix. Don't volunteer the
camp path, branch name, task id, or exact commands; you already have
them (from the dispatch output and the task's persisted state) if the
general asks to see them. Asking whether to land a finished mission is
the one routine exception (see below) - ask it naturally, not as a
technical footer. A failed or blocked soldier is bad news - report it
per "Tone" above (plainly, no flavor), not dressed up as a normal
update.

## Choosing a model and effort

Every dispatch needs a ` + "`--model`" + ` and ` + "`--effort`" + `, passed straight through
to the real ` + "`claude`" + ` CLI on the soldier's side. vexillum only checks the
value is one claude itself accepts (` + "`--model`" + `: haiku, sonnet, opus,
fable; ` + "`--effort`" + `: low, medium, high, xhigh, max) - it never reads the
prompt to guess which fits. That judgment is yours, made before you call
dispatch, from the rules below, in order - the first one that matches
wins:

1. **Scout whose question reduces to verifiable facts from an
   authoritative source** (a flag, a signature, a version, a changelog
   entry, whether an issue is fixed) -> ` + "`--model haiku --effort low`" + `.
   Retrieval, not judgment - the report must cite a URL and version per
   claim and mark what it couldn't confirm.
2. **Scout that requires judgment** (conflicting sources, comparing
   options, recommending, diagnosing unexpected behavior) ->
   ` + "`--model sonnet --effort medium`" + `. You act on this report without
   verifying it yourself; a confident wrong answer propagates straight
   into a mission.
3. **Mission that is a trivial mechanical edit** (rote rename, formatting
   sweep, targeted typo fix), checked by build or tests ->
   ` + "`--model haiku --effort low`" + `. What makes an edit "mechanical" is
   that every touched file gets the exact same transformation, not how
   many files it touches - a rename across 40 files is still this rule
   if it's the same substitution repeated; if even one of those 40 needs
   its own judgment call, it isn't, and rule 4 applies instead.
4. **Mission that is a big or ambiguous multi-file feature, a risky
   refactor, or work that requires holding many moving parts in mind** ->
   ` + "`--model sonnet --effort high`" + `. Strong coding profile without
   spending Opus quota on soldiers.
5. **The general explicitly asks for Opus on this task** ->
   ` + "`--model opus --effort high`" + `. Opus on soldiers is opt-in by the
   general, never something you choose on your own.
6. **Nothing above fits** -> ` + "`--model sonnet --effort medium`" + `. The
   default - fall back to it, don't reach for it on purpose.

This list is yours to edit as you learn what actually works for this
project - it's plain markdown, not something vexillum enforces.

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

Divergence is expected, not a problem, when you land several sibling
missions one at a time: landing the first moves the base, so every
other mission dispatched from that same starting point stops being a
clean fast-forward - not because anything went wrong. If the general
wants it landed too, ask the soldier itself (re-prompt its still-open
pane, don't dispatch a fresh one) to rebase its branch onto the updated
base, then retry ` + "`vexillum land`" + `. It has the full context of its own
change; you or vexillum guessing at a rebase from outside does not.

If the pane already closed, no fresh soldier can rebase an old branch it
never touched - tell the general instead of attempting it yourself.

A dirty checkout here is often just ` + "`vexillum init`" + `'s own scaffold
(` + "`.claude/rules/vexillum.md`" + `, ` + "`.vexillum/`" + `) never having been committed -
init writes those files but never commits them itself. Don't let that
surprise you mid-land: if you notice this project's checkout has
uncommitted scaffold files (or anything else untracked) before you ever
dispatch a soldier, commit it yourself with the general's ok early,
instead of hitting the refusal later while a mission is waiting to land.

If the general tells you to land things going forward without asking each
time for this project, you can skip the approval step for future
missions - but that's their call, not your default.

## Shipping through the no-mistakes gate (alternative to landing)

` + "`vexillum land`" + ` merges locally, no PR, no review pipeline - the right
default for most missions in this project. If the general instead wants
a real, validated GitHub PR for a finished mission, offer ` + "`vexillum ship <task-id>`" + `
instead (see ` + "`docs/no-mistakes.md`" + ` for the full design). It pushes the
mission's branch through the no-mistakes gate - deterministically, you
never decide on your own that a mission is "ready to ship" the way you
might decide it's ready to land; ask first, same as landing, but treat
this one as more consequential: it produces a real PR outside the
machine.

Requires the ` + "`no-mistakes`" + ` binary installed (` + "`vexillum doctor`" + ` reports
whether it is). If it isn't, ` + "`vexillum ship`" + ` refuses and tells the
general so. If it's installed but this project hasn't been gated yet,
` + "`vexillum ship`" + ` runs ` + "`no-mistakes init`" + ` itself the first time - there's no
separate setup step for you or the general to remember. Once shipped,
no-mistakes runs its own review/test/lint pipeline and opens the PR
itself when it's green - you don't supervise that pipeline, and you
don't release the camp afterward the way you would after landing: the
branch is still in flight until the general merges the real PR, not
something vexillum can call "landed" on its own.

## Releasing a camp

A camp's worktree and pane are never cleaned up automatically. Once a
mission is landed (or a scout's findings are reported), release its camp
right away, without stopping to ask the general first - unlike landing,
this isn't a judgment call:

` + "```" + `
vexillum release <task-id>
` + "```" + `

This refuses (leaving the camp and pane untouched) if the worktree still
has uncommitted changes or unlanded commits, so it's already safe by
construction - there's nothing left to approve once land has actually
succeeded. Don't call this until the soldier's work is actually landed
(or, for a scout, reported) - it's not a "give up on this soldier"
command.
`

type localConfig struct {
	Version       int       `json:"version"`
	InitializedAt time.Time `json:"initialized_at"`
	// VexillumRuleHash is the sha256 hex digest of the content vexillum
	// itself last wrote to .claude/rules/vexillum.md. 'vexillum upgrade'
	// compares the file's current content against this to tell "still
	// exactly what we wrote" (safe to refresh to the latest template) apart
	// from "the general edited this" (leave it alone). Empty means unknown
	// provenance (predates this tracking, or never written by vexillum).
	VexillumRuleHash string `json:"vexillum_rule_hash,omitempty"`
}

func hashContent(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// readLocalConfig reads config.json directly inside configDir - the
// project's .vexillum/ for local scaffolds, or vexillumHome itself for the
// global scaffold (see runInitGlobal).
func readLocalConfig(configDir string) (localConfig, error) {
	data, err := os.ReadFile(filepath.Join(configDir, "config.json"))
	if err != nil {
		return localConfig{}, err
	}
	var cfg localConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return localConfig{}, err
	}
	return cfg, nil
}

// recordScaffoldHash updates the stored content hash for
// .claude/rules/vexillum.md in configDir/config.json, preserving the rest
// of the config.
func recordScaffoldHash(configDir string, updated bool) error {
	if !updated {
		return nil
	}
	cfg, err := readLocalConfig(configDir)
	if err != nil {
		return err
	}
	cfg.VexillumRuleHash = hashContent(productVexillumRule)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(configDir, "config.json"), data, 0o644)
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

	configDir := filepath.Join(projectDir, ".vexillum")
	alreadyInitialized := projectAlreadyInitialized(projectDir)
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
	ruleCreated, err := writeFileIfMissing(ruleDir, "vexillum.md", productVexillumRule)
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
			if err := recordScaffoldHash(configDir, ruleCreated); err != nil {
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
	if err := recordScaffoldHash(configDir, ruleCreated); err != nil {
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
	created, err := ensureDir(vexillumHome)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: cannot create %s: %v\n", vexillumHome, err)
		return 1
	}
	if created {
		fmt.Fprintf(stdout, "Created %s\n", vexillumHome)
	}

	alreadyInitialized := globalAlreadyInitialized(vexillumHome)
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
	ruleCreated, err := writeFileIfMissing(ruleDir, "vexillum.md", productVexillumRule)
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
		if err := recordScaffoldHash(vexillumHome, ruleCreated); err != nil {
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
	if err := recordScaffoldHash(vexillumHome, ruleCreated); err != nil {
		fmt.Fprintf(stderr, "vexillum: warning: could not record scaffold hash: %v\n", err)
	}

	fmt.Fprintln(stdout, "vexillum initialized globally - this applies to every Claude Code session on this machine.")
	return 0
}

func globalAlreadyInitialized(vexillumHome string) bool {
	_, err := os.Stat(filepath.Join(vexillumHome, "config.json"))
	return err == nil
}

const (
	// sentinelHookCommand is registered as an async Stop hook
	// ("asyncRewake": true, a generous "timeout"): it blocks, polling
	// for a wake, until one appears or the timeout elapses - so the
	// commander gets woken even if its turn already ended before a
	// soldier settled, not just when a wake happens to already be
	// pending at the exact moment a turn is ending. Verified live: a
	// real asyncRewake Stop hook that sleeps then exits 2 with a stderr
	// message woke an idle Claude Code session on its own, no new user
	// prompt sent - "Stop hook feedback" arrived automatically.
	//
	// This gets written into .claude/settings.json in the project
	// directory, which git tracks - so it reaches every checkout,
	// including a worktree an unrelated tool creates for its own
	// headless Claude Code turn (e.g. no-mistakes' review/test/lint
	// steps). runSentinelAwaitGuarded (see cli/sentinel.go) is what
	// keeps that turn from sitting blocked on a wake the sentinel can
	// never produce for it: it only actually waits inside a
	// herdr-managed pane (HERDR_WORKSPACE_ID set), same as dispatch and
	// redispatch already require of their own caller.
	sentinelHookCommand = "vexillum sentinel await"

	// legacySentinelHookCommand is the older, synchronous-only hook
	// (an instant check-and-return, registered without asyncRewake)
	// this replaces. ensureSentinelHook detects and upgrades it in
	// place instead of leaving a stale, redundant hook alongside the
	// new one.
	legacySentinelHookCommand = "vexillum sentinel drain"

	// sentinelHookTimeoutSeconds bounds how long a single async hook
	// invocation may block. runSentinelAwait's own internal deadline
	// (sentinelAwaitMaxWait) stays comfortably under this so it always
	// exits 0 cleanly on its own before Claude Code would have to kill
	// it - Claude Code re-fires this hook on every turn end regardless,
	// so a shorter self-imposed deadline costs nothing.
	sentinelHookTimeoutSeconds = 3600
)

// ensureSentinelHook merges the async sentinel Stop hook into the
// project's .claude/settings.json, so a soldier's status change surfaces
// to the commander instead of it quietly ending its turn - even if that
// turn already ended before anything settled. Reads and merges rather
// than overwriting - existing hooks and settings are preserved untouched.
// An older project's synchronous-only hook (legacySentinelHookCommand)
// is upgraded in place, not duplicated. A malformed existing file is
// left untouched and reported as an error rather than risk corrupting it.
func ensureSentinelHook(projectDir string) (added bool, err error) {
	path := filepath.Join(projectDir, ".claude", "settings.json")

	settings := map[string]any{}
	data, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		if jsonErr := json.Unmarshal(data, &settings); jsonErr != nil {
			return false, fmt.Errorf("%s has invalid JSON, leaving it untouched: %w", path, jsonErr)
		}
	case os.IsNotExist(readErr):
		// settings stays the empty map created above.
	default:
		return false, readErr
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	stopGroups, _ := hooks["Stop"].([]any)

	newEntry := map[string]any{
		"type":        "command",
		"command":     sentinelHookCommand,
		"asyncRewake": true,
		"timeout":     sentinelHookTimeoutSeconds,
	}

	found := false
	changed := false
	for _, g := range stopGroups {
		group, ok := g.(map[string]any)
		if !ok {
			continue
		}
		entries, _ := group["hooks"].([]any)
		for i, e := range entries {
			entry, ok := e.(map[string]any)
			if !ok {
				continue
			}
			switch cmd, _ := entry["command"].(string); cmd {
			case sentinelHookCommand:
				found = true
				asyncOK, _ := entry["asyncRewake"].(bool)
				if !asyncOK || entry["timeout"] == nil {
					entries[i] = newEntry
					group["hooks"] = entries
					changed = true
				}
			case legacySentinelHookCommand:
				entries[i] = newEntry
				group["hooks"] = entries
				found = true
				changed = true
			}
		}
	}

	if !found {
		stopGroups = append(stopGroups, map[string]any{
			"hooks": []any{newEntry},
		})
		hooks["Stop"] = stopGroups
		settings["hooks"] = hooks
		changed = true
	}

	if !changed {
		return false, nil
	}

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return false, err
	}
	out = append(out, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return false, err
	}
	return true, nil
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

// writeLocalConfig creates a fresh config.json directly inside configDir -
// the project's .vexillum/ for local scaffolds, or vexillumHome itself for
// the global scaffold (see runInitGlobal).
func writeLocalConfig(configDir string) error {
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return err
	}

	cfg := localConfig{Version: 1, InitializedAt: time.Now().UTC()}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	return os.WriteFile(filepath.Join(configDir, "config.json"), data, 0o644)
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
