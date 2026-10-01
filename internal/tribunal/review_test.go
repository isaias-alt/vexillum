package tribunal

import "testing"

func TestParseVerdict_Pass(t *testing.T) {
	passed, detail := parseVerdict("Looks good, no issues found.\n" + VerdictPrefix + " PASS")
	if !passed {
		t.Fatalf("expected passed=true, detail=%q", detail)
	}
	if detail != "" {
		t.Errorf("expected no detail on a pass, got %q", detail)
	}
}

func TestParseVerdict_FailWithReason(t *testing.T) {
	passed, detail := parseVerdict("Found a nil pointer dereference.\n" + VerdictPrefix + " FAIL: nil deref in Foo when x is empty")
	if passed {
		t.Fatal("expected passed=false")
	}
	if detail != "nil deref in Foo when x is empty" {
		t.Errorf("expected the reason to be extracted verbatim, got %q", detail)
	}
}

func TestParseVerdict_FailWithoutReason(t *testing.T) {
	passed, detail := parseVerdict(VerdictPrefix + " FAIL")
	if passed {
		t.Fatal("expected passed=false")
	}
	if detail == "" {
		t.Error("expected a fallback reason when none was given")
	}
}

func TestParseVerdict_NoVerdictLineIsTreatedAsFailure(t *testing.T) {
	passed, detail := parseVerdict("The soldier rambled and never gave a verdict.")
	if passed {
		t.Fatal("expected an ambiguous review (no verdict line) to be treated as a failure, not a pass")
	}
	if detail == "" {
		t.Error("expected a detail explaining the missing verdict")
	}
}

func TestParseVerdict_UsesTheLastVerdictLine(t *testing.T) {
	// A soldier's own reasoning might mention "PASS"/"FAIL" in passing
	// before its actual final verdict - only the last matching line
	// counts.
	output := "if this were failing I'd write " + VerdictPrefix + " FAIL here, but it isn't.\n" + VerdictPrefix + " PASS"
	passed, _ := parseVerdict(output)
	if !passed {
		t.Fatal("expected the last verdict line (PASS) to win")
	}
}

func TestRunReview_NoChangesSkipsWithoutInvokingClaude(t *testing.T) {
	dir := newTribunalRepo(t)
	// git-only PATH, no claude stub: if runReview tried to invoke claude
	// here, it would fail with "executable file not found".
	t.Setenv("PATH", gitOnlyPath(t))

	sr, err := runReview(dir, "base")
	if err != nil {
		t.Fatalf("runReview: %v", err)
	}
	if !sr.Passed {
		t.Errorf("expected no-diff to pass trivially, got: %+v", sr)
	}
}

func TestRunReview_PassVerdict(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "change.txt", "hi\n")
	t.Setenv("PATH", fakeClaude(t, "reviewed, no issues.\n"+VerdictPrefix+" PASS"))

	sr, err := runReview(dir, "base")
	if err != nil {
		t.Fatalf("runReview: %v", err)
	}
	if !sr.Passed {
		t.Errorf("expected the review to pass, got: %+v", sr)
	}
}

func TestRunReview_FailVerdictBlocks(t *testing.T) {
	dir := newTribunalRepo(t)
	commitFile(t, dir, "change.txt", "hi\n")
	t.Setenv("PATH", fakeClaude(t, "found a bug.\n"+VerdictPrefix+" FAIL: off-by-one in the loop"))

	sr, err := runReview(dir, "base")
	if err != nil {
		t.Fatalf("runReview: %v", err)
	}
	if sr.Passed {
		t.Fatal("expected the review to fail")
	}
	if sr.Detail != "off-by-one in the loop" {
		t.Errorf("expected the failure reason in Detail, got %q", sr.Detail)
	}
}
