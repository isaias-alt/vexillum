package tribunal

import (
	"fmt"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/state"
)

// Marker phrases for the optional prompt parts. They pin "this part is
// present", not any particular wording around it: change them together with
// the matching sentence in review.go or fix.go.
const (
	phraseReReview    = "This is a re-review"
	phraseAdditions   = "no part of the request asked for"
	phraseNeverCommit = "Never commit"
)

const emDash = "\u2014"

func sampleReviewInput() reviewInput {
	return reviewInput{
		Branch:     "vexillum/feature",
		BaseBranch: "main",
		BaseSHA:    "1111111aaaaaaa",
		TargetSHA:  "2222222bbbbbbb",
		Files:      []string{"cmd/root.go", "internal/a/a.go", "internal/a/a_test.go"},
	}
}

// wordsOutsideData counts the prompt's words without the schema and the
// changed-file list, which the word budgets exclude.
func wordsOutsideData(prompt string, in reviewInput) int {
	p := strings.Replace(prompt, reportSchema, "", 1)
	for _, f := range in.Files {
		p = strings.Replace(p, f, "", 1)
	}
	return len(strings.Fields(p))
}

func TestReviewPrompt_CarriesTheFactsTheReviewerNeeds(t *testing.T) {
	in := sampleReviewInput()
	in.Intent = "Add the thing"
	p := buildReviewPrompt(in)

	for _, want := range []string{in.Branch, in.BaseBranch, in.BaseSHA, in.TargetSHA} {
		if !strings.Contains(p, want) {
			t.Errorf("review prompt is missing %q", want)
		}
	}
	for _, f := range in.Files {
		if !strings.Contains(p, f+"\n") {
			t.Errorf("review prompt does not list changed file %q on its own line", f)
		}
	}
	if !strings.Contains(p, fmt.Sprintf("(%d)", len(in.Files))) {
		t.Error("review prompt does not state how many files must be covered")
	}
}

func TestReviewPrompt_SchemaIsTheLastElement(t *testing.T) {
	for _, in := range []reviewInput{
		sampleReviewInput(),
		func() reviewInput { i := sampleReviewInput(); i.Intent = "x"; i.FixStartSHA = "abc"; return i }(),
	} {
		p := buildReviewPrompt(in)
		if !strings.HasSuffix(p, reportSchema) {
			t.Errorf("schema is not the last element of the prompt:\n%s", p)
		}
		if strings.Count(p, reportSchema) != 1 {
			t.Error("schema must appear exactly once")
		}
	}
}

func TestReviewPrompt_NamesTheWireVocabulary(t *testing.T) {
	p := buildReviewPrompt(sampleReviewInput())
	var want []string
	want = append(want, "findings", "reviewed_paths", "risk_level", "risk_rationale", "pr_title", "pr_description")
	want = append(want, "file", "line", "severity", "action", "description", "failure_scenario", "sibling_sites")
	want = append(want, knownSeverities...)
	want = append(want, knownActions...)
	want = append(want, knownRisks...)
	for _, w := range want {
		if !strings.Contains(p, w) {
			t.Errorf("review prompt does not mention %q", w)
		}
	}
	// The prompt prose, not just the schema, has to explain the vocabulary
	// that decides blocking and routing, and what the verdict means.
	prose := strings.Replace(p, reportSchema, "", 1)
	for _, w := range []string{"risk_level", "risk_rationale"} {
		if !strings.Contains(prose, w) {
			t.Errorf("review prompt prose never explains %q", w)
		}
	}
	for _, w := range append(append([]string{}, knownSeverities...), knownActions...) {
		if !strings.Contains(prose, w) {
			t.Errorf("review prompt prose never explains %q", w)
		}
	}
}

func TestReviewPrompt_MissionBlockOnlyWithIntent(t *testing.T) {
	in := sampleReviewInput()
	without := buildReviewPrompt(in)
	in.Intent = "Rename the flag to --force"
	with := buildReviewPrompt(in)

	if strings.Contains(without, "<mission>") || strings.Contains(without, "</mission>") || strings.Contains(without, in.Intent) {
		t.Error("without an intent the prompt must carry no mission block")
	}
	if strings.Contains(without, phraseAdditions) {
		t.Error("without an intent the prompt must not ask for unrequested additions")
	}
	if !strings.Contains(with, "<mission>\n"+in.Intent+"\n</mission>") {
		t.Errorf("with an intent the prompt must carry the delimited mission block:\n%s", with)
	}
	if !strings.Contains(with, phraseAdditions) {
		t.Error("with an intent the prompt must ask for unrequested additions")
	}
	if len(without) >= len(with) {
		t.Errorf("expected the prompt without an intent to be shorter (%d vs %d)", len(without), len(with))
	}
	if blank := buildReviewPrompt(reviewInput{Branch: "b", BaseBranch: "m", Intent: "  \n "}); strings.Contains(blank, "<mission>") {
		t.Error("a whitespace-only intent counts as no intent")
	}
}

func TestReviewPrompt_MissionKeepsTheRequestFirstAndLaterInstructionsInOrder(t *testing.T) {
	opts := Options{
		TaskPrompt: "ORIGINAL request text",
		TaskAmendments: []state.Amendment{
			{Text: "FIRST later instruction"},
			{Text: "SECOND later instruction"},
		},
	}
	in := sampleReviewInput()
	in.Intent = opts.intent()
	p := buildReviewPrompt(in)

	i0 := strings.Index(p, "ORIGINAL request text")
	i1 := strings.Index(p, "FIRST later instruction")
	i2 := strings.Index(p, "SECOND later instruction")
	if i0 < 0 || i1 < 0 || i2 < 0 || !(i0 < i1 && i1 < i2) {
		t.Fatalf("expected original, first, second in that order, got offsets %d %d %d:\n%s", i0, i1, i2, p)
	}
	open, closing := strings.Index(p, "<mission>"), strings.Index(p, "</mission>")
	if !(open < i0 && i2 < closing) {
		t.Error("the whole composed intent must sit between the mission markers")
	}
}

func TestReviewPrompt_RepairCommitClauseOnlyWithFixStart(t *testing.T) {
	in := sampleReviewInput()
	first := buildReviewPrompt(in)
	if strings.Contains(first, phraseReReview) {
		t.Error("a first review must not carry the repair-commit clause")
	}
	in.FixStartSHA = "cafe1234feed"
	again := buildReviewPrompt(in)
	if !strings.Contains(again, phraseReReview) || !strings.Contains(again, in.FixStartSHA) {
		t.Errorf("a re-review must carry the clause and the pre-repair commit:\n%s", again)
	}
	if strings.Contains(first, in.FixStartSHA) {
		t.Error("the fix start commit leaked into a first review")
	}
}

func TestReviewPrompt_TextRules(t *testing.T) {
	full := sampleReviewInput()
	full.Intent = "Do the thing"
	full.FixStartSHA = "abc123"
	for name, in := range map[string]reviewInput{"bare": sampleReviewInput(), "full": full} {
		p := buildReviewPrompt(in)
		if strings.Contains(p, emDash) {
			t.Errorf("%s prompt contains an em dash", name)
		}
		if n := wordsOutsideData(p, in); n > 1200 {
			t.Errorf("%s prompt is %d words outside the schema and file list, budget is about 1200", name, n)
		}
		for _, banned := range []string{"--resume", "--continue", "previous session", "earlier session", "resume"} {
			if strings.Contains(strings.ToLower(p), banned) {
				t.Errorf("%s prompt refers to prior sessions (%q)", name, banned)
			}
		}
	}
}

func TestReviewPrompt_AsksForPullRequestText(t *testing.T) {
	p := buildReviewPrompt(sampleReviewInput())
	prose := strings.Replace(p, reportSchema, "", 1)
	for _, field := range []string{"pr_title", "pr_description"} {
		if !strings.Contains(prose, field) {
			t.Errorf("the prose must ask for %s, not only the schema", field)
		}
	}
}

// Through the real runReview and the fake claude: the review is a brand-new
// process, every changed file reaches the prompt, and no diff content does.
func TestRunReview_PromptCarriesNoDiffAndEveryFile(t *testing.T) {
	dir := newTribunalRepo(t)
	files := []string{"a.txt", "dir/b.txt", "dir/deeper/c.txt", "d.txt", "e.txt", "f.txt"}
	for _, f := range files {
		commitFile(t, dir, f, "DIFF_BODY_MARKER "+f+"\n")
	}
	stub := newClaudeStub(t)
	stub.out(1, reportJSON("", files...))

	sr, err := runReview(dir, "base", Options{Branch: "vexillum/x", TaskPrompt: "Add six files"}, "")
	if err != nil {
		t.Fatalf("runReview: %v", err)
	}
	if !sr.Passed {
		t.Fatalf("expected a pass, got %+v", sr)
	}
	p := stub.prompt(1)
	for _, f := range files {
		if !strings.Contains(p, f) {
			t.Errorf("prompt does not name changed file %s", f)
		}
	}
	for _, leak := range []string{"DIFF_BODY_MARKER", "diff --git", "\n@@ ", "\n+++ ", "\n--- a/"} {
		if strings.Contains(p, leak) {
			t.Errorf("prompt carries diff content (%q)", leak)
		}
	}
	base := strings.TrimSpace(run(t, dir, "rev-parse", "base"))
	head := strings.TrimSpace(run(t, dir, "rev-parse", "HEAD"))
	for _, sha := range []string{base, head} {
		if !strings.Contains(p, sha) {
			t.Errorf("prompt does not carry commit %s", sha)
		}
	}
}

func TestRunReview_ArgvIsAFreshProcessWithTheConfiguredModel(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "x.txt", "x\n")
	stub := newClaudeStub(t)
	stub.out(1, reportJSON("", "x.txt"))

	if _, err := runReview(dir, "base", Options{Branch: "b"}, ""); err != nil {
		t.Fatalf("runReview: %v", err)
	}
	args := stub.args(1)
	for _, want := range []string{reviewModel, reviewEffort} {
		if !strings.Contains(args, want) {
			t.Errorf("argv does not carry %q:\n%s", want, args)
		}
	}
	for _, line := range strings.Split(args, "\n") {
		switch line {
		case "--resume", "-r", "--continue", "-c":
			t.Errorf("argv resumes or continues a session: %q", line)
		}
	}
}

func TestRunReview_RetryAppendsTheValidationErrorAfterTheSamePrompt(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "x.txt", "x\n")
	stub := newClaudeStub(t)
	stub.out(1, `{"findings": [], "reviewed_paths": ["x.txt"], "risk_level": "low"}`)
	stub.out(2, reportJSON("", "x.txt"))

	sr, err := runReview(dir, "base", Options{Branch: "b"}, "")
	if err != nil {
		t.Fatalf("runReview: %v", err)
	}
	if !sr.Passed || stub.calls() != 2 {
		t.Fatalf("expected a pass on the second attempt, got %+v after %d calls", sr, stub.calls())
	}
	first, second := stub.prompt(1), stub.prompt(2)
	if !strings.HasPrefix(second, first) || len(second) <= len(first) {
		t.Error("the retry must be the original prompt with the rejection appended after it")
	}
	if !strings.Contains(second[len(first):], "risk_rationale") {
		t.Errorf("the appended text must carry the validation error, got:\n%s", second[len(first):])
	}
	if strings.Contains(stub.args(2), "--resume") {
		t.Error("the retry must not resume the rejected session")
	}
}

func TestFixPrompt_RoleBranchFindingsAndNeverCommit(t *testing.T) {
	findings := []Finding{{
		File: "a.go", Line: 7, Severity: SeverityError, Action: ActionAutoFix,
		Description: "check missing", FailureScenario: "empty id returns success",
		SiblingSites: []string{"b.go:3 same gap"},
	}}
	p := buildFixPrompt(findings, "", "vexillum/fixme")

	if !strings.HasPrefix(p, fixerRoleMarker) {
		t.Errorf("fix prompt must start with the role marker, got:\n%s", p)
	}
	if strings.Contains(buildReviewPrompt(sampleReviewInput()), fixerRoleMarker) {
		t.Error("the review prompt must not carry the fixer role marker")
	}
	if !strings.Contains(p, "vexillum/fixme") {
		t.Error("fix prompt does not name the branch")
	}
	if !strings.Contains(p, FormatFindings(findings)) {
		t.Error("fix prompt does not carry the findings in the formatter's shape")
	}
	if !strings.Contains(p, phraseNeverCommit) {
		t.Error("fix prompt must state the fixer never commits")
	}
	// The only mention of committing is the prohibition.
	if strings.Count(strings.ToLower(p), "commit") != strings.Count(strings.ToLower(p), strings.ToLower(phraseNeverCommit))+strings.Count(strings.ToLower(p), "the caller commits") {
		t.Errorf("fix prompt talks about committing beyond the prohibition:\n%s", p)
	}
	if strings.Contains(p, emDash) {
		t.Error("fix prompt contains an em dash")
	}
}

func TestFixPrompt_MissionBlockOnlyWithIntent(t *testing.T) {
	findings := []Finding{{File: "a.go", Severity: SeverityWarning, Description: "d"}}
	without := buildFixPrompt(findings, "", "b")
	with := buildFixPrompt(findings, "Ship the feature", "b")
	if strings.Contains(without, "<mission>") || strings.Contains(without, "Ship the feature") {
		t.Error("without an intent the fix prompt must carry no mission block")
	}
	if !strings.Contains(with, "<mission>\nShip the feature\n</mission>") {
		t.Errorf("with an intent the fix prompt must carry the delimited mission block:\n%s", with)
	}
}

func TestFixPrompt_WordBudget(t *testing.T) {
	findings := []Finding{{File: "a.go", Severity: SeverityWarning, Description: "d"}}
	p := buildFixPrompt(findings, "x", "b")
	p = strings.Replace(p, FormatFindings(findings), "", 1)
	p = strings.Replace(p, "<mission>\nx\n</mission>", "", 1)
	if n := len(strings.Fields(p)); n > 450 {
		t.Errorf("fix prompt is %d words outside the findings and the mission, budget is about 450", n)
	}
}
