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

	defaultPromptTimeoutMS  = 10 * 60 * 1000 // 10 minutes
	defaultReadLines        = 500
	trustDialogSettleWindow = 15 * time.Second
	trustDialogPollInterval = 200 * time.Millisecond

	paneBusyMaxAttempts = 5
	paneBusyRetryDelay  = 500 * time.Millisecond
)

// RunInHerdr runs task's prompt through a real, interactive Claude Code
// session in a herdr pane inside c, instead of the headless one-shot Run
// uses - the soldier is visible and attachable, not silent in the
// background.
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

	status, err := client.AgentPrompt(agentName, task.Prompt, defaultPromptTimeoutMS)
	if err != nil {
		return failHerdrTask(vexillumHome, task, fmt.Errorf("prompting soldier: %w", err))
	}

	if output, readErr := client.AgentRead(agentName, defaultReadLines); readErr == nil {
		task.Output = output
	}

	task.Status = mapAgentStatus(status)
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

func mapAgentStatus(status string) state.Status {
	switch status {
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
		fallback := candidateName + "-" + shortSuffix(taskID)
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

func shortSuffix(taskID string) string {
	const n = 6
	if len(taskID) <= n {
		return taskID
	}
	return taskID[:n]
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
