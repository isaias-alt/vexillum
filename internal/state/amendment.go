package state

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Amendment is one instruction the general gave a task after its dispatch
// prompt, through a vx command run by the commander ("vx prompt" or the
// answer text of "vx decide"). The tribunal judges a mission's change against
// the dispatch prompt followed by these (see ComposeIntent), so a component
// the general asked for later is not reported as unrequested.
//
// Amendments live only in the task's state file under the project root, never
// in a file inside the camp: the soldier works in the camp and must not be
// able to widen its own mandate. Only AddAmendment, called from the vx
// commands, writes them.
type Amendment struct {
	Text   string    `json:"text"`
	At     time.Time `json:"at"`
	Source string    `json:"source"`
}

// Sources an Amendment can come from: the vx command that recorded it.
const (
	AmendmentSourcePrompt = "vx prompt"
	AmendmentSourceDecide = "vx decide"
)

// Bounds on the amendments a task carries. A task's state file is read by
// every vx command and its amendments end up inside the reviewer's prompt, so
// both the size of one amendment and their number are capped.
const (
	// MaxAmendmentRunes is the longest amendment text kept in full; a longer
	// one is cut and marked with amendmentTruncatedMarker.
	MaxAmendmentRunes = 2000
	// MaxAmendments is how many amendments a task keeps. Past that the oldest
	// are dropped: the most recent instructions are the ones that matter most
	// for what the final change should look like.
	MaxAmendments = 20

	amendmentTruncatedMarker = " [truncated]"
)

// AddAmendment records text as an instruction the general gave this task,
// from source (one of the AmendmentSource constants), at the given time.
// Blank text is ignored. The text is trimmed and capped at MaxAmendmentRunes,
// and only the newest MaxAmendments are kept. The caller persists the task.
func (t *Task) AddAmendment(source, text string, at time.Time) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if utf8.RuneCountInString(text) > MaxAmendmentRunes {
		runes := []rune(text)
		text = strings.TrimSpace(string(runes[:MaxAmendmentRunes])) + amendmentTruncatedMarker
	}
	t.Amendments = append(t.Amendments, Amendment{Text: text, At: at.UTC(), Source: source})
	if over := len(t.Amendments) - MaxAmendments; over > 0 {
		t.Amendments = append([]Amendment(nil), t.Amendments[over:]...)
	}
}

// Intent is what the tribunal judges the task's change against: the dispatch
// prompt followed by the task's amendments (see ComposeIntent).
func (t Task) Intent() string {
	return ComposeIntent(t.Prompt, t.Amendments)
}

// ComposeIntent builds a mission's intent from its dispatch prompt and the
// amendments the general gave afterward. With no amendments it is just the
// trimmed prompt. Otherwise the amendments follow the prompt, numbered in the
// order they were given and labeled as the general's later instructions, so
// the reader can tell the original request from what widened it. A blank
// prompt yields "": amendments alone are not a mission statement.
func ComposeIntent(prompt string, amendments []Amendment) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || len(amendments) == 0 {
		return prompt
	}
	var b strings.Builder
	b.WriteString(prompt)
	b.WriteString("\n\nInstructions the general gave afterward, in the order given (they are part of the mission's intent, and what they ask for is required):\n")
	for i, a := range amendments {
		fmt.Fprintf(&b, "%d. [%s, via %s] %s\n", i+1, a.At.UTC().Format(time.RFC3339), a.Source, a.Text)
	}
	return strings.TrimRight(b.String(), "\n")
}
