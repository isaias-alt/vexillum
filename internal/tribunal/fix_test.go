package tribunal

import (
	"strings"
	"testing"
)

func TestIsFixCommit(t *testing.T) {
	if !IsFixCommit(fixCommitMessage(2)) {
		t.Errorf("expected the fix loop's own commit subject %q to be recognized", fixCommitMessage(2))
	}
	for _, subject := range []string{"fix: handle empty input", "feat: add change.txt", "", "address tribunal review findings"} {
		if IsFixCommit(subject) {
			t.Errorf("expected %q not to be a fix loop commit", subject)
		}
	}
}

func TestBuildFixPrompt_CarriesRulesFindingsAndOptionalMission(t *testing.T) {
	findings := []Finding{{File: "a.go", Line: 4, Severity: SeverityError, Action: ActionAutoFix, Description: "DESC", FailureScenario: "SCENARIO", SiblingSites: []string{"b.go:9 twin"}}}
	bare := buildFixPrompt(findings, "", "br")
	if !strings.HasPrefix(bare, fixerRoleMarker) || strings.Contains(bare, "<mission>") {
		t.Errorf("expected the role marker first and no mission block:\n%s", bare)
	}
	for _, want := range []string{"branch is br", "Never commit", "[error] a.go:4 (auto-fix) DESC", "scenario: SCENARIO", "sibling sites: b.go:9 twin"} {
		if !strings.Contains(bare, want) {
			t.Errorf("expected the fix prompt to contain %q:\n%s", want, bare)
		}
	}
	if strings.Contains(bare, "—") {
		t.Error("the prompt must not contain an em dash")
	}
	if with := buildFixPrompt(findings, "THE MISSION", "br"); !strings.Contains(with, "<mission>\nTHE MISSION\n</mission>") {
		t.Errorf("expected the mission block:\n%s", with)
	}
}
