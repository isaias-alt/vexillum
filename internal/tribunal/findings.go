package tribunal

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
)

// Severity and action vocabularies of a review finding. Severity decides
// whether a finding blocks the ship; action decides who may resolve it (see
// Finding).
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
	SeverityInfo    = "info"

	ActionAskUser = "ask-user"
	ActionAutoFix = "auto-fix"
	ActionNoOp    = "no-op"
)

var (
	knownSeverities = []string{SeverityError, SeverityWarning, SeverityInfo}
	knownActions    = []string{ActionAskUser, ActionAutoFix, ActionNoOp}
	knownRisks      = []string{"low", "medium", "high"}
)

// Finding is one defect (or, for the simplification pass, one unrequired
// component) the adversarial reviewer reports. A finding names a class once,
// anchored at its primary site, with every sibling site listed in the same
// finding rather than one finding per site.
type Finding struct {
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	// Action: "ask-user" needs a human decision and is never auto-fixed;
	// "auto-fix" is a non-functional issue a fixer may correct without
	// discussing the author's intent; "no-op" is informational.
	Action      string `json:"action"`
	Description string `json:"description"`
	// FailureScenario is the concrete sequence from the change's intended
	// usage that produces the failure - the evidence bar that stands in for
	// a second-pass verifier. For a simplification warning it states which
	// requirement the component exceeds.
	FailureScenario string   `json:"failure_scenario"`
	SiblingSites    []string `json:"sibling_sites"`
}

// Blocking reports whether the finding blocks the ship: error and warning
// do (including every ask-user), info does not.
func (f Finding) Blocking() bool {
	return f.Severity == SeverityError || f.Severity == SeverityWarning
}

// location renders "file:line", or just "file" for a file-level finding.
func (f Finding) location() string {
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d", f.File, f.Line)
	}
	return f.File
}

// Report is the reviewer's validated structured output.
type Report struct {
	Findings []Finding `json:"findings"`
	// ReviewedPaths is the reviewer's own coverage record: the changed
	// files it actually read and judged. An omitted file counts as not
	// reviewed, never as clean.
	ReviewedPaths []string `json:"reviewed_paths"`
	RiskLevel     string   `json:"risk_level"`
	RiskRationale string   `json:"risk_rationale"`
	// PRTitle and PRDescription are the reviewer's proposal for the pull
	// request text, written from the diff and the commits. Both are optional
	// and unvalidated here: a reviewer that omits them or gets them wrong
	// never fails the review, and vx ship falls back to text derived from the
	// branch's commits (see internal/prbody).
	PRTitle       string `json:"pr_title"`
	PRDescription string `json:"pr_description"`
}

// Blocking returns the findings that block the ship, in report order.
func (r Report) Blocking() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Blocking() {
			out = append(out, f)
		}
	}
	return out
}

// Info returns the non-blocking findings.
func (r Report) Info() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if !f.Blocking() {
			out = append(out, f)
		}
	}
	return out
}

// reportSchema is the JSON contract embedded in the reviewer's prompt.
const reportSchema = `{
  "findings": [
    {
      "file": "path/relative/to/repo/root.go",
      "line": 42,
      "severity": "error | warning | info",
      "action": "ask-user | auto-fix | no-op",
      "description": "the defect class, once, with the invariant it violates",
      "failure_scenario": "the concrete sequence from intended usage that produces the failure",
      "sibling_sites": ["other/file.go:10 a few words", "..."]
    }
  ],
  "reviewed_paths": ["every changed file you actually read and judged"],
  "risk_level": "low | medium | high",
  "risk_rationale": "one sentence",
  "pr_title": "feat: concise conventional-commit style title of the whole change, up to about 72 characters",
  "pr_description": "3 to 8 lines of plain markdown: what changed and why"
}`

// parseReport extracts the reviewer's JSON object from its raw stdout and
// validates it against reportSchema's rules. The error is phrased for the
// reviewer: it is fed back verbatim on the bounded retry.
func parseReport(output string) (Report, error) {
	raw, err := extractReportJSON(output)
	if err != nil {
		return Report{}, err
	}

	var payload struct {
		Findings      *[]Finding `json:"findings"`
		ReviewedPaths *[]string  `json:"reviewed_paths"`
		RiskLevel     string     `json:"risk_level"`
		RiskRationale string     `json:"risk_rationale"`
		// Decoded leniently: a wrongly typed pr field is dropped, it must
		// not reject an otherwise valid report.
		PRTitle       json.RawMessage `json:"pr_title"`
		PRDescription json.RawMessage `json:"pr_description"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Report{}, fmt.Errorf("the JSON does not match the schema: %w", err)
	}

	var problems []string
	if payload.Findings == nil {
		problems = append(problems, `"findings" array is missing (use [] when there are none)`)
	}
	if payload.ReviewedPaths == nil {
		problems = append(problems, `"reviewed_paths" array is missing`)
	}
	report := Report{
		RiskLevel:     strings.ToLower(strings.TrimSpace(payload.RiskLevel)),
		RiskRationale: strings.TrimSpace(payload.RiskRationale),
		PRTitle:       optionalString(payload.PRTitle),
		PRDescription: optionalString(payload.PRDescription),
	}
	if payload.Findings != nil {
		report.Findings = *payload.Findings
	}
	if payload.ReviewedPaths != nil {
		report.ReviewedPaths = *payload.ReviewedPaths
	}

	if !slices.Contains(knownRisks, report.RiskLevel) {
		problems = append(problems, fmt.Sprintf(`"risk_level" must be one of low, medium, high (got %q)`, payload.RiskLevel))
	}
	if report.RiskRationale == "" {
		problems = append(problems, `"risk_rationale" is empty`)
	}

	for i := range report.Findings {
		f := &report.Findings[i]
		f.Severity = strings.ToLower(strings.TrimSpace(f.Severity))
		f.Action = strings.ToLower(strings.TrimSpace(f.Action))
		f.File = strings.TrimSpace(f.File)
		f.Description = strings.TrimSpace(f.Description)
		f.FailureScenario = strings.TrimSpace(f.FailureScenario)

		if !slices.Contains(knownSeverities, f.Severity) {
			problems = append(problems, fmt.Sprintf("finding %d: severity must be one of error, warning, info (got %q)", i, f.Severity))
		}
		if !slices.Contains(knownActions, f.Action) {
			problems = append(problems, fmt.Sprintf("finding %d: action must be one of ask-user, auto-fix, no-op (got %q)", i, f.Action))
		}
		if f.File == "" {
			problems = append(problems, fmt.Sprintf("finding %d: file is empty", i))
		}
		if f.Line < 0 {
			problems = append(problems, fmt.Sprintf("finding %d: line must be a one-indexed line number (0 only for a file-level finding)", i))
		}
		if f.Description == "" {
			problems = append(problems, fmt.Sprintf("finding %d: description is empty", i))
		}
		if f.Blocking() && f.FailureScenario == "" {
			problems = append(problems, fmt.Sprintf("finding %d: an error or warning needs a concrete failure_scenario from the change's intended usage; without one, drop it or downgrade it to info", i))
		}
	}

	if len(problems) > 0 {
		return Report{}, errors.New(strings.Join(problems, "; "))
	}
	return report, nil
}

// optionalString decodes raw as a JSON string, returning "" when it is
// absent, null or not a string.
func optionalString(raw json.RawMessage) string {
	var v string
	if len(raw) == 0 || json.Unmarshal(raw, &v) != nil {
		return ""
	}
	return strings.TrimSpace(v)
}

// extractReportJSON finds the reviewer's final JSON object in free-form
// stdout: the last top-level object carrying a "findings" key. Prose and
// code fences around it are tolerated; stray braces in prose are skipped.
func extractReportJSON(output string) (json.RawMessage, error) {
	var found json.RawMessage
	for i := 0; i < len(output); {
		if output[i] != '{' {
			i++
			continue
		}
		dec := json.NewDecoder(strings.NewReader(output[i:]))
		var obj map[string]json.RawMessage
		if err := dec.Decode(&obj); err != nil {
			i++
			continue
		}
		end := i + int(dec.InputOffset())
		if _, ok := obj["findings"]; ok {
			found = json.RawMessage(output[i:end])
		}
		i = end
	}
	if found == nil {
		return nil, errors.New(`the output did not end with a JSON object containing a "findings" key`)
	}
	return found, nil
}

// normalizePath makes a path comparable across the reviewer's spelling and
// git's: slash-separated, no leading "./".
func normalizePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	return path.Clean(strings.ReplaceAll(p, "\\", "/"))
}

// uncovered returns the files in expected the reviewer did not list in
// reviewed, in expected's order.
func uncovered(expected, reviewed []string) []string {
	seen := make(map[string]bool, len(reviewed))
	for _, p := range reviewed {
		seen[normalizePath(p)] = true
	}
	var missing []string
	for _, p := range expected {
		if !seen[normalizePath(p)] {
			missing = append(missing, p)
		}
	}
	return missing
}

// FormatFindings renders findings for a terminal, most severe first, one
// block per finding: header line, then the failure scenario and sibling
// sites indented underneath.
func FormatFindings(findings []Finding) string {
	ordered := slices.Clone(findings)
	rank := func(s string) int { return slices.Index(knownSeverities, s) }
	slices.SortStableFunc(ordered, func(a, b Finding) int { return rank(a.Severity) - rank(b.Severity) })

	var b strings.Builder
	for _, f := range ordered {
		fmt.Fprintf(&b, "[%s] %s (%s) %s\n", f.Severity, f.location(), f.Action, f.Description)
		if f.FailureScenario != "" {
			fmt.Fprintf(&b, "    scenario: %s\n", f.FailureScenario)
		}
		if len(f.SiblingSites) > 0 {
			fmt.Fprintf(&b, "    sibling sites: %s\n", strings.Join(f.SiblingSites, "; "))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// Note renders a finding as one line for a pull request body.
func (f Finding) Note() string {
	line := fmt.Sprintf("`%s` - %s", f.location(), f.Description)
	if f.FailureScenario != "" {
		line += " (" + f.FailureScenario + ")"
	}
	return strings.Join(strings.Fields(line), " ")
}
