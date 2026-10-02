package soldier_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/soldier"
	"github.com/isaias-alt/vexillum/internal/state"
)

// askUserQuestionCapture reconstructs a raw --source visible --ansi
// capture of Claude Code's AskUserQuestion modal, matching the byte shape
// documented in the durable decision record's capture report (captured
// live against herdr 0.9.3 / Claude Code CLI v2.1.280): a gray rule, a
// black-on-lavender "☐ <label>" header box, a bold-white question line, a
// numbered option list (the selected option's label in blue), a second
// rule directly before the two Claude-Code-injected options, and the
// modal's own footer.
func askUserQuestionCapture(label, question string, modelOptions []string) string {
	const (
		gray  = "\x1b[38;2;153;153;153m"
		reset = "\x1b[0m"
		hdr   = "\x1b[38;2;0;0;0m\x1b[48;2;177;185;249m"
		bold  = "\x1b[1m\x1b[38;2;255;255;255m"
		blue  = "\x1b[38;2;74;165;240m"
	)

	rule := gray + strings.Repeat("─", 60) + reset

	var b strings.Builder
	b.WriteString(rule + "\n")
	b.WriteString(hdr + " ☐ " + label + reset + "\n\n")
	b.WriteString(bold + question + reset + "\n\n")

	all := append(append([]string{}, modelOptions...), "Type something.", "Chat about this")
	for i, opt := range all {
		n := i + 1
		if n == len(all) {
			// Second rule, directly before the last (auto-injected)
			// option.
			b.WriteString(rule + "\n")
			b.WriteString("  " + n1(n) + ". " + opt + "\n")
			continue
		}
		if n == 1 {
			b.WriteString(blue + "❯ " + n1(n) + ". " + opt + reset + "\n")
		} else {
			b.WriteString("  " + gray + n1(n) + "." + reset + " " + opt + "\n")
		}
	}
	b.WriteString("\n" + gray + "Enter to select · ↑/↓ to navigate · Esc to cancel" + reset + "\n")
	return b.String()
}

func n1(n int) string {
	return string(rune('0' + n))
}

func TestParseAskUserQuestionModal_TwoOptions(t *testing.T) {
	capture := askUserQuestionCapture("File name", "Should the test file be named foo.txt or bar.txt?",
		[]string{"foo.txt", "bar.txt"})

	got, err := soldier.ParseAskUserQuestionModal(capture)
	if err != nil {
		t.Fatalf("ParseAskUserQuestionModal: %v", err)
	}
	if got.Question != "Should the test file be named foo.txt or bar.txt?" {
		t.Errorf("Question = %q", got.Question)
	}
	want := []string{"foo.txt", "bar.txt", "Type something.", "Chat about this"}
	if len(got.Options) != len(want) {
		t.Fatalf("Options = %v, want %v", got.Options, want)
	}
	for i, opt := range want {
		if got.Options[i] != opt {
			t.Errorf("Options[%d] = %q, want %q", i, got.Options[i], opt)
		}
	}
	if got.Kind != state.DecisionKindModal {
		t.Errorf("Kind = %q, want %q", got.Kind, state.DecisionKindModal)
	}
	if got.AskedAt.IsZero() {
		t.Error("expected AskedAt to be set")
	}
}

// ANSI codes never leak into the parsed Question/Options text.
func TestParseAskUserQuestionModal_StripsANSI(t *testing.T) {
	capture := askUserQuestionCapture("File name", "Which one?", []string{"A", "B"})

	got, err := soldier.ParseAskUserQuestionModal(capture)
	if err != nil {
		t.Fatalf("ParseAskUserQuestionModal: %v", err)
	}
	if strings.Contains(got.Question, "\x1b") {
		t.Errorf("expected Question free of ANSI codes, got %q", got.Question)
	}
	for _, opt := range got.Options {
		if strings.Contains(opt, "\x1b") {
			t.Errorf("expected option free of ANSI codes, got %q", opt)
		}
	}
}

// A blocked shape that isn't AskUserQuestion at all (no matching footer -
// e.g. the workspace-trust dialog's "Enter to confirm ... Esc to cancel")
// fails loud instead of guessing.
func TestParseAskUserQuestionModal_NotThisModalFailsLoud(t *testing.T) {
	capture := "Is this a project you created or one you trust?\n" +
		"❯ No, exit\n  Yes, I trust this folder\n\n" +
		"Enter to confirm · Esc to cancel\n"

	_, err := soldier.ParseAskUserQuestionModal(capture)
	if !errors.Is(err, soldier.ErrNotAskUserQuestionModal) {
		t.Errorf("expected ErrNotAskUserQuestionModal, got %v", err)
	}
}

// The footer alone, with nothing recognizable above it, still fails loud -
// never an ambiguous or partially-parsed Decision.
func TestParseAskUserQuestionModal_FooterWithoutContentFailsLoud(t *testing.T) {
	capture := "Enter to select · ↑/↓ to navigate · Esc to cancel\n"

	_, err := soldier.ParseAskUserQuestionModal(capture)
	if !errors.Is(err, soldier.ErrNotAskUserQuestionModal) {
		t.Errorf("expected ErrNotAskUserQuestionModal, got %v", err)
	}
}

func TestParseAskUserQuestionModal_EmptyCapture(t *testing.T) {
	_, err := soldier.ParseAskUserQuestionModal("")
	if !errors.Is(err, soldier.ErrNotAskUserQuestionModal) {
		t.Errorf("expected ErrNotAskUserQuestionModal, got %v", err)
	}
}

// The modal sits at the bottom of a pane that still shows the conversation
// above it: the question is the modal's own, never the first line of the
// pane, and conversation text that mentions the footer or numbered lists
// above the modal's header is not part of it.
func TestParseAskUserQuestionModal_IgnoresConversationAboveTheModal(t *testing.T) {
	modal := askUserQuestionCapture("Database", "Which database should this use?", []string{"Postgres", "SQLite"})
	pane := "I looked at the options.\n1. an earlier numbered list item\nPress Enter to select something else later.\n\n" + modal

	got, err := soldier.ParseAskUserQuestionModal(pane)
	if err != nil {
		t.Fatalf("ParseAskUserQuestionModal: %v", err)
	}
	if got.Question != "Which database should this use?" {
		t.Errorf("Question = %q, want the modal's own question", got.Question)
	}
	want := []string{"Postgres", "SQLite", "Type something.", "Chat about this"}
	if len(got.Options) != len(want) {
		t.Fatalf("Options = %v, want %v", got.Options, want)
	}
}

// A footer with no modal header above it is not this modal.
func TestParseAskUserQuestionModal_FooterWithoutHeaderFailsLoud(t *testing.T) {
	capture := "Some prose\n1. one\n2. two\n\nEnter to select · ↑/↓ to navigate · Esc to cancel\n"
	if _, err := soldier.ParseAskUserQuestionModal(capture); !errors.Is(err, soldier.ErrNotAskUserQuestionModal) {
		t.Errorf("expected ErrNotAskUserQuestionModal, got %v", err)
	}
}
