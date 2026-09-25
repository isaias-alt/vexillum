package soldier_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/report"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

// testCampProjectDir is the fake project directory every test camp.Camp
// literal below carries as ProjectDir - RunInHerdr resolves a task's
// project root from it (internal/project.Root), so tests that load the
// task back afterward need the exact same resolution, not the bare
// vexillumHome the old flat layout used.
const testCampProjectDir = "/fake/project"

func testProjectRoot(t *testing.T, vexillumHome string) string {
	t.Helper()
	root, err := project.Root(vexillumHome, testCampProjectDir)
	if err != nil {
		t.Fatalf("project.Root: %v", err)
	}
	return root
}

// fakeHerdr implements herdr.Client for tests, so soldier's orchestration
// (status mapping, the trust-dialog retry, task persistence) can be
// exercised without a real herdr server or a real Claude Code session.
type fakeHerdr struct {
	createTabErr error
	tabID        string
	paneID       string
	createTabEnv [][]string // env passed on each CreateTab call

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

	promptStatus          string
	promptErr             error
	promptCalls           []string
	promptNames           []string
	promptStalledForCalls int // AgentPrompt reports agent_prompt_stalled for this many calls before promptErr/promptStatus

	tabCloseErr   error
	tabCloseCalls []string
}

func (f *fakeHerdr) CreateTab(workspaceID, cwd, label string, env ...string) (string, string, error) {
	f.createTabEnv = append(f.createTabEnv, env)
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
	if len(f.promptCalls) <= f.promptStalledForCalls {
		return "", &herdr.APIError{Code: "agent_prompt_stalled", Message: "no observed working or blocked state"}
	}
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

func newScoutTask(t *testing.T, prompt string) state.Task {
	t.Helper()
	task, err := state.New(state.KindScout, prompt)
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
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

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
	wantEnv := "CHROME_DEVTOOLS_AXI_SESSION=vx-" + task.ID
	if len(client.createTabEnv) != 1 || len(client.createTabEnv[0]) != 1 || client.createTabEnv[0][0] != wantEnv {
		t.Errorf("expected the pane to carry %q (PRD v2, B.3), got %v", wantEnv, client.createTabEnv)
	}

	persisted, err := state.Load(testProjectRoot(t, home), task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Status != state.StatusDone {
		t.Errorf("expected persisted status done, got %s", persisted.Status)
	}
}

// A task carrying Model/Effort (set by the commander per ADR-05's rules,
// before dispatch ever reaches this package) gets them appended to the
// real claude launch, unexamined - RunInHerdr never decides between them.
func TestRunInHerdr_PassesModelAndEffort(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	task.Model = "haiku"
	task.Effort = "low"
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{tabID: "w1:t2", paneID: "w1:p2", promptStatus: "done"}

	if _, err := soldier.RunInHerdr(home, "w1", task, c, client); err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}

	want := []string{"--dangerously-skip-permissions", "--model", "haiku", "--effort", "low"}
	if len(client.startArgs) != 1 || !slices.Equal(client.startArgs[0], want) {
		t.Errorf("expected the soldier to start with %v, got %v", want, client.startArgs)
	}
}

// A scout's submitted prompt is the original prompt plus instructions
// pointing at its own report file (internal/report.Path) - a mission's
// is not, since a mission never has a report to write (internal/report's
// package doc).
func TestRunInHerdr_ScoutPromptIncludesReportInstructions(t *testing.T) {
	home := t.TempDir()
	task := newScoutTask(t, "look into it")
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{tabID: "w1:t2", paneID: "w1:p2", promptStatus: "done"}

	got, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}

	wantPath := report.Path(testProjectRoot(t, home), got.HerdrAgentName)
	if len(client.promptCalls) != 1 {
		t.Fatalf("expected exactly one prompt submission, got %d", len(client.promptCalls))
	}
	if !strings.Contains(client.promptCalls[0], wantPath) {
		t.Errorf("expected the submitted prompt to reference the report path %q, got: %s", wantPath, client.promptCalls[0])
	}
	if !strings.HasPrefix(client.promptCalls[0], task.Prompt) {
		t.Errorf("expected the report instructions to be appended after the original prompt, got: %s", client.promptCalls[0])
	}

	// task.Prompt itself is never mutated - 'vexillum redispatch' reuses
	// it verbatim, and must not accumulate a copy of the suffix on every
	// re-dispatch.
	persisted, err := state.Load(testProjectRoot(t, home), task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Prompt != "look into it" {
		t.Errorf("expected the persisted prompt to stay untouched, got %q", persisted.Prompt)
	}
}

// A mission's submitted prompt is exactly its original prompt - no report
// instructions appended, since a mission never has one.
func TestRunInHerdr_MissionPromptHasNoReportInstructions(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{tabID: "w1:t2", paneID: "w1:p2", promptStatus: "done"}

	if _, err := soldier.RunInHerdr(home, "w1", task, c, client); err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}

	if len(client.promptCalls) != 1 || client.promptCalls[0] != task.Prompt {
		t.Errorf("expected the mission's prompt to be submitted unmodified, got %v", client.promptCalls)
	}
}

// Two scouts dispatched close enough to collide on the same candidate
// agent name (same as TestRunInHerdr_FallsBackOnNameCollision) end up with
// distinct report paths too, since the report is named after whichever
// agent name actually ended up live - the disambiguated one, not the
// colliding candidate. This is what actually keeps two concurrent scouts'
// reports from colliding in the flat reports/ directory.
func TestRunInHerdr_ScoutReportPathUsesDisambiguatedName(t *testing.T) {
	home := t.TempDir()
	task := newScoutTask(t, "do the thing")
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

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
	if got.HerdrAgentName == "vx-do-the-thing" {
		t.Fatal("expected a disambiguated agent name, not the colliding candidate")
	}

	wantPath := report.Path(testProjectRoot(t, home), got.HerdrAgentName)
	collidingPath := report.Path(testProjectRoot(t, home), "vx-do-the-thing")
	if !strings.Contains(client.promptCalls[0], wantPath) {
		t.Errorf("expected the prompt to reference the disambiguated report path %q, got: %s", wantPath, client.promptCalls[0])
	}
	if strings.Contains(client.promptCalls[0], collidingPath) {
		t.Errorf("expected the prompt to NOT reference the colliding candidate's report path %q", collidingPath)
	}
}

// L4-02: the task is persisted as running, with its camp and herdr
// assignment recorded, before the agent is even started - so a vexillum
// crash mid-run leaves an honest, inspectable trail.
func TestRunInHerdr_WriteAheadRunningState(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{tabID: "w1:t2", paneID: "w1:p2", promptStatus: "done"}

	// AgentStart is the first herdr call after CreateTab+Save, so by the
	// time it's invoked the running state must already be on disk.
	var sawRunning bool
	checkingClient := &checkingHerdr{fakeHerdr: client, projectRoot: testProjectRoot(t, home), taskID: task.ID, sawRunning: &sawRunning}

	if _, err := soldier.RunInHerdr(home, "w1", task, c, checkingClient); err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if !sawRunning {
		t.Fatal("expected the task to be persisted as running before the agent started")
	}
}

type checkingHerdr struct {
	*fakeHerdr
	projectRoot string
	taskID      string
	sawRunning  *bool
}

func (c *checkingHerdr) AgentStart(name, kind, paneID string, agentArgs ...string) error {
	got, err := state.Load(c.projectRoot, c.taskID)
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
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{tabID: "w1:t2", paneID: "w1:p2", promptStatus: "blocked"}

	got, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if got.Status != state.StatusBlocked {
		t.Errorf("expected status blocked, got %s", got.Status)
	}
}

// L4-03 (durable decision): a soldier that settles blocked gets a
// structured Decision extracted from its transcript and persisted right
// alongside its status - not just a bare "blocked" with the question
// buried in Output's free-text prose.
func TestRunInHerdr_BlockedExtractsDecision(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{
		tabID: "w1:t2", paneID: "w1:p2", promptStatus: "blocked",
		readOutput: "Which database should this use?\n\n1. Postgres\n2. SQLite\n",
	}

	got, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if got.Decision == nil {
		t.Fatal("expected a decision to be extracted for a blocked task")
	}
	if got.Decision.Question != "Which database should this use?" {
		t.Errorf("Decision.Question = %q", got.Decision.Question)
	}
	if len(got.Decision.Options) != 2 {
		t.Errorf("Decision.Options = %v, want 2 options", got.Decision.Options)
	}

	persisted, err := state.Load(testProjectRoot(t, home), task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Decision == nil || persisted.Decision.Question != got.Decision.Question {
		t.Errorf("expected the decision to be persisted, got %+v", persisted.Decision)
	}
}

// A soldier that settles anywhere other than blocked (done, here) never
// gets a Decision attached - only a genuine block carries one.
func TestRunInHerdr_DoneHasNoDecision(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{tabID: "w1:t2", paneID: "w1:p2", promptStatus: "done", readOutput: "all done"}

	got, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if got.Decision != nil {
		t.Errorf("expected no decision for a done task, got %+v", got.Decision)
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
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

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

	persisted, loadErr := state.Load(testProjectRoot(t, home), task.ID)
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
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

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

// agent_not_running (herdr's own CHANGELOG: returned when the target
// pane closes while a --wait call is watching it) fails the task with a
// clear, specific message instead of the raw herdr error string - there's
// nothing left in that pane to retry against, unlike agent_prompt_stalled.
func TestRunInHerdr_NotRunningFailsWithClearMessage(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{
		tabID:     "w1:t2",
		paneID:    "w1:p2",
		promptErr: &herdr.APIError{Code: "agent_not_running", Message: "agent is no longer running in the target pane"},
	}

	got, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err == nil {
		t.Fatal("expected an error when the pane closed before prompting")
	}
	if got.Status != state.StatusFailed {
		t.Errorf("expected status failed, got %s", got.Status)
	}
	if !strings.Contains(err.Error(), "redispatch") {
		t.Errorf("expected a clear, actionable message, got: %v", err)
	}
}

// A pane that briefly reports agent_pane_busy right after creation (a
// real, undocumented race observed in use) is retried instead of failing
// immediately.
func TestRunInHerdr_RetriesPaneBusy(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

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
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

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

// A prompt submission that herdr reports as agent_prompt_stalled (a
// startup race right after a freshly started agent - see
// herdr.IsStalled) is retried instead of failing the task outright.
// Verified live: the pane's prompt line read back completely empty
// after a stalled attempt, and a plain retry of the same call resolved
// instantly - so it's safe to resubmit, not a real failure.
func TestRunInHerdr_RetriesStalledPrompt(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{
		tabID:                 "w1:t2",
		paneID:                "w1:p2",
		promptStalledForCalls: 2,
		promptStatus:          "done",
	}

	got, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if got.Status != state.StatusDone {
		t.Errorf("expected status done after the prompt stopped being stalled, got %s", got.Status)
	}
	if len(client.promptCalls) != 3 {
		t.Errorf("expected 3 prompt attempts (2 stalled + 1 success), got %d", len(client.promptCalls))
	}
}

// A prompt that stays stalled past the retry budget fails clearly
// instead of retrying forever.
func TestRunInHerdr_GivesUpOnPersistentlyStalledPrompt(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{
		tabID:                 "w1:t2",
		paneID:                "w1:p2",
		promptStalledForCalls: 100,
	}

	got, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err == nil {
		t.Fatal("expected an error when the prompt never stops being stalled")
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
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

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

	persisted, err := state.Load(testProjectRoot(t, home), task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.HerdrAgentName != got.HerdrAgentName {
		t.Errorf("expected the disambiguated name to be persisted, got %q vs returned %q", persisted.HerdrAgentName, got.HerdrAgentName)
	}
}

// A candidate name already at herdr's 32-char max (a long enough prompt
// fills the slug out to exactly the limit) still produces a valid
// fallback name on collision instead of herdr rejecting it outright.
// Caught live: this exact prompt slugifies to a 32-char candidate, and
// the naive candidateName+"-"+suffix fallback came out to 39 chars -
// herdr refused it with invalid_agent_name, so the collision recovery
// failed harder than the collision it was meant to recover from.
func TestRunInHerdr_DisambiguatedNameStaysWithinLengthLimit(t *testing.T) {
	home := t.TempDir()
	task, err := state.New(state.KindMission, "Write a Python module implementing a simple binary search tree")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{
		tabID:        "w1:t2",
		paneID:       "w1:p2",
		startErr:     &herdr.APIError{Code: "agent_name_taken", Message: "agent name already used"},
		promptStatus: "done",
	}

	got, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if len(got.HerdrAgentName) > 32 {
		t.Errorf("expected the fallback agent name to stay within herdr's 32-char limit, got %d chars: %q", len(got.HerdrAgentName), got.HerdrAgentName)
	}
	if strings.HasSuffix(got.HerdrAgentName, "-") || strings.Contains(got.HerdrAgentName, "--") {
		t.Errorf("expected a clean fallback name, got %q", got.HerdrAgentName)
	}
}

// L4-04: the one-time workspace trust dialog on a never-before-seen camp
// is recognized and dismissed automatically, then the run proceeds
// normally.
func TestRunInHerdr_DismissesTrustDialog(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

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
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

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
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{createTabErr: &herdr.APIError{Code: "not_found", Message: "workspace not found"}}

	_, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err == nil {
		t.Fatal("expected an error when the herdr tab can't be created")
	}

	if _, loadErr := state.Load(testProjectRoot(t, home), task.ID); loadErr == nil {
		t.Error("expected no task state to be persisted when the tab was never created")
	}
}

// newBlockedTask persists a task already in StatusBlocked with a real
// decision attached - the shape internal/cli.Decide hands AnswerBlocked in
// the real flow, after loading it fresh off disk.
func newBlockedTask(t *testing.T, projectRoot, agentName string) state.Task {
	t.Helper()
	task, err := state.New(state.KindMission, "refactor the auth module")
	if err != nil {
		t.Fatalf("state.New: %v", err)
	}
	task.HerdrAgentName = agentName
	task.Status = state.StatusBlocked
	task.Output = "Which auth library should I use?\n\n1. Auth0\n2. Keycloak\n"
	task.Decision = soldier.ExtractDecision(task.Output)
	if err := state.Save(projectRoot, task); err != nil {
		t.Fatalf("state.Save: %v", err)
	}
	return task
}

// AnswerBlocked delivers the answer to the soldier's pane, records it
// against the decision, and reflects a fast settle immediately.
func TestAnswerBlocked_DeliversAndSettles(t *testing.T) {
	home := t.TempDir()
	projectRoot := testProjectRoot(t, home)
	task := newBlockedTask(t, projectRoot, "vx-refactor-the-auth-module")

	client := &fakeHerdr{promptStatus: "done", readOutput: "used Auth0, done"}

	got, err := soldier.AnswerBlocked(projectRoot, task, "Use Auth0", client)
	if err != nil {
		t.Fatalf("AnswerBlocked: %v", err)
	}
	if got.Status != state.StatusDone {
		t.Errorf("expected status done, got %s", got.Status)
	}
	if got.Decision == nil || got.Decision.Answer != "Use Auth0" {
		t.Errorf("expected the answer recorded on the decision, got %+v", got.Decision)
	}
	if got.Decision.AnsweredAt.IsZero() {
		t.Error("expected AnsweredAt to be set")
	}
	if len(client.promptCalls) != 1 || client.promptCalls[0] != "Use Auth0" || client.promptNames[0] != task.HerdrAgentName {
		t.Errorf("expected the answer delivered to the soldier's own pane, got calls=%v names=%v", client.promptCalls, client.promptNames)
	}

	persisted, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Status != state.StatusDone || persisted.Decision.Answer != "Use Auth0" {
		t.Errorf("expected the answered task persisted, got %+v", persisted)
	}
}

// A soldier still working past the quick-settle probe after its answer is
// left Running, same as a fresh dispatch - the sentinel picks up the
// eventual settle.
func TestAnswerBlocked_StillWorkingLeftRunning(t *testing.T) {
	home := t.TempDir()
	projectRoot := testProjectRoot(t, home)
	task := newBlockedTask(t, projectRoot, "vx-refactor-the-auth-module")

	client := &fakeHerdr{promptErr: &herdr.APIError{Code: "timeout", Message: "no settle observed"}}

	got, err := soldier.AnswerBlocked(projectRoot, task, "Use Auth0", client)
	if err != nil {
		t.Fatalf("AnswerBlocked: %v", err)
	}
	if got.Status != state.StatusRunning {
		t.Errorf("expected status running, got %s", got.Status)
	}
	if got.Decision == nil || got.Decision.Answer != "Use Auth0" {
		t.Errorf("expected the answer still recorded even while running, got %+v", got.Decision)
	}
}

// If the soldier immediately asks another question, AnswerBlocked replaces
// the decision with the new one rather than leaving the just-answered
// question in place.
func TestAnswerBlocked_ReblocksWithNewDecision(t *testing.T) {
	home := t.TempDir()
	projectRoot := testProjectRoot(t, home)
	task := newBlockedTask(t, projectRoot, "vx-refactor-the-auth-module")

	client := &fakeHerdr{
		promptStatus: "blocked",
		readOutput:   "Should the auth0 tenant be single or multi-region?\n\n1. Single\n2. Multi\n",
	}

	got, err := soldier.AnswerBlocked(projectRoot, task, "Use Auth0", client)
	if err != nil {
		t.Fatalf("AnswerBlocked: %v", err)
	}
	if got.Status != state.StatusBlocked {
		t.Errorf("expected status blocked, got %s", got.Status)
	}
	if got.Decision == nil || got.Decision.Question != "Should the auth0 tenant be single or multi-region?" {
		t.Errorf("expected the new question, got %+v", got.Decision)
	}
	if got.Decision.Answer != "" {
		t.Errorf("expected the fresh decision to carry no answer yet, got %+v", got.Decision)
	}
}

// If the pane is actually gone by the time the answer is delivered,
// AnswerBlocked marks the task Interrupted itself (internal/sentinel never
// polls a Blocked task, so nothing else would ever catch this) and returns
// a clear error pointing at redispatch.
func TestAnswerBlocked_PaneGoneMarksInterrupted(t *testing.T) {
	home := t.TempDir()
	projectRoot := testProjectRoot(t, home)
	task := newBlockedTask(t, projectRoot, "vx-refactor-the-auth-module")

	client := &fakeHerdr{promptErr: &herdr.APIError{Code: "agent_not_running", Message: "pane closed"}}

	got, err := soldier.AnswerBlocked(projectRoot, task, "Use Auth0", client)
	if err == nil {
		t.Fatal("expected an error when the pane is gone")
	}
	if !strings.Contains(err.Error(), "redispatch") {
		t.Errorf("expected the error to point at redispatch, got: %v", err)
	}
	if got.Status != state.StatusInterrupted {
		t.Errorf("expected status interrupted, got %s", got.Status)
	}

	persisted, err := state.Load(projectRoot, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Status != state.StatusInterrupted {
		t.Errorf("expected the interrupted status persisted, got %s", persisted.Status)
	}
}
