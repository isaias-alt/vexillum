package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// verdictMark is the verdict as Markdown shows it: the ones that need a
// reader's eye are bold.
func verdictMark(v verdict) string {
	switch v {
	case verdictSlower, verdictFailed:
		return "**" + string(v) + "**"
	case verdictFaster, verdictNoisy:
		return "_" + string(v) + "_"
	}
	return string(v)
}

// writeMarkdown writes the full report as Markdown: the header, one table per
// group (side by side with the baseline when cv is set), the guards, the
// subprocess breakdown and the package init costs.
func writeMarkdown(w io.Writer, rep report, cv *compareView) {
	title := "vx benchmark"
	if cv != nil {
		title = "vx benchmark: before and after"
	}
	fmt.Fprintf(w, "# %s\n\n", title)
	fmt.Fprintf(w, "- vx: `%s`\n- host: %s\n- runs per scenario: %d timed after %d warm-up, taken %s\n", rep.VX, rep.Host, rep.Runs, rep.Warmup, rep.GeneratedAt)
	if rep.LoadStart > 0 || rep.LoadEnd > 0 {
		fmt.Fprintf(w, "- load average: %.2f at start, %.2f at end (%d CPUs)\n", rep.LoadStart, rep.LoadEnd, rep.Host.CPUs)
	}
	if cv != nil {
		fmt.Fprintf(w, "- baseline: `%s`, taken %s on `%s` with vx `%s`\n", cv.basePath, cv.base.date(), cv.base.Host.Label, cv.base.VX)
		if !cv.base.Host.sameMachineAs(rep.Host) {
			fmt.Fprintf(w, "- **the baseline was taken on a different machine (%s): percentages are not meaningful**\n", cv.base.Host)
		}
		fmt.Fprintf(w, "- a scenario is called slower only when its min and its median both rose by more than %s (or its own spread, when that is larger) and the median rose by at least %s, after %d re-runs; the host moved %s since the baseline\n",
			fmtAbsPct(cv.thr.Percent), fmtMS(cv.thr.AbsFloorMS), cv.thr.Rechecks, fmtPct(cv.sum.HostDrift))
	}
	fmt.Fprintln(w)
	if cv != nil {
		fmt.Fprintf(w, "**%s**\n\n", strings.TrimSpace(summaryLine(palette{}, rep, cv)))
	}
	fmt.Fprintln(w, "Times are wall-clock milliseconds, from before fork to after wait. `first` is the first run, before warm-up has settled. `cpu` is the median user+sys time of the process and the children it waited for. `spawns` is the number of subprocesses one traced run started. Trust min and median; p95 and max move with whatever else the machine is doing.")

	by := groupedResults(rep.Results)
	var cmps map[string]comparison
	if cv != nil {
		cmps = cv.byName()
	}
	for _, g := range groupTitles {
		rs := by[g.key]
		if len(rs) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n## %s\n\n", g.title)
		if cv == nil {
			writeMarkdownPlain(w, rs)
		} else {
			writeMarkdownCompare(w, rs, cmps)
		}
	}

	writeMarkdownGuards(w, rep)

	var bad []string
	for _, r := range rep.Results {
		if len(r.BadExits) > 0 {
			bad = append(bad, fmt.Sprintf("%s: %v", r.Name, r.BadExits))
		}
	}
	if len(bad) > 0 {
		fmt.Fprintf(w, "\nUnexpected exit codes (count per code): %s\n", strings.Join(bad, "; "))
	}

	floor, okF := rep.find(hostDriftScenario)
	version, okV := rep.find("vx --version")
	if okF && okV && floor.timed() && version.timed() {
		fmt.Fprintf(w, "\nStartup cost of vx itself: `vx --version` median %s minus the empty Go program median %s = %s.\n",
			fmtMS(version.Wall.P50MS), fmtMS(floor.Wall.P50MS), fmtMS(version.Wall.P50MS-floor.Wall.P50MS))
	}

	if len(rep.InitCosts) > 0 {
		fmt.Fprintf(w, "\n## Package init (GODEBUG=inittrace=1, median of 15 runs)\n\nSum over %d packages: %s. Most expensive:\n\n", len(rep.InitCosts), fmtMS(rep.InitTotalMS))
		fmt.Fprintln(w, "| package | init (ms) |")
		fmt.Fprintln(w, "|---|---:|")
		for i, c := range rep.InitCosts {
			if i == 8 {
				break
			}
			fmt.Fprintf(w, "| %s | %s |\n", c.Package, fmtNum(c.MS))
		}
	}

	writeMarkdownSpawns(w, rep)
}

func writeMarkdownPlain(w io.Writer, rs []result) {
	fmt.Fprintln(w, "| scenario | first (ms) | min (ms) | median (ms) | p95 (ms) | max (ms) | cpu (ms) | spawns |")
	fmt.Fprintln(w, "|---|---:|---:|---:|---:|---:|---:|---:|")
	for _, r := range rs {
		switch {
		case r.Skipped != "":
			fmt.Fprintf(w, "| %s | skipped: %s | | | | | | |\n", r.Name, r.Skipped)
		case r.Failure != "":
			fmt.Fprintf(w, "| %s | FAILED: %s | | | | | | |\n", r.Name, oneLine(r.Failure))
		default:
			spawns := "-"
			if r.Spawns != nil {
				spawns = fmt.Sprintf("%d", r.Spawns.Count)
			}
			first, cpu := "-", "-"
			if r.FirstMS > 0 {
				first = fmtNum(r.FirstMS)
			}
			if r.CPUMedMS > 0 {
				cpu = fmtNum(r.CPUMedMS)
			}
			fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s | %s | %s |\n", r.Name,
				first, fmtNum(r.Wall.MinMS), fmtNum(r.Wall.P50MS), fmtNum(r.Wall.P95MS), fmtNum(r.Wall.MaxMS), cpu, spawns)
		}
	}
}

func writeMarkdownCompare(w io.Writer, rs []result, cmps map[string]comparison) {
	fmt.Fprintln(w, "| scenario | min before | min after | Δ min | median before | median after | Δ median | spawns | verdict |")
	fmt.Fprintln(w, "|---|---:|---:|---:|---:|---:|---:|---:|---|")
	for _, r := range rs {
		c := cmps[r.Name]
		switch {
		case r.Skipped != "":
			fmt.Fprintf(w, "| %s | skipped: %s | | | | | | | |\n", r.Name, r.Skipped)
		case r.Failure != "":
			fmt.Fprintf(w, "| %s | FAILED: %s | | | | | | | |\n", r.Name, oneLine(r.Failure))
		case c.Verdict == verdictNew:
			fmt.Fprintf(w, "| %s | - | %s | - | - | %s | - | %s | new |\n", r.Name, fmtNum(r.Wall.MinMS), fmtNum(r.Wall.P50MS), spawnsText(c))
		default:
			fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", r.Name,
				fmtNum(c.Base.MinMS), fmtNum(c.Cur.MinMS), fmtPct(c.DMinPct),
				fmtNum(c.Base.P50MS), fmtNum(c.Cur.P50MS), fmtPct(c.DMedPct),
				spawnsText(c), verdictMark(c.Verdict))
		}
	}
	for _, r := range rs {
		if c := cmps[r.Name]; c.Note != "" && c.Verdict != verdictSkipped && c.Verdict != verdictFailed && c.Verdict != verdictSame {
			fmt.Fprintf(w, "\n- %s (%s): %s\n", r.Name, c.Verdict, c.Note)
		}
	}
}

func spawnsText(c comparison) string {
	switch {
	case c.SpawnsCur < 0:
		return "-"
	case c.SpawnsBase < 0 || c.SpawnsBase == c.SpawnsCur:
		return fmt.Sprintf("%d", c.SpawnsCur)
	}
	return fmt.Sprintf("%d → %d", c.SpawnsBase, c.SpawnsCur)
}

func writeMarkdownGuards(w io.Writer, rep report) {
	var rows []string
	for _, r := range rep.Results {
		g := r.Guard
		if g == nil {
			continue
		}
		state := "held"
		if g.Failed {
			state = "**FAILED**"
		}
		rows = append(rows, fmt.Sprintf("| %s | %s | %s | %s | %s | %s |", r.Name, g.Stat, fmtNum(g.ObservedMS), fmtNum(g.LimitMS), state, g.Why))
	}
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(w, "\n## Guards\n\nA guard is a hard limit, checked on every run, whatever the baseline says.\n\n")
	fmt.Fprintln(w, "| scenario | statistic | observed (ms) | limit (ms) | | what a failure means |")
	fmt.Fprintln(w, "|---|---|---:|---:|---|---|")
	for _, row := range rows {
		fmt.Fprintln(w, row)
	}
}

func writeMarkdownSpawns(w io.Writer, rep report) {
	var traced []result
	for _, r := range rep.Results {
		if r.Spawns != nil && r.Spawns.Count > 0 {
			traced = append(traced, r)
		}
	}
	if len(traced) == 0 {
		return
	}
	fmt.Fprintf(w, "\n## Subprocesses per command (one traced run)\n\nDurations are the time the tool itself took, measured inside the shim; the shim adds a little process start on top.\n")
	for _, r := range traced {
		fmt.Fprintf(w, "\n### %s: %d spawns, %s in subprocesses\n\n", r.Name, r.Spawns.Count, fmtMS(r.Spawns.TotalMS))
		fmt.Fprintln(w, "| call | count | total (ms) |")
		fmt.Fprintln(w, "|---|---:|---:|")
		calls := append([]callStat(nil), r.Spawns.Calls...)
		sort.SliceStable(calls, func(i, j int) bool { return calls[i].TotalMS > calls[j].TotalMS })
		for _, c := range calls {
			fmt.Fprintf(w, "| %s | %d | %s |\n", c.Call, c.Count, fmtNum(c.TotalMS))
		}
	}
}
