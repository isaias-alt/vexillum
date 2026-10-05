package tribunal

import (
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
