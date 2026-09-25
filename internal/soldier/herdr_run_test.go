package soldier_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/pause"
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
	if len(client.promptCalls) != 1 || !strings.HasPrefix(client.promptCalls[0], task.Prompt) {
		t.Errorf("expected the task's prompt to be submitted once, got %v", client.promptCalls)
	}
	wantBaseArgs := []string{"--dangerously-skip-permissions", "--prompt-suggestions", "false"}
	if len(client.startArgs) != 1 || !slices.Equal(client.startArgs[0], wantBaseArgs) {
		t.Errorf("expected the soldier to start with %v, got %v", wantBaseArgs, client.startArgs)
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

// The camp's Base is persisted onto the task (CampBase) alongside its
// other camp identifiers - internal/sentinel needs it (with CampPath) to
// ask internal/camp.HasNewCommits whether a mission actually produced a
// commit, since the sentinel never has the project's own checkout
// directory to resolve it another way.
func TestRunInHerdr_PersistsCampBase(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID, Base: "main"}

	client := &fakeHerdr{tabID: "w1:t2", paneID: "w1:p2", promptStatus: "done"}

	got, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if got.CampBase != "main" {
		t.Errorf("expected CampBase to be persisted from the camp, got %q", got.CampBase)
	}

	persisted, err := state.Load(testProjectRoot(t, home), task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.CampBase != "main" {
		t.Errorf("expected the persisted task to carry CampBase, got %q", persisted.CampBase)
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

	want := []string{"--dangerously-skip-permissions", "--prompt-suggestions", "false", "--model", "haiku", "--effort", "low"}
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

	wantPath := report.Path(testProjectRoot(t, home), got.HerdrAgentName, got.ID)
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

// A mission's submitted prompt carries no report instructions - a mission
// never has one - but does carry the pause-declaration instructions every
// soldier gets, appended after the (untouched) original prompt.
func TestRunInHerdr_MissionPromptHasNoReportInstructions(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

	client := &fakeHerdr{tabID: "w1:t2", paneID: "w1:p2", promptStatus: "done"}

	got, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}

	if len(client.promptCalls) != 1 {
		t.Fatalf("expected exactly one prompt submission, got %d", len(client.promptCalls))
	}
	if !strings.HasPrefix(client.promptCalls[0], task.Prompt) {
		t.Errorf("expected the original prompt untouched at the start, got: %s", client.promptCalls[0])
	}
	if strings.Contains(client.promptCalls[0], report.Path(testProjectRoot(t, home), got.HerdrAgentName, got.ID)) {
		t.Errorf("expected no report path referenced in a mission's prompt, got: %s", client.promptCalls[0])
	}
	wantPausePath := pause.Path(testProjectRoot(t, home), got.HerdrAgentName)
	if !strings.Contains(client.promptCalls[0], wantPausePath) {
		t.Errorf("expected the pause path %q referenced in a mission's prompt, got: %s", wantPausePath, client.promptCalls[0])
	}
}

// Every soldier - mission or scout - gets the pause-declaration
// instructions pointing at its own pause file (internal/pause.Path), not
// just scouts (which additionally get the report instructions).
func TestRunInHerdr_PromptIncludesPauseInstructions(t *testing.T) {
	for _, tc := range []struct {
		name string
		task func(t *testing.T) state.Task
	}{
		{"mission", newMissionTask},
		{"scout", func(t *testing.T) state.Task { return newScoutTask(t, "look into it") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			task := tc.task(t)
			c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}

			client := &fakeHerdr{tabID: "w1:t2", paneID: "w1:p2", promptStatus: "done"}

			got, err := soldier.RunInHerdr(home, "w1", task, c, client)
			if err != nil {
				t.Fatalf("RunInHerdr: %v", err)
			}

			wantPath := pause.Path(testProjectRoot(t, home), got.HerdrAgentName)
			if len(client.promptCalls) != 1 {
				t.Fatalf("expected exactly one prompt submission, got %d", len(client.promptCalls))
			}
			if !strings.Contains(client.promptCalls[0], wantPath) {
				t.Errorf("expected the submitted prompt to reference the pause path %q, got: %s", wantPath, client.promptCalls[0])
			}
		})
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

	wantPath := report.Path(testProjectRoot(t, home), got.HerdrAgentName, got.ID)
	collidingPath := report.Path(testProjectRoot(t, home), "vx-do-the-thing", got.ID)
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

func writeAndCommit(t *testing.T, dir, name, content, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	runGitT(t, dir, "add", name)
	runGitT(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", message)
}

// newEmptyMissionCamp builds a standalone git "camp" (RunInHerdr only ever
// reads a git worktree by the path/base it's given, it never acquires one
// itself) whose branch carries no commit ahead of its base ("main") - the
// same "no completion signal" shape internal/sentinel's own tests use
// (newEmptyMissionCamp there), mirrored here so this package's quick-settle
// corroboration test exercises the real internal/camp.HasNewCommits path
// instead of faking it via an empty CampBase.
func newEmptyMissionCamp(t *testing.T) (campPath, base string) {
	t.Helper()
	dir := t.TempDir()
	runGitT(t, dir, "init", "-q")
	runGitT(t, dir, "symbolic-ref", "HEAD", "refs/heads/main")
	writeAndCommit(t, dir, "README.md", "hi\n", "initial commit")
	runGitT(t, dir, "checkout", "-q", "-b", "vexillum/task")
	return dir, "main"
}

func writePauseFile(t *testing.T, proj, agentName, content string) {
	t.Helper()
	if err := os.MkdirAll(pause.Dir(proj), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(pause.Path(proj, agentName), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// The motivating E2E bug, reproduced exactly: a soldier that declares a
// real, currently valid pause (internal/pause) before its turn goes idle,
// with no new commit in its camp, must not be recorded as Done straight
// out of dispatch's own quick-settle probe. Before this test existed,
// RunInHerdr wrote task.Status = MapAgentStatus(status) unconditionally -
// it never consulted internal/pause or internal/camp.HasNewCommits at
// all, so it settled this exact case Done and won the race against
// internal/sentinel's own polling loop, which would have refused to. Both
// paths now share the same corroboration (internal/settle,
// internal/pause.Active), reached here instead of on the sentinel's next
// tick.
func TestRunInHerdr_DeclaredPauseNeverSettlesDoneFromQuickSettle(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	campPath, base := newEmptyMissionCamp(t)
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: campPath, Slot: 1, Branch: "vexillum/" + task.ID, Base: base}

	proj := testProjectRoot(t, home)
	writePauseFile(t, proj, "vx-do-the-thing", "paused: waiting on my own e2e validation run\nuntil: the run finishes\n")

	client := &fakeHerdr{tabID: "w1:t2", paneID: "w1:p2", promptStatus: "idle"}

	got, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if got.Status == state.StatusDone {
		t.Fatalf("expected the declared pause (with no new commits) to block a Done settle, got status %s", got.Status)
	}
	if got.Status != state.StatusUnconfirmed {
		t.Errorf("expected status unconfirmed (the same verdict internal/sentinel's own corroboration would reach), got %s", got.Status)
	}

	persisted, err := state.Load(proj, task.ID)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if persisted.Status != state.StatusUnconfirmed {
		t.Errorf("expected persisted status unconfirmed, got %s", persisted.Status)
	}
}

// The normal completion case must stay fast and unaffected: a mission
// with a real commit ahead of its camp's base settles Done from the
// quick-settle probe exactly as before, regardless of whether a pause
// file also happens to exist (a soldier can legitimately declare a pause
// earlier in its own turn and still go on to finish for real).
func TestRunInHerdr_RealCompletionStillSettlesDoneFromQuickSettle(t *testing.T) {
	home := t.TempDir()
	task := newMissionTask(t)
	campPath, base := newEmptyMissionCamp(t)
	writeAndCommit(t, campPath, "output.txt", "soldier's work\n", "soldier's work")
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: campPath, Slot: 1, Branch: "vexillum/" + task.ID, Base: base}

	proj := testProjectRoot(t, home)
	writePauseFile(t, proj, "vx-do-the-thing", "paused: an earlier wait, already resolved\n")

	client := &fakeHerdr{tabID: "w1:t2", paneID: "w1:p2", promptStatus: "done"}

	got, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if got.Status != state.StatusDone {
		t.Errorf("expected a real completion (new commit ahead of base) to still settle done, got %s", got.Status)
	}
}
