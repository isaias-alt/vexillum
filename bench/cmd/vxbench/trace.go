package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// spawn is one line of the spawnlog file: a tool a vx command started.
type spawn struct {
	Tool     string
	Args     string
	Duration time.Duration
	Exit     int
}

// callStat groups the spawns of one kind of call, e.g. "git rev-parse".
type callStat struct {
	Call    string  `json:"call"`
	Count   int     `json:"count"`
	TotalMS float64 `json:"total_ms"`
}

// spawnSummary is what one traced run of a command started.
type spawnSummary struct {
	Count   int        `json:"count"`
	TotalMS float64    `json:"total_ms"`
	Calls   []callStat `json:"calls"`
}

// readSpawns returns the spawns in the log whose parent is pid, so a detached
// sentinel's own polling never counts against the command.
func readSpawns(path string, pid int) ([]spawn, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []spawn
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), "\t", 6)
		if len(parts) < 6 {
			continue
		}
		ppid, err1 := strconv.Atoi(parts[2])
		durNS, err2 := strconv.ParseInt(parts[1], 10, 64)
		code, err3 := strconv.Atoi(parts[4])
		if err1 != nil || err2 != nil || err3 != nil || ppid != pid {
			continue
		}
		out = append(out, spawn{Tool: parts[3], Args: parts[5], Duration: time.Duration(durNS), Exit: code})
	}
	return out, sc.Err()
}

// summarizeSpawns groups spawns by their tool and first two arguments, most
// expensive group first.
func summarizeSpawns(spawns []spawn) *spawnSummary {
	sum := &spawnSummary{Count: len(spawns)}
	byCall := map[string]*callStat{}
	for _, s := range spawns {
		fields := strings.Fields(s.Args)
		if len(fields) > 2 {
			fields = fields[:2]
		}
		key := strings.TrimSpace(s.Tool + " " + strings.Join(fields, " "))
		c := byCall[key]
		if c == nil {
			c = &callStat{Call: key}
			byCall[key] = c
		}
		c.Count++
		c.TotalMS += ms(s.Duration)
		sum.TotalMS += ms(s.Duration)
	}
	for _, c := range byCall {
		sum.Calls = append(sum.Calls, *c)
	}
	sort.Slice(sum.Calls, func(i, j int) bool { return sum.Calls[i].TotalMS > sum.Calls[j].TotalMS })
	return sum
}

// initCost is the median time one package's init functions take.
type initCost struct {
	Package string  `json:"package"`
	MS      float64 `json:"ms"`
}

var initTraceLine = regexp.MustCompile(`^init (\S+) @[\d.]+ ms, ([\d.]+) ms clock`)

// traceInit runs "vx --version" runs times with GODEBUG=inittrace=1 and returns
// the median clock time of each package's init, most expensive first, and the
// sum of those medians.
func traceInit(vx string, runs int) ([]initCost, float64, error) {
	samples := map[string][]float64{}
	for i := 0; i < runs; i++ {
		cmd := exec.Command(vx, "--version")
		cmd.Env = append(os.Environ(), "GODEBUG=inittrace=1")
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return nil, 0, fmt.Errorf("running %s --version: %w", vx, err)
		}
		for _, line := range strings.Split(stderr.String(), "\n") {
			m := initTraceLine.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			v, err := strconv.ParseFloat(m[2], 64)
			if err != nil {
				continue
			}
			samples[m[1]] = append(samples[m[1]], v)
		}
	}

	var costs []initCost
	total := 0.0
	for pkg, vs := range samples {
		sort.Float64s(vs)
		med := vs[len(vs)/2]
		costs = append(costs, initCost{Package: pkg, MS: med})
		total += med
	}
	sort.Slice(costs, func(i, j int) bool { return costs[i].MS > costs[j].MS })
	return costs, total, nil
}
