// Command vxbench times real vx invocations end to end and reports min, median
// and p95 wall time, the CPU time vx used, and the subprocesses it starts. Given
// a baseline (-compare) it also says, scenario by scenario, whether anything got
// slower by more than noise can explain.
//
// It never touches the maintainer's state: every scenario runs in a world of
// its own under one temporary directory, with a temporary HOME and a scratch
// git project, and the dispatch, land, strike and wake scenarios use a fake
// herdr and a fake claude from bench/fakes, so no model, terminal or herdr
// server is needed. See bench/README.md and bench/run.sh.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// tracedRuns is how many times a command is run under the spawn tracer.
const tracedRuns = 3

// Exit codes: 0 all well, 1 a regression, a failed guard or a failed scenario,
// 2 a bad command line.
const (
	exitOK         = 0
	exitRegression = 1
	exitUsage      = 2
)

type config struct {
	vx, floor, spawnlog, fakes string
	n, warmup                  int
	filter                     string
	slow, realHerdr, realTools bool
	noTrace, keep              bool

	jsonOut, mdOut string
	label          string
	vxCommit       string
	vxDirty        bool

	compare, against string
	thr              thresholds

	color   string
	verbose bool
}

type bench struct {
	cfg         config
	root        string // the temporary directory every world lives in
	fakes       string
	filter      *regexp.Regexp
	shimN       int
	hermeticDir string
}

func main() {
	os.Exit(run())
}

func usageErr(format string, args ...any) int {
	fmt.Fprintf(os.Stderr, "vxbench: "+format+"\n", args...)
	return exitUsage
}

func parseFlags() (config, error) {
	var cfg config
	fs := flag.NewFlagSet("vxbench", flag.ContinueOnError)
	fs.StringVar(&cfg.vx, "vx", "", "path to the vx binary to time (bench/run.sh builds and passes it)")
	fs.StringVar(&cfg.floor, "floor", "", "path to the empty Go program that sets the process-start floor")
	fs.StringVar(&cfg.spawnlog, "spawnlog", "", "path to the spawnlog shim binary (required unless -no-trace)")
	fs.StringVar(&cfg.fakes, "fakes", "bench/fakes", "directory holding the fake herdr, claude, pi, opencode and gh")
	fs.IntVar(&cfg.n, "n", 30, "timed runs per scenario")
	fs.IntVar(&cfg.warmup, "warmup", 3, "untimed warm-up runs before the timed ones")
	fs.StringVar(&cfg.filter, "only", "", "run only the scenarios whose name matches this regexp")
	fs.BoolVar(&cfg.slow, "slow", false, "also run the scenarios that wait out a 15s timeout (3 runs each)")
	fs.BoolVar(&cfg.realHerdr, "real-herdr", false, "also time read-only calls to the maintainer's real herdr server")
	fs.BoolVar(&cfg.realTools, "real-tools", false, "also time this machine's real pi, opencode, gh and herdr, and a doctor that runs them (machine specific)")
	fs.BoolVar(&cfg.noTrace, "no-trace", false, "skip the traced run that counts subprocesses")
	fs.BoolVar(&cfg.keep, "keep", false, "keep the temporary worlds instead of deleting them")

	fs.StringVar(&cfg.jsonOut, "json", "", "also write the full report to this JSON file (this is the baseline format)")
	fs.StringVar(&cfg.mdOut, "md", "", "also write the report as Markdown to this file (side by side with the baseline when comparing)")
	fs.StringVar(&cfg.label, "label", "", "name of this machine in the report (default: its host name)")
	fs.StringVar(&cfg.vxCommit, "vx-commit", "", "commit the vx under test was built from, for the report (bench/run.sh passes it)")
	fs.BoolVar(&cfg.vxDirty, "vx-dirty", false, "the vx under test was built from a working tree with uncommitted changes")

	fs.StringVar(&cfg.compare, "compare", "", "compare against this baseline JSON file and flag regressions")
	fs.StringVar(&cfg.against, "against", "", "with -compare: compare the baseline with this existing report instead of running anything")
	fs.Float64Var(&cfg.thr.Percent, "threshold", defaultThresholds.Percent, "smallest change in percent that can be a regression")
	fs.Float64Var(&cfg.thr.AbsFloorMS, "abs-floor", defaultThresholds.AbsFloorMS, "smallest change of the median in ms that can be a regression")
	fs.IntVar(&cfg.thr.Rechecks, "recheck", defaultThresholds.Rechecks, "times a scenario that looks slower is run again before the verdict stands")

	fs.StringVar(&cfg.color, "color", "auto", "paint the terminal report: auto (a terminal and no NO_COLOR), always or never")
	fs.BoolVar(&cfg.verbose, "v", false, "also print the subprocess breakdown and package init costs")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), `Usage: bench/run.sh [flags]

Times real vx invocations in throwaway worlds and reports min, median and p95 of
each scenario. With -compare it also says which scenarios got slower than a
baseline by more than noise explains, and exits 1 if any did. bench/run.sh
builds vx and the helper programs and passes the first four flags itself.

Flags:
`)
		fs.PrintDefaults()
		fmt.Fprint(fs.Output(), `
Exit status: 0 all well; 1 a scenario regressed, a guard or a scenario failed;
2 a bad command line. See bench/README.md.
`)
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		return cfg, err
	}
	if fs.NArg() > 0 {
		return cfg, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	return cfg, nil
}

func run() int {
	cfg, err := parseFlags()
	if err != nil {
		if err == flag.ErrHelp {
			return exitOK
		}
		return usageErr("%v", err)
	}
	switch cfg.color {
	case "auto", "always", "never":
	default:
		return usageErr("-color must be auto, always or never")
	}
	p := palette{on: colorEnabled(cfg.color, os.Stdout, os.Getenv)}

	if cfg.against != "" {
		if cfg.compare == "" {
			return usageErr("-against needs -compare")
		}
		return diffReports(cfg, p)
	}

	if cfg.vx == "" || cfg.floor == "" || (cfg.spawnlog == "" && !cfg.noTrace) {
		return usageErr("-vx, -floor and -spawnlog are required (bench/run.sh builds and passes them)")
	}
	if cfg.n < 1 {
		return usageErr("-n must be at least 1")
	}
	for _, path := range []*string{&cfg.vx, &cfg.floor, &cfg.spawnlog, &cfg.fakes} {
		if *path == "" {
			continue
		}
		abs, err := filepath.Abs(*path)
		if err != nil {
			return usageErr("%v", err)
		}
		*path = abs
	}

	b := &bench{cfg: cfg, fakes: cfg.fakes}
	if cfg.filter != "" {
		re, err := regexp.Compile(cfg.filter)
		if err != nil {
			return usageErr("-only: %v", err)
		}
		b.filter = re
	}

	// Read the baseline before spending minutes on a run that cannot be compared.
	var base *report
	if cfg.compare != "" {
		rep, err := readReport(cfg.compare)
		if err != nil {
			return usageErr("%v", err)
		}
		base = &rep
	}

	root, err := os.MkdirTemp("", "vxbench-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "vxbench: %v\n", err)
		return exitRegression
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
		Schema:      schemaVersion,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Host:        detectHost(cfg.label),
		Runs:        cfg.n,
		Warmup:      cfg.warmup,
	}
	if out, err := exec.Command(cfg.vx, "--version").Output(); err == nil {
		rep.VX = parseVXVersion(string(out), cfg.vxCommit, cfg.vxDirty)
	}
	rep.LoadStart, _ = loadAverage()

	rep.Results = b.runAll(base, cfg.thr)

	if b.filter == nil || b.filter.MatchString("package init") {
		fmt.Fprintln(os.Stderr, "vxbench: package init costs (GODEBUG=inittrace=1)")
		costs, total, err := traceInit(cfg.vx, 15)
		if err != nil {
			fmt.Fprintf(os.Stderr, "vxbench: %v\n", err)
		} else {
			rep.InitCosts, rep.InitTotalMS = costs, total
		}
	}

	rep.LoadEnd, _ = loadAverage()
	return finish(cfg, p, rep, base)
}

// finish prints the report, writes the files asked for and returns the exit
// code.
func finish(cfg config, p palette, rep report, base *report) int {
	var cv *compareView
	if base != nil {
		cmps := compareReports(*base, rep, cfg.thr)
		cv = &compareView{base: *base, basePath: cfg.compare, cmps: cmps, sum: tally(*base, rep, cmps, cfg.thr), thr: cfg.thr}
	}
	fmt.Fprintln(os.Stderr)
	printTerminal(os.Stdout, p, rep, cv, cfg.verbose)

	code := exitOK
	if cv != nil && cv.sum.regressed() {
		code = exitRegression
	}
	if cv == nil {
		if failed, _ := countGuards(rep); failed > 0 || hasFailure(rep) {
			code = exitRegression
		}
	}
	if cfg.jsonOut != "" {
		if err := writeReport(cfg.jsonOut, rep); err != nil {
			fmt.Fprintf(os.Stderr, "vxbench: %v\n", err)
			return exitRegression
		}
	}
	if cfg.mdOut != "" {
		if err := writeMarkdownFile(cfg.mdOut, rep, cv); err != nil {
			fmt.Fprintf(os.Stderr, "vxbench: %v\n", err)
			return exitRegression
		}
	}
	return code
}

func hasFailure(rep report) bool {
	for _, r := range rep.Results {
		if r.Failure != "" {
			return true
		}
	}
	return false
}

func writeMarkdownFile(path string, rep report, cv *compareView) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	writeMarkdown(f, rep, cv)
	return f.Close()
}

// diffReports compares two report files without running anything: the
// baseline given to -compare against the one given to -against.
func diffReports(cfg config, p palette) int {
	base, err := readReport(cfg.compare)
	if err != nil {
		return usageErr("%v", err)
	}
	cur, err := readReport(cfg.against)
	if err != nil {
		return usageErr("%v", err)
	}
	cmps := compareReports(base, cur, cfg.thr)
	cv := &compareView{base: base, basePath: cfg.compare, cmps: cmps, sum: tally(base, cur, cmps, cfg.thr), thr: cfg.thr}
	printTerminal(os.Stdout, p, cur, cv, cfg.verbose)
	if cfg.mdOut != "" {
		if err := writeMarkdownFile(cfg.mdOut, cur, cv); err != nil {
			fmt.Fprintf(os.Stderr, "vxbench: %v\n", err)
			return exitRegression
		}
	}
	if cv.sum.regressed() {
		return exitRegression
	}
	return exitOK
}

var vxVersionPattern = regexp.MustCompile(`^(.*?) \(([0-9a-f]{7,40})(?:, [^)]*)?\)$`)

// parseVXVersion reads `vx --version`. A build that carries its own identity
// ("vx dev (a1b2c3d, 2026-10-05, dirty)") is taken at its word; for one that
// does not, the commit and dirty flag come from the command line.
func parseVXVersion(out, commit string, dirty bool) vxInfo {
	line := strings.TrimSpace(out)
	if m := vxVersionPattern.FindStringSubmatch(line); m != nil {
		return vxInfo{Version: m[1], Commit: m[2], Dirty: strings.Contains(line, ", dirty")}
	}
	return vxInfo{Version: line, Commit: commit, Dirty: dirty && commit != ""}
}

// runAll runs the scenarios that match, in order, and returns their results.
// With a baseline it runs a scenario again, up to thr.Rechecks times, when
// it looks slower than the baseline: noise only adds time, so the attempt with
// the lowest median stands.
func (b *bench) runAll(base *report, thr thresholds) []result {
	var shared *world
	defer func() {
		if shared != nil {
			shared.teardown()
		}
	}()
	// One world is shared by every scenario that leaves no state behind.
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

	var selected []scenario
	for _, sc := range b.scenarios() {
		if b.filter == nil || b.filter.MatchString(sc.name) {
			selected = append(selected, sc)
		}
	}

	var results []result
	drift := 0.0
	for i, sc := range selected {
		fmt.Fprintf(os.Stderr, "vxbench: [%d/%d] %s\n", i+1, len(selected), sc.name)
		if sc.multi != nil {
			results = append(results, b.runMulti(sc)...)
			continue
		}
		res := b.runScenario(sc, getShared)
		if base != nil && !sc.informational && !sc.noRecheck {
			res = b.recheck(sc, res, base, thr, drift, getShared)
		}
		if res.Name == hostDriftScenario && base != nil {
			drift = hostDrift(*base, report{Results: []result{res}}, thr.AbsFloorMS)
		}
		results = append(results, res)
	}
	return results
}

// runMulti runs a scenario that produces several rows, or none when it is
// skipped by a flag.
func (b *bench) runMulti(sc scenario) []result {
	rows := sc.multi(b)
	for i := range rows {
		rows[i].Group = sc.group
	}
	return rows
}

// recheck runs a scenario that looks slower than its baseline again and keeps
// the attempt with the lowest median.
func (b *bench) recheck(sc scenario, res result, base *report, thr thresholds, drift float64, getShared func() (*world, error)) result {
	row, ok := base.find(res.Name)
	if !ok {
		return res
	}
	best, attempts := res, 1
	for attempts <= thr.Rechecks {
		if c := compareResult(&row, best, thr, drift); !needsRecheck(c.Verdict) {
			break
		}
		fmt.Fprintf(os.Stderr, "vxbench:   median %s against the baseline: running it again to rule out noise\n", fmtPct(pctChange(row.Wall.P50MS, best.Wall.P50MS)))
		next := b.runScenario(sc, getShared)
		attempts++
		if next.timed() && next.Wall.P50MS < best.Wall.P50MS {
			best = next
		}
	}
	if attempts > 1 {
		best.Attempts = attempts
	}
	return best
}

var slugPattern = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.Trim(slugPattern.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if len(s) > 24 {
		s = s[:24]
	}
	return s
}

// skipReason says why a scenario is not run, or "" when it is.
func (b *bench) skipReason(sc scenario) string {
	switch {
	case sc.needs != "" && !onPath(sc.needs):
		return sc.needs + " is not on PATH"
	case sc.slow && !b.cfg.slow:
		return "slow, pass -slow"
	case sc.realHerdr && !b.cfg.realHerdr:
		return "touches the real herdr, pass -real-herdr"
	case sc.realTools && !b.cfg.realTools:
		return "machine specific, pass -real-tools"
	}
	return ""
}

func (b *bench) runScenario(sc scenario, getShared func() (*world, error)) result {
	res := result{Name: sc.name, Group: sc.group, Informational: sc.informational}
	if reason := b.skipReason(sc); reason != "" {
		res.Skipped = reason
		return res
	}

	n, warmup := b.cfg.n, b.cfg.warmup
	if sc.maxRuns > 0 {
		n = min(n, sc.maxRuns)
	}
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
		// A guard that three runs already break is not worth the rest: a
		// dispatch that waits 15 s would otherwise take minutes to fail.
		if sc.guard != nil && len(walls) >= 3 && sc.guard.evaluate(summarize(walls)).Failed {
			break
		}
	}
	res.Wall = summarize(walls)
	res.CPUMedMS = summarize(cpus).P50MS
	if sc.guard != nil {
		res.Guard = sc.guard.evaluate(res.Wall)
	}

	if !sc.noTrace && !b.cfg.noTrace && (res.Guard == nil || !res.Guard.Failed) {
		spawns, err := b.traceScenario(sc, w)
		if err != nil {
			res.Failure = err.Error()
			return res
		}
		res.Spawns = spawns
	}
	return res
}

// traceScenario runs the scenario under the spawn tracer and returns the
// subprocesses it started. Counts are exact on every run but durations are
// noisy, so the summary kept is the traced run with the median total
// subprocess time.
func (b *bench) traceScenario(sc scenario, w *world) (*spawnSummary, error) {
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
			return nil, fmt.Errorf("traced run: %w", err)
		}
		spawns, err := readSpawns(logPath, inv.pid)
		if err != nil {
			return nil, fmt.Errorf("reading the spawn log: %w", err)
		}
		runs = append(runs, summarizeSpawns(spawns))
		if sc.after != nil {
			if err := sc.after(b, w, inv); err != nil {
				return nil, fmt.Errorf("after: %w", err)
			}
		}
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].TotalMS < runs[j].TotalMS })
	return runs[len(runs)/2], nil
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
