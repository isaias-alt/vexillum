package soldier

import (
	"fmt"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/pause"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/report"
	"github.com/isaias-alt/vexillum/internal/settle"
	"github.com/isaias-alt/vexillum/internal/state"
)

const (
	// quickSettleTimeoutMS bounds a short probe, not the soldier's real
	// work budget: "did this settle fast" (a trivial prompt), not "wait
	// for it to finish". A real task almost always outlives this and
	// hands off to internal/sentinel's polling instead - see RunInHerdr.
	quickSettleTimeoutMS    = 15 * 1000 // 15 seconds
	defaultReadLines        = 500
	trustDialogSettleWindow = 15 * time.Second
	trustDialogPollInterval = 200 * time.Millisecond

	paneBusyMaxAttempts = 5
	paneBusyRetryDelay  = 500 * time.Millisecond

	// promptStalledMaxAttempts/promptStalledRetryDelay retry a prompt
	// submission that herdr reports as agent_prompt_stalled - a startup
	// race, not a real failure (see herdr.IsStalled's doc comment for
	// the live evidence: the pane's prompt line reads back empty after
	// the first attempt, and a plain retry resolves it).
	promptStalledMaxAttempts = 3
	promptStalledRetryDelay  = 500 * time.Millisecond
)

// RunInHerdr runs task's prompt through a real, interactive Claude Code
// session in a herdr pane inside c, instead of a headless one-shot
// process - the soldier is visible and attachable, not silent in the
// background.
//
// It returns quickly, not once the soldier's work is done: it submits
// the prompt and only waits out a short quick-settle probe
// (quickSettleTimeoutMS). If the soldier is still going after that
// (the normal case for real work), the task is left Running and
// RunInHerdr returns successfully - internal/sentinel's polling is what
// detects and records its eventual settlement, not this function.
//
// The soldier runs with --dangerously-skip-permissions: it has full host
// access under the invoking OS user - there is no container, chroot, or
// other OS-level sandbox. The camp's git worktree isolation only bounds
// where a soldier's commits can land, not what its process can read,
// write, or exfiltrate elsewhere on the machine (credentials, SSH keys,
// a sibling project's .env, etc.). The real safety control is the
// landing approval step (camp.Land): nothing a soldier does reaches the
// project's real history until a human explicitly approves it - not a
// permission prompt mid-task. Stopping to ask about every tool call
// would mean going to each soldier's pane individually to unblock it,
// which defeats having one commander as the single point of contact. A
// soldier can still end up StatusBlocked if Claude Code asks a genuine
// clarifying question (herdr's "blocked" also covers that, not just
// permission prompts) - just far less often now. Never dispatch a
// soldier against a prompt, repository, or machine where reading
// sensitive host state would be a problem.
//
// workspaceID is the herdr workspace to create the soldier's tab in -
// normally the caller's own $HERDR_WORKSPACE_ID, since vexillum expects
// to be dispatched from within a herdr-managed session.
func RunInHerdr(vexillumHome, workspaceID string, task state.Task, c camp.Camp, client herdr.Client) (state.Task, error) {
	projectRoot, err := project.Root(vexillumHome, c.ProjectDir)
	if err != nil {
		return task, fmt.Errorf("resolving project root: %w", err)
	}

	agentName := herdrAgentName(task)

	tabID, paneID, err := client.CreateTab(workspaceID, c.Path, agentName, chromeDevtoolsSessionEnvVar+"="+chromeDevtoolsSessionName(task.ID))
	if err != nil {
		return task, fmt.Errorf("creating herdr tab for camp: %w", err)
	}

	task.CampSlot = c.Slot
	task.CampPath = c.Path
	task.CampBranch = c.Branch
	task.CampBase = c.Base
	task.HerdrWorkspaceID = workspaceID
	task.HerdrTabID = tabID
	task.HerdrPaneID = paneID
	task.HerdrAgentName = agentName
	task.Status = state.StatusRunning
	task.UpdatedAt = time.Now().UTC()
	if err := state.Save(projectRoot, task); err != nil {
		return task, fmt.Errorf("persisting running state: %w", err)
	}

	agentName, err = startAgent(client, agentName, task.ID, paneID, defaultHarness.ModelEffortArgs(task))
	if err != nil {
		return failHerdrTask(projectRoot, task, err)
	}
	if agentName != task.HerdrAgentName {
		// The candidate name collided with another live agent (two
		// prompts producing the same slug); startAgent fell back to a
		// disambiguated one. Persist the name that's actually live.
		task.HerdrAgentName = agentName
		task.UpdatedAt = time.Now().UTC()
		if err := state.Save(projectRoot, task); err != nil {
			return task, fmt.Errorf("persisting disambiguated agent name: %w", err)
		}
	}

	// Mark the moment submission actually starts, not just when the task
	// was first marked Running above - startAgent can itself take a
	// while (the workspace trust dialog alone allows up to
	// trustDialogSettleWindow). internal/sentinel.Tick anchors its own
	// settle-race grace period to this timestamp, and an earlier one
	// would leave that guard covering the wrong window on a slow start.
	task.UpdatedAt = time.Now().UTC()
	if err := state.Save(projectRoot, task); err != nil {
		return task, fmt.Errorf("persisting pre-prompt state: %w", err)
	}

	// Submit the prompt and probe briefly for a fast settle - not a full
	// wait for completion. firstmate's own fm-spawn.sh never blocks on
	// worker completion either; a dedicated daemon supervises it
	// (AGENTS.md section 8, "Supervision protocol"). Blocking here for
	// the soldier's whole real task (previously up to 10 minutes) was
	// exactly why internal/sentinel could never win the race for it: by
	// the time the sentinel's next poll ran, dispatch had already
	// written the final status itself. A short timeout means dispatch
	// only self-reports for genuinely trivial prompts; anything real is
	// handed off to the sentinel, which already polls every Running task
	// (see internal/sentinel.Tick).
	promptText := task.Prompt
	if task.Kind == state.KindScout {
		// Only a scout gets told to write a report - a mission's
		// deliverable is the PR/merge itself, never a report.md (see
		// internal/report's package doc; confirmed against firstmate,
		// where report.md is exclusive to worker tasks, never optional
		// for a ship). The instruction is appended to what's actually
		// submitted, not stored back onto task.Prompt: task.Prompt stays
		// the general's original ask, since 'vexillum redispatch' reuses
		// it verbatim and would otherwise accumulate a new copy of this
		// suffix on every re-dispatch.
		promptText += scoutReportInstructions(report.Path(projectRoot, task.HerdrAgentName, task.ID))
	}
	// Every soldier - mission or scout - gets the pause-declaration
	// instructions, same reasoning as above about never mutating
	// task.Prompt itself: background-and-pause is legitimate for either
	// kind (internal/pause's package doc), and internal/sentinel refuses
	// to trust an idle turn as done without either this or the kind's own
	// strong completion signal.
	promptText += pauseInstructions(pause.Path(projectRoot, task.HerdrAgentName))
	status, err := promptWithStalledRetry(client, agentName, promptText, quickSettleTimeoutMS)
	if err != nil {
		if herdr.IsTimeout(err) {
			// Not a failure: the soldier is still working past the quick
			// probe window, which is the normal case for real work. The
			// task stays Running (already persisted above) - the
			// sentinel owns detecting and recording its eventual
			// settlement.
			return task, nil
		}
		if herdr.IsNotRunning(err) {
			// The pane closed while herdr was waiting on it (confirmed
			// via herdr's own CHANGELOG - see herdr.IsNotRunning's doc
			// comment), not a startup timing blip like IsStalled - there's
			// nothing left to retry against. A clear, specific message
			// here means the commander doesn't have to infer what
			// happened from a raw herdr error string; a fresh dispatch
			// starts a new camp/pane regardless, so there's nothing to
			// recover in this one.
			return failHerdrTask(projectRoot, task, fmt.Errorf("the soldier's herdr pane closed before it could be prompted (not something vexillum did) - safe to redispatch: %w", err))
		}
		return failHerdrTask(projectRoot, task, fmt.Errorf("prompting soldier: %w", err))
	}

	if output, readErr := client.AgentRead(agentName, defaultReadLines); readErr == nil {
		task.Output = output
	}

	task.Status = corroboratedStatus(projectRoot, task, status)
	if task.Status == state.StatusBlocked {
		task.Decision = ExtractDecision(task.Output)
	}
	task.UpdatedAt = time.Now().UTC()
	if err := state.Save(projectRoot, task); err != nil {
		return task, fmt.Errorf("persisting final state: %w", err)
	}
	return task, nil
}

// AnswerBlocked delivers answer to a blocked task's still-open herdr pane -
// reusing the exact same client.AgentPrompt submission path RunInHerdr uses
// for a task's original prompt - records it against task.Decision, and
// updates task's status from whatever the soldier does next. The caller
// (internal/cli.Decide) is responsible for confirming task is actually
// StatusBlocked before calling in; this only ever touches the live pane and
// persists the result.
//
// Mirrors RunInHerdr's own quick-settle probe: a fast idle/done/blocked
// settlement is reflected immediately, otherwise the task is left Running
// for the sentinel to pick up the eventual settle - exactly as if this were
// a fresh prompt submission, because functionally it is one.
//
// If the pane itself is gone (herdr's agent_not_running - the task was
// actually Interrupted, not Blocked, and vexillum just hadn't observed that
// yet), AnswerBlocked marks the task Interrupted itself rather than leaving
// it stranded as Blocked forever: internal/sentinel never polls a Blocked
// task, so nothing else would ever catch this. The caller should point the
// general at 'vexillum redispatch' instead.
func AnswerBlocked(projectRoot string, task state.Task, answer string, client herdr.Client) (state.Task, error) {
	status, err := promptWithStalledRetry(client, task.HerdrAgentName, answer, quickSettleTimeoutMS)
	if err != nil {
		if herdr.IsNotRunning(err) {
			task.Status = state.StatusInterrupted
			task.UpdatedAt = time.Now().UTC()
			if task.Output != "" {
				task.Output += "\n\n"
			}
			task.Output += "[vexillum] this soldier's herdr pane was already gone by the time its answer could be delivered - marked interrupted. Use 'vexillum redispatch' instead."
			if saveErr := state.Save(projectRoot, task); saveErr != nil {
				return task, fmt.Errorf("persisting interrupted task (after: %v): %w", err, saveErr)
			}
			return task, fmt.Errorf("the soldier's herdr pane is gone (not something vexillum did) - marked interrupted, use 'vexillum redispatch' instead: %w", err)
		}
		if !herdr.IsTimeout(err) {
			return task, fmt.Errorf("delivering answer to soldier: %w", err)
		}
		// Timeout: still working past the quick-settle probe - the normal
		// case for real work, same as a fresh dispatch's own probe. err
		// stays set so the status branch below leaves this task Running.
	}

	if output, readErr := client.AgentRead(task.HerdrAgentName, defaultReadLines); readErr == nil {
		task.Output = output
	}

	if task.Decision != nil {
		task.Decision.Answer = answer
		task.Decision.AnsweredAt = time.Now().UTC()
	}

	if err == nil {
		task.Status = MapAgentStatus(status)
		if task.Status == state.StatusBlocked {
			// Settled straight back into another question - extract it
			// the same way a fresh block does, so the general sees the
			// new question, not the one they just answered.
			if d := ExtractDecision(task.Output); d != nil {
				task.Decision = d
			}
		}
	} else {
		task.Status = state.StatusRunning
	}
	task.UpdatedAt = time.Now().UTC()
	if err := state.Save(projectRoot, task); err != nil {
		return task, fmt.Errorf("persisting answered task: %w", err)
	}
	return task, nil
}

const (
	agentNamePrefix = "vx-"
	agentNameMaxLen = 32
)

// ReleaseInHerdr releases c back to the camp pool (camp.Release - never a
// dirty or unlanded camp, never the wrong owner) and, only once that
// succeeds, stops any orphaned chrome-devtools-axi browser bridge the
// soldier left running (PRD v2, B.3) and closes the soldier's herdr tab.
// Both happen at the same moment as the worktree return, not when the
// soldier's turn merely finishes: matches firstmate's own teardown
// ("committed work must be landed before the worktree is returned...
// cleanup closes only the exact recorded task pane", docs/herdr-backend.md).
// If camp.Release refuses, neither the browser nor the pane is touched -
// there's still something worth looking at.
func ReleaseInHerdr(task state.Task, c camp.Camp, client herdr.Client, homeDir string) error {
	if err := camp.Release(c, task.ID); err != nil {
		return err
	}
	stopOrphanBrowser(task.ID, homeDir)
	if task.HerdrTabID == "" {
		return nil
	}
	if err := client.TabClose(task.HerdrTabID); err != nil {
		return fmt.Errorf("closing soldier pane: %w", err)
	}
	return nil
}

// DiscardInHerdr is ReleaseInHerdr's destructive counterpart (PRD v2,
// A.2): it throws away c (camp.Discard - a dead soldier's dirty,
// possibly unlanded worktree) instead of insisting it's clean and
// landed, then closes whatever herdr tab the task still had recorded.
// The tab close is best-effort: a task reaching this point is normally
// Interrupted precisely because its herdr agent is already gone, so a
// TabClose error here is reported but never blocks the caller from
// proceeding to re-dispatch - there's nothing left to clean up on
// herdr's side either way.
func DiscardInHerdr(task state.Task, c camp.Camp, client herdr.Client, homeDir string) error {
	if err := camp.Discard(c, task.ID); err != nil {
		return err
	}
	stopOrphanBrowser(task.ID, homeDir)
	if task.HerdrTabID == "" {
		return nil
	}
	// Best-effort: an Interrupted task's tab is typically already gone
	// (that's usually why it's Interrupted in the first place) - a
	// failure here is nothing to act on, and must never block the
	// re-dispatch that called this.
	_ = client.TabClose(task.HerdrTabID)
	return nil
}

// scoutReportInstructions tells a scout soldier where to write its final
// report - the deliverable 'vexillum release' gates on for a scout (see
// internal/report, internal/cli.runRelease). reportPath is already
// resolved from the soldier's own (possibly disambiguated) agent name, so
// the soldier never has to compute or guess it.
func scoutReportInstructions(reportPath string) string {
	return "\n\n---\n\nBefore you finish, write your final report as a single Markdown file at:\n\n  " +
		reportPath +
		"\n\nCreate any missing parent directories yourself. This report is your deliverable: " +
		"'vexillum release' will refuse to release your camp without it."
}

// pauseInstructions tells a soldier (mission or scout) how to declare
// that it's deliberately pausing its own turn to wait on something of its
// own - a background job it started, a validation run still in flight -
// instead of blocking on it with a polling loop. Without this, vexillum's
// sentinel can't tell that apart from the turn simply being done: see
// internal/pause's package doc for why that ambiguity matters and why a
// file is what resolves it, never the turn's own prose.
func pauseInstructions(pausePath string) string {
	return "\n\n---\n\nIf you need to pause your own turn on purpose to wait for something of your own " +
		"(a background job you started, a validation run still in flight) instead of polling for it, " +
		"write that BEFORE you end your turn as a file at:\n\n  " +
		pausePath +
		"\n\nFirst line: \"paused: <why>\". Optional second line: \"until: <when you expect it to resolve>\".\n" +
		"This is the only way vexillum knows you're deliberately waiting rather than finished - without it, " +
		"an idle turn with no finished deliverable is treated as unconfirmed, not successful. Only use this " +
		"for a real external wait, not an ordinary pause between steps."
}

// herdrAgentName builds a readable candidate name for the soldier's
// agent (and tab label): "vx-<slug of the task's prompt>". It's only a
// candidate: two prompts that produce the same slug can still collide if
// dispatched close together, which startAgent handles by falling back to
// a disambiguated name. Names must match herdr's
// [a-z][a-z0-9_-]{0,31} pattern.
func herdrAgentName(task state.Task) string {
	slug := slugify(task.Prompt, agentNameMaxLen-len(agentNamePrefix))
	if slug == "" {
		return agentNamePrefix + task.ID[:min(len(task.ID), agentNameMaxLen-len(agentNamePrefix))]
	}
	return agentNamePrefix + slug
}

// slugify lowercases s, collapses every run of non [a-z0-9] characters
// into a single "-", and trims to at most maxLen characters without
// cutting mid-word where avoidable.
func slugify(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}

	var b strings.Builder
	pendingDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingDash = false
			b.WriteRune(r)
		default:
			pendingDash = true
		}
		if b.Len() >= maxLen {
			break
		}
	}

	out := b.String()
	if len(out) > maxLen {
		out = out[:maxLen]
	}
	return strings.Trim(out, "-")
}

// MapAgentStatus maps herdr's agent_status vocabulary (idle, working,
// blocked, done, unknown) to a Task status. Shared with internal/sentinel,
// which needs the exact same mapping when reconciling a live status
// against a task's persisted one.
func MapAgentStatus(status string) state.Status {
	switch status {
	case "working":
		// Only ever observed by a live poll (internal/sentinel) - a
		// settled agent prompt --wait result never returns this.
		return state.StatusRunning
	case "blocked":
		return state.StatusBlocked
	case "idle", "done":
		return state.StatusDone
	default:
		return state.StatusFailed
	}
}

// corroboratedStatus maps liveStatus the same way MapAgentStatus does,
// but never trusts an apparent Done on its own - idle only ever means the
// turn stopped responding, never why (internal/pause's package doc).
// Mirrors internal/sentinel's own settleIdleTask, via the shared
// internal/settle.HasCompletionSignal: a scout's report file or a
// mission's own commit ahead of its camp's base settles Done as before;
// otherwise a currently valid declared pause (internal/pause.Active)
// means the soldier is deliberately waiting on something of its own, not
// finished - StatusUnconfirmed, the same verdict the sentinel's own
// polling loop would reach for this task on its next tick, reached here
// instead so dispatch's own quick-settle can't race ahead of it and
// record a false Done first (the bug: a soldier with a valid pause file
// and no new commits used to be written Done straight out of dispatch,
// before the sentinel ever got a chance to check). A mapped
// Blocked/Running/Failed needs no corroboration and is returned
// untouched, so the common case (a real completion, or real ongoing
// work) stays exactly as fast as before this existed.
func corroboratedStatus(projectRoot string, task state.Task, liveStatus string) state.Status {
	mapped := MapAgentStatus(liveStatus)
	if mapped != state.StatusDone {
		return mapped
	}

	if strong, _, err := settle.HasCompletionSignal(projectRoot, task); err == nil && strong {
		return state.StatusDone
	}

	if _, active, err := pause.Active(projectRoot, task.HerdrAgentName, time.Now()); err == nil && active {
		return state.StatusUnconfirmed
	}

	return state.StatusDone
}

func failHerdrTask(projectRoot string, task state.Task, cause error) (state.Task, error) {
	task.Status = state.StatusFailed
	task.Output = cause.Error()
	task.UpdatedAt = time.Now().UTC()
	if err := state.Save(projectRoot, task); err != nil {
		return task, fmt.Errorf("persisting failed state (after: %v): %w", cause, err)
	}
	return task, cause
}

// startAgent starts the soldier's interactive session. A never-before-seen
// camp directory triggers Claude Code's one-time workspace trust dialog,
// which herdr surfaces as an "agent_not_ready" start failure rather than
// treating the agent as ready for prompts. Since a camp is always a
// worktree of a project the general already trusted enough to run
// `vexillum init` on, startAgent recognizes that specific dialog and
// dismisses it - but refuses to guess at any other kind of startup block.
//
// A just-created pane can also briefly report "agent_pane_busy" before
// its shell settles at an interactive prompt (observed in real use,
// undocumented); that's retried a few times before giving up, since
// there's no dialog to act on - just a race to wait out.
//
// If candidateName collides with another live agent (two prompts close
// enough to produce the same slug, observed dispatching soldiers in
// parallel), startAgent falls back once to a name disambiguated with a
// slice of taskID and returns whichever name actually ended up live -
// the caller needs that exact name for every later call.
func startAgent(client herdr.Client, candidateName, taskID, paneID string, extraArgs []string) (string, error) {
	err := startAgentWithBusyRetry(client, candidateName, paneID, extraArgs)
	if herdr.IsNameTaken(err) {
		fallback := disambiguatedName(candidateName, taskID)
		fbErr := startAgentWithBusyRetry(client, fallback, paneID, extraArgs)
		if fbErr != nil {
			return fallback, fmt.Errorf("starting soldier agent: %w", fbErr)
		}
		return fallback, nil
	}
	if err != nil {
		return candidateName, fmt.Errorf("starting soldier agent: %w", err)
	}
	return candidateName, nil
}

func startAgentWithBusyRetry(client herdr.Client, name, paneID string, extraArgs []string) error {
	err := startAgentOnce(client, name, paneID, extraArgs)
	for attempt := 1; attempt < paneBusyMaxAttempts && herdr.IsPaneBusy(err); attempt++ {
		time.Sleep(paneBusyRetryDelay)
		err = startAgentOnce(client, name, paneID, extraArgs)
	}
	return err
}

// promptWithStalledRetry retries a prompt submission that herdr reports
// as agent_prompt_stalled - a startup race right after a freshly started
// agent (see herdr.IsStalled), not a real failure, and safe to retry: a
// stalled attempt never actually reached the agent in the first place.
func promptWithStalledRetry(client herdr.Client, name, text string, timeoutMS int) (string, error) {
	status, err := client.AgentPrompt(name, text, timeoutMS)
	for attempt := 1; attempt < promptStalledMaxAttempts && herdr.IsStalled(err); attempt++ {
		time.Sleep(promptStalledRetryDelay)
		status, err = client.AgentPrompt(name, text, timeoutMS)
	}
	return status, err
}

func shortSuffix(taskID string) string {
	const n = 6
	if len(taskID) <= n {
		return taskID
	}
	return taskID[:n]
}

// disambiguatedName builds candidateName's collision fallback
// ("<candidate>-<suffix>"), truncating candidateName as needed so the
// result never exceeds herdr's agentNameMaxLen. A live case caught this:
// herdrAgentName already fills a slug out to exactly the 32-char limit
// for any long enough prompt, so appending "-<6-char-suffix>" without
// trimming produced a 39-char name herdr rejected outright
// (invalid_agent_name) - the fallback path failed even harder than the
// collision it was meant to recover from.
func disambiguatedName(candidateName, taskID string) string {
	suffix := shortSuffix(taskID)
	fallback := candidateName + "-" + suffix
	if len(fallback) <= agentNameMaxLen {
		return fallback
	}

	keep := agentNameMaxLen - 1 - len(suffix)
	if keep < 0 {
		keep = 0
	}
	if keep > len(candidateName) {
		keep = len(candidateName)
	}
	trimmed := strings.TrimRight(candidateName[:keep], "-")
	return trimmed + "-" + suffix
}

func startAgentOnce(client herdr.Client, name, paneID string, extraArgs []string) error {
	args := append(defaultHarness.HerdrExtraArgs(), extraArgs...)
	err := client.AgentStart(name, defaultHarness.HerdrAgentKind(), paneID, args...)
	if err == nil {
		return nil
	}
	if herdr.IsPaneBusy(err) {
		return err
	}
	if !herdr.IsNotReady(err) {
		return err
	}

	output, readErr := client.AgentRead(name, 20)
	if readErr != nil || !strings.Contains(strings.ToLower(output), "trust this folder") {
		return err
	}
	if err := client.AgentSendKeys(name, "down", "enter"); err != nil {
		return fmt.Errorf("dismissing workspace trust dialog: %w", err)
	}

	deadline := time.Now().Add(trustDialogSettleWindow)
	for time.Now().Before(deadline) {
		if ready, err := client.AgentReady(name); err == nil && ready {
			return nil
		}
		time.Sleep(trustDialogPollInterval)
	}
	return fmt.Errorf("soldier agent did not become ready after dismissing the workspace trust dialog")
}
