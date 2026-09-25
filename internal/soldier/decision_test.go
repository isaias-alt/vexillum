package soldier_test

import (
	"testing"

	"github.com/isaias-alt/vexillum/internal/soldier"
)

// A soldier asking a genuine clarifying question with numbered options is
// extracted as a real question string plus a real options list - not a
// pointer back into the raw transcript.
func TestExtractDecision_QuestionWithNumberedOptions(t *testing.T) {
	transcript := "I've reviewed the auth module and found two viable paths.\n\n" +
		"Which auth library should I use?\n\n" +
		"1. Auth0 - hosted, less code to maintain\n" +
		"2. Keycloak - self-hosted, more control\n"

	got := soldier.ExtractDecision(transcript)
	if got == nil {
		t.Fatal("expected a non-nil decision")
	}
	if got.Question != "Which auth library should I use?" {
		t.Errorf("Question = %q, want the trailing question line", got.Question)
	}
	if len(got.Options) != 2 || got.Options[0] != "Auth0 - hosted, less code to maintain" || got.Options[1] != "Keycloak - self-hosted, more control" {
		t.Errorf("Options = %v, want the two numbered options in order", got.Options)
	}
	if got.AskedAt.IsZero() {
		t.Error("expected AskedAt to be set")
	}
	if got.Answer != "" || !got.AnsweredAt.IsZero() {
		t.Errorf("expected no answer yet, got %+v", got)
	}
}

// Bulleted options in the same paragraph as the question (no blank line
// separating them) are still split out correctly.
func TestExtractDecision_QuestionWithBulletedOptionsSameParagraph(t *testing.T) {
	transcript := "Should I delete the legacy config or keep it for now?\n" +
		"- delete it\n" +
		"- keep it behind a flag\n"

	got := soldier.ExtractDecision(transcript)
	if got == nil {
		t.Fatal("expected a non-nil decision")
	}
	if got.Question != "Should I delete the legacy config or keep it for now?" {
		t.Errorf("Question = %q", got.Question)
	}
	if len(got.Options) != 2 || got.Options[0] != "delete it" || got.Options[1] != "keep it behind a flag" {
		t.Errorf("Options = %v", got.Options)
	}
}

// A plain question with no options at all still produces a decision - the
// options field is optional, the question is not.
func TestExtractDecision_QuestionWithoutOptions(t *testing.T) {
	transcript := "Some earlier progress notes.\n\nDo you want me to proceed with the migration now, or wait for the freeze to lift?\n"

	got := soldier.ExtractDecision(transcript)
	if got == nil {
		t.Fatal("expected a non-nil decision")
	}
	if got.Question != "Do you want me to proceed with the migration now, or wait for the freeze to lift?" {
		t.Errorf("Question = %q", got.Question)
	}
	if len(got.Options) != 0 {
		t.Errorf("expected no options, got %v", got.Options)
	}
}

// Trailing blank lines (a common transcript artifact) don't change the
// extracted question.
func TestExtractDecision_IgnoresTrailingBlankLines(t *testing.T) {
	transcript := "Which environment should this deploy to?\n\n\n   \n"

	got := soldier.ExtractDecision(transcript)
	if got == nil {
		t.Fatal("expected a non-nil decision")
	}
	if got.Question != "Which environment should this deploy to?" {
		t.Errorf("Question = %q", got.Question)
	}
}

// An empty transcript (e.g. AgentRead itself failed) yields no decision at
// all - there's nothing here to fabricate a question out of.
func TestExtractDecision_EmptyTranscript(t *testing.T) {
	if got := soldier.ExtractDecision(""); got != nil {
		t.Errorf("expected nil for an empty transcript, got %+v", got)
	}
	if got := soldier.ExtractDecision("   \n\n  \n"); got != nil {
		t.Errorf("expected nil for a blank transcript, got %+v", got)
	}
}

// A transcript whose only content is a bare options list, with no leading
// prose anywhere, still produces a decision rather than none at all - the
// task is genuinely blocked and needs some commander-facing text.
func TestExtractDecision_OptionsOnlyFallsBackToOptionsAsQuestion(t *testing.T) {
	transcript := "1. Option A\n2. Option B\n"

	got := soldier.ExtractDecision(transcript)
	if got == nil {
		t.Fatal("expected a non-nil decision")
	}
	if got.Question == "" {
		t.Error("expected a non-empty fallback question")
	}
	if len(got.Options) != 0 {
		t.Errorf("expected the fallback to consume the options into the question, got %v", got.Options)
	}
}
