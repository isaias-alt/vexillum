package main

import (
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
)

// groupTitles lists the scenario groups in the order a report shows them.
var groupTitles = []struct{ key, title string }{
	{"startup", "Startup (process start, no work)"},
	{"cheap", "Cheap commands"},
	{"doctor", "doctor, with fake pi and opencode on PATH"},
	{"forum", "Forum commands that need no browser"},
	{"camp", "Soldier lifecycle, against a fake herdr and claude"},
	{"wake", "Wake latency: soldier finishes to Stop hook returns"},
	{"tools", "External tools, for scale"},
	{"real", "This machine's real tools (machine specific)"},
}

// compareView is a run set beside the baseline it was compared with.
type compareView struct {
	base     report
	basePath string
	cmps     []comparison
	sum      summary
	thr      thresholds
}

func (cv *compareView) byName() map[string]comparison {
	m := make(map[string]comparison, len(cv.cmps))
	for _, c := range cv.cmps {
		m[c.Name] = c
	}
	return m
}

// groupedResults splits results by group, in the order of groupTitles.
func groupedResults(results []result) map[string][]result {
	by := map[string][]result{}
	for _, r := range results {
		by[r.Group] = append(by[r.Group], r)
	}
	return by
}

// printTerminal writes the report for a person at a terminal: a header, one
// aligned table per group, the notes that matter and a one-line summary. The
// subprocess breakdown and package init costs are only in the Markdown report
// unless verbose.
func printTerminal(w io.Writer, p palette, rep report, cv *compareView, verbose bool) {
	printHeader(w, p, rep, cv)

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
		if reasons, all := allSkipped(rs); all {
			fmt.Fprintf(w, "\n%s  %s\n", p.paint(styleBold, g.title), p.paint(styleDim, "(skipped: "+reasons+")"))
			continue
		}
		fmt.Fprintf(w, "\n%s  %s\n", p.paint(styleBold, g.title), p.paint(styleDim, "(ms)"))
		var timed []result
		for _, r := range rs {
			if r.timed() {
				timed = append(timed, r)
			}
		}
		if len(timed) > 0 {
			if cv == nil {
				cols, rows := dropEmptyColumns(plainColumns, plainRows(p, timed))
				writeTable(w, p, "  ", cols, rows)
			} else {
				writeTable(w, p, "  ", compareColumns, compareRows(timed, cmps))
			}
		}
		for _, r := range rs {
			switch {
			case r.Skipped != "":
				fmt.Fprintf(w, "  %s\n", p.paint(styleDim, "skipped  "+r.Name+"  ("+r.Skipped+")"))
			case r.Failure != "":
				fmt.Fprintf(w, "  %s\n", p.paint(styleRed, "FAILED   "+r.Name+"  "+oneLine(r.Failure)))
			}
		}
	}

	printNotes(w, p, rep, cv)
	if verbose {
		printVerbose(w, p, rep)
	}
	fmt.Fprintf(w, "\n%s\n", summaryLine(p, rep, cv))
}

// allSkipped reports whether every result of a group was skipped, and why:
// the reasons, each once, in the order they come.
func allSkipped(rs []result) (reasons string, all bool) {
	var list []string
	for _, r := range rs {
		if r.Skipped == "" {
			return "", false
		}
		if !slices.Contains(list, r.Skipped) {
			list = append(list, r.Skipped)
		}
	}
	return strings.Join(list, "; "), len(rs) > 0
}

func printHeader(w io.Writer, p palette, rep report, cv *compareView) {
	fmt.Fprintln(w, p.paint(styleBold, "vx benchmark"))
	row := func(label, value string) {
		fmt.Fprintf(w, "  %s  %s\n", p.paint(styleDim, fmt.Sprintf("%-7s", label)), value)
	}
	row("vx", rep.VX.String())
	row("host", rep.Host.String())
	row("runs", fmt.Sprintf("%d timed after %d warm-up, taken %s", rep.Runs, rep.Warmup, rep.GeneratedAt))
	if rep.LoadStart > 0 || rep.LoadEnd > 0 {
		load := fmt.Sprintf("%.2f at start, %.2f at end (%d CPUs)", rep.LoadStart, rep.LoadEnd, rep.Host.CPUs)
		if rep.Host.busy(rep.LoadStart) || rep.Host.busy(rep.LoadEnd) {
			load = p.paint(styleYellow, load+": a busy machine, expect noisy timings")
		}
		row("load", load)
	}
	if cv != nil {
		row("against", fmt.Sprintf("%s (%s, %s, %s)", cv.basePath, cv.base.date(), cv.base.VX, cv.base.Host.Label))
		if !cv.base.Host.sameMachineAs(rep.Host) {
			fmt.Fprintf(w, "  %s\n", p.paint(styleYellow, "the baseline was taken on a different machine ("+cv.base.Host.String()+"): percentages are not meaningful"))
		}
	}
}

var plainColumns = []column{
	{"scenario", false},
	{"first", true}, {"min", true}, {"median", true}, {"p95", true}, {"max", true},
	{"cpu", true}, {"spawns", true},
}

func plainRows(p palette, rs []result) [][]cell {
	rows := make([][]cell, 0, len(rs))
	for _, r := range rs {
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
		rows = append(rows, []cell{
			plain(r.Name),
			{first, styleDim},
			plain(fmtNum(r.Wall.MinMS)),
			{fmtNum(r.Wall.P50MS), styleBold},
			{fmtNum(r.Wall.P95MS), styleDim},
			{fmtNum(r.Wall.MaxMS), styleDim},
			{cpu, styleDim},
			{spawns, styleDim},
		})
	}
	return rows
}

var compareColumns = []column{
	{"scenario", false},
	{"base", true}, {"now", true}, {"Δ median", true}, {"Δ min", true},
	{"spawns", true}, {"verdict", false},
}

func verdictStyle(v verdict) style {
	switch v {
	case verdictFaster:
		return styleGreen
	case verdictSlower, verdictFailed:
		return styleRed
	case verdictNoisy:
		return styleYellow
	case verdictNew:
		return styleCyan
	}
	return styleDim
}

// deltaStyle paints a change by what the verdict made of it.
func deltaStyle(v verdict) style {
	switch v {
	case verdictFaster, verdictSlower, verdictNoisy:
		return verdictStyle(v)
	}
	return stylePlain
}

func spawnsCell(c comparison) cell {
	switch {
	case c.SpawnsCur < 0:
		return cell{"-", styleDim}
	case c.SpawnsBase < 0 || c.SpawnsBase == c.SpawnsCur:
		return cell{fmt.Sprintf("%d", c.SpawnsCur), styleDim}
	case c.SpawnsCur > c.SpawnsBase:
		return cell{fmt.Sprintf("%d→%d", c.SpawnsBase, c.SpawnsCur), styleRed}
	}
	return cell{fmt.Sprintf("%d→%d", c.SpawnsBase, c.SpawnsCur), styleGreen}
}

func compareRows(rs []result, cmps map[string]comparison) [][]cell {
	rows := make([][]cell, 0, len(rs))
	for _, r := range rs {
		c := cmps[r.Name]
		if c.Verdict == verdictNew {
			rows = append(rows, []cell{
				plain(r.Name), {"-", styleDim}, plain(fmtNum(r.Wall.P50MS)), {"-", styleDim}, {"-", styleDim},
				spawnsCell(c), {string(verdictNew), styleCyan},
			})
			continue
		}
		ds := deltaStyle(c.Verdict)
		rows = append(rows, []cell{
			plain(r.Name),
			{fmtNum(c.Base.P50MS), styleDim},
			{fmtNum(c.Cur.P50MS), styleBold},
			{fmtPct(c.DMedPct), ds},
			{fmtPct(c.DMinPct), ds},
			spawnsCell(c),
			{string(c.Verdict), verdictStyle(c.Verdict)},
		})
	}
	return rows
}

// printNotes lists what a reader of the tables needs to know: why a verdict
// is not clear, the guards, and runs that did not exit as expected.
func printNotes(w io.Writer, p palette, rep report, cv *compareView) {
	var lines []string
	if cv != nil {
		for _, c := range cv.cmps {
			if c.Note == "" || (c.Verdict != verdictSlower && c.Verdict != verdictNoisy && c.Verdict != verdictNew) {
				continue
			}
			lines = append(lines, p.paint(verdictStyle(c.Verdict), fmt.Sprintf("  %-7s %s: %s", c.Verdict, c.Name, c.Note)))
		}
	}
	for _, r := range rep.Results {
		if r.Attempts > 1 {
			lines = append(lines, p.paint(styleDim, fmt.Sprintf("  re-run  %s: ran %d times, the row is the attempt with the lowest median", r.Name, r.Attempts)))
		}
		if len(r.BadExits) > 0 {
			lines = append(lines, p.paint(styleYellow, fmt.Sprintf("  exit    %s: unexpected exit codes (code: count) %v", r.Name, r.BadExits)))
		}
	}
	if len(lines) > 0 {
		fmt.Fprintf(w, "\n%s\n", p.paint(styleBold, "Notes"))
		for _, l := range lines {
			fmt.Fprintln(w, l)
		}
	}

	var guards []string
	for _, r := range rep.Results {
		g := r.Guard
		if g == nil {
			continue
		}
		mark, st, rel := "✓", styleGreen, "<="
		if g.Failed {
			mark, st, rel = "✗", styleRed, ">"
		}
		guards = append(guards, fmt.Sprintf("  %s %s  %s\n      %s",
			p.paint(st, mark), r.Name,
			p.paint(st, fmt.Sprintf("%s %s %s %s", g.Stat, fmtMS(g.ObservedMS), rel, fmtMS(g.LimitMS))),
			p.paint(styleDim, g.Why)))
	}
	if len(guards) > 0 {
		fmt.Fprintf(w, "\n%s\n", p.paint(styleBold, "Guards"))
		for _, g := range guards {
			fmt.Fprintln(w, g)
		}
	}
}

// printVerbose adds what the Markdown report always has: the subprocesses each
// command started and the package init costs.
func printVerbose(w io.Writer, p palette, rep report) {
	var rows [][]cell
	for _, r := range rep.Results {
		if r.Spawns == nil || r.Spawns.Count == 0 {
			continue
		}
		calls := append([]callStat(nil), r.Spawns.Calls...)
		sort.SliceStable(calls, func(i, j int) bool { return calls[i].TotalMS > calls[j].TotalMS })
		var top []string
		for i, c := range calls {
			if i == 2 {
				top = append(top, fmt.Sprintf("+%d more", len(calls)-2))
				break
			}
			top = append(top, fmt.Sprintf("%s (%dx, %s)", c.Call, c.Count, fmtMS(c.TotalMS)))
		}
		rows = append(rows, []cell{plain(r.Name), plain(fmt.Sprintf("%d", r.Spawns.Count)), plain(fmtNum(r.Spawns.TotalMS)), {strings.Join(top, ", "), styleDim}})
	}
	if len(rows) > 0 {
		fmt.Fprintf(w, "\n%s  %s\n", p.paint(styleBold, "Subprocesses per command"), p.paint(styleDim, "(one traced run; ms is time inside the tools)"))
		writeTable(w, p, "  ", []column{{"scenario", false}, {"spawns", true}, {"ms", true}, {"slowest calls", false}}, rows)
	}
	if len(rep.InitCosts) > 0 {
		fmt.Fprintf(w, "\n%s  %s\n", p.paint(styleBold, "Package init"), p.paint(styleDim, fmt.Sprintf("(%d packages, %s in total; the most expensive)", len(rep.InitCosts), fmtMS(rep.InitTotalMS))))
		var irows [][]cell
		for i, c := range rep.InitCosts {
			if i == 8 {
				break
			}
			irows = append(irows, []cell{plain(c.Package), plain(fmtNum(c.MS))})
		}
		writeTable(w, p, "  ", []column{{"package", false}, {"ms", true}}, irows)
	}
}

// summaryLine is the last line of the report: one sentence on how it went.
func summaryLine(p palette, rep report, cv *compareView) string {
	guardsFailed, guardsHeld := countGuards(rep)
	var timed, skipped, failed int
	for _, r := range rep.Results {
		switch {
		case r.Failure != "":
			failed++
		case r.Skipped != "":
			skipped++
		default:
			timed++
		}
	}

	if cv == nil {
		text := fmt.Sprintf("%s timed, %d skipped, %d failed; %s held", count(timed, "scenario"), skipped, failed, count(guardsHeld, "guard"))
		if guardsFailed > 0 {
			text += fmt.Sprintf(", %d FAILED", guardsFailed)
		}
		if failed > 0 || guardsFailed > 0 {
			return p.paint(styleRed, "✗ "+text)
		}
		return p.paint(styleGreen, "✓ ") + text
	}

	s := cv.sum
	text := fmt.Sprintf("%s compared: %d same, %d faster, %d slower, %d noisy", count(s.Total-s.Skipped-s.Failed-s.New, "scenario"), s.Same+s.Info, s.Faster, s.Slower, s.Noisy)
	if s.New > 0 {
		text += fmt.Sprintf(", %d new", s.New)
	}
	text += "; " + count(s.GuardsHeld, "guard") + " held"
	if s.GuardsFailed > 0 {
		text += fmt.Sprintf(", %d FAILED", s.GuardsFailed)
	}
	if s.Failed > 0 {
		text += fmt.Sprintf("; %s FAILED", count(s.Failed, "scenario"))
	}
	text += fmt.Sprintf("; host %s vs baseline", fmtPct(s.HostDrift))
	if s.regressed() {
		return p.paint(styleRed, "✗ regression: "+text)
	}
	return p.paint(styleGreen, "✓ no regression: "+text)
}
