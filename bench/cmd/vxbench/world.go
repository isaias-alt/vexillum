package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/state"
)

// world is one isolated place to run vx in: its own HOME, and a project
// (a git repository with "vx init" applied and committed) under it. Nothing
// outside the bench temp directory is read or written.
type world struct {
	dir      string
	home     string
	project  string
	sentinel *exec.Cmd
	b        *bench
	// extraEnv is added to the environment of every command run in the world,
	// the sentinel included.
	extraEnv []string
}

// invocation is one finished command.
type invocation struct {
	wall   time.Duration
	cpu    time.Duration
	exit   int
	stdout string
	stderr string
	pid    int
}

// spec describes a command to run in a world.
type spec struct {
	exe      string // "vx", "floor", or a tool name resolved in PATH
	args     []string
	fake     bool     // fake herdr and claude first in PATH, HERDR_WORKSPACE_ID set
	hermetic bool     // PATH holds only the fakes and the few real tools vx needs
	env      []string // extra KEY=VALUE
	realHome bool     // keep the caller's HOME (read-only calls to real tools)
	traceLog string   // when set, tools run through the spawnlog shims
	dir      string   // working directory, default the world's project
}

func (b *bench) newWorld(name string) (*world, error) {
	dir, err := os.MkdirTemp(b.root, name+"-")
	if err != nil {
		return nil, err
	}
	w := &world{dir: dir, home: filepath.Join(dir, "home"), project: filepath.Join(dir, "project"), b: b}
	// A real install has ~/.vexillum by the time anything runs.
	for _, d := range []string{filepath.Join(w.home, ".vexillum"), w.project} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	setup := [][]string{
		{"git", "init", "-q"},
		{"git", "symbolic-ref", "HEAD", "refs/heads/main"},
	}
	for _, args := range setup {
		if err := w.git(w.project, args[1:]...); err != nil {
			return nil, err
		}
	}
	if err := os.WriteFile(filepath.Join(w.project, "README.md"), []byte("bench project\n"), 0o644); err != nil {
		return nil, err
	}
	if err := w.git(w.project, "add", "-A"); err != nil {
		return nil, err
	}
	if err := w.git(w.project, "commit", "-q", "-m", "initial commit"); err != nil {
		return nil, err
	}
	init, err := b.invoke(w, spec{exe: "vx", args: []string{"init", "--yes", "--skills"}})
	if err != nil || init.exit != 0 {
		return nil, fmt.Errorf("vx init in the bench project: exit %d, err %v\n%s%s", init.exit, err, init.stdout, init.stderr)
	}
	if err := w.git(w.project, "add", "-A"); err != nil {
		return nil, err
	}
	if err := w.git(w.project, "commit", "-q", "-m", "vx init"); err != nil {
		return nil, err
	}
	return w, nil
}

// git runs git in dir with a fixed identity.
func (w *world) git(dir string, args ...string) error {
	_, err := w.gitOut(dir, args...)
	return err
}

func (w *world) gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = w.baseEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

func (w *world) baseEnv() []string {
	return []string{
		"HOME=" + w.home,
		"PATH=" + os.Getenv("PATH"),
		"TMPDIR=" + os.TempDir(),
		"TERM=dumb",
		"LANG=C",
		"GIT_AUTHOR_NAME=Bench", "GIT_AUTHOR_EMAIL=bench@example.com",
		"GIT_COMMITTER_NAME=Bench", "GIT_COMMITTER_EMAIL=bench@example.com",
	}
}

// commitInCamp gives the camp of task id one commit, so land has something to
// fast-forward and the sentinel has proof the soldier delivered.
func (w *world) commitInCamp(id string) error {
	root, err := project.Root(w.vexillumHome(), w.project)
	if err != nil {
		return err
	}
	task, err := state.Load(root, id)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(task.CampPath, "bench-"+id+".txt"), []byte("soldier work\n"), 0o644); err != nil {
		return err
	}
	if err := w.git(task.CampPath, "add", "-A"); err != nil {
		return err
	}
	return w.git(task.CampPath, "commit", "-q", "-m", "soldier work "+id)
}

// vexillumHome is the ~/.vexillum of the world.
func (w *world) vexillumHome() string { return filepath.Join(w.home, ".vexillum") }

// startSentinel runs "vx sentinel" in the background, the steady state of a
// real machine, so dispatch finds one running instead of starting its own.
func (w *world) startSentinel() error {
	logFile, err := os.Create(filepath.Join(w.dir, "sentinel.log"))
	if err != nil {
		return err
	}
	cmd := exec.Command(w.b.cfg.vx, "sentinel")
	cmd.Dir = w.project
	cmd.Env = w.env(spec{fake: true})
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		return err
	}
	w.sentinel = cmd
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(w.vexillumHome(), "sentinel.json")); err == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("the sentinel did not publish sentinel.json within 10s (see %s)", logFile.Name())
}

// teardown stops everything the world started and removes its directory.
func (w *world) teardown() {
	if w.b.cfg.keep {
		fmt.Fprintf(os.Stderr, "vxbench: keeping %s\n", w.dir)
		return
	}
	// A forum server and its listener are detached; ask them to stop.
	_, _ = w.b.invoke(w, spec{exe: "vx", args: []string{"forum", "stop"}})
	if w.sentinel != nil && w.sentinel.Process != nil {
		_ = w.sentinel.Process.Signal(syscall.SIGTERM)
		_, _ = w.sentinel.Process.Wait()
	}
	w.b.removeUnderRoot(w.dir)
}

// env builds the environment of a command: only what vx needs, with HOME
// pointing into the world so nothing of the maintainer's is touched.
func (w *world) env(s spec) []string {
	env := w.baseEnv()
	path := os.Getenv("PATH")
	switch {
	case s.hermetic:
		path = w.b.hermeticPath()
	case s.fake:
		path = w.b.fakes + string(os.PathListSeparator) + path
	}
	if s.fake {
		env = append(env, "HERDR_WORKSPACE_ID=w1")
	}
	if s.realHome {
		env[0] = "HOME=" + os.Getenv("HOME")
	}
	if s.traceLog != "" {
		env = append(env, "VXBENCH_REAL_PATH="+path, "VXBENCH_SPAWN_LOG="+s.traceLog)
		path = w.b.makeShims(path) + string(os.PathListSeparator) + path
	}
	env = append(env, "PATH="+path)
	env = append(env, w.extraEnv...)
	return append(env, s.env...)
}

// invoke runs one command to completion and times it from before the fork to
// after the wait.
func (b *bench) invoke(w *world, s spec) (invocation, error) {
	exe := s.exe
	switch exe {
	case "vx":
		exe = b.cfg.vx
	case "floor":
		exe = b.cfg.floor
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(exe, s.args...)
	cmd.Dir = s.dir
	if cmd.Dir == "" {
		cmd.Dir = w.project
	}
	cmd.Env = w.env(s)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	start := time.Now()
	if err := cmd.Start(); err != nil {
		return invocation{}, fmt.Errorf("starting %s: %w", exe, err)
	}
	pid := cmd.Process.Pid
	waitErr := cmd.Wait()
	wall := time.Since(start)

	inv := invocation{wall: wall, stdout: stdout.String(), stderr: stderr.String(), pid: pid}
	if cmd.ProcessState != nil {
		inv.exit = cmd.ProcessState.ExitCode()
		inv.cpu = cmd.ProcessState.UserTime() + cmd.ProcessState.SystemTime()
	}
	if waitErr != nil && cmd.ProcessState == nil {
		return inv, waitErr
	}
	return inv, nil
}

var taskIDPattern = regexp.MustCompile(`task_id=([0-9a-f]{16})`)

// dispatch runs "vx dispatch" against the fake herdr and returns the new
// task's id.
func (b *bench) dispatch(w *world) (string, invocation, error) {
	inv, err := b.invoke(w, spec{exe: "vx", args: []string{"dispatch", "--kind", "mission", "benchmark task"}, fake: true})
	if err != nil {
		return "", inv, err
	}
	if inv.exit != 0 {
		return "", inv, fmt.Errorf("vx dispatch exited %d\n%s%s", inv.exit, inv.stdout, inv.stderr)
	}
	m := taskIDPattern.FindStringSubmatch(inv.stdout)
	if m == nil {
		return "", inv, fmt.Errorf("no task_id in the dispatch output:\n%s", inv.stdout)
	}
	return m[1], inv, nil
}

// removeUnderRoot deletes path, but only when it is inside the temporary
// directory this run created: the harness never removes anything else.
func (b *bench) removeUnderRoot(path string) {
	if path != b.root && !strings.HasPrefix(path, b.root+string(os.PathSeparator)) {
		fmt.Fprintf(os.Stderr, "vxbench: refusing to remove %s, it is not inside %s\n", path, b.root)
		return
	}
	// Worktrees keep their files read-write, but be safe about modes.
	_ = os.RemoveAll(path)
}

// hermeticTools are the real programs a hermetic PATH keeps: the ones vx and
// the fake tools run. Everything else on the machine, a pi or an opencode
// above all, is out of reach, so the numbers do not depend on what is installed.
var hermeticTools = []string{"git", "ps", "sh", "env", "sleep"}

// hermeticPath returns a directory that holds the fakes and the real tools in
// hermeticTools, and nothing else, created on first use.
func (b *bench) hermeticPath() string {
	if b.hermeticDir != "" {
		return b.hermeticDir
	}
	dir := filepath.Join(b.root, "hermetic-bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "vxbench: %v\n", err)
		return dir
	}
	link := func(name, target string) {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			fmt.Fprintf(os.Stderr, "vxbench: %v\n", err)
		}
	}
	entries, _ := os.ReadDir(b.fakes)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			link(e.Name(), filepath.Join(b.fakes, e.Name()))
		}
	}
	for _, tool := range hermeticTools {
		if p, err := exec.LookPath(tool); err == nil {
			link(tool, p)
		}
	}
	b.hermeticDir = dir
	return dir
}

// tracedTools are the tools a traced run puts a spawnlog shim in front of.
var tracedTools = []string{"git", "herdr", "gh", "claude", "pi", "opencode", "ps"}

// makeShims creates a directory of symlinks to the spawnlog binary, one per
// traced tool that exists in realPath, so a tool that is not installed does
// not appear to be.
func (b *bench) makeShims(realPath string) string {
	b.shimN++
	dir := filepath.Join(b.root, fmt.Sprintf("shims-%d", b.shimN))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "vxbench: %v\n", err)
		return dir
	}
	for _, tool := range tracedTools {
		found := false
		for _, d := range filepath.SplitList(realPath) {
			p := filepath.Join(d, tool)
			if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		if err := os.Symlink(b.cfg.spawnlog, filepath.Join(dir, tool)); err != nil {
			fmt.Fprintf(os.Stderr, "vxbench: %v\n", err)
		}
	}
	return dir
}
