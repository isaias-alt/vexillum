package tribunal

import (
	"strings"
	"testing"
)

func TestParseReport_AcceptsProseAndFencesAndPicksTheLastObject(t *testing.T) {
	out := "I considered {braces} in prose.\n```json\n" +
		`{"findings": [], "reviewed_paths": ["old.go"], "risk_level": "high", "risk_rationale": "draft"}` + "\n```\n" +
		"Final answer:\n" + reportJSON(finding("Warning", "Auto-Fix", "a.go"), "a.go")
	r, err := parseReport(out)
	if err != nil {
		t.Fatalf("parseReport: %v", err)
	}
	if len(r.Findings) != 1 || r.Findings[0].Severity != "warning" || r.Findings[0].Action != "auto-fix" {
		t.Errorf("expected the last object, normalized, got %+v", r)
	}
	if r.RiskLevel != "low" {
		t.Errorf("expected the last object's risk, got %q", r.RiskLevel)
	}
}

func TestParseReport_Rejections(t *testing.T) {
	cases := map[string]string{
		"no json":            "all good",
		"no findings key":    `{"reviewed_paths": [], "risk_level": "low", "risk_rationale": "r"}`,
		"null findings":      `{"findings": null, "reviewed_paths": [], "risk_level": "low", "risk_rationale": "r"}`,
		"no reviewed_paths":  `{"findings": [], "risk_level": "low", "risk_rationale": "r"}`,
		"bad risk":           `{"findings": [], "reviewed_paths": [], "risk_level": "extreme", "risk_rationale": "r"}`,
		"empty rationale":    `{"findings": [], "reviewed_paths": [], "risk_level": "low", "risk_rationale": " "}`,
		"bad severity":       reportJSON(finding("fatal", "auto-fix", "a.go")),
		"bad action":         reportJSON(finding("error", "maybe", "a.go")),
		"empty file":         reportJSON(finding("error", "auto-fix", "")),
		"error w/o scenario": reportJSON(`{"file": "a.go", "line": 1, "severity": "error", "action": "auto-fix", "description": "d", "failure_scenario": ""}`),
		"negative line":      reportJSON(`{"file": "a.go", "line": -1, "severity": "info", "action": "no-op", "description": "d"}`),
		"wrong types":        `{"findings": "none", "reviewed_paths": [], "risk_level": "low", "risk_rationale": "r"}`,
	}
	for name, out := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseReport(out); err == nil {
				t.Errorf("expected %q to be rejected", name)
			}
		})
	}
}

func TestParseReport_InfoNeedsNoScenario(t *testing.T) {
	r, err := parseReport(reportJSON(`{"file": "a.go", "line": 0, "severity": "info", "action": "no-op", "description": "nice to have"}`))
	if err != nil {
		t.Fatalf("parseReport: %v", err)
	}
	if len(r.Info()) != 1 || len(r.Blocking()) != 0 {
		t.Errorf("expected one info finding and no blockers, got %+v", r)
	}
}

func TestUncovered_NormalizesPaths(t *testing.T) {
	missing := uncovered([]string{"a/b.go", "c.go", "d/e.go"}, []string{"./a/b.go", "d//e.go"})
	if len(missing) != 1 || missing[0] != "c.go" {
		t.Errorf("expected only c.go uncovered, got %v", missing)
	}
}

func TestFormatFindings_MostSevereFirst(t *testing.T) {
	out := FormatFindings([]Finding{
		{File: "i.go", Line: 1, Severity: "info", Action: "no-op", Description: "note"},
		{File: "e.go", Line: 2, Severity: "error", Action: "auto-fix", Description: "bug", FailureScenario: "x then y", SiblingSites: []string{"f.go:3 same"}},
	})
	if strings.Index(out, "[error] e.go:2") > strings.Index(out, "[info] i.go:1") || strings.Index(out, "[error]") < 0 {
		t.Errorf("expected the error before the info:\n%s", out)
	}
	for _, want := range []string{"scenario: x then y", "sibling sites: f.go:3 same"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in:\n%s", want, out)
		}
	}
}
