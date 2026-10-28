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

// scenario is one command timed n times, or, with multi, a measurement that
// produces several rows at once.
type scenario struct {
	name  string
	group string
	spec  spec

	// needs names a tool that must be on PATH, or the scenario is skipped.
	needs string
	// slow scenarios run only with -slow, and with a small n.
	slow bool
	// realHerdr scenarios read the maintainer's real herdr server and run only
	// on request (-real-herdr).
	realHerdr bool
	// realTools scenarios run whatever pi, opencode, gh or herdr this machine
	// has, so their numbers are the machine's and not vx's: they run only on
	// request (-real-tools) and are never judged against a baseline.
	realTools bool
	// informational scenarios measure the machine or a polling interval: they
	// are compared with the baseline and shown, never judged.
	informational bool
	// guard is a hard limit the scenario must stay under, whatever the baseline.
	guard *guard
	// maxRuns caps the timed runs of a scenario that takes seconds a run.
	maxRuns int
	// noTrace skips the traced run (nothing of vx to trace).
	noTrace bool
	// noRecheck keeps a comparison from running the scenario again.
	noRecheck bool
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

	// multi runs a scenario that measures several things at once (the wake
	// latency, split into phases) and returns one result per measurement. The
	// fields above that shape a single timed command do not apply to it.
	multi func(b *bench) []result
}

// Simulated tool start-up costs of the "slow tools" doctor scenario, taken
// from a real laptop: pi starts node (about 165 ms), opencode about 62 ms.
const (
	slowPiDelay       = "0.165"
	slowOpenCodeDelay = "0.062"
)

// dispatchProbeCeilingMS is the longest a dispatch of a soldier that is still
// working may take. It used to wait out the 15 s quick-settle probe; it now
// returns as soon as herdr reports the soldier working, in under 100 ms.
const dispatchProbeCeilingMS = 5000

func (b *bench) scenarios() []scenario {
	vxSpec := func(args ...string) spec { return spec{exe: "vx", args: args} }
	fakeSpec := func(args ...string) spec { return spec{exe: "vx", args: args, fake: true} }
	doctorSpec := func(env ...string) spec {
		return spec{exe: "vx", args: []string{"doctor"}, fake: true, hermetic: true, env: env}
	}

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
				if err := w.commitInCamp(id); err != nil {
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
		scenario{name: "floor: empty Go program", group: "startup", spec: spec{exe: "floor"}, noTrace: true, informational: true},
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
		scenario{name: "vx sentinel await (outside a pane)", group: "cheap", spec: vxSpec("sentinel", "await")},
		scenario{name: "vx forum inbox (no server)", group: "cheap", spec: vxSpec("forum", "inbox")},
		scenario{name: "vx forum stop (nothing running)", group: "cheap", spec: vxSpec("forum", "stop")},
	)

	// doctor with fake pi and opencode on a PATH that holds nothing else of the
	// machine's. pi and opencode are what make it slow (a node start and a
	// second runtime), so the slow variant has them cost what they cost on a
	// laptop, and doctor, which runs its checks side by side, should take about
	// as long as the slowest of them, not their sum.
	out = append(out,
		scenario{name: "vx doctor (fast tools)", group: "doctor", spec: doctorSpec(), okExit: []int{0, 1}},
		scenario{name: "vx doctor (slow tools: pi 165 ms, opencode 62 ms)", group: "doctor",
			spec: doctorSpec("VXBENCH_PI_DELAY="+slowPiDelay, "VXBENCH_OPENCODE_DELAY="+slowOpenCodeDelay), okExit: []int{0, 1}},
	)

	// Forum commands that do not need a browser: they start, reach or stop the
	// local server and listener.
	out = append(out,
		scenario{name: "vx forum <file> (cold: starts server)", group: "forum",
			spec: spec{exe: "vx", args: []string{"forum", "HTML", "--no-open"}}, setup: writeHTML,
			prepare: func(b *bench, w *world, s *spec) error {
				// The previous run left a server up.
				if _, err := b.invoke(w, spec{exe: "vx", args: []string{"forum", "stop"}}); err != nil {
					return err
				}
				s.args = []string{"forum", htmlPath(w), "--no-open"}
				return nil
			}},
		scenario{name: "vx forum <file> (warm: server up)", group: "forum",
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
		scenario{name: "vx dispatch (new camp)", group: "camp", spec: dispatchSpec, sentinel: true},
		scenario{name: "vx dispatch (reused camp)", group: "camp", spec: dispatchSpec, sentinel: true, after: strikeAfter},
		scenario{name: "vx strike", group: "camp", spec: fakeSpec("strike", "ID"), sentinel: true, prepare: prepareWithTask("strike", false)},
		scenario{name: "vx land (ff merge + strike)", group: "camp", spec: fakeSpec("land", "ID"), sentinel: true, prepare: prepareWithTask("land", true)},
	)
	// A real soldier is still at work when the prompt is submitted (the normal
	// case), and herdr reports it working at once: dispatch must not wait. The
	// probe used to sleep out its 15 s bound here (fixed in f18f213), so this
	// scenario carries a guard that fails the run if that ever comes back.
	busy := dispatchSpec
	busy.env = []string{"VXBENCH_PROMPT_MODE=busy"}
	out = append(out, scenario{name: "vx dispatch (soldier still working)", group: "camp", spec: busy, sentinel: true,
		guard: &guard{Stat: "median", LimitMS: dispatchProbeCeilingMS,
			Why: "dispatch must return once herdr reports the soldier working, not wait out the 15 s quick-settle probe"}})
	// herdr sees none of the states dispatch's probe stops at: the probe waits
	// out its whole bound, which is what a still-working soldier cost before
	// the probe stopped at "working". The time is the fake's sleep, so it is
	// shown but never judged.
	silent := dispatchSpec
	silent.env = []string{"VXBENCH_PROMPT_MODE=silent"}
	out = append(out, scenario{name: "vx dispatch (state never reported)", group: "camp", spec: silent, sentinel: true, slow: true, informational: true})

	// Wake latency end to end, against the real sentinel binary: a soldier
	// finishes, the sentinel notices, records the wake, and the commander's Stop
	// hook ("vx sentinel await") returns.
	out = append(out, scenario{name: "wake latency (soldier idle to Stop hook)", group: "wake", multi: (*bench).runWake})

	// External tools vx shells out to, for scale. These are not vx, but every
	// machine has them.
	for _, t := range []struct {
		name string
		args []string
	}{
		{"git rev-parse --abbrev-ref HEAD", []string{"rev-parse", "--abbrev-ref", "HEAD"}},
		{"git status --porcelain", []string{"status", "--porcelain"}},
	} {
		out = append(out, scenario{name: t.name, group: "tools", spec: spec{exe: "git", args: t.args}, noTrace: true, informational: true})
	}
	// What doctor's Stop hook check runs: a bare environment, a shell, and
	// whichever vx that shell finds. Absolute paths, so the spawn tracer cannot see it.
	out = append(out, scenario{name: "doctor's Stop hook probe (env -i sh -c <hook>)", group: "tools",
		spec: spec{exe: "/usr/bin/env"}, noTrace: true, informational: true,
		prepare: func(b *bench, w *world, s *spec) error {
			s.dir = w.dir
			s.args = []string{"-i", "HOME=" + w.home, "PATH=/usr/bin:/bin", "/bin/sh", "-c", scaffold.SentinelHookCommand}
			return nil
		}})

	// What this machine has installed. Machine specific: these are the numbers
	// that made doctor slow on one laptop and say nothing about another.
	out = append(out, scenario{name: "vx doctor (this machine's real tools)", group: "real", spec: vxSpec("doctor"),
		okExit: []int{0, 1}, realTools: true, informational: true})
	for _, t := range []struct {
		tool string
		args []string
	}{
		{"herdr", []string{"--version"}},
		{"pi", []string{"--version"}},
		{"opencode", []string{"--version"}},
		{"gh", []string{"--version"}},
	} {
		out = append(out, scenario{name: t.tool + " " + strings.Join(t.args, " ") + " (real)", group: "real",
			spec: spec{exe: t.tool, args: t.args}, needs: t.tool, noTrace: true, realTools: true, informational: true})
	}
	// Read-only calls to the maintainer's real herdr server, the cost the
	// sentinel pays per watched task on every tick.
	for _, args := range [][]string{{"agent", "list"}, {"agent", "get", "vxbench-no-such-agent"}} {
		out = append(out, scenario{name: "herdr " + strings.Join(args, " ") + " (real server)", group: "real",
			spec: spec{exe: "herdr", args: args, realHome: true}, needs: "herdr", realHerdr: true, noTrace: true, informational: true, okExit: []int{0, 1}})
	}

	return out
}
