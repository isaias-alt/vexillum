package soldier_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

// ocCamp acquires a real camp (a git worktree) of the project for task and
// saves the task, so RunInHerdr works on files that exist.
func ocCamp(t *testing.T, projectDir, home string, task state.Task) camp.Camp {
	t.Helper()
	c, err := camp.Acquire(projectDir, home, task.ID)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	root, err := project.Root(home, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Save(root, task); err != nil {
		t.Fatal(err)
	}
	return c
}

func ocClient() *fakeHerdr {
	return &fakeHerdr{tabID: "w1:t2", paneID: "w1:p2", promptStatus: "done", readOutput: "ok"}
}

func commitFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitT(t, dir, "add", "-f", name)
	runGitT(t, dir, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "add "+name)
}

func excludeFile(t *testing.T, repo string) string {
	t.Helper()
	path := strings.TrimSpace(runGitT(t, repo, "rev-parse", "--git-path", "info/exclude"))
	if !filepath.IsAbs(path) {
		path = filepath.Join(repo, path)
	}
	b, _ := os.ReadFile(path)
	return string(b)
}

// campIsUntouched fails the test when the camp, the project's own checkout or
// the repository's info/exclude shows anything vexillum would have written for
// the soldier: the V2 launch writes nothing anywhere.
func campIsUntouched(t *testing.T, projectDir string, c camp.Camp) {
	t.Helper()
	if out := runGitT(t, c.Path, "status", "--porcelain", "--ignored"); out != "" {
		t.Errorf("the camp has changes or ignored files:\n%s", out)
	}
	if out := runGitT(t, projectDir, "status", "--porcelain", "--ignored"); out != "" {
		t.Errorf("the project's own checkout has changes or ignored files:\n%s", out)
	}
	if got := excludeFile(t, projectDir); strings.Contains(got, "vexillum") || strings.Contains(got, "opencode") {
		t.Errorf("info/exclude was written to:\n%s", got)
	}
	gitDir := strings.TrimSpace(runGitT(t, c.Path, "rev-parse", "--absolute-git-dir"))
	if _, err := os.Stat(filepath.Join(gitDir, "vexillum-provisioned.json")); err == nil {
		t.Error("a provisioning sidecar was written")
	}
}

func TestRunInHerdr_OpenCodeV2StartsOnItsOwnSessionAndWritesNothing(t *testing.T) {
	projectDir := initStrikeTestProject(t)
	home := t.TempDir()
	f := newFakeSessions()
	useSessions(t, f)
	task := ocTask(t, 2, "opencode/deepseek-v4.1-flash-free#max", "medium")
	c := ocCamp(t, projectDir, home, task)
	client := ocClient()

	saved, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}

	if !slices.Equal(client.tabEnv, []string{"OPENCODE_DISABLE_AUTOUPDATE=1"}) {
		t.Errorf("tab env = %v", client.tabEnv)
	}
	if len(client.startArgs) != 1 || !slices.Equal(client.startArgs[0], []string{"--auto", "--session", "ses_fake1"}) {
		t.Errorf("start args = %v, want [--auto --session ses_fake1]", client.startArgs)
	}
	if len(f.created) != 1 || f.created[0].Directory != c.Path || f.created[0].Model.Variant != "max" {
		t.Errorf("sessions created = %+v", f.created)
	}
	campIsUntouched(t, projectDir, c)
	for _, name := range []string{"opencode.json", "opencode.jsonc", ".opencode"} {
		if _, err := os.Stat(filepath.Join(c.Path, name)); err == nil {
			t.Errorf("%s exists in the camp", name)
		}
	}

	// The session is on the task, in the value returned and on disk.
	if saved.HarnessSession != "ses_fake1" {
		t.Errorf("returned task session = %q", saved.HarnessSession)
	}
	onDisk, err := state.Load(testProjectRootFor(t, projectDir, home), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if onDisk.HarnessSession != "ses_fake1" {
		t.Errorf("task on disk has session %q", onDisk.HarnessSession)
	}
}

func testProjectRootFor(t *testing.T, projectDir, home string) string {
	t.Helper()
	root, err := project.Root(home, projectDir)
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// A project that tracks its own OpenCode configuration is no longer a
// problem: nothing of the project's is written over, merged or refused.
func TestRunInHerdr_OpenCodeV2LivesBesideTheProjectsOwnConfig(t *testing.T) {
	for _, name := range []string{"opencode.json", "opencode.jsonc", ".opencode/agents/vx-soldier.md", ".opencode/skills/x/SKILL.md"} {
		t.Run(name, func(t *testing.T) {
			projectDir := initStrikeTestProject(t)
			if dir := filepath.Dir(name); dir != "." {
				if err := os.MkdirAll(filepath.Join(projectDir, dir), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			commitFile(t, projectDir, name, "PROJECT-OWNED\n")
			home := t.TempDir()
			f := newFakeSessions()
			useSessions(t, f)
			task := ocTask(t, 2, "outprov/m1#high", "")
			c := ocCamp(t, projectDir, home, task)
			client := ocClient()

			if _, err := soldier.RunInHerdr(home, "w1", task, c, client); err != nil {
				t.Fatalf("RunInHerdr: %v", err)
			}
			if client.createTabs != 1 || len(f.created) != 1 {
				t.Errorf("tabs %d, sessions %d: the soldier should have started", client.createTabs, len(f.created))
			}
			got, _ := os.ReadFile(filepath.Join(c.Path, filepath.FromSlash(name)))
			if string(got) != "PROJECT-OWNED\n" {
				t.Errorf("the project's file was changed: %q", got)
			}
			if out := runGitT(t, c.Path, "status", "--porcelain", "--ignored"); out != "" {
				t.Errorf("the camp has changes:\n%s", out)
			}
		})
	}
}

// A re-dispatch relaunches on the session the task recorded, in the same camp,
// instead of creating another.
func TestRunInHerdr_OpenCodeV2ARedispatchReusesTheSession(t *testing.T) {
	projectDir := initStrikeTestProject(t)
	home := t.TempDir()
	f := newFakeSessions()
	useSessions(t, f)
	task := ocTask(t, 2, "outprov/m1", "")
	c := ocCamp(t, projectDir, home, task)
	if _, err := soldier.RunInHerdr(home, "w1", task, c, ocClient()); err != nil {
		t.Fatal(err)
	}
	// What a re-dispatch does: the task is loaded from disk with its session,
	// the old camp is discarded and the slot is acquired again.
	root := testProjectRootFor(t, projectDir, home)
	again, err := state.Load(root, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := soldier.DiscardInHerdr(again, c, ocClient()); err != nil {
		t.Fatal(err)
	}
	again, err = state.Load(root, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := camp.Acquire(projectDir, home, again.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c2.Path != c.Path {
		t.Fatalf("expected the same camp, got %s then %s", c.Path, c2.Path)
	}
	client := ocClient()
	if _, err := soldier.RunInHerdr(home, "w1", again, c2, client); err != nil {
		t.Fatalf("second RunInHerdr: %v", err)
	}
	if len(f.created) != 1 {
		t.Errorf("%d sessions were created, want the first one reused", len(f.created))
	}
	if len(client.startArgs) != 1 || !slices.Equal(client.startArgs[0], []string{"--auto", "--session", "ses_fake1"}) {
		t.Errorf("start args = %v", client.startArgs)
	}
}

func TestRunInHerdr_OpenCodeV2AnUnknownModelFailsBeforeAnyPaneWithWhatExists(t *testing.T) {
	projectDir := initStrikeTestProject(t)
	home := t.TempDir()
	f := newFakeSessions()
	useSessions(t, f)
	task := ocTask(t, 2, "outprov/nope", "")
	c := ocCamp(t, projectDir, home, task)
	client := ocClient()

	_, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err == nil {
		t.Fatal("expected the dispatch to fail")
	}
	for _, want := range []string{"outprov/nope", `no model "nope"`, "m1, m2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	if client.createTabs != 0 || len(f.created) != 0 {
		t.Errorf("tabs %d, sessions %d: nothing may be created", client.createTabs, len(f.created))
	}
	campIsUntouched(t, projectDir, c)
}

// The session is recorded as soon as it exists: a failure after it (here the
// pane cannot be created) still leaves it on the task for the next attempt.
func TestRunInHerdr_OpenCodeV2TheSessionSurvivesALaterFailure(t *testing.T) {
	projectDir := initStrikeTestProject(t)
	home := t.TempDir()
	f := newFakeSessions()
	useSessions(t, f)
	task := ocTask(t, 2, "outprov/m1", "")
	c := ocCamp(t, projectDir, home, task)
	client := ocClient()
	client.createTabErr = errors.New("herdr is down")

	returned, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err == nil {
		t.Fatal("expected an error")
	}
	if returned.HarnessSession != "ses_fake1" {
		t.Errorf("the returned task (which the CLI saves as failed) lost the session: %q", returned.HarnessSession)
	}
	onDisk, _ := state.Load(testProjectRootFor(t, projectDir, home), task.ID)
	if onDisk.HarnessSession != "ses_fake1" {
		t.Errorf("task on disk has session %q", onDisk.HarnessSession)
	}
}

// Without a model a V2 soldier starts plain, on the project's defaults, with
// no session and no use of the service.
func TestRunInHerdr_OpenCodeV2WithoutAModelStartsPlain(t *testing.T) {
	projectDir := initStrikeTestProject(t)
	home := t.TempDir()
	f := newFakeSessions()
	useSessions(t, f)
	task := ocTask(t, 2, "", "")
	c := ocCamp(t, projectDir, home, task)
	client := ocClient()
	if _, err := soldier.RunInHerdr(home, "w1", task, c, client); err != nil {
		t.Fatal(err)
	}
	if len(client.startArgs) != 1 || !slices.Equal(client.startArgs[0], []string{"--auto"}) {
		t.Errorf("start args = %v", client.startArgs)
	}
	if len(f.calls()) != 0 {
		t.Errorf("the service was used: %v", f.calls())
	}
}

// writeLegacyProvisioning reproduces what an older vexillum left in a camp for
// a V2 soldier: the two files, the sidecar that records their hashes, and the
// lines in info/exclude.
func writeLegacyProvisioning(t *testing.T, c camp.Camp) (files map[string]string) {
	t.Helper()
	files = map[string]string{
		".opencode/agents/vx-soldier.md": "---\ndescription: vexillum soldier\nmode: primary\nmodel: \"opencode/glm-5.3-flash-free\"\n---\n",
		"opencode.json":                  "{\n  \"default_agent\": \"vx-soldier\"\n}\n",
	}
	record := map[string]map[string]string{"files": {}}
	for path, content := range files {
		full := filepath.Join(c.Path, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(content))
		record["files"][path] = hex.EncodeToString(sum[:])
	}
	raw, _ := json.Marshal(record)
	gitDir := strings.TrimSpace(runGitT(t, c.Path, "rev-parse", "--absolute-git-dir"))
	if err := os.WriteFile(filepath.Join(gitDir, "vexillum-provisioned.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return files
}

// A camp provisioned by an older vexillum is cleaned before the next launch,
// whatever harness runs there, as long as the files are what vexillum wrote.
func TestRunInHerdr_LeftoversOfTheOldRecipeAreRetractedForAnyHarness(t *testing.T) {
	for _, tc := range []struct {
		name string
		task func(t *testing.T) state.Task
	}{
		{"opencode V2", func(t *testing.T) state.Task { return ocTask(t, 2, "outprov/m1", "") }},
		{"opencode V1", func(t *testing.T) state.Task { return ocTask(t, 1, "opencode/glm-5.3-flash-free", "") }},
		{"claude", func(t *testing.T) state.Task { return newTask(t) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := initStrikeTestProject(t)
			home := t.TempDir()
			useSessions(t, newFakeSessions())
			task := tc.task(t)
			c := ocCamp(t, projectDir, home, task)
			writeLegacyProvisioning(t, c)

			if _, err := soldier.RunInHerdr(home, "w1", task, c, ocClient()); err != nil {
				t.Fatalf("RunInHerdr: %v", err)
			}
			for _, left := range []string{"opencode.json", ".opencode"} {
				if _, err := os.Stat(filepath.Join(c.Path, left)); err == nil {
					t.Errorf("%s from the old recipe is still in the camp", left)
				}
			}
			gitDir := strings.TrimSpace(runGitT(t, c.Path, "rev-parse", "--absolute-git-dir"))
			if _, err := os.Stat(filepath.Join(gitDir, "vexillum-provisioned.json")); err == nil {
				t.Error("the sidecar was left behind")
			}
		})
	}
}

// A file someone changed since is not vexillum's to remove, and is no reason
// to refuse the launch any more: it stays, and counts as the project's.
func TestRunInHerdr_AChangedLeftoverIsKeptAndNoLongerRefusesTheLaunch(t *testing.T) {
	projectDir := initStrikeTestProject(t)
	home := t.TempDir()
	f := newFakeSessions()
	useSessions(t, f)
	task := ocTask(t, 2, "outprov/m1", "")
	c := ocCamp(t, projectDir, home, task)
	writeLegacyProvisioning(t, c)
	if err := os.WriteFile(filepath.Join(c.Path, "opencode.json"), []byte(`{"theme":"mine"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := soldier.RunInHerdr(home, "w1", task, c, ocClient()); err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(c.Path, "opencode.json")); string(got) != `{"theme":"mine"}` {
		t.Errorf("the changed file was touched: %q", got)
	}
	if _, err := os.Stat(filepath.Join(c.Path, ".opencode")); err == nil {
		t.Error("the unchanged agent file was not retracted")
	}
	if len(f.created) != 1 {
		t.Errorf("sessions created = %d", len(f.created))
	}
}

func TestRunInHerdr_OpenCodeV1StartsWithModelFlagAndConfigEnvAndNoFiles(t *testing.T) {
	projectDir := initStrikeTestProject(t)
	home := t.TempDir()
	task := ocTask(t, 1, "anthropic/claude-sonnet-5#high", "medium")
	c := ocCamp(t, projectDir, home, task)
	client := ocClient()

	saved, err := soldier.RunInHerdr(home, "w1", task, c, client)
	if err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if len(client.startArgs) != 1 || !slices.Equal(client.startArgs[0], []string{"--auto", "--model", "anthropic/claude-sonnet-5"}) {
		t.Errorf("start args = %v", client.startArgs)
	}
	want := []string{
		"OPENCODE_CONFIG_CONTENT={\"agent\":{\"build\":{\"model\":\"anthropic/claude-sonnet-5\",\"variant\":\"high\"}}}",
		"OPENCODE_DISABLE_AUTOUPDATE=1",
	}
	if !slices.Equal(client.tabEnv, want) {
		t.Errorf("tab env = %q, want %q", client.tabEnv, want)
	}
	for _, name := range []string{"opencode.json", ".opencode"} {
		if _, err := os.Stat(filepath.Join(c.Path, name)); err == nil {
			t.Errorf("V1 wrote %s into the camp", name)
		}
	}
	if saved.HarnessSession != "" {
		t.Errorf("V1 has no session, got %q", saved.HarnessSession)
	}
}

func TestRunInHerdr_OpenCodeWithoutARecordedMajorFailsBeforeAnyPane(t *testing.T) {
	home := t.TempDir()
	task := ocTask(t, 0, "opencode/glm-5.3-flash-free", "")
	c := camp.Camp{ProjectDir: testCampProjectDir, Path: "/camps/1/project", Slot: 1, Branch: "vexillum/" + task.ID}
	client := ocClient()

	_, err := runInHerdr(t, home, "w1", task, c, client)
	if err == nil || !strings.Contains(err.Error(), "major version") {
		t.Fatalf("error = %v", err)
	}
	if client.createTabs != 0 {
		t.Error("a pane was created")
	}
}

// Soldiers dispatched together each get a session of their own and share no
// file, so nothing is serialized between them.
func TestRunInHerdr_OpenCodeConcurrentSoldiersEachGetTheirOwnSession(t *testing.T) {
	projectDir := initStrikeTestProject(t)
	home := t.TempDir()
	f := newFakeSessions()
	useSessions(t, f)
	const n = 6
	tasks := make([]state.Task, n)
	camps := make([]camp.Camp, n)
	for i := range tasks {
		tasks[i] = ocTask(t, 2, "outprov/m1#low", "")
		camps[i] = ocCamp(t, projectDir, home, tasks[i])
	}
	var wg sync.WaitGroup
	errs := make([]error, n)
	results := make([]state.Task, n)
	for i := range tasks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = soldier.RunInHerdr(home, "w1", tasks[i], camps[i], ocClient())
		}()
	}
	wg.Wait()
	seen := map[string]bool{}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("soldier %d: %v", i, err)
		}
		id := results[i].HarnessSession
		if id == "" || seen[id] {
			t.Errorf("soldier %d got session %q (seen: %v)", i, id, seen)
		}
		seen[id] = true
	}
	for i := range camps {
		if out := runGitT(t, camps[i].Path, "status", "--porcelain", "--ignored"); out != "" {
			t.Errorf("camp %d:\n%s", i, out)
		}
	}
	if got := excludeFile(t, projectDir); strings.Contains(got, "opencode") {
		t.Errorf("info/exclude was written to:\n%s", got)
	}
}
