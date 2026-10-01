package forum

import (
	"strings"
	"testing"
)

func TestBoundTranscript_NewestMessageIsKeptHoweverLarge(t *testing.T) {
	huge := Message{ID: "m_huge", Role: RoleAgent, Text: strings.Repeat("x", maxTranscriptBytes+1)}
	kept, evicted := boundTranscript([]Message{{ID: "m_1"}, {ID: "m_2"}, huge})
	if len(kept) != 1 || kept[0].ID != "m_huge" || len(evicted) != 2 {
		t.Errorf("kept %d, evicted %d; want only the huge newest message kept", len(kept), len(evicted))
	}
}

func TestBoundTranscript_UnderTheCapsKeepsEverything(t *testing.T) {
	kept, evicted := boundTranscript([]Message{{ID: "m_1"}, {ID: "m_2"}})
	if len(kept) != 2 || len(evicted) != 0 {
		t.Errorf("kept %d, evicted %d", len(kept), len(evicted))
	}
}
