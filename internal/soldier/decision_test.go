package soldier_test

import (
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
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

// A prose-extracted Decision is tagged DecisionKindProse, so
// AnswerBlocked knows to answer it as plain text, not a modal digit key.
func TestExtractDecision_TaggedAsProseKind(t *testing.T) {
	got := soldier.ExtractDecision("Which environment should this deploy to?\n\n1. Staging\n2. Prod\n")
	if got == nil {
		t.Fatal("expected a non-nil decision")
	}
	if got.Kind != state.DecisionKindProse {
		t.Errorf("Kind = %q, want %q", got.Kind, state.DecisionKindProse)
	}
}

// A soldier that follows needsDecisionInstructions and ends its turn with
// a needs-decision: line produces a Decision from that line alone - the
// only thing that can turn a plain-prose question into a real block
// (herdr's own classifier never catches ordinary prose).
func TestExtractNeedsDecisionSignal_MatchesLine(t *testing.T) {
	transcript := "I looked into the migration path.\n\n" +
		"needs-decision: should the migration run online or require downtime?\n"

	got, found := soldier.ExtractNeedsDecisionSignal(transcript)
	if !found {
		t.Fatal("expected the needs-decision line to be found")
	}
	if got.Question != "should the migration run online or require downtime?" {
		t.Errorf("Question = %q", got.Question)
	}
	if got.Kind != state.DecisionKindProse {
		t.Errorf("Kind = %q, want %q", got.Kind, state.DecisionKindProse)
	}
	if got.AskedAt.IsZero() {
		t.Error("expected AskedAt to be set")
	}
}

// The match is case-insensitive on the verb, and tolerant of leading
// whitespace - matching how a soldier's own shell/echo output might be
// indented.
func TestExtractNeedsDecisionSignal_CaseInsensitiveAndIndented(t *testing.T) {
	transcript := "  Needs-Decision:   use Postgres or SQLite?  \n"

	got, found := soldier.ExtractNeedsDecisionSignal(transcript)
	if !found {
		t.Fatal("expected the needs-decision line to be found")
	}
	if got.Question != "use Postgres or SQLite?" {
		t.Errorf("Question = %q", got.Question)
	}
}

// Several needs-decision lines in the transcript (e.g. an earlier one from
// a previous turn) resolve to the last one - the most recent question is
// what's actually still open.
func TestExtractNeedsDecisionSignal_TakesTheLastMatch(t *testing.T) {
	transcript := "needs-decision: an old question already resolved\n" +
		"working on it...\n" +
		"needs-decision: the current open question\n"

	got, found := soldier.ExtractNeedsDecisionSignal(transcript)
	if !found {
		t.Fatal("expected a match")
	}
	if got.Question != "the current open question" {
		t.Errorf("Question = %q, want the last match", got.Question)
	}
}

// No needs-decision line at all (the common case - ordinary output, or a
// soldier that used AskUserQuestion instead) reports no signal, never an
// invented Decision.
func TestExtractNeedsDecisionSignal_NoMatch(t *testing.T) {
	if _, found := soldier.ExtractNeedsDecisionSignal("all done, opened PR #4\n"); found {
		t.Error("expected no signal for ordinary output")
	}
	if _, found := soldier.ExtractNeedsDecisionSignal(""); found {
		t.Error("expected no signal for an empty transcript")
	}
}

// promptTrailer is the last sentence of every dispatched prompt.
const promptTrailer = "do not use it for an ordinary status update."

// The bug seen in a real session: the pane echoes the whole dispatched
// prompt, so the template line it teaches ("needs-decision: <a one-line
// summary ...>") is in the transcript of every soldier. It is documentation,
// never a question.
func TestExtractNeedsDecisionSignal_TemplateLineIsNotAQuestion(t *testing.T) {
	cases := map[string]string{
		"the template alone":        "needs-decision: <a one-line summary of the question and any options>\n",
		"the template, indented":    "  needs-decision: <a one-line summary of the question and any options>\n",
		"another bare placeholder":  "needs-decision: <question>\n",
		"a placeholder with spaces": "needs-decision:   <one line, any options>  \n",
	}
	for name, transcript := range cases {
		t.Run(name, func(t *testing.T) {
			if d, found := soldier.ExtractNeedsDecisionSignal(transcript); found {
				t.Errorf("expected no signal, got %+v", d)
			}
		})
	}
}

// Angle brackets inside a real question are fine: only text that is
// entirely one bracketed group is a placeholder.
func TestExtractNeedsDecisionSignal_AngleBracketsInsideARealQuestion(t *testing.T) {
	got, found := soldier.ExtractNeedsDecisionSignal("needs-decision: should Foo<T> stay generic or become Foo<int>?\n")
	if !found || got.Question != "should Foo<T> stay generic or become Foo<int>?" {
		t.Fatalf("expected the real question, got %+v found=%v", got, found)
	}
}

// Only what follows the end of the dispatched prompt counts: a
// needs-decision line inside the echoed prompt (a task that quotes one, or
// the template itself) must not block, whatever it says.
func TestExtractNeedsDecisionSignal_IgnoresTheEchoedPrompt(t *testing.T) {
	echoed := "> fix the parser. If unsure, write\n" +
		"needs-decision: which grammar version should I target?\n\n" +
		"  needs-decision: <a one-line summary of the question and any options>\n\n" +
		"This is the only way vexillum can tell... " + promptTrailer + "\n"

	if d, found := soldier.ExtractNeedsDecisionSignal(echoed + "\nAll done, opened a PR.\n"); found {
		t.Errorf("expected nothing from the echoed prompt, got %+v", d)
	}

	got, found := soldier.ExtractNeedsDecisionSignal(echoed + "\nneeds-decision: use PEG or LALR?\n")
	if !found || got.Question != "use PEG or LALR?" {
		t.Errorf("expected the soldier's own line after the prompt, got %+v found=%v", got, found)
	}
}

// A placeholder after the prompt does not hide a real question before it
// from being found, and a real last line still wins.
func TestExtractNeedsDecisionSignal_SkipsPlaceholdersButKeepsRealLines(t *testing.T) {
	transcript := promptTrailer + "\nneeds-decision: pick A or B?\nneeds-decision: <question>\n"
	got, found := soldier.ExtractNeedsDecisionSignal(transcript)
	if !found || got.Question != "pick A or B?" {
		t.Errorf("expected the real question, got %+v found=%v", got, found)
	}
}

// The scrollback keeps a question after it was answered (or dismissed), so
// an answered question's line must not block the task again.
func TestFinalTurnNeedsDecision_IgnoresAnAlreadyAnsweredQuestion(t *testing.T) {
	transcript := promptTrailer + "\nneeds-decision: which database?\nUse Postgres\nall done\n"

	task := state.Task{}
	if _, found := soldier.FinalTurnNeedsDecision(task, transcript); !found {
		t.Fatal("expected the line to count for a task with no earlier decision")
	}

	task.Decision = &state.Decision{Question: "which database?", AskedAt: time.Now()}
	if _, found := soldier.FinalTurnNeedsDecision(task, transcript); !found {
		t.Error("expected an unanswered decision's question to still count (it is the open question)")
	}

	task.Decision.AnsweredAt = time.Now()
	if d, found := soldier.FinalTurnNeedsDecision(task, transcript); found {
		t.Errorf("expected the answered question to be ignored, got %+v", d)
	}

	// A different question after the answer is a genuinely new block.
	next := transcript + "needs-decision: and which cache?\n"
	got, found := soldier.FinalTurnNeedsDecision(task, next)
	if !found || got.Question != "and which cache?" {
		t.Errorf("expected the new question to count, got %+v found=%v", got, found)
	}
}
