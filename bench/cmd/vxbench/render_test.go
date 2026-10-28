package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

var ansi = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansi.ReplaceAllString(s, "") }

func TestFmtNumKeepsThreeSignificantFigures(t *testing.T) {
	cases := map[float64]string{
		0.754:   "0.75",
		3.676:   "3.68",
		9.996:   "10.00", // rounding up keeps the precision of its own range
		12.34:   "12.3",
		99.94:   "99.9",
		100.4:   "100",
		334.49:  "334",
		15094.2: "15094",
	}
	for in, want := range cases {
		if got := fmtNum(in); got != want {
			t.Errorf("fmtNum(%v) = %q, want %q", in, got, want)
		}
	}
	if got := fmtMS(8.236); got != "8.24 ms" {
		t.Errorf("fmtMS = %q", got)
	}
}

func TestFmtPct(t *testing.T) {
	cases := map[float64]string{
		3.21:    "+3.2%",
		-12.46:  "-12.5%",
		0:       "0.0%",
		0.04:    "0.0%",
		-0.04:   "0.0%",
		215.3:   "+215%",
		-100.0:  "-100%",
		99.94:   "+99.9%",
		-0.0501: "-0.1%",
	}
	for in, want := range cases {
		if got := fmtPct(in); got != want {
			t.Errorf("fmtPct(%v) = %q, want %q", in, got, want)
		}
	}
	if got := fmtAbsPct(-20); got != "20%" {
		t.Errorf("fmtAbsPct(-20) = %q", got)
	}
	if got := fmtAbsPct(32.46); got != "32.5%" {
		t.Errorf("fmtAbsPct(32.46) = %q", got)
	}
}

func TestPaletteOffLeavesTextAlone(t *testing.T) {
	if got := (palette{}).paint(styleRed, "slower"); got != "slower" {
		t.Errorf("an off palette must not add escapes, got %q", got)
	}
	on := palette{on: true}
	if got := on.paint(styleGreen, "faster"); got != "\x1b[32mfaster\x1b[0m" {
		t.Errorf("got %q", got)
	}
	if got := on.paint(styleRed, ""); got != "" {
		t.Errorf("empty text stays empty, got %q", got)
	}
	if got := on.paint(stylePlain, "x"); got != "x" {
		t.Errorf("plain text stays plain, got %q", got)
	}
}

func TestColorEnabled(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

	if colorEnabled("auto", file, env(nil)) {
		t.Error("a file is not a terminal: no color")
	}
	if !colorEnabled("always", file, env(map[string]string{"NO_COLOR": "1"})) {
		t.Error("-color always wins")
	}
	if colorEnabled("never", file, env(nil)) {
		t.Error("-color never wins")
	}
	// The terminal itself is not available to a test; /dev/null is a character
	// device, which is what the check looks at.
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Skip("no /dev/null")
	}
	defer null.Close()
	if !colorEnabled("auto", null, env(map[string]string{"TERM": "xterm-256color"})) {
		t.Error("a character device with no NO_COLOR gets color")
	}
	if colorEnabled("auto", null, env(map[string]string{"NO_COLOR": "1"})) {
		t.Error("NO_COLOR turns it off")
	}
	if colorEnabled("auto", null, env(map[string]string{"TERM": "dumb"})) {
		t.Error("a dumb terminal gets none")
	}
	if !colorEnabled("auto", null, env(map[string]string{"NO_COLOR": ""})) {
		t.Error("an empty NO_COLOR counts as unset (no-color.org)")
	}
}

func TestWriteTableAlignsColumnsWhetherOrNotPainted(t *testing.T) {
	cols := []column{{"scenario", false}, {"median", true}, {"verdict", false}}
	rows := [][]cell{
		{plain("vx --version"), {"8.24", styleBold}, {"same", styleDim}},
		{plain("vx doctor (slow tools)"), {"204", styleBold}, {"SLOWER", styleRed}},
	}
	var off, on bytes.Buffer
	writeTable(&off, palette{}, "  ", cols, rows)
	writeTable(&on, palette{on: true}, "  ", cols, rows)

	if stripANSI(on.String()) != off.String() {
		t.Errorf("painting must not move anything:\n%s\nvs\n%s", stripANSI(on.String()), off.String())
	}
	lines := strings.Split(strings.TrimRight(off.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("heading, rule and two rows, got %d lines:\n%s", len(lines), off.String())
	}
	// The right-aligned numbers end in the same column.
	end := func(line string) int {
		return utf8.RuneCountInString(line[:strings.LastIndexAny(line, "0123456789")+1])
	}
	if end(lines[2]) != end(lines[3]) {
		t.Errorf("numbers are not right aligned:\n%s", off.String())
	}
	for _, l := range lines {
		if l != strings.TrimRight(l, " ") {
			t.Errorf("trailing spaces in %q", l)
		}
	}
	if !strings.Contains(lines[1], "──") {
		t.Errorf("a rule under the heading, got %q", lines[1])
	}
}

func TestWriteTableCountsRunesNotBytes(t *testing.T) {
	cols := []column{{"scenario", false}, {"Δ median", true}}
	rows := [][]cell{{plain("a"), plain("+3.2%")}, {plain("b"), plain("-12.5%")}}
	var buf bytes.Buffer
	writeTable(&buf, palette{}, "", cols, rows)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	w := utf8.RuneCountInString(lines[0])
	for _, l := range lines[1:] {
		if utf8.RuneCountInString(l) != w {
			t.Errorf("line %q is not as wide as the heading %q", l, lines[0])
		}
	}
}

func renderFixture() (report, *compareView) {
	base := reportOf(
		timedResult(hostDriftScenario, st(4, 4.2), -1),
		timedResult("vx status", st(100, 105), 2),
		timedResult("vx models", st(100, 105), 2),
		timedResult("vx strike", st(100, 105), 12),
	)
	base.Host = hostInfo{Label: "laptop", CPUModel: "M1", CPUs: 8, OS: "darwin", Arch: "arm64", GoVersion: "go1.26.8"}
	base.VX = vxInfo{Version: "vx dev", Commit: "aaaaaaa"}
	base.GeneratedAt = "2026-10-01T10:00:00Z"

	cur := reportOf(
		timedResult(hostDriftScenario, st(4, 4.2), -1),
		timedResult("vx status", st(150, 158), 4),
		timedResult("vx models", st(50, 52), 2),
		timedResult("vx strike", st(100, 105), 12),
		result{Name: "vx doctor (real)", Group: "real", Skipped: "machine specific, pass -real-tools"},
	)
	cur.Results[0].Group = "startup"
	cur.Results[2].Group = "cheap"
	cur.Results[3].Group = "camp"
	cur.Results[3].Guard = &guardResult{Stat: "median", LimitMS: 5000, ObservedMS: 105, Why: "must not wait out the probe"}
	cur.Host = base.Host
	cur.VX = vxInfo{Version: "vx dev", Commit: "bbbbbbb"}
	cur.GeneratedAt = "2026-10-28T16:00:00Z"

	cmps := compareReports(base, cur, defaultThresholds)
	return cur, &compareView{base: base, basePath: "bench/baseline.json", cmps: cmps, sum: tally(base, cur, cmps, defaultThresholds), thr: defaultThresholds}
}

func TestPrintTerminalPlain(t *testing.T) {
	rep := sampleReport()
	var buf bytes.Buffer
	printTerminal(&buf, palette{}, rep, nil, false)
	out := buf.String()

	if strings.Contains(out, "\x1b") {
		t.Error("no escapes with the palette off")
	}
	for _, want := range []string{
		"vx benchmark", "vx dev (b19a002)", "laptop, Apple M1 Pro, 8 CPUs, darwin/arm64, go1.26.8",
		"30 timed after 3 warm-up", "Startup (process start, no work)  (ms)",
		"vx --version", "Guards", "skipped  slow one  (slow, pass -slow)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "✓ ") || !strings.Contains(last, "3 scenarios timed, 1 skipped, 0 failed") || !strings.Contains(last, "1 guard held") {
		t.Errorf("the last line is the summary, got %q", last)
	}
}

func TestPrintTerminalSummaryFailsOnAFailedGuardOrScenario(t *testing.T) {
	rep := sampleReport()
	rep.Results[1].Guard.Failed = true
	if got := summaryLine(palette{}, rep, nil); !strings.HasPrefix(got, "✗ ") || !strings.Contains(got, "1 FAILED") {
		t.Errorf("got %q", got)
	}
	rep = sampleReport()
	rep.Results = append(rep.Results, result{Name: "broken", Group: "cheap", Failure: "boom"})
	if got := summaryLine(palette{}, rep, nil); !strings.HasPrefix(got, "✗ ") || !strings.Contains(got, "1 failed") {
		t.Errorf("got %q", got)
	}
}

func TestPrintTerminalCompare(t *testing.T) {
	cur, cv := renderFixture()
	var buf bytes.Buffer
	printTerminal(&buf, palette{}, cur, cv, false)
	out := buf.String()

	for _, want := range []string{
		"against  bench/baseline.json (2026-10-01, vx dev (aaaaaaa), laptop)",
		"Δ median", "+50.5%", "-50.5%", "SLOWER", "faster", "same",
		"2→4", // the spawn count of vx status changed
		"This machine's real tools (machine specific)  (skipped: machine specific, pass -real-tools)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "✗ regression: ") || !strings.Contains(last, "1 slower") || !strings.Contains(last, "1 faster") {
		t.Errorf("the last line is the summary of a regression, got %q", last)
	}
	if !strings.Contains(last, "host 0.0% vs baseline") {
		t.Errorf("the summary reports how far the host drifted, got %q", last)
	}
}

func TestPrintTerminalCompareWarnsAboutAnotherMachine(t *testing.T) {
	cur, cv := renderFixture()
	cv.base.Host.CPUModel = "Xeon"
	var buf bytes.Buffer
	printTerminal(&buf, palette{}, cur, cv, false)
	if !strings.Contains(buf.String(), "taken on a different machine") {
		t.Errorf("no warning in:\n%s", buf.String())
	}
}

func TestPrintTerminalColorsFollowTheVerdict(t *testing.T) {
	cur, cv := renderFixture()
	var buf bytes.Buffer
	printTerminal(&buf, palette{on: true}, cur, cv, false)
	out := buf.String()
	if !strings.Contains(out, "\x1b[31mSLOWER\x1b[0m") {
		t.Error("slower is red")
	}
	if !strings.Contains(out, "\x1b[32mfaster\x1b[0m") {
		t.Error("faster is green")
	}
	if !strings.Contains(out, "\x1b[32m✓") && !strings.Contains(out, "\x1b[31m✗") {
		t.Error("the summary is painted")
	}
	if stripANSI(out) == out {
		t.Error("expected escapes")
	}
}

func TestPrintTerminalVerboseAddsBreakdowns(t *testing.T) {
	rep := sampleReport()
	rep.Results[0].Spawns = &spawnSummary{Count: 3, TotalMS: 30, Calls: []callStat{
		{Call: "git rev-parse", Count: 2, TotalMS: 14}, {Call: "herdr tab create", Count: 1, TotalMS: 16}, {Call: "ps -x", Count: 1, TotalMS: 2},
	}}
	var plainOut, verboseOut bytes.Buffer
	printTerminal(&plainOut, palette{}, rep, nil, false)
	printTerminal(&verboseOut, palette{}, rep, nil, true)
	if strings.Contains(plainOut.String(), "Subprocesses per command") {
		t.Error("the breakdown is only for -v")
	}
	v := verboseOut.String()
	for _, want := range []string{"Subprocesses per command", "herdr tab create (1x, 16.0 ms)", "+1 more", "Package init"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q in:\n%s", want, v)
		}
	}
}

func TestWriteMarkdownPlain(t *testing.T) {
	var buf bytes.Buffer
	writeMarkdown(&buf, sampleReport(), nil)
	out := buf.String()
	for _, want := range []string{
		"# vx benchmark\n", "- vx: `vx dev (b19a002)`",
		"| scenario | first (ms) | min (ms) | median (ms) | p95 (ms) | max (ms) | cpu (ms) | spawns |",
		"| vx --version | 9.30 | 7.40 | 8.20 | 10.1 | 19.7 | 6.00 | 0 |",
		"| slow one | skipped: slow, pass -slow |", "## Guards", "## Package init",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") {
		t.Error("Markdown carries no escapes")
	}
}

func TestWriteMarkdownCompareIsSideBySide(t *testing.T) {
	cur, cv := renderFixture()
	var buf bytes.Buffer
	writeMarkdown(&buf, cur, cv)
	out := buf.String()
	for _, want := range []string{
		"# vx benchmark: before and after",
		"- baseline: `bench/baseline.json`, taken 2026-10-01",
		"| scenario | min before | min after | Δ min | median before | median after | Δ median | spawns | verdict |",
		"| vx status | 100 | 150 | +50.0% | 105 | 158 | +50.5% | 2 → 4 | **SLOWER** |",
		"| vx models | 100 | 50.0 | -50.0% | 105 | 52.0 | -50.5% | 2 | _faster_ |",
		"| vx strike | 100 | 100 | 0.0% | 105 | 105 | 0.0% | 12 | same |",
		"**✗ regression:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestSpawnsText(t *testing.T) {
	cases := []struct {
		c    comparison
		want string
	}{
		{comparison{SpawnsBase: 7, SpawnsCur: 7}, "7"},
		{comparison{SpawnsBase: 7, SpawnsCur: 9}, "7 → 9"},
		{comparison{SpawnsBase: -1, SpawnsCur: 3}, "3"},
		{comparison{SpawnsBase: 3, SpawnsCur: -1}, "-"},
	}
	for _, c := range cases {
		if got := spawnsText(c.c); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.c, got, c.want)
		}
	}
}

func TestDropEmptyColumns(t *testing.T) {
	cols := []column{{"scenario", false}, {"min", true}, {"cpu", true}, {"spawns", true}}
	rows := [][]cell{
		{plain("a"), plain("1.0"), plain("-"), plain("3")},
		{plain("b"), plain("2.0"), plain("-"), plain("-")},
	}
	gotCols, gotRows := dropEmptyColumns(cols, rows)
	if len(gotCols) != 3 || gotCols[2].title != "spawns" {
		t.Errorf("the all-dash cpu column goes, the rest stay: %+v", gotCols)
	}
	if len(gotRows[0]) != 3 || gotRows[0][2].text != "3" || gotRows[1][2].text != "-" {
		t.Errorf("rows must follow the columns: %+v", gotRows)
	}
	same, _ := dropEmptyColumns(cols[:2], rows[:0])
	if len(same) != 1 {
		t.Errorf("with no rows every data column is empty, got %+v", same)
	}
}

func TestCount(t *testing.T) {
	for want, got := range map[string]string{"0 scenarios": count(0, "scenario"), "1 scenario": count(1, "scenario"), "5 guards": count(5, "guard")} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestPrintTerminalHeaderWarnsAboutABusyMachine(t *testing.T) {
	rep := sampleReport() // 8 CPUs
	rep.LoadStart, rep.LoadEnd = 0.8, 1.1
	var calm bytes.Buffer
	printTerminal(&calm, palette{}, rep, nil, false)
	if !strings.Contains(calm.String(), "0.80 at start, 1.10 at end (8 CPUs)") || strings.Contains(calm.String(), "busy machine") {
		t.Errorf("a calm machine is reported without a warning:\n%s", calm.String())
	}
	rep.LoadStart = 5.2
	var busy bytes.Buffer
	printTerminal(&busy, palette{}, rep, nil, false)
	if !strings.Contains(busy.String(), "a busy machine, expect noisy timings") {
		t.Errorf("a busy machine gets a warning:\n%s", busy.String())
	}
}
