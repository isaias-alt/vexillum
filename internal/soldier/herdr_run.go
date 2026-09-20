package soldier

import (
	"fmt"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/state"
)

const (
	herdrAgentKind = "claude"
	herdrAgentArg  = "--dangerously-skip-permissions"

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
// session in a herdr pane inside c, instead of the headless one-shot Run
// uses - the soldier is visible and attachable, not silent in the
// background.
//
// It returns quickly, not once the soldier's work is done: it submits
// the prompt and only waits out a short quick-settle probe
// (quickSettleTimeoutMS). If the soldier is still going after that
// (the normal case for real work), the task is left Running and
// RunInHerdr returns successfully - internal/sentinel's polling is what
// detects and records its eventual settlement, not this function.
//
// The soldier runs with --dangerously-skip-permissions: the camp's git
// worktree isolation bounds the blast radius to that disposable
// worktree, and nothing it does reaches the project's real history until
// a human explicitly approves landing it (camp.Land) - that approval
// gate is the actual safety control, not a permission prompt mid-task.
// Stopping to ask about every tool call would mean going to each
// soldier's pane individually to unblock it, which defeats having one
// commander as the single point of contact. A soldier can still end up
// StatusBlocked if Claude Code asks a genuine clarifying question
// (herdr's "blocked" also covers that, not just permission prompts) -
// just far less often now.
//
// workspaceID is the herdr workspace to create the soldier's tab in -
// normally the caller's own $HERDR_WORKSPACE_ID, since vexillum expects
// to be dispatched from within a herdr-managed session.
func RunInHerdr(vexillumHome, workspaceID string, task state.Task, c camp.Camp, client herdr.Client) (state.Task, error) {
	agentName := herdrAgentName(task)

	tabID, paneID, err := client.CreateTab(workspaceID, c.Path, agentName)
	if err != nil {
		return task, fmt.Errorf("creating herdr tab for camp: %w", err)
	}

	task.CampSlot = c.Slot
	task.CampPath = c.Path
	task.CampBranch = c.Branch
	task.HerdrWorkspaceID = workspaceID
	task.HerdrTabID = tabID
	task.HerdrPaneID = paneID
	task.HerdrAgentName = agentName
	task.Status = state.StatusRunning
	task.UpdatedAt = time.Now().UTC()
	if err := state.Save(vexillumHome, task); err != nil {
		return task, fmt.Errorf("persisting running state: %w", err)
	}

	agentName, err = startAgent(client, agentName, task.ID, paneID)
	if err != nil {
		return failHerdrTask(vexillumHome, task, err)
	}
	if agentName != task.HerdrAgentName {
		// The candidate name collided with another live agent (two
		// prompts producing the same slug); startAgent fell back to a
		// disambiguated one. Persist the name that's actually live.
		task.HerdrAgentName = agentName
		task.UpdatedAt = time.Now().UTC()
		if err := state.Save(vexillumHome, task); err != nil {
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
	if err := state.Save(vexillumHome, task); err != nil {
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
	status, err := promptWithStalledRetry(client, agentName, task.Prompt, quickSettleTimeoutMS)
	if err != nil {
		if herdr.IsTimeout(err) {
			// Not a failure: the soldier is still working past the quick
			// probe window, which is the normal case for real work. The
			// task stays Running (already persisted above) - the
			// sentinel owns detecting and recording its eventual
			// settlement.
			return task, nil
		}
		return failHerdrTask(vexillumHome, task, fmt.Errorf("prompting soldier: %w", err))
	}

	if output, readErr := client.AgentRead(agentName, defaultReadLines); readErr == nil {
		task.Output = output
	}

	task.Status = MapAgentStatus(status)
	task.UpdatedAt = time.Now().UTC()
	if err := state.Save(vexillumHome, task); err != nil {
		return task, fmt.Errorf("persisting final state: %w", err)
	}
	return task, nil
}

const (
	agentNamePrefix = "vx-"
	agentNameMaxLen = 32
)

// ReleaseInHerdr releases c back to the camp pool (camp.Release - never a
// dirty or unlanded camp, never the wrong owner) and, only once that
// succeeds, closes the soldier's herdr tab. Closing happens at the same
// moment as the worktree return, not when the soldier's turn merely
// finishes: matches firstmate's own teardown ("committed work must be
// landed before the worktree is returned... cleanup closes only the
// exact recorded task pane", docs/herdr-backend.md). If camp.Release
// refuses, the pane stays open too - there's still something worth
// looking at.
func ReleaseInHerdr(task state.Task, c camp.Camp, client herdr.Client) error {
	if err := camp.Release(c, task.ID); err != nil {
		return err
	}
	if task.HerdrTabID == "" {
		return nil
	}
	if err := client.TabClose(task.HerdrTabID); err != nil {
		return fmt.Errorf("closing soldier pane: %w", err)
	}
	return nil
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

func failHerdrTask(vexillumHome string, task state.Task, cause error) (state.Task, error) {
	task.Status = state.StatusFailed
	task.Output = cause.Error()
	task.UpdatedAt = time.Now().UTC()
	if err := state.Save(vexillumHome, task); err != nil {
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
func startAgent(client herdr.Client, candidateName, taskID, paneID string) (string, error) {
	err := startAgentWithBusyRetry(client, candidateName, paneID)
	if herdr.IsNameTaken(err) {
		fallback := disambiguatedName(candidateName, taskID)
		fbErr := startAgentWithBusyRetry(client, fallback, paneID)
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

func startAgentWithBusyRetry(client herdr.Client, name, paneID string) error {
	err := startAgentOnce(client, name, paneID)
	for attempt := 1; attempt < paneBusyMaxAttempts && herdr.IsPaneBusy(err); attempt++ {
		time.Sleep(paneBusyRetryDelay)
		err = startAgentOnce(client, name, paneID)
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

func startAgentOnce(client herdr.Client, name, paneID string) error {
	err := client.AgentStart(name, herdrAgentKind, paneID, herdrAgentArg)
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
