package main

import "math"

// The comparison answers one question per scenario: is this run slower than
// the baseline by more than noise can explain?
//
// Timings on a laptop in use vary by about 2x from run to run, and the noise
// only ever adds time, so the two statistics that resist it are the minimum
// (the run the machine disturbed least) and the median. A real regression
// moves both. Load moves the median and leaves the minimum, or the reverse by
// luck. So a scenario is called slower only when:
//
//   - the minimum and the median both rose by more than the limit, and
//   - the median rose by at least the absolute floor, and
//   - the host itself has not drifted by more than the limit since the
//     baseline (the empty-program scenario tells), and
//   - the rise is still there after the scenario was run again (the caller's
//     part: see needsRecheck).
//
// The limit is the threshold, or the baseline's own spread (median over min)
// when that is larger: a change smaller than the scenario's jitter in the
// baseline cannot be told from it. The current run's spread does not widen the
// limit: a run that is noisy now is a reason to run it again, not to believe
// it. p95 and max are never used.

// thresholds are the knobs of the comparison.
type thresholds struct {
	// Percent is the smallest change, in percent, that can be a regression.
	Percent float64
	// AbsFloorMS is the smallest change of the median, in ms, that can be one:
	// a process that takes 8 ms moves by 1 ms for no reason at all.
	AbsFloorMS float64
	// Rechecks is how many times a scenario that looks slower, or that the
	// statistics disagree about, is run again before the verdict stands.
	Rechecks int
}

var defaultThresholds = thresholds{Percent: 20, AbsFloorMS: 1, Rechecks: 2}

// verdict is the outcome of comparing one scenario with its baseline.
type verdict string

const (
	verdictSame    verdict = "same"
	verdictFaster  verdict = "faster"
	verdictSlower  verdict = "SLOWER"
	verdictNoisy   verdict = "noisy"
	verdictInfo    verdict = "info"
	verdictNew     verdict = "new"
	verdictSkipped verdict = "skipped"
	verdictFailed  verdict = "FAILED"
)

// hostDriftScenario is the scenario that measures the machine alone: an empty
// Go program, so any change in it is the host, not vx.
const hostDriftScenario = "floor: empty Go program"

// comparison is one scenario set beside its baseline.
type comparison struct {
	Name    string
	Group   string
	Base    stats
	Cur     stats
	Verdict verdict
	// Note says why a verdict is not a plain yes or no.
	Note string

	DMinPct float64
	DMedPct float64
	DMedMS  float64

	// SpawnsBase and SpawnsCur are the subprocess counts, -1 when untraced.
	// They are exact, so any difference is a real change.
	SpawnsBase int
	SpawnsCur  int
}

// pctChange is the change from base to cur in percent.
func pctChange(base, cur float64) float64 {
	if base <= 0 {
		return 0
	}
	return (cur - base) / base * 100
}

// spread is how far the median sits above the minimum, in percent: the
// scenario's own jitter in one run.
func spread(s stats) float64 {
	return pctChange(s.MinMS, s.P50MS)
}

// classify compares a run with its baseline. hostDrift is the change of the
// empty-program minimum in percent (positive: the host is slower now).
func classify(base, cur stats, thr thresholds, hostDrift float64) (verdict, string) {
	if base.N == 0 || cur.N == 0 || base.MinMS <= 0 || base.P50MS <= 0 {
		return verdictSame, ""
	}
	dMin := pctChange(base.MinMS, cur.MinMS)
	dMed := pctChange(base.P50MS, cur.P50MS)
	dMedMS := cur.P50MS - base.P50MS

	limit := math.Max(thr.Percent, spread(base))
	note := ""
	if limit > thr.Percent {
		note = "limit widened to " + fmtAbsPct(limit) + " by the baseline's own spread"
	}
	if math.Abs(dMedMS) < thr.AbsFloorMS {
		return verdictSame, note
	}

	slowMin, slowMed := dMin > limit, dMed > limit
	fastMin, fastMed := dMin < -limit, dMed < -limit
	switch {
	case slowMin && slowMed && hostDrift > thr.Percent:
		return verdictNoisy, "the host is " + fmtAbsPct(hostDrift) + " slower than at baseline"
	case slowMin && slowMed:
		return verdictSlower, ""
	case slowMin || slowMed:
		return verdictNoisy, "min " + fmtPct(dMin) + " and median " + fmtPct(dMed) + " disagree"
	case fastMin && fastMed:
		return verdictFaster, ""
	}
	return verdictSame, note
}

// needsRecheck reports whether a verdict is worth running the scenario again
// for: noise only adds time, so another run can only clear it.
func needsRecheck(v verdict) bool {
	return v == verdictSlower || v == verdictNoisy
}

// hostDrift says how much slower the host is than at the baseline, in percent
// (negative: faster), from the empty-program scenario. Like a regression it
// needs the minimum and the median to agree and the median to have moved by at
// least the absolute floor, because a 3 ms program wobbles by a third of a
// millisecond for no reason. It is 0 when either report lacks the scenario or
// the host has not clearly moved.
func hostDrift(base, cur report, absFloorMS float64) float64 {
	b, okB := base.find(hostDriftScenario)
	c, okC := cur.find(hostDriftScenario)
	if !okB || !okC || !b.timed() || !c.timed() {
		return 0
	}
	if math.Abs(c.Wall.P50MS-b.Wall.P50MS) < absFloorMS {
		return 0
	}
	dMin, dMed := pctChange(b.Wall.MinMS, c.Wall.MinMS), pctChange(b.Wall.P50MS, c.Wall.P50MS)
	if dMin*dMed <= 0 {
		return 0
	}
	if dMin > 0 {
		return math.Min(dMin, dMed)
	}
	return math.Max(dMin, dMed)
}

// compareResult sets one result beside its baseline row (nil when the
// baseline has none).
func compareResult(base *result, cur result, thr thresholds, drift float64) comparison {
	c := comparison{Name: cur.Name, Group: cur.Group, Cur: cur.Wall, SpawnsBase: -1, SpawnsCur: -1}
	if cur.Spawns != nil {
		c.SpawnsCur = cur.Spawns.Count
	}
	switch {
	case cur.Failure != "":
		c.Verdict, c.Note = verdictFailed, cur.Failure
		return c
	case cur.Skipped != "":
		c.Verdict, c.Note = verdictSkipped, cur.Skipped
		return c
	case base == nil:
		c.Verdict = verdictNew
		return c
	case !base.timed():
		c.Verdict, c.Note = verdictNew, "the baseline has no timing for it"
		return c
	}
	c.Base = base.Wall
	if base.Spawns != nil {
		c.SpawnsBase = base.Spawns.Count
	}
	c.DMinPct = pctChange(base.Wall.MinMS, cur.Wall.MinMS)
	c.DMedPct = pctChange(base.Wall.P50MS, cur.Wall.P50MS)
	c.DMedMS = cur.Wall.P50MS - base.Wall.P50MS

	c.Verdict, c.Note = classify(base.Wall, cur.Wall, thr, drift)
	if cur.Informational || base.Informational {
		c.Verdict = verdictInfo
		if c.Note == "" {
			c.Note = "not judged: it measures the machine or a polling interval"
		}
	}
	return c
}

// compareReports sets every result of cur beside its baseline row.
func compareReports(base, cur report, thr thresholds) []comparison {
	drift := hostDrift(base, cur, thr.AbsFloorMS)
	out := make([]comparison, 0, len(cur.Results))
	for _, r := range cur.Results {
		var b *result
		if br, ok := base.find(r.Name); ok {
			b = &br
		}
		out = append(out, compareResult(b, r, thr, drift))
	}
	return out
}

// summary counts what a comparison found.
type summary struct {
	Total, Same, Faster, Slower, Noisy, Info, New, Skipped, Failed int
	// NotRun counts baseline scenarios the current report has no row for.
	NotRun int
	// GuardsFailed and GuardsHeld count the hard limits.
	GuardsFailed, GuardsHeld int
	// SpawnsMore and SpawnsFewer count scenarios whose subprocess count changed.
	SpawnsMore, SpawnsFewer int
	HostDrift               float64
}

func tally(base, cur report, cmps []comparison, thr thresholds) summary {
	s := summary{HostDrift: hostDrift(base, cur, thr.AbsFloorMS)}
	for _, c := range cmps {
		s.Total++
		switch c.Verdict {
		case verdictSame:
			s.Same++
		case verdictFaster:
			s.Faster++
		case verdictSlower:
			s.Slower++
		case verdictNoisy:
			s.Noisy++
		case verdictInfo:
			s.Info++
		case verdictNew:
			s.New++
		case verdictSkipped:
			s.Skipped++
		case verdictFailed:
			s.Failed++
		}
		switch {
		case c.SpawnsBase >= 0 && c.SpawnsCur > c.SpawnsBase:
			s.SpawnsMore++
		case c.SpawnsBase >= 0 && c.SpawnsCur >= 0 && c.SpawnsCur < c.SpawnsBase:
			s.SpawnsFewer++
		}
	}
	for _, r := range base.Results {
		if _, ok := cur.find(r.Name); !ok && r.timed() {
			s.NotRun++
		}
	}
	s.GuardsFailed, s.GuardsHeld = countGuards(cur)
	return s
}

// countGuards counts the guards of a report that failed and that held.
func countGuards(rep report) (failed, held int) {
	for _, r := range rep.Results {
		switch {
		case r.Guard == nil:
		case r.Guard.Failed:
			failed++
		default:
			held++
		}
	}
	return failed, held
}

// regressed says whether the run should fail a script: a scenario confirmed
// slower, a failed scenario, or a failed guard.
func (s summary) regressed() bool {
	return s.Slower > 0 || s.Failed > 0 || s.GuardsFailed > 0
}
