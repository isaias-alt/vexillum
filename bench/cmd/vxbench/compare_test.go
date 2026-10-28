package main

import (
	"math"
	"testing"
)

func st(min, median float64) stats {
	return stats{N: 30, MinMS: min, P50MS: median, P95MS: median * 2, MaxMS: median * 3}
}

func TestPctChange(t *testing.T) {
	cases := []struct {
		base, cur, want float64
	}{
		{100, 150, 50},
		{100, 50, -50},
		{8, 8, 0},
		{0, 5, 0}, // no baseline time: no percentage
	}
	for _, c := range cases {
		if got := pctChange(c.base, c.cur); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("pctChange(%v, %v) = %v, want %v", c.base, c.cur, got, c.want)
		}
	}
}

func TestClassify(t *testing.T) {
	thr := thresholds{Percent: 20, AbsFloorMS: 1, Rechecks: 2}
	cases := []struct {
		name      string
		base, cur stats
		drift     float64
		want      verdict
	}{
		{"identical", st(8, 8.4), st(8, 8.4), 0, verdictSame},
		{"min and median both up 50 percent", st(100, 105), st(150, 158), 0, verdictSlower},
		{"min and median both down 50 percent", st(100, 105), st(50, 52), 0, verdictFaster},
		{"median up and min flat is load, not a regression", st(100, 105), st(101, 160), 0, verdictNoisy},
		{"median up and min only a little", st(100, 105), st(110, 140), 0, verdictNoisy},
		{"min up and median flat disagree too", st(100, 150), st(160, 160), 0, verdictNoisy},
		{"a noisy run now does not widen the limit", st(100, 105), st(130, 200), 0, verdictSlower},
		{"a rise below the percentage threshold", st(100, 105), st(110, 115), 0, verdictSame},
		{"a big percentage of almost nothing is under the absolute floor", st(2, 2.1), st(3, 3.05), 0, verdictSame},
		{"a faster host is no excuse", st(100, 105), st(150, 158), -30, verdictSlower},
		{"a host that drifted slower makes a regression noisy", st(100, 105), st(150, 158), 30, verdictNoisy},
		{"a host drift under the threshold does not", st(100, 105), st(150, 158), 10, verdictSlower},
		{"the baseline's own spread widens the limit", st(10, 15), st(12.5, 18.5), 0, verdictSame},
		{"... but not beyond a real regression", st(10, 15), st(20, 30), 0, verdictSlower},
		{"no baseline time", stats{}, st(1, 2), 0, verdictSame},
		{"no current time", st(1, 2), stats{}, 0, verdictSame},
	}
	for _, c := range cases {
		got, _ := classify(c.base, c.cur, thr, c.drift)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestClassifyIgnoresP95AndMax(t *testing.T) {
	base, cur := st(100, 105), st(100, 105)
	cur.P95MS, cur.MaxMS = 900, 5000 // one bad run on a busy laptop
	if got, _ := classify(base, cur, defaultThresholds, 0); got != verdictSame {
		t.Errorf("a heavy tail alone is not a regression, got %q", got)
	}
}

func TestClassifyExplainsWhyItIsNotSure(t *testing.T) {
	_, note := classify(st(100, 105), st(101, 160), defaultThresholds, 0)
	if note == "" {
		t.Error("a noisy verdict must say why")
	}
	_, note = classify(st(100, 105), st(150, 158), defaultThresholds, 40)
	if note == "" {
		t.Error("a verdict demoted because of host drift must say so")
	}
	if _, note := classify(st(10, 15), st(12.5, 18.5), defaultThresholds, 0); note == "" {
		t.Error("a limit widened by the baseline's spread must say so")
	}
}

func TestNeedsRecheckOnlyForDoubt(t *testing.T) {
	for v, want := range map[verdict]bool{
		verdictSlower: true, verdictNoisy: true,
		verdictSame: false, verdictFaster: false, verdictInfo: false, verdictNew: false,
	} {
		if got := needsRecheck(v); got != want {
			t.Errorf("needsRecheck(%q) = %v, want %v", v, got, want)
		}
	}
}

func timedResult(name string, s stats, spawns int) result {
	r := result{Name: name, Group: "cheap", Wall: s}
	if spawns >= 0 {
		r.Spawns = &spawnSummary{Count: spawns}
	}
	return r
}

func TestCompareResultKinds(t *testing.T) {
	base := timedResult("vx status", st(8, 8.4), 1)
	thr := defaultThresholds

	t.Run("new scenario", func(t *testing.T) {
		c := compareResult(nil, timedResult("vx new", st(8, 9), 0), thr, 0)
		if c.Verdict != verdictNew {
			t.Errorf("got %q", c.Verdict)
		}
	})
	t.Run("skipped", func(t *testing.T) {
		c := compareResult(&base, result{Name: "vx status", Skipped: "slow, pass -slow"}, thr, 0)
		if c.Verdict != verdictSkipped || c.Note == "" {
			t.Errorf("got %q (%q)", c.Verdict, c.Note)
		}
	})
	t.Run("failed", func(t *testing.T) {
		c := compareResult(&base, result{Name: "vx status", Failure: "boom"}, thr, 0)
		if c.Verdict != verdictFailed {
			t.Errorf("got %q", c.Verdict)
		}
	})
	t.Run("baseline row without timing", func(t *testing.T) {
		skippedBase := result{Name: "vx status", Skipped: "slow"}
		c := compareResult(&skippedBase, timedResult("vx status", st(8, 9), 0), thr, 0)
		if c.Verdict != verdictNew {
			t.Errorf("got %q", c.Verdict)
		}
	})
	t.Run("informational rows are shown, not judged", func(t *testing.T) {
		b := timedResult("wake: await poll", st(100, 2500), -1)
		cur := timedResult("wake: await poll", st(900, 4000), -1)
		cur.Informational = true
		c := compareResult(&b, cur, thr, 0)
		if c.Verdict != verdictInfo {
			t.Errorf("got %q", c.Verdict)
		}
		if c.DMedPct == 0 {
			t.Error("the change is still reported")
		}
	})
	t.Run("percentages and spawn counts", func(t *testing.T) {
		c := compareResult(&base, timedResult("vx status", st(12, 12.6), 3), thr, 0)
		if math.Abs(c.DMinPct-50) > 1e-9 || math.Abs(c.DMedPct-50) > 1e-9 || c.SpawnsBase != 1 || c.SpawnsCur != 3 {
			t.Errorf("unexpected comparison: %+v", c)
		}
	})
}

func reportOf(results ...result) report {
	return report{Schema: schemaVersion, Runs: 30, Warmup: 3, Results: results}
}

func TestHostDrift(t *testing.T) {
	floor := func(min, median float64) report {
		return reportOf(timedResult(hostDriftScenario, st(min, median), -1))
	}
	const absFloor = 1.0
	cases := []struct {
		name      string
		base, cur report
		want      float64
	}{
		{"slower host: the smaller of the two changes", floor(10, 11), floor(13, 16), 30},
		{"faster host", floor(10, 11), floor(7, 8), -27.272727272727273},
		{"min and median disagree: no clear drift", floor(10, 11), floor(10, 20), 0},
		{"a wobble under the absolute floor", floor(3, 3.5), floor(3.7, 3.9), 0},
		{"no scenario in the current report", floor(4, 4.2), reportOf(timedResult("other", st(1, 2), 0)), 0},
		{"no scenario in the baseline", reportOf(timedResult("other", st(1, 2), 0)), floor(4, 4.2), 0},
	}
	for _, c := range cases {
		if got := hostDrift(c.base, c.cur, absFloor); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: drift = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCompareReportsAndTally(t *testing.T) {
	base := reportOf(
		timedResult(hostDriftScenario, st(4, 4.2), -1),
		timedResult("a", st(100, 105), 2),
		timedResult("b", st(100, 105), 2),
		timedResult("c", st(100, 105), 2),
		timedResult("gone", st(100, 105), 2),
	)
	cur := reportOf(
		timedResult(hostDriftScenario, st(4, 4.2), -1),
		timedResult("a", st(100, 105), 2),  // same
		timedResult("b", st(150, 158), 4),  // slower, with more spawns
		timedResult("c", st(50, 52), 1),    // faster, with fewer
		timedResult("fresh", st(1, 2), 0),  // not in the baseline
		result{Name: "slow", Skipped: "x"}, // skipped
	)
	cur.Results[1].Guard = &guardResult{}
	cur.Results[2].Guard = &guardResult{Failed: true}

	cmps := compareReports(base, cur, defaultThresholds)
	if len(cmps) != len(cur.Results) {
		t.Fatalf("one comparison per current result, got %d", len(cmps))
	}
	s := tally(base, cur, cmps, defaultThresholds)
	if s.Total != 6 || s.Same != 2 || s.Slower != 1 || s.Faster != 1 || s.New != 1 || s.Skipped != 1 {
		t.Errorf("unexpected tally: %+v", s)
	}
	if s.SpawnsMore != 1 || s.SpawnsFewer != 1 {
		t.Errorf("spawn changes not counted: %+v", s)
	}
	if s.NotRun != 1 {
		t.Errorf("the baseline's 'gone' scenario was not run: %+v", s)
	}
	if s.GuardsHeld != 1 || s.GuardsFailed != 1 {
		t.Errorf("guards not counted: %+v", s)
	}
	if !s.regressed() {
		t.Error("a slower scenario and a failed guard must fail the run")
	}
}

func TestRegressedOnlyForConfirmedProblems(t *testing.T) {
	for name, s := range map[string]summary{
		"clean":          {Total: 5, Same: 3, Faster: 2},
		"noisy is doubt": {Total: 5, Same: 4, Noisy: 1},
		"new and skipped": {
			Total: 5, Same: 3, New: 1, Skipped: 1,
		},
	} {
		if s.regressed() {
			t.Errorf("%s: must not fail the run", name)
		}
	}
	for name, s := range map[string]summary{
		"slower":       {Slower: 1},
		"failed":       {Failed: 1},
		"failed guard": {GuardsFailed: 1},
	} {
		if !s.regressed() {
			t.Errorf("%s: must fail the run", name)
		}
	}
}

func TestGuardEvaluate(t *testing.T) {
	g := guard{Stat: "median", LimitMS: 5000, Why: "no 15 s waits"}
	if r := g.evaluate(st(70, 77)); r.Failed || r.ObservedMS != 77 {
		t.Errorf("a fast dispatch holds the guard: %+v", r)
	}
	if r := g.evaluate(st(15094, 15097)); !r.Failed {
		t.Errorf("a dispatch that waits out the 15 s probe breaks the guard: %+v", r)
	}
	byMax := guard{Stat: "max", LimitMS: 100}
	if r := byMax.evaluate(stats{N: 3, MinMS: 1, P50MS: 2, MaxMS: 150}); !r.Failed || r.ObservedMS != 150 {
		t.Errorf("a max guard looks at the max: %+v", r)
	}
}
