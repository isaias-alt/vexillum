package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

var groupTitles = []struct{ key, title string }{
	{"startup", "Startup (process start, no work)"},
	{"cheap", "Cheap commands"},
	{"doctor", "doctor"},
	{"forum", "Forum commands that need no browser"},
	{"camp", "Soldier lifecycle against a fake herdr and fake claude"},
	{"tools", "External tools vx shells out to (for scale)"},
}

func printReport(w io.Writer, rep report) {
	fmt.Fprintf(w, "# vx benchmark\n\n")
	fmt.Fprintf(w, "- vx: `%s`\n- host: %s/%s, %d CPUs, %s\n- runs per scenario: %d timed after %d warm-up, %s\n\n",
		rep.VXVersion, rep.OS, rep.Arch, rep.CPUs, rep.GoVersion, rep.Runs, rep.Warmup, rep.GeneratedAt)
	fmt.Fprintln(w, "Wall time is from before fork to after wait, in milliseconds. `first` is the first run of the scenario, before warm-up has settled. `cpu` is the median user+sys time of the process and the children it waited for. `spawns` is the number of subprocesses one traced run started.")

	byGroup := map[string][]result{}
	for _, r := range rep.Results {
		byGroup[r.Group] = append(byGroup[r.Group], r)
	}
	for _, g := range groupTitles {
		rs := byGroup[g.key]
		if len(rs) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n## %s\n\n", g.title)
		fmt.Fprintln(w, "| scenario | first | min | median | p95 | max | cpu | spawns |")
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
				fmt.Fprintf(w, "| %s | %s | %s | %s | %s | %s | %s | %s |\n", r.Name,
					fmtMS(r.FirstMS), fmtMS(r.Wall.MinMS), fmtMS(r.Wall.P50MS), fmtMS(r.Wall.P95MS), fmtMS(r.Wall.MaxMS), fmtMS(r.CPUMedMS), spawns)
			}
		}
	}

	var bad []string
	for _, r := range rep.Results {
		if len(r.BadExits) > 0 {
			bad = append(bad, fmt.Sprintf("%s: %v", r.Name, r.BadExits))
		}
	}
	if len(bad) > 0 {
		fmt.Fprintf(w, "\nUnexpected exit codes (count per code): %s\n", strings.Join(bad, "; "))
	}

	var floor, version float64
	for _, r := range rep.Results {
		switch r.Name {
		case "floor: empty Go program":
			floor = r.Wall.P50MS
		case "vx --version":
			version = r.Wall.P50MS
		}
	}
	if floor > 0 && version > 0 {
		fmt.Fprintf(w, "\nStartup cost of vx itself: `vx --version` median %s minus the empty Go program median %s = %s.\n", fmtMS(version), fmtMS(floor), fmtMS(version-floor))
	}

	if len(rep.InitCosts) > 0 {
		fmt.Fprintf(w, "\n## Package init (GODEBUG=inittrace=1, median of 15 runs)\n\nSum over %d packages: %s. Most expensive:\n\n", len(rep.InitCosts), fmtMS(rep.InitTotalMS))
		fmt.Fprintln(w, "| package | init |")
		fmt.Fprintln(w, "|---|---:|")
		for i, c := range rep.InitCosts {
			if i == 8 {
				break
			}
			fmt.Fprintf(w, "| %s | %s |\n", c.Package, fmtMS(c.MS))
		}
	}

	var traced []result
	for _, r := range rep.Results {
		if r.Spawns != nil && r.Spawns.Count > 0 {
			traced = append(traced, r)
		}
	}
	if len(traced) > 0 {
		fmt.Fprintf(w, "\n## Subprocesses per command (one traced run)\n\nDurations are the time the tool itself took, measured inside the shim; the shim adds a little process start on top.\n")
		for _, r := range traced {
			fmt.Fprintf(w, "\n### %s: %d spawns, %s in subprocesses\n\n", r.Name, r.Spawns.Count, fmtMS(r.Spawns.TotalMS))
			fmt.Fprintln(w, "| call | count | total |")
			fmt.Fprintln(w, "|---|---:|---:|")
			calls := append([]callStat(nil), r.Spawns.Calls...)
			sort.SliceStable(calls, func(i, j int) bool { return calls[i].TotalMS > calls[j].TotalMS })
			for _, c := range calls {
				fmt.Fprintf(w, "| %s | %d | %s |\n", c.Call, c.Count, fmtMS(c.TotalMS))
			}
		}
	}
}

func fmtMS(v float64) string {
	switch {
	case v >= 100:
		return fmt.Sprintf("%.0f ms", v)
	case v >= 10:
		return fmt.Sprintf("%.1f ms", v)
	default:
		return fmt.Sprintf("%.2f ms", v)
	}
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 160 {
		s = s[:160] + "..."
	}
	return s
}
