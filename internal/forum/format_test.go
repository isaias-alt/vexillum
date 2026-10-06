package forum

import (
	"strings"
	"testing"
)

// The poll guidance is agent-facing prose; these tests pin the facts an agent
// acts on (what to run, what not to do), not the sentences.
func TestPollNextStepEndedKeepsTheKeyFacts(t *testing.T) {
	final := PollResponse{Status: PollEnded, EndedBy: EndedByUser, Prompts: []Prompt{{}}}
	got := PollNextStep("/p/a.html", final)
	for _, want := range []string{"closed", "apply it", "not be sent again", "user requests"} {
		if !strings.Contains(got, want) {
			t.Errorf("final ended guidance lacks %q: %s", want, got)
		}
	}
	if !strings.Contains(PollNextStep("/p/a.html", PollResponse{Status: PollEnded}), "Polling this session is finished") {
		t.Error("an ended session without prompts must tell the agent to stop polling")
	}
	all := PollNextStep("", PollResponse{Status: PollEnded, All: true, File: "/p/a.html"})
	if !strings.Contains(all, "poll --all") || !strings.Contains(all, "no_sessions") {
		t.Errorf("a multiplexed end must keep the agent on poll --all: %s", all)
	}
}

func TestPollNextStepDisconnectedLeavesTheChoiceToTheUser(t *testing.T) {
	got := PollNextStep("/p/a.html", PollResponse{Status: PollBrowserDisconnected})
	for _, want := range []string{"forum /p/a.html", "forum end /p/a.html", "user", "neither"} {
		if !strings.Contains(got, want) {
			t.Errorf("disconnected guidance lacks %q: %s", want, got)
		}
	}
}
