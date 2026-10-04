package tribunal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const realisticHandlerBefore = `package handlers

func GetUser(id string) string { return "user:" + id }
`

// The handler has two entry points that must both validate the id; the
// "change" only validates one of them, which is the defect the fake reviewer
// reports with a sibling site.
const realisticHandlerAfter = `package handlers

import "errors"

var ErrEmptyID = errors.New("empty id")

func GetUser(id string) (string, error) {
	if id == "" {
		return "", ErrEmptyID
	}
	return "user:" + id, nil
}

func DeleteUser(id string) error {
	return nil
}
`

const realisticHandlerFixed = `package handlers

import "errors"

var ErrEmptyID = errors.New("empty id")

func GetUser(id string) (string, error) {
	if id == "" {
		return "", ErrEmptyID
	}
	return "user:" + id, nil
}

func DeleteUser(id string) error {
	if id == "" {
		return ErrEmptyID
	}
	return nil
}
`

// Drives the whole review-fix-review loop with a fake claude on a realistic
// multi-file change: first review reports one class with a sibling site, the
// fixer rewrites the sibling, a fresh reviewer passes. It pins what each of
// the three prompts must (and must not) carry.
func TestLoop_RealisticChangeReviewedFixedAndReReviewed(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "handlers/users.go", realisticHandlerBefore)
	run(t, dir, "branch", "-f", "base", "HEAD")
	commitFile(t, dir, "handlers/users.go", realisticHandlerAfter)
	commitFile(t, dir, "handlers/users_test.go", "package handlers\n\n// TEST_ONLY_MARKER\n")
	startHead := strings.TrimSpace(run(t, dir, "rev-parse", "HEAD"))

	stub := newClaudeStub(t)
	blocking := `{"file": "handlers/users.go", "line": 9, "severity": "error", "action": "auto-fix",
		"description": "the id is validated on the read path only, so the empty-id invariant does not hold on the write path",
		"failure_scenario": "DeleteUser(\"\") returns nil and the caller reports success for an id that never existed",
		"sibling_sites": ["handlers/users.go:15 DeleteUser skips the same empty-id check"]}`
	stub.out(1, reportJSON(blocking, "handlers/users.go", "handlers/users_test.go"))
	fixed := filepath.Join(dir, "handlers", "users.go")
	stub.hook(2, "cat > handlers/users.go <<'GO'\n"+realisticHandlerFixed+"GO\n")
	stub.out(3, reportJSON("", "handlers/users.go", "handlers/users_test.go"))

	opts := Options{Branch: "vexillum/realistic", TaskPrompt: "Validate the user id in the user handlers", Fix: true}
	result, err := Run(dir, "base", opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.Passed() {
		t.Fatalf("expected the loop to end passing, got %+v", result.Steps)
	}
	if stub.calls() != 3 {
		t.Fatalf("expected review, fix, review = 3 claude calls, got %d", stub.calls())
	}

	for n := 1; n <= 3; n++ {
		p := stub.prompt(n)
		if strings.Contains(p, "\u2014") {
			t.Errorf("prompt %d contains an em dash", n)
		}
		for _, leak := range []string{"TEST_ONLY_MARKER", "diff --git", "\n@@ ", "ErrEmptyID = errors.New"} {
			if strings.Contains(p, leak) {
				t.Errorf("prompt %d must not carry diff content (%q):\n%s", n, leak, p)
			}
		}
	}

	review := stub.prompt(1)
	for _, want := range []string{
		"## Scope", "## Method", "## Evidence bar", "## Protected resources and user data", "## Coverage",
		"## How to write findings", "## Pull request text", "## Mission", "## Simplification pass", "## Verdict and output",
		"- handlers/users.go\n", "- handlers/users_test.go\n",
		"<mission>\nValidate the user id in the user handlers\n</mission>",
		`"reviewed_paths"`, `"sibling_sites"`, `"severity": "error | warning | info"`,
	} {
		if !strings.Contains(review, want) {
			t.Errorf("expected the review prompt to contain %q", want)
		}
	}
	if strings.Contains(review, "## Fix-round provenance") {
		t.Error("the first review must not carry the provenance section")
	}
	// The JSON schema is the last thing in the prompt.
	if !strings.HasSuffix(strings.TrimSpace(review), "}") || strings.LastIndex(review, "## Verdict and output") < strings.LastIndex(review, "## Simplification pass") {
		t.Error("expected the verdict and schema to close the review prompt")
	}

	fix := stub.prompt(2)
	for _, want := range []string{
		fixerRoleMarker, "vexillum/realistic", "Never commit",
		"[error] handlers/users.go:9 (auto-fix)",
		`scenario: DeleteUser("") returns nil`,
		"sibling sites: handlers/users.go:15 DeleteUser skips the same empty-id check",
		"<mission>\nValidate the user id in the user handlers\n</mission>",
		"invariant", "remove the path rather than hardening it", "Do not run the whole repository's tests",
	} {
		if !strings.Contains(fix, want) {
			t.Errorf("expected the fix prompt to contain %q:\n%s", want, fix)
		}
	}

	rereview := stub.prompt(3)
	if !strings.Contains(rereview, "## Fix-round provenance") || !strings.Contains(rereview, "Every commit after "+startHead) {
		t.Errorf("expected the re-review to name the pre-fix head %s in the provenance section", startHead)
	}
	if strings.Contains(stub.args(3), "--resume") {
		t.Error("the re-review must be a fresh process")
	}

	// vexillum committed the fixer's edit on top of the change.
	if subject := strings.TrimSpace(run(t, dir, "log", "-1", "--format=%s")); !IsFixCommit(subject) {
		t.Errorf("expected the fixer's commit on top, got %q", subject)
	}
	got, err := os.ReadFile(fixed)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != realisticHandlerFixed {
		t.Errorf("expected the fixer's edit in the camp, got:\n%s", got)
	}
}

// Without a mission statement the prompt has no mission or simplification
// section, and the fix-round provenance section appears only on re-reviews.
func TestBuildReviewPrompt_OptionalSectionsComeAndGoAsAUnit(t *testing.T) {
	base := reviewInput{Branch: "b", BaseBranch: "main", BaseSHA: "aaa", TargetSHA: "bbb", Files: []string{"x.go"}}
	bare := buildReviewPrompt(base)
	for _, absent := range []string{"## Mission", "<mission>", "## Simplification pass", "## Fix-round provenance"} {
		if strings.Contains(bare, absent) {
			t.Errorf("bare prompt must not contain %q", absent)
		}
	}
	full := base
	full.Intent = "do it"
	full.FixStartSHA = "ccc"
	p := buildReviewPrompt(full)
	for _, present := range []string{"## Mission", "## Simplification pass", "## Fix-round provenance", "Every commit after ccc"} {
		if !strings.Contains(p, present) {
			t.Errorf("full prompt must contain %q", present)
		}
	}
}
