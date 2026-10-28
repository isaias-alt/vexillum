// Command vxbench times real vx invocations end to end and reports min, median
// and p95 wall time, the CPU time vx used, and the subprocesses it starts.
//
// It never touches the maintainer's state: every scenario runs in a world of
// its own under one temporary directory, with a temporary HOME and a scratch
// git project, and the dispatch, land and strike scenarios use a fake herdr and
// a fake claude from bench/fakes, so no model, terminal or herdr server is
// needed. See "Benchmarking" in CONTRIBUTING.md and bench/run.sh.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

// tracedRuns is how many times a command is run under the spawn tracer.
const tracedRuns = 3

type config struct {
	vx, floor, spawnlog, fakes string
	n, warmup                  int
	filter                     string
	slow, realHerdr            bool
	noTrace, keep              bool
	jsonOut                    string
}

type bench struct {
	cfg    config
	root   string // the temporary directory every world lives in
	fakes  string
	filter *regexp.Regexp
	shimN  int
}

// result is one scenario's outcome.
type result struct {
	Name      string        `json:"name"`
	Group     string        `json:"group"`
	Skipped   string        `json:"skipped,omitempty"`
	Failure   string        `json:"failure,omitempty"`
	FirstMS   float64       `json:"first_run_ms,omitempty"`
	Wall      stats         `json:"wall"`
	CPUMedMS  float64       `json:"cpu_median_ms,omitempty"`
	Spawns    *spawnSummary `json:"spawns,omitempty"`
	BadExits  map[int]int   `json:"unexpected_exit_codes,omitempty"`
	TracedRun bool          `json:"traced,omitempty"`
}

type report struct {
	GeneratedAt string     `json:"generated_at"`
	GoVersion   string     `json:"go_version"`
	OS          string     `json:"os"`
	Arch        string     `json:"arch"`
	CPUs        int        `json:"cpus"`
	VXVersion   string     `json:"vx_version"`
	Runs        int        `json:"runs"`
	Warmup      int        `json:"warmup"`
	Results     []result   `json:"results"`
	InitCosts   []initCost `json:"package_init_ms,omitempty"`
	InitTotalMS float64    `json:"package_init_total_ms,omitempty"`
}

func main() {
	os.Exit(run())
}

func run() int {
	var cfg config
	flag.StringVar(&cfg.vx, "vx", "", "path to the vx binary to time (required)")
	flag.StringVar(&cfg.floor, "floor", "", "path to the empty Go program that sets the process-start floor (required)")
	flag.StringVar(&cfg.spawnlog, "spawnlog", "", "path to the spawnlog shim binary (required unless -no-trace)")
	flag.StringVar(&cfg.fakes, "fakes", "bench/fakes", "directory holding the fake herdr and claude")
	flag.IntVar(&cfg.n, "n", 30, "timed runs per scenario")
	flag.IntVar(&cfg.warmup, "warmup", 3, "untimed warm-up runs before the timed ones")
	flag.StringVar(&cfg.filter, "only", "", "run only the scenarios whose name matches this regexp")
	flag.BoolVar(&cfg.slow, "slow", false, "also run the scenarios that wait out a 15s timeout (3 runs each)")
	flag.BoolVar(&cfg.realHerdr, "real-herdr", false, "also time a read-only call to the maintainer's real herdr server")
	flag.BoolVar(&cfg.noTrace, "no-trace", false, "skip the traced run that counts subprocesses")
	flag.BoolVar(&cfg.keep, "keep", false, "keep the temporary worlds instead of deleting them")
	flag.StringVar(&cfg.jsonOut, "json", "", "also write the full report to this JSON file")
	flag.Parse()

	if cfg.vx == "" || cfg.floor == "" || (cfg.spawnlog == "" && !cfg.noTrace) {
		fmt.Fprintln(os.Stderr, "vxbench: -vx, -floor and -spawnlog are required (bench/run.sh builds and passes them)")
		return 2
	}
	if cfg.n < 1 {
		fmt.Fprintln(os.Stderr, "vxbench: -n must be at least 1")
		return 2
	}
	for _, p := range []*string{&cfg.vx, &cfg.floor, &cfg.spawnlog, &cfg.fakes} {
		if *p == "" {
			continue
		}
		abs, err := filepath.Abs(*p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "vxbench: %v\n", err)
			return 2
		}
		*p = abs
	}

	b := &bench{cfg: cfg, fakes: cfg.fakes}
	if cfg.filter != "" {
		re, err := regexp.Compile(cfg.filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "vxbench: -only: %v\n", err)
			return 2
		}
		b.filter = re
	}

	root, err := os.MkdirTemp("", "vxbench-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "vxbench: %v\n", err)
		return 1
	}
	// Worlds are created under a symlink-free root so paths in the reports match.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	b.root = root
	if cfg.keep {
		fmt.Fprintf(os.Stderr, "vxbench: worlds are kept under %s\n", root)
	} else {
		defer b.removeUnderRoot(root)
	}

	rep := report{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		GoVersion:   runtime.Version(),
		OS:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		CPUs:        runtime.NumCPU(),
		Runs:        cfg.n,
		Warmup:      cfg.warmup,
	}
	if out, err := exec.Command(cfg.vx, "--version").Output(); err == nil {
		rep.VXVersion = strings.TrimSpace(string(out))
	}

	// One world is shared by every scenario that leaves no state behind.
	var shared *world
	defer func() {
		if shared != nil {
			shared.teardown()
		}
	}()
	getShared := func() (*world, error) {
		if shared == nil {
			w, err := b.newWorld("shared")
			if err != nil {
				return nil, err
			}
			shared = w
		}
		return shared, nil
	}

	for _, sc := range b.scenarios() {
		if b.filter != nil && !b.filter.MatchString(sc.name) {
			continue
		}
		fmt.Fprintf(os.Stderr, "vxbench: %s\n", sc.name)
		rep.Results = append(rep.Results, b.runScenario(sc, getShared))
	}

	if b.filter == nil || b.filter.MatchString("package init") {
		fmt.Fprintln(os.Stderr, "vxbench: package init costs (GODEBUG=inittrace=1)")
		costs, total, err := traceInit(cfg.vx, 15)
		if err != nil {
			fmt.Fprintf(os.Stderr, "vxbench: %v\n", err)
		} else {
			rep.InitCosts, rep.InitTotalMS = costs, total
		}
	}

	printReport(os.Stdout, rep)
	if cfg.jsonOut != "" {
		data, err := json.MarshalIndent(rep, "", "  ")
		if err == nil {
			err = os.WriteFile(cfg.jsonOut, append(data, '\n'), 0o644)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "vxbench: writing %s: %v\n", cfg.jsonOut, err)
			return 1
		}
	}
	return 0
}

var slugPattern = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.Trim(slugPattern.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 24 {
		s = s[:24]
	}
	return s
}

func (b *bench) runScenario(sc scenario, getShared func() (*world, error)) result {
	res := result{Name: sc.name, Group: sc.group}
	switch {
	case sc.needs != "" && !onPath(sc.needs):
		res.Skipped = sc.needs + " is not on PATH"
		return res
	case sc.slow && !b.cfg.slow:
		res.Skipped = "slow, pass -slow"
		return res
	case sc.realHerdr && !b.cfg.realHerdr:
		res.Skipped = "touches the real herdr, pass -real-herdr"
		return res
	}

	n, warmup := b.cfg.n, b.cfg.warmup
	if sc.slow {
		n, warmup = min(n, 3), 1
	}

	var w *world
	var err error
	if sc.setup == nil && !sc.sentinel && sc.prepare == nil && sc.after == nil {
		w, err = getShared()
	} else {
		w, err = b.newWorld(slug(sc.name))
		if err == nil {
			defer w.teardown()
		}
	}
	if err != nil {
		res.Failure = err.Error()
		return res
	}
	if sc.setup != nil {
		if err := sc.setup(b, w); err != nil {
			res.Failure = "setup: " + err.Error()
			return res
		}
	}
	if sc.sentinel {
		if err := w.startSentinel(); err != nil {
			res.Failure = "starting the sentinel: " + err.Error()
			return res
		}
	}

	ok := map[int]bool{0: true}
	if len(sc.okExit) > 0 {
		ok = map[int]bool{}
		for _, c := range sc.okExit {
			ok[c] = true
		}
	}

	var walls, cpus []time.Duration
	for i := 0; i < warmup+n; i++ {
		s := sc.spec
		inv, err := b.step(sc, w, &s)
		if err != nil {
			res.Failure = err.Error()
			return res
		}
		if !ok[inv.exit] {
			if res.BadExits == nil {
				res.BadExits = map[int]int{}
			}
			res.BadExits[inv.exit]++
		}
		if i == 0 {
			res.FirstMS = ms(inv.wall)
		}
		if i >= warmup {
			walls = append(walls, inv.wall)
			cpus = append(cpus, inv.cpu)
		}
		if sc.after != nil {
			if err := sc.after(b, w, inv); err != nil {
				res.Failure = "after: " + err.Error()
				return res
			}
		}
	}
	res.Wall = summarize(walls)
	res.CPUMedMS = summarize(cpus).P50MS

	if !sc.noTrace && !b.cfg.noTrace {
		// Counts are exact on every run but durations are noisy, so the summary
		// kept is the traced run with the median total subprocess time.
		traces := tracedRuns
		if sc.slow {
			traces = 1
		}
		var runs []*spawnSummary
		for i := 0; i < traces; i++ {
			logPath := filepath.Join(w.dir, fmt.Sprintf("spawn-%s-%d.log", slug(sc.name), i))
			s := sc.spec
			s.traceLog = logPath
			inv, err := b.step(sc, w, &s)
			if err != nil {
				res.Failure = "traced run: " + err.Error()
				return res
			}
			spawns, err := readSpawns(logPath, inv.pid)
			if err != nil {
				res.Failure = "reading the spawn log: " + err.Error()
				return res
			}
			runs = append(runs, summarizeSpawns(spawns))
			if sc.after != nil {
				if err := sc.after(b, w, inv); err != nil {
					res.Failure = "after: " + err.Error()
					return res
				}
			}
		}
		sort.Slice(runs, func(i, j int) bool { return runs[i].TotalMS < runs[j].TotalMS })
		res.Spawns = runs[len(runs)/2]
		res.TracedRun = true
	}
	return res
}

// step runs the untimed prepare of a scenario, then the command itself.
func (b *bench) step(sc scenario, w *world, s *spec) (invocation, error) {
	if sc.prepare != nil {
		if err := sc.prepare(b, w, s); err != nil {
			return invocation{}, fmt.Errorf("prepare: %w", err)
		}
	}
	return b.invoke(w, *s)
}

func onPath(tool string) bool {
	_, err := exec.LookPath(tool)
	return err == nil
}
