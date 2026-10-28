package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
)

// schemaVersion is the version of the report file format, which is also the
// baseline file format. Bump it when a field changes meaning; a reader refuses
// a file of any other version instead of guessing.
const schemaVersion = 1

// result is one scenario's outcome.
type result struct {
	Name  string `json:"name"`
	Group string `json:"group"`
	// Informational rows are shown and compared but never judged: they measure
	// the machine or a polling interval, not something vx can regress.
	Informational bool          `json:"informational,omitempty"`
	Skipped       string        `json:"skipped,omitempty"`
	Failure       string        `json:"failure,omitempty"`
	FirstMS       float64       `json:"first_run_ms,omitempty"`
	Wall          stats         `json:"wall"`
	CPUMedMS      float64       `json:"cpu_median_ms,omitempty"`
	Spawns        *spawnSummary `json:"spawns,omitempty"`
	BadExits      map[int]int   `json:"unexpected_exit_codes,omitempty"`
	Guard         *guardResult  `json:"guard,omitempty"`
	// Attempts is how many times the scenario ran when a comparison re-ran it
	// to rule out noise; the row kept is the attempt with the lowest median.
	Attempts int `json:"attempts,omitempty"`
}

// timed reports whether the row has timings to compare.
func (r result) timed() bool {
	return r.Skipped == "" && r.Failure == "" && r.Wall.N > 0
}

// guard is a hard limit a scenario must stay under whatever the baseline says:
// it catches a regression of a kind a percentage cannot, such as dispatch
// waiting out a 15 s probe again.
type guard struct {
	// Stat is the statistic held to the limit: "median" or "max".
	Stat    string
	LimitMS float64
	// Why says what a failure means, in a sentence.
	Why string
}

// guardResult is the outcome of a guard, kept in the report.
type guardResult struct {
	Stat       string  `json:"stat"`
	LimitMS    float64 `json:"limit_ms"`
	ObservedMS float64 `json:"observed_ms"`
	Why        string  `json:"why"`
	Failed     bool    `json:"failed,omitempty"`
}

// observed returns the statistic a guard looks at.
func (g guard) observed(s stats) float64 {
	if g.Stat == "max" {
		return s.MaxMS
	}
	return s.P50MS
}

func (g guard) evaluate(s stats) *guardResult {
	obs := g.observed(s)
	return &guardResult{Stat: g.Stat, LimitMS: g.LimitMS, ObservedMS: obs, Why: g.Why, Failed: obs > g.LimitMS}
}

// report is everything one run measured. Its JSON form is the baseline file.
type report struct {
	Schema      int      `json:"schema_version"`
	GeneratedAt string   `json:"generated_at"`
	Host        hostInfo `json:"host"`
	VX          vxInfo   `json:"vx"`
	// LoadStart and LoadEnd are the machine's one-minute load average when the
	// run began and ended; 0 where the platform does not say.
	LoadStart float64 `json:"load_start,omitempty"`
	LoadEnd   float64 `json:"load_end,omitempty"`
	// Runs and Warmup are the defaults; a scenario that caps them (the slow
	// ones) says so in its own Wall.N.
	Runs        int        `json:"runs"`
	Warmup      int        `json:"warmup"`
	Results     []result   `json:"results"`
	InitCosts   []initCost `json:"package_init_ms,omitempty"`
	InitTotalMS float64    `json:"package_init_total_ms,omitempty"`
}

// date is the day the report was taken, for headers.
func (r report) date() string {
	if t, err := time.Parse(time.RFC3339, r.GeneratedAt); err == nil {
		return t.UTC().Format("2006-01-02")
	}
	return r.GeneratedAt
}

// find returns the result called name.
func (r report) find(name string) (result, bool) {
	for _, res := range r.Results {
		if res.Name == name {
			return res, true
		}
	}
	return result{}, false
}

// validate checks that a report read from a file is one this program can
// compare against.
func (r report) validate() error {
	if r.Schema != schemaVersion {
		return fmt.Errorf("schema version %d, this vxbench reads version %d", r.Schema, schemaVersion)
	}
	if len(r.Results) == 0 {
		return fmt.Errorf("no results")
	}
	seen := map[string]bool{}
	for _, res := range r.Results {
		switch {
		case res.Name == "":
			return fmt.Errorf("a result has no name")
		case seen[res.Name]:
			return fmt.Errorf("scenario %q appears twice", res.Name)
		}
		seen[res.Name] = true
	}
	return nil
}

// encodeReport is the file form of a report: indented, one trailing newline,
// stable enough to diff.
func encodeReport(rep report) ([]byte, error) {
	rep.Schema = schemaVersion
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// decodeReport parses and validates a report file.
func decodeReport(data []byte) (report, error) {
	var rep report
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&rep); err != nil {
		return report{}, err
	}
	if err := rep.validate(); err != nil {
		return report{}, err
	}
	return rep, nil
}

func writeReport(path string, rep report) error {
	data, err := encodeReport(rep)
	if err != nil {
		return fmt.Errorf("encoding the report: %w", err)
	}
	if err := atomicfile.Write(path, data); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func readReport(path string) (report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return report{}, fmt.Errorf("reading %s: %w", path, err)
	}
	rep, err := decodeReport(data)
	if err != nil {
		return report{}, fmt.Errorf("%s: %w", path, err)
	}
	return rep, nil
}
