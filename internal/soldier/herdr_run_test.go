package soldier_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

// fakeHerdr implements herdr.Client for tests, so soldier's orchestration
// (status mapping, the trust-dialog retry, task persistence) can be
// exercised without a real herdr server or a real Claude Code session.
type fakeHerdr struct {
	createTabErr error
	tabID        string
	paneID       string

	startErr      error // returned once busyForCalls attempts have passed
	busyForCalls  int   // AgentStart reports agent_pane_busy for this many calls before startErr/success
	startCalls    int
	startArgs     [][]string // agentArgs passed on each AgentStart call
	readOutput    string
	readErr       error
	sendKeysCalls [][]string
	sendKeysErr   error
	readyAfter    int // AgentReady returns true starting from this call number
	readyCalls    int

	promptStatus string
	promptErr    error
	promptCalls  []string
	promptNames  []string

	tabCloseErr   error
	tabCloseCalls []string
}

func (f *fakeHerdr) CreateTab(workspaceID, cwd, label string) (string, string, error) {
	if f.createTabErr != nil {
		return "", "", f.createTabErr
	}
	return f.tabID, f.paneID, nil
}

func (f *fakeHerdr) AgentStart(name, kind, paneID string, agentArgs ...string) error {
	f.startCalls++
	f.startArgs = append(f.startArgs, agentArgs)
	if f.startCalls <= f.busyForCalls {
		return &herdr.APIError{Code: "agent_pane_busy", Message: "pane not at an interactive prompt yet"}
	}
	if f.startCalls == f.busyForCalls+1 && f.startErr != nil {
		return f.startErr
	}
	return nil
}

func (f *fakeHerdr) AgentSendKeys(name string, keys ...string) error {
	f.sendKeysCalls = append(f.sendKeysCalls, keys)
	return f.sendKeysErr
}

func (f *fakeHerdr) AgentReady(name string) (bool, error) {
	f.readyCalls++
	return f.readyCalls >= f.readyAfter, nil
}

func (f *fakeHerdr) AgentStatus(name string) (string, error) {
	return f.promptStatus, nil
}

func (f *fakeHerdr) AgentPrompt(name, text string, timeoutMS int) (string, error) {
	f.promptCalls = append(f.promptCalls, text)
	f.promptNames = append(f.promptNames, name)
	if f.promptErr != nil {
		return "", f.promptErr
	}
	return f.promptStatus, nil
}

func (f *fakeHerdr) AgentRead(name string, lines int) (string, error) {
	return f.readOutput, f.readErr
}

func (f *fakeHerdr) TabClose(tabID string) error {
	f.tabCloseCalls = append(f.tabCloseCalls, tabID)
	return f.tabCloseErr
}

func newMissionTask(t *testing.T) state.Task {
	t.Helper()
	task, err := state.New(state.KindMission, "do the thing")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	return task
}

// L4-01: a clean run creates the tab, starts the agent, prompts it, and
// persists the task as done with the herdr identifiers and transcript
// recorded.
func TestRunInHerdr_Success(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{
		tabID:        "w1:t2",
		paneID:       "w1:p2",
		promptStatus: "done",
		readOutput:   "soldier transcript",
	}

	got, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}

	if got.Status != state.StatusDone {
		t.Errorf("expected status done, got %s", got.Status)
	}
	if got.HerdrWorkspaceID != "w1" || got.HerdrTabID != "w1:t2" || got.HerdrPaneID != "w1:p2" {
		t.Errorf("expected herdr ids recorded, got %+v", got)
	}
	if got.HerdrAgentName != "vx-do-the-thing" {
		t.Errorf("expected a readable agent name derived from the prompt (vx-do-the-thing), got %q", got.HerdrAgentName)
	}
	if got.Output != "soldier transcript" {
		t.Errorf("expected transcript captured, got %q", got.Output)
	}
	if len(client.promptCalls) != 1 || client.promptCalls[0] != task.Prompt {
		t.Errorf("expected the task's prompt to be submitted once, got %v", client.promptCalls)
	}
	if len(client.startArgs) != 1 || len(client.startArgs[0]) != 1 || client.startArgs[0][0] != "--dangerously-skip-permissions" {
		t.Errorf("expected the soldier to start with --dangerously-skip-permissions, got %v", client.startArgs)
	}

	persisted, err := state.Load(home, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Status != state.StatusDone {
		t.Errorf("expected persisted status done, got %s", persisted.Status)
	}
}

// L4-02: the task is persisted as running, with its camp and herdr
// assignment recorded, before the agent is even started - so a vexillum
// crash mid-run leaves an honest, inspectable trail.
func TestRunInHerdr_WriteAheadRunningState(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{tabID: "w1:t2", paneID: "w1:p2", promptStatus: "done"}

	// AgentStart is the first herdr call after CreateTab+Save, so by the
	// time it's invoked the running state must already be on disk.
	var sawRunning bool
	checkingClient := &checkingHerdr{fakeHerdr: client, home: home, taskID: task.ID, sawRunning: &sawRunning}

	if _, err := soldier.RunInHerdr(home, "w1", task, c, checkingClient); err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if !sawRunning {
		t.Fatal("expected the task to be persisted as running before the agent started")
	}
}

type checkingHerdr struct {
	*fakeHerdr
	home       string
	taskID     string
	sawRunning *bool
}

func (c *checkingHerdr) AgentStart(name, kind, paneID string, agentArgs ...string) error {
	got, err := state.Load(c.home, c.taskID)
	if err == nil && got.Status == state.StatusRunning && got.HerdrPaneID == paneID {
		*c.sawRunning = true
	}
	return c.fakeHerdr.AgentStart(name, kind, paneID, agentArgs...)
}

// L4-03: a soldier that ends up blocked (needs approval or input) is
// reflected as StatusBlocked, not silently treated as done or left
// running forever.
func TestRunInHerdr_Blocked(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{tabID: "w1:t2", paneID: "w1:p2", promptStatus: "blocked"}

	got, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if got.Status != state.StatusBlocked {
		t.Errorf("expected status blocked, got %s", got.Status)
	}
}

// A soldier still working past the quick-settle probe (the normal case
// for real work) is left Running, not treated as a failure - the whole
// point of the redesign that fixed the sentinel's race condition
// (dispatch used to block for up to 10 minutes and self-report the final
// status, so the sentinel's poll never got a chance to see a transition).
func TestRunInHerdr_TimeoutHandsOffToSentinel(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{
		tabID:     "w1:t2",
		paneID:    "w1:p2",
		promptErr: &herdr.APIError{Code: "timeout", Message: "agent prompt wait timed out"},
	}

	got, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: expected no error on a quick-settle timeout, got: %v", err)
	}
	if got.Status != state.StatusRunning {
		t.Errorf("expected status running, got %s", got.Status)
	}

	persisted, loadErr := state.Load(home, task.ID)
	if loadErr != nil {
		t.Fatalf("state.Load: %v", loadErr)
	}
	if persisted.Status != state.StatusRunning {
		t.Errorf("expected persisted status running, got %s", persisted.Status)
	}
}

// A real AgentPrompt failure (not the quick-settle timeout) still fails
// the task, same as before this change.
func TestRunInHerdr_NonTimeoutPromptErrorFails(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{
		tabID:     "w1:t2",
		paneID:    "w1:p2",
		promptErr: fmt.Errorf("herdr socket exploded"),
	}

	got, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err == nil {
		t.Fatal("expected an error for a non-timeout prompt failure")
	}
	if got.Status != state.StatusFailed {
		t.Errorf("expected status failed, got %s", got.Status)
	}
}

// A pane that briefly reports agent_pane_busy right after creation (a
// real, undocumented race observed in use) is retried instead of failing
// immediately.
func TestRunInHerdr_RetriesPaneBusy(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{
		tabID:        "w1:t2",
		paneID:       "w1:p2",
		busyForCalls: 2,
		promptStatus: "done",
	}

	got, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if got.Status != state.StatusDone {
		t.Errorf("expected status done after the pane stopped being busy, got %s", got.Status)
	}
	if client.startCalls != 3 {
		t.Errorf("expected 3 start attempts (2 busy + 1 success), got %d", client.startCalls)
	}
}

// A pane that stays busy past the retry budget fails clearly instead of
// retrying forever.
func TestRunInHerdr_GivesUpOnPersistentPaneBusy(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{
		tabID:        "w1:t2",
		paneID:       "w1:p2",
		busyForCalls: 100,
	}

	got, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err == nil {
		t.Fatal("expected an error when the pane never stops being busy")
	}
	if got.Status != state.StatusFailed {
		t.Errorf("expected status failed, got %s", got.Status)
	}
}

// A candidate agent name that collides with another live agent (two
// prompts producing the same slug, seen dispatching soldiers close
// together) falls back to a disambiguated name instead of failing the
// whole run, and that's the name used for the rest of the interaction
// and persisted on the task.
func TestRunInHerdr_FallsBackOnNameCollision(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{
		tabID:        "w1:t2",
		paneID:       "w1:p2",
		startErr:     &herdr.APIError{Code: "agent_name_taken", Message: "agent name vx-do-the-thing is already used"},
		promptStatus: "done",
	}

	got, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if got.Status != state.StatusDone {
		t.Errorf("expected status done, got %s", got.Status)
	}
	if got.HerdrAgentName == "vx-do-the-thing" {
		t.Fatal("expected a disambiguated agent name, not the colliding candidate")
	}
	if !strings.HasPrefix(got.HerdrAgentName, "vx-do-the-thing-") {
		t.Errorf("expected the fallback name to still be based on the candidate, got %q", got.HerdrAgentName)
	}
	if client.startCalls != 2 {
		t.Errorf("expected exactly 2 start attempts (collision + fallback), got %d", client.startCalls)
	}
	// AgentPrompt must be addressed to the name that actually ended up
	// live, not the original candidate.
	if len(client.promptNames) != 1 || client.promptNames[0] != got.HerdrAgentName {
		t.Errorf("expected AgentPrompt to be called with the disambiguated name %q, got %v", got.HerdrAgentName, client.promptNames)
	}

	persisted, err := state.Load(home, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.HerdrAgentName != got.HerdrAgentName {
		t.Errorf("expected the disambiguated name to be persisted, got %q vs returned %q", persisted.HerdrAgentName, got.HerdrAgentName)
	}
}

// L4-04: the one-time workspace trust dialog on a never-before-seen camp
// is recognized and dismissed automatically, then the run proceeds
// normally.
func TestRunInHerdr_DismissesTrustDialog(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{
		tabID:        "w1:t2",
		paneID:       "w1:p2",
		startErr:     &herdr.APIError{Code: "agent_not_ready", Message: "blocked during startup"},
		readOutput:   "Is this a project you created or one you trust?\n❯ No, exit\n  Yes, I trust this folder",
		readyAfter:   1,
		promptStatus: "done",
	}

	got, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if got.Status != state.StatusDone {
		t.Errorf("expected status done after dismissing the trust dialog, got %s", got.Status)
	}
	if len(client.sendKeysCalls) != 1 {
		t.Fatalf("expected exactly one send-keys call, got %d", len(client.sendKeysCalls))
	}
	if strings.Join(client.sendKeysCalls[0], ",") != "down,enter" {
		t.Errorf("expected down,enter to dismiss the dialog, got %v", client.sendKeysCalls[0])
	}
}

// An unrecognized startup block (not the trust dialog) is not guessed
// at - it fails clearly instead of sending blind keystrokes.
func TestRunInHerdr_UnrecognizedStartupBlockFails(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{
		tabID:      "w1:t2",
		paneID:     "w1:p2",
		startErr:   &herdr.APIError{Code: "agent_not_ready", Message: "blocked during startup"},
		readOutput: "Pick a theme:\n❯ Dark\n  Light",
	}

	got, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err == nil {
		t.Fatal("expected an error for an unrecognized startup block")
	}
	if got.Status != state.StatusFailed {
		t.Errorf("expected status failed, got %s", got.Status)
	}
	if len(client.sendKeysCalls) != 0 {
		t.Errorf("expected no keys sent for an unrecognized dialog, got %v", client.sendKeysCalls)
	}
}

// L4-05: a herdr-level failure (couldn't create the tab) is a real
// vexillum-side error, and the task never gets a false running/done
// state.
func TestRunInHerdr_CreateTabFails(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{createTabErr: &herdr.APIError{Code: "not_found", Message: "workspace not found"}}

	_, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err == nil {
		t.Fatal("expected an error when the herdr tab can't be created")
	}

	if _, loadErr := state.Load(home, task.ID); loadErr == nil {
		t.Error("expected no task state to be persisted when the tab was never created")
	}
}
