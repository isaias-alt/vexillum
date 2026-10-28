package sentinel

import (
	"errors"
	"fmt"
	"strings"

	"github.com/isaias-alt/vexillum/internal/cmdname"
)

// DrainAll consumes both kinds of pending wake of the project: soldier status
// changes (Drain) and forum feedback (DrainForum). Whatever one drain returned
// is kept even if the other failed, since a drained wake is already consumed.
// A caller that can fail to hand the wakes on uses Claim instead.
func DrainAll(projectRoot string) ([]Wake, []ForumWake, error) {
	c, err := Claim(projectRoot)
	return c.Wakes, c.ForumWakes, errors.Join(err, c.Commit())
}

// WakeReason is the text a commander is handed when it is woken: one section
// for soldier status changes, one for forum feedback waiting in the inbox. A
// forum wake names the session and how many messages wait, never their text:
// that is read with "vx forum inbox". The Stop hook's await, drain and the
// sentinel's OpenCode injector all use it, so every commander hears the same
// words.
func WakeReason(wakes []Wake, forumWakes []ForumWake) string {
	var sections []string
	if len(wakes) > 0 {
		var lines []string
		for _, w := range wakes {
			lines = append(lines, fmt.Sprintf("- %s %s: %s -> %s", w.Kind, w.TaskID, w.OldStatus, w.NewStatus))
		}
		sections = append(sections, "A vexillum soldier's status changed:\n"+
			strings.Join(lines, "\n")+
			"\nCheck on it (and report to the general, or land/strike as appropriate) before ending your turn.")
	}
	if len(forumWakes) > 0 {
		var lines []string
		for _, w := range forumWakes {
			noun := "new messages"
			if w.Count == 1 {
				noun = "new message"
			}
			lines = append(lines, fmt.Sprintf("forum session %s: %d %s (ended: %t). Run %s forum inbox.", singleLine(w.File), w.Count, noun, w.Ended, cmdname.Name))
		}
		sections = append(sections, "Forum feedback is waiting for you:\n"+
			strings.Join(lines, "\n")+
			"\nRead it, act on it, and answer with "+cmdname.Name+" forum reply before ending your turn.")
	}
	return strings.Join(sections, "\n\n")
}

// singleLine keeps a user-chosen path from breaking the wake text's lines.
func singleLine(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}
