package sentinel

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/isaias-alt/vexillum/internal/cmdname"
)

// DrainAll consumes both kinds of pending wake of the project: soldier status
// changes (Drain) and forum feedback (DrainForum). Whatever one drain returned
// is kept even if the other failed, since a drained wake is already consumed.
func DrainAll(projectRoot string) ([]Wake, []ForumWake, error) {
	wakes, werr := Drain(projectRoot)
	forumWakes, ferr := DrainForum(projectRoot)
	return wakes, forumWakes, errors.Join(werr, ferr)
}

// Requeue puts wakes that were drained but could not be delivered back, so the
// next drain finds them again and judges them anew (a wake that went stale in
// the meantime is dropped then, like any other). A task wake whose file already
// exists is left alone: that one is newer, and a wake is about the task's
// current status. A forum wake merges into a newer one for its session the way
// RecordForumWake always does. The first error stops the rest from being put
// back and is returned with what was lost.
func Requeue(projectRoot string, wakes []Wake, forumWakes []ForumWake) error {
	for _, w := range wakes {
		if w.TaskID == "" {
			continue
		}
		if _, err := os.Stat(wakePath(projectRoot, w.TaskID)); err == nil {
			continue
		}
		if err := writeWake(projectRoot, w); err != nil {
			return fmt.Errorf("putting back the wake for task %s: %w", w.TaskID, err)
		}
	}
	for _, w := range forumWakes {
		if err := RecordForumWake(projectRoot, w); err != nil {
			return fmt.Errorf("putting back the forum wake for session %s: %w", w.Session, err)
		}
	}
	return nil
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
