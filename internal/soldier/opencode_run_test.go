package soldier_test

import (
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

func TestRunInHerdr_OpenCodeV2WritesTheRecipeIntoTheCampAndKeepsItClean(t *testing.T) {
	projectDir := initStrikeTestProject(t)
	home := t.TempDir()
	task := ocTask(t, 2, "opencode/deepseek-v4.1-flash-free#max", "medium")
	c := ocCamp(t, projectDir, home, task)
	client := ocClient()

	if _, err := soldier.RunInHerdr(home, "w1", task, c, client); err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}

	if !slices.Equal(client.tabEnv, []string{"OPENCODE_DISABLE_AUTOUPDATE=1"}) {
		t.Errorf("tab env = %v", client.tabEnv)
	}
	if len(client.startArgs) != 1 || !slices.Equal(client.startArgs[0], []string{"--auto"}) {
		t.Errorf("start args = %v, want [--auto]", client.startArgs)
	}
	agent, err := os.ReadFile(filepath.Join(c.Path, ".opencode", "agents", "vx-soldier.md"))
	if err != nil || !strings.Contains(string(agent), `model: "opencode/deepseek-v4.1-flash-free#max"`) {
		t.Errorf("agent file: %v\n%s", err, agent)
	}
	if _, err := os.Stat(filepath.Join(c.Path, "opencode.json")); err != nil {
		t.Errorf("opencode.json: %v", err)
	}
	if out := runGitT(t, c.Path, "status", "--porcelain"); out != "" {
		t.Errorf("the camp is dirty after provisioning:\n%s", out)
	}
	if out := runGitT(t, projectDir, "status", "--porcelain"); out != "" {
		t.Errorf("the project's own checkout is dirty:\n%s", out)
	}
	exclude := excludeFile(t, projectDir)
	for _, want := range []string{"/.opencode/", "/opencode.json"} {
		if strings.Count(exclude, want+"\n") != 1 {
			t.Errorf("info/exclude should list %s once:\n%s", want, exclude)
		}
	}
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

// A project that tracks its own opencode.json is never overwritten: the
// launch fails before a pane exists and names the file.
func TestRunInHerdr_OpenCodeV2RefusesToOverwriteTheProjectsConfig(t *testing.T) {
	for _, name := range []string{"opencode.json", "opencode.jsonc", ".opencode/agents/vx-soldier.md"} {
		t.Run(name, func(t *testing.T) {
			projectDir := initStrikeTestProject(t)
			if dir := filepath.Dir(name); dir != "." {
				if err := os.MkdirAll(filepath.Join(projectDir, dir), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			commitFile(t, projectDir, name, "PROJECT-OWNED\n")
			home := t.TempDir()
			task := ocTask(t, 2, "opencode/glm-5.3-flash-free", "")
			c := ocCamp(t, projectDir, home, task)
			client := ocClient()

			_, err := soldier.RunInHerdr(home, "w1", task, c, client)
			if err == nil {
				t.Fatal("expected the dispatch to fail on the collision")
			}
			for _, want := range []string{name, "never overwrites or merges", "no merge policy"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
			if client.createTabs != 0 {
				t.Error("a pane was created although the launch cannot proceed")
			}
			got, _ := os.ReadFile(filepath.Join(c.Path, filepath.FromSlash(name)))
			if string(got) != "PROJECT-OWNED\n" {
				t.Errorf("the project's file was changed: %q", got)
			}
			for _, left := range []string{"opencode.json", ".opencode/agents/vx-soldier.md"} {
				if left == name {
					continue
				}
				if _, err := os.Stat(filepath.Join(c.Path, filepath.FromSlash(left))); err == nil {
					t.Errorf("%s was written although the launch failed", left)
				}
			}
		})
	}
}

// A project with other .opencode content (skills, plugins) is no collision:
// only the exact files vexillum writes are protected.
func TestRunInHerdr_OpenCodeV2LivesBesideTheProjectsOwnOpenCodeDirectory(t *testing.T) {
	projectDir := initStrikeTestProject(t)
	if err := os.MkdirAll(filepath.Join(projectDir, ".opencode", "skills", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	commitFile(t, projectDir, ".opencode/skills/x/SKILL.md", "---\nname: x\n---\n")
	home := t.TempDir()
	task := ocTask(t, 2, "opencode/glm-5.3-flash-free", "")
	c := ocCamp(t, projectDir, home, task)

	if _, err := soldier.RunInHerdr(home, "w1", task, c, ocClient()); err != nil {
		t.Fatalf("RunInHerdr: %v", err)
	}
	if out := runGitT(t, c.Path, "status", "--porcelain"); out != "" {
		t.Errorf("dirty camp:\n%s", out)
	}
}

// The files of an earlier soldier are still in a reused camp (git leaves
// ignored files alone), and are not a collision: vexillum recognizes its own.
func TestRunInHerdr_OpenCodeReusedCampOverwritesOnlyItsOwnLeftovers(t *testing.T) {
	projectDir := initStrikeTestProject(t)
	home := t.TempDir()

	first := ocTask(t, 2, "opencode/glm-5.3-flash-free", "")
	c := ocCamp(t, projectDir, home, first)
	if _, err := soldier.RunInHerdr(home, "w1", first, c, ocClient()); err != nil {
		t.Fatalf("first: %v", err)
	}
	// Strike the camp so the slot is reused, as vx strike does.
	if err := camp.Strike(c, first.ID); err != nil {
		t.Fatalf("Strike: %v", err)
	}

	second := ocTask(t, 2, "opencode/space-bunny-free#high", "")
	c2 := ocCamp(t, projectDir, home, second)
	if c2.Path != c.Path {
		t.Fatalf("expected the slot to be reused, got %s then %s", c.Path, c2.Path)
	}
	if _, err := soldier.RunInHerdr(home, "w1", second, c2, ocClient()); err != nil {
		t.Fatalf("second: %v", err)
	}
	agent, _ := os.ReadFile(filepath.Join(c2.Path, ".opencode", "agents", "vx-soldier.md"))
	if !strings.Contains(string(agent), "space-bunny-free#high") {
		t.Errorf("the second soldier got the first one's agent file:\n%s", agent)
	}
	if got := excludeFile(t, projectDir); strings.Count(got, "/opencode.json\n") != 1 {
		t.Errorf("info/exclude has duplicates:\n%s", got)
	}
}

// A file the previous soldier changed is not vexillum's any more: it counts
// as the project's, and the next dispatch refuses to overwrite it.
func TestRunInHerdr_OpenCodeModifiedLeftoverIsACollision(t *testing.T) {
	projectDir := initStrikeTestProject(t)
	home := t.TempDir()
	first := ocTask(t, 2, "opencode/glm-5.3-flash-free", "")
	c := ocCamp(t, projectDir, home, first)
	if _, err := soldier.RunInHerdr(home, "w1", first, c, ocClient()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(c.Path, "opencode.json"), []byte(`{"theme":"mine"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := camp.Strike(c, first.ID); err != nil {
		t.Fatal(err)
	}

	second := ocTask(t, 2, "opencode/glm-5.3-flash-free", "")
	c2 := ocCamp(t, projectDir, home, second)
	_, err := soldier.RunInHerdr(home, "w1", second, c2, ocClient())
	if err == nil || !strings.Contains(err.Error(), "opencode.json") {
		t.Fatalf("error = %v, want a collision on opencode.json", err)
	}
	if got, _ := os.ReadFile(filepath.Join(c2.Path, "opencode.json")); string(got) != `{"theme":"mine"}` {
		t.Errorf("the modified file was overwritten: %q", got)
	}
}

// A camp that held an OpenCode V2 soldier and is reused by any other harness
// does not keep the V2 files: a V1 or Claude soldier must not read them.
func TestRunInHerdr_LeftoverOpenCodeFilesAreRetractedForAnyHarness(t *testing.T) {
	projectDir := initStrikeTestProject(t)
	home := t.TempDir()
	first := ocTask(t, 2, "opencode/glm-5.3-flash-free", "")
	c := ocCamp(t, projectDir, home, first)
	if _, err := soldier.RunInHerdr(home, "w1", first, c, ocClient()); err != nil {
		t.Fatal(err)
	}
	if err := camp.Strike(c, first.ID); err != nil {
		t.Fatal(err)
	}

	second := ocTask(t, 1, "opencode/glm-5.3-flash-free", "")
	c2 := ocCamp(t, projectDir, home, second)
	if _, err := soldier.RunInHerdr(home, "w1", second, c2, ocClient()); err != nil {
		t.Fatalf("V1 task in the reused camp: %v", err)
	}
	for _, left := range []string{"opencode.json", ".opencode"} {
		if _, err := os.Stat(filepath.Join(c2.Path, left)); err == nil {
			t.Errorf("%s from the V2 soldier is still in the camp", left)
		}
	}
}

func TestRunInHerdr_OpenCodeV1StartsWithModelFlagAndConfigEnvAndNoFiles(t *testing.T) {
	projectDir := initStrikeTestProject(t)
	home := t.TempDir()
	task := ocTask(t, 1, "anthropic/claude-sonnet-5#high", "medium")
	c := ocCamp(t, projectDir, home, task)
	client := ocClient()

	if _, err := soldier.RunInHerdr(home, "w1", task, c, client); err != nil {
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

// Soldiers dispatched together append to the repository's one info/exclude.
func TestRunInHerdr_OpenCodeConcurrentCampsKeepOneCopyOfEachExcludePattern(t *testing.T) {
	projectDir := initStrikeTestProject(t)
	home := t.TempDir()
	const n = 6
	tasks := make([]state.Task, n)
	camps := make([]camp.Camp, n)
	for i := range tasks {
		tasks[i] = ocTask(t, 2, "opencode/glm-5.3-flash-free", "")
		camps[i] = ocCamp(t, projectDir, home, tasks[i])
	}
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range tasks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = soldier.RunInHerdr(home, "w1", tasks[i], camps[i], ocClient())
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("soldier %d: %v", i, err)
		}
	}
	exclude := excludeFile(t, projectDir)
	for _, p := range []string{"/.opencode/", "/opencode.json"} {
		if got := strings.Count(exclude, p+"\n"); got != 1 {
			t.Errorf("%s appears %d times in info/exclude:\n%s", p, got, exclude)
		}
	}
}
