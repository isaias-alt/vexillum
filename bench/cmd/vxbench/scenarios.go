package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/isaias-alt/vexillum/internal/pending"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/scaffold"
	"github.com/isaias-alt/vexillum/internal/state"
)

// scenario is one command timed n times.
type scenario struct {
	name  string
	group string
	spec  spec

	// needs names a tool that must be on PATH, or the scenario is skipped.
	needs string
	// slow scenarios run only with -slow, and with a small n.
	slow bool
	// opt-in scenarios touch the maintainer's real tools and run only on request.
	realHerdr bool
	// noTrace skips the traced run (nothing of vx to trace).
	noTrace bool
	// okExit lists the exit codes that count as a normal run; default {0}.
	okExit []int
	// setup builds the world to run in; default is a plain initialized project.
	setup func(b *bench, w *world) error
	// sentinel starts a background sentinel in the world before the runs.
	sentinel bool
	// prepare runs untimed before each run and may replace the arguments.
	prepare func(b *bench, w *world, s *spec) error
	// after runs untimed after each run.
	after func(b *bench, w *world, inv invocation) error
}

func (b *bench) scenarios() []scenario {
	vxSpec := func(args ...string) spec { return spec{exe: "vx", args: args} }
	fakeSpec := func(args ...string) spec { return spec{exe: "vx", args: args, fake: true} }

	seedTasks := func(n int) func(*bench, *world) error {
		return func(b *bench, w *world) error {
			root, err := project.Root(w.vexillumHome(), w.project)
			if err != nil {
				return err
			}
			for i := 0; i < n; i++ {
				task, err := state.New(state.KindMission, fmt.Sprintf("task %d: refactor the parser and keep the tests green", i))
				if err != nil {
					return err
				}
				task.Status = state.StatusDone
				task.CampBranch = "vexillum/" + task.ID
				if err := state.Save(root, task); err != nil {
					return err
				}
			}
			return nil
		}
	}
	seedPending := func(n int) func(*bench, *world) error {
		return func(b *bench, w *world) error {
			root, err := project.Root(w.vexillumHome(), w.project)
			if err != nil {
				return err
			}
			for i := 0; i < n; i++ {
				if _, err := pending.Add(root, fmt.Sprintf("decision %d", i), []string{"yes", "no"}, 1); err != nil {
					return err
				}
			}
			return nil
		}
	}

	htmlPath := func(w *world) string { return filepath.Join(w.project, "bench.html") }
	writeHTML := func(b *bench, w *world) error {
		return os.WriteFile(htmlPath(w), []byte("<!doctype html><title>bench</title><p>bench</p>\n"), 0o644)
	}
	openForum := func(b *bench, w *world) error {
		inv, err := b.invoke(w, spec{exe: "vx", args: []string{"forum", htmlPath(w), "--no-open"}})
		if err != nil || inv.exit != 0 {
			return fmt.Errorf("opening the forum session: exit %d, err %v\n%s%s", inv.exit, err, inv.stdout, inv.stderr)
		}
		return nil
	}

	// commitInCamp gives the task's camp one commit, so land has something to
	// fast-forward.
	commitInCamp := func(b *bench, w *world, id string) error {
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

	dispatchSpec := fakeSpec("dispatch", "--kind", "mission", "benchmark task")
	strikeAfter := func(b *bench, w *world, inv invocation) error {
		m := taskIDPattern.FindStringSubmatch(inv.stdout)
		if m == nil {
			return fmt.Errorf("no task_id in the dispatch output:\n%s", inv.stdout)
		}
		out, err := b.invoke(w, fakeSpec("strike", m[1]))
		if err != nil || out.exit != 0 {
			return fmt.Errorf("striking %s: exit %d, err %v\n%s%s", m[1], out.exit, err, out.stdout, out.stderr)
		}
		return nil
	}
	prepareWithTask := func(verb string, withCommit bool) func(*bench, *world, *spec) error {
		return func(b *bench, w *world, s *spec) error {
			id, _, err := b.dispatch(w)
			if err != nil {
				return err
			}
			if withCommit {
				if err := commitInCamp(b, w, id); err != nil {
					return err
				}
			}
			s.args = []string{verb, id}
			return nil
		}
	}

	var out []scenario

	// Startup: what every invocation pays before it does any work.
	out = append(out,
		scenario{name: "floor: empty Go program", group: "startup", spec: spec{exe: "floor"}, noTrace: true},
		scenario{name: "vx --version", group: "startup", spec: vxSpec("--version")},
		scenario{name: "vx --help", group: "startup", spec: vxSpec("--help")},
	)

	// Cheap commands, against an initialized project.
	out = append(out,
		scenario{name: "vx models", group: "cheap", spec: vxSpec("models")},
		scenario{name: "vx status (0 tasks)", group: "cheap", spec: vxSpec("status")},
		scenario{name: "vx status (50 tasks)", group: "cheap", spec: vxSpec("status"), setup: seedTasks(50)},
		scenario{name: "vx status --json (50 tasks)", group: "cheap", spec: vxSpec("status", "--json"), setup: seedTasks(50)},
		scenario{name: "vx status (500 tasks)", group: "cheap", spec: vxSpec("status"), setup: seedTasks(500)},
		scenario{name: "vx pending list (0 items)", group: "cheap", spec: vxSpec("pending", "list")},
		scenario{name: "vx pending list (50 items)", group: "cheap", spec: vxSpec("pending", "list"), setup: seedPending(50)},
		scenario{name: "vx yolo", group: "cheap", spec: vxSpec("yolo")},
		scenario{name: "vx sentinel drain (nothing pending)", group: "cheap", spec: vxSpec("sentinel", "drain")},
		scenario{name: "vx sentinel await (outside a herdr pane)", group: "cheap", spec: vxSpec("sentinel", "await")},
		scenario{name: "vx forum inbox (no server)", group: "cheap", spec: vxSpec("forum", "inbox")},
		scenario{name: "vx forum stop (nothing running)", group: "cheap", spec: vxSpec("forum", "stop")},
		scenario{name: "vx doctor", group: "doctor", spec: vxSpec("doctor"), okExit: []int{0, 1}},
	)

	// Forum commands that do not need a browser: they start, reach or stop the
	// local server and listener.
	out = append(out,
		scenario{name: "vx forum <file> --no-open (cold: starts server and listener)", group: "forum",
			spec: spec{exe: "vx", args: []string{"forum", "HTML", "--no-open"}}, setup: writeHTML,
			prepare: func(b *bench, w *world, s *spec) error {
				// The previous run left a server up.
				if _, err := b.invoke(w, spec{exe: "vx", args: []string{"forum", "stop"}}); err != nil {
					return err
				}
				s.args = []string{"forum", htmlPath(w), "--no-open"}
				return nil
			}},
		scenario{name: "vx forum <file> --no-open (warm: server already up)", group: "forum",
			spec: spec{exe: "vx", args: []string{"forum", "HTML", "--no-open"}},
			setup: func(b *bench, w *world) error {
				if err := writeHTML(b, w); err != nil {
					return err
				}
				return openForum(b, w)
			},
			prepare: func(b *bench, w *world, s *spec) error {
				s.args = []string{"forum", htmlPath(w), "--no-open"}
				return nil
			}},
		scenario{name: "vx forum end <file>", group: "forum",
			spec: spec{exe: "vx", args: []string{"forum", "end", "HTML"}}, setup: writeHTML,
			prepare: func(b *bench, w *world, s *spec) error {
				if err := openForum(b, w); err != nil {
					return err
				}
				s.args = []string{"forum", "end", htmlPath(w)}
				return nil
			}},
	)

	// The soldier lifecycle against a fake herdr and a fake claude: no model, no
	// terminal, so what is left is vexillum's own work plus its git and herdr calls.
	out = append(out,
		scenario{name: "vx dispatch (new camp: git worktree add)", group: "camp", spec: dispatchSpec, sentinel: true},
		scenario{name: "vx dispatch (reused camp)", group: "camp", spec: dispatchSpec, sentinel: true, after: strikeAfter},
		scenario{name: "vx strike", group: "camp", spec: fakeSpec("strike", "ID"), sentinel: true, prepare: prepareWithTask("strike", false)},
		scenario{name: "vx land (fast-forward merge + strike)", group: "camp", spec: fakeSpec("land", "ID"), sentinel: true, prepare: prepareWithTask("land", true)},
	)
	// A real soldier is still at work when the quick-settle probe expires, so
	// dispatch waits out the whole probe. The fake herdr reproduces that.
	busy := dispatchSpec
	busy.env = []string{"VXBENCH_PROMPT_MODE=busy"}
	out = append(out, scenario{name: "vx dispatch (soldier still working: quick-settle probe expires)", group: "camp", spec: busy, sentinel: true, slow: true})

	// External tools vx shells out to, for scale. These are not vx.
	for _, t := range []struct {
		tool, name string
		args       []string
	}{
		{"git", "git rev-parse --abbrev-ref HEAD", []string{"rev-parse", "--abbrev-ref", "HEAD"}},
		{"git", "git status --porcelain", []string{"status", "--porcelain"}},
		{"herdr", "herdr --version", []string{"--version"}},
		{"pi", "pi --version", []string{"--version"}},
		{"opencode", "opencode --version", []string{"--version"}},
		{"gh", "gh --version", []string{"--version"}},
	} {
		out = append(out, scenario{name: t.name, group: "tools", spec: spec{exe: t.tool, args: t.args}, needs: t.tool, noTrace: true})
	}
	// What doctor's Stop hook check runs: a bare environment, a shell, and
	// whichever vx that shell finds. Absolute paths, so the spawn tracer cannot see it.
	out = append(out, scenario{name: "doctor's Stop hook probe (env -i sh -c <hook>)", group: "tools",
		spec: spec{exe: "/usr/bin/env"}, noTrace: true,
		prepare: func(b *bench, w *world, s *spec) error {
			s.dir = w.dir
			s.args = []string{"-i", "HOME=" + w.home, "PATH=/usr/bin:/bin", "/bin/sh", "-c", scaffold.SentinelHookCommand}
			return nil
		}})
	// Read-only calls to the maintainer's real herdr server, the cost the
	// sentinel pays per watched task on every tick.
	for _, args := range [][]string{{"agent", "list"}, {"agent", "get", "vxbench-no-such-agent"}} {
		out = append(out, scenario{name: "herdr " + strings.Join(args, " ") + " (the real herdr server)", group: "tools",
			spec: spec{exe: "herdr", args: args, realHome: true}, needs: "herdr", realHerdr: true, noTrace: true, okExit: []int{0, 1}})
	}

	return out
}
