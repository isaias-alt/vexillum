package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func sampleReport() report {
	return report{
		Schema:      schemaVersion,
		GeneratedAt: "2026-10-28T16:00:00Z",
		Host:        hostInfo{Label: "laptop", CPUModel: "Apple M1 Pro", CPUs: 8, OS: "darwin", Arch: "arm64", GoVersion: "go1.26.8"},
		VX:          vxInfo{Version: "vx dev", Commit: "b19a002"},
		Runs:        30,
		Warmup:      3,
		Results: []result{
			{
				Name: "vx --version", Group: "startup", FirstMS: 9.3,
				Wall:     stats{N: 30, MinMS: 7.4, P50MS: 8.2, P95MS: 10.1, MaxMS: 19.7, MeanMS: 8.9},
				CPUMedMS: 6, Spawns: &spawnSummary{Count: 0},
			},
			{
				Name: "vx dispatch (soldier still working)", Group: "camp",
				Wall:  stats{N: 30, MinMS: 70, P50MS: 77, P95MS: 90, MaxMS: 95, MeanMS: 78},
				Guard: &guardResult{Stat: "median", LimitMS: 5000, ObservedMS: 77, Why: "no 15 s waits"},
			},
			{Name: "wake: await poll", Group: "wake", Informational: true, Wall: stats{N: 10, MinMS: 5, P50MS: 2400, MaxMS: 4900}},
			{Name: "slow one", Group: "camp", Skipped: "slow, pass -slow"},
		},
		InitCosts: []initCost{{Package: "p", MS: 0.1}},
	}
}

func TestReportRoundTripsThroughItsFileFormat(t *testing.T) {
	want := sampleReport()
	data, err := encodeReport(want)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "}\n") {
		t.Error("the file ends with one newline")
	}
	got, err := decodeReport(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the report changed on the way through the file:\n got %+v\nwant %+v", got, want)
	}
}

// The field names are the baseline file format: a rename is a schema change.
func TestReportFileFormatKeys(t *testing.T) {
	data, err := encodeReport(sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema_version", "generated_at", "host", "vx", "runs", "warmup", "results"} {
		if _, ok := top[key]; !ok {
			t.Errorf("top-level key %q is missing", key)
		}
	}
	var host map[string]any
	if err := json.Unmarshal(top["host"], &host); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"label", "cpu_model", "cpus", "os", "arch", "go_version"} {
		if _, ok := host[key]; !ok {
			t.Errorf("host key %q is missing", key)
		}
	}
	var vx map[string]any
	if err := json.Unmarshal(top["vx"], &vx); err != nil {
		t.Fatal(err)
	}
	if vx["version"] != "vx dev" || vx["commit"] != "b19a002" {
		t.Errorf("vx identity not written as version and commit: %v", vx)
	}
	var results []map[string]json.RawMessage
	if err := json.Unmarshal(top["results"], &results); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"name", "group", "wall", "first_run_ms", "cpu_median_ms", "spawns"} {
		if _, ok := results[0][key]; !ok {
			t.Errorf("result key %q is missing", key)
		}
	}
	var wall map[string]float64
	if err := json.Unmarshal(results[0]["wall"], &wall); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"n", "min_ms", "median_ms", "p95_ms", "max_ms", "mean_ms"} {
		if _, ok := wall[key]; !ok {
			t.Errorf("wall key %q is missing", key)
		}
	}
}

func TestDecodeReportRefusesWhatItCannotCompareAgainst(t *testing.T) {
	cases := map[string]func(*report){
		"another schema version": func(r *report) { r.Schema = schemaVersion + 1 },
		"no results":             func(r *report) { r.Results = nil },
		"an unnamed result":      func(r *report) { r.Results[0].Name = "" },
		"a repeated scenario":    func(r *report) { r.Results[1].Name = r.Results[0].Name },
	}
	for name, mutate := range cases {
		rep := sampleReport()
		mutate(&rep)
		data, err := json.Marshal(rep) // not encodeReport: it would repair the schema version
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeReport(data); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := decodeReport([]byte("not json")); err == nil {
		t.Error("garbage must be an error")
	}
}

func TestWriteAndReadReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	want := sampleReport()
	if err := writeReport(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := readReport(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Error("the report read back differs from the one written")
	}
	if _, err := readReport(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Error("a missing baseline is an error")
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"schema_version": 99}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readReport(bad); err == nil || !strings.Contains(err.Error(), bad) {
		t.Errorf("the error names the file: %v", err)
	}
}

func TestReportFindAndTimed(t *testing.T) {
	rep := sampleReport()
	if r, ok := rep.find("vx --version"); !ok || !r.timed() {
		t.Errorf("find/timed: %+v %v", r, ok)
	}
	if _, ok := rep.find("nope"); ok {
		t.Error("find must miss an unknown scenario")
	}
	if r, _ := rep.find("slow one"); r.timed() {
		t.Error("a skipped scenario has no timing")
	}
	if got := rep.date(); got != "2026-10-28" {
		t.Errorf("date() = %q", got)
	}
}

func TestParseVXVersion(t *testing.T) {
	cases := []struct {
		name, out, commit string
		dirty             bool
		want              vxInfo
	}{
		{"a build without identity takes the flags", "vx dev\n", "b19a002", false, vxInfo{Version: "vx dev", Commit: "b19a002"}},
		{"dirty flag", "vx dev\n", "b19a002", true, vxInfo{Version: "vx dev", Commit: "b19a002", Dirty: true}},
		{"no commit at all", "vx dev\n", "", true, vxInfo{Version: "vx dev"}},
		{"a build with its own identity", "vx dev (a1b2c3d, 2026-10-05)\n", "ffffffff", true, vxInfo{Version: "vx dev", Commit: "a1b2c3d"}},
		{"its own identity, dirty", "vx dev (a1b2c3d, 2026-10-05, dirty)\n", "", false, vxInfo{Version: "vx dev", Commit: "a1b2c3d", Dirty: true}},
		{"a release", "vx 0.1.1\n", "", false, vxInfo{Version: "vx 0.1.1"}},
	}
	for _, c := range cases {
		if got := parseVXVersion(c.out, c.commit, c.dirty); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestVXInfoString(t *testing.T) {
	for want, v := range map[string]vxInfo{
		"vx dev":                  {Version: "vx dev"},
		"vx dev (b19a002)":        {Version: "vx dev", Commit: "b19a002"},
		"vx dev (b19a002, dirty)": {Version: "vx dev", Commit: "b19a002", Dirty: true},
	} {
		if got := v.String(); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestParseCPUInfo(t *testing.T) {
	data := "processor\t: 0\nvendor_id\t: GenuineIntel\nmodel name\t: Intel(R) Core(TM)  i7-9750H CPU @ 2.60GHz\nmodel name\t: second\n"
	if got := parseCPUInfo(data); got != "Intel(R) Core(TM) i7-9750H CPU @ 2.60GHz" {
		t.Errorf("got %q", got)
	}
	if got := parseCPUInfo("nothing useful"); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestHostSameMachineIgnoresTheLabel(t *testing.T) {
	a := hostInfo{Label: "laptop", CPUModel: "M1", CPUs: 8, OS: "darwin", Arch: "arm64", GoVersion: "go1.26.8"}
	b := a
	b.Label, b.GoVersion = "renamed", "go1.27.0"
	if !a.sameMachineAs(b) {
		t.Error("a new label or Go version is still the same machine")
	}
	b.CPUs = 4
	if a.sameMachineAs(b) {
		t.Error("a different core count is another machine")
	}
	if got := a.String(); !strings.Contains(got, "laptop") || !strings.Contains(got, "M1") || !strings.Contains(got, "8 CPUs") || !strings.Contains(got, "darwin/arm64") {
		t.Errorf("host description misses something: %q", got)
	}
}

func TestParseLoadAverage(t *testing.T) {
	cases := map[string]float64{
		"{ 3.38 2.51 2.68 }\n":      3.38,
		"0.52 0.58 0.59 1/512 99\n": 0.52,
	}
	for in, want := range cases {
		if got, ok := parseLoadAverage(in); !ok || got != want {
			t.Errorf("parseLoadAverage(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "{ }", "load: n/a"} {
		if _, ok := parseLoadAverage(bad); ok {
			t.Errorf("parseLoadAverage(%q) must fail", bad)
		}
	}
}

func TestHostBusy(t *testing.T) {
	h := hostInfo{CPUs: 8}
	if h.busy(3.9) || !h.busy(4.1) {
		t.Error("busy means more than half the cores occupied")
	}
	if (hostInfo{}).busy(100) {
		t.Error("an unknown core count is never busy")
	}
}
