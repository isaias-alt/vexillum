package sentinel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
	"github.com/isaias-alt/vexillum/internal/inbox"
)

// ForumWake is the doorbell for forum feedback: the forum listener rings it
// after it stored a user's prompt in the project's inbox (internal/inbox), and
// the Stop hook's await surfaces it so an idle commander runs "vx forum inbox".
// It is a sibling of Wake, not a Wake with a flag: a Wake is task-shaped (its
// currency is a task's status) and Drain, which only ever lists the wakes
// directly under <project root>/wakes/, stays exactly as it was. Forum wakes
// live in <project root>/wakes/forum/, one file per forum session, so repeated
// messages coalesce into one wake.
//
// The wake carries no message text: the inbox is the source of truth. Count
// and Ended below are what the listener saw when it rang; DrainForum
// recomputes them from the inbox, so what the commander is told is current.
type ForumWake struct {
	Session    string    `json:"session"`
	File       string    `json:"file"`
	Count      int       `json:"count"`
	Ended      bool      `json:"ended"`
	DetectedAt time.Time `json:"detected_at"`
}

func forumWakesDir(projectRoot string) string {
	return filepath.Join(wakesDir(projectRoot), "forum")
}

func forumWakePath(projectRoot, session string) string {
	return filepath.Join(forumWakesDir(projectRoot), session+".json")
}

// RecordForumWake writes the forum wake for w.Session under projectRoot,
// replacing an earlier undrained one for the same session (coalescing). Ended is
// sticky across the replacement: a session that ended stays ended.
func RecordForumWake(projectRoot string, w ForumWake) error {
	if w.Session == "" || filepath.Base(w.Session) != w.Session || w.Session == "." || w.Session == ".." {
		return fmt.Errorf("invalid forum session key %q", w.Session)
	}
	if err := os.MkdirAll(forumWakesDir(projectRoot), 0o755); err != nil {
		return err
	}
	path := forumWakePath(projectRoot, w.Session)
	if data, err := os.ReadFile(path); err == nil {
		var prev ForumWake
		if json.Unmarshal(data, &prev) == nil {
			w.Ended = w.Ended || prev.Ended
			if !prev.DetectedAt.IsZero() && prev.DetectedAt.Before(w.DetectedAt) {
				w.DetectedAt = prev.DetectedAt
			}
		}
	}
	if w.DetectedAt.IsZero() {
		w.DetectedAt = time.Now().UTC()
	}
	return atomicfile.WriteJSON(path, w)
}

// DrainForum returns the forum wakes of the project that are still news, oldest
// first, consuming every wake file it finds. Like Drain it claims each file by
// rename (exactly one of two racing drains wins) before reading it, and it drops
// a stale wake without returning it: a forum wake is current only while its
// session still has unread messages in the inbox, so a commander that already
// ran "vx forum inbox --ack" is not woken for what it handled. Count and Ended
// of the returned wakes come from the inbox. Anything it cannot establish (an
// unreadable inbox) counts as current.
func DrainForum(projectRoot string) ([]ForumWake, error) {
	dir := forumWakesDir(projectRoot)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing forum wakes: %w", err)
	}
	var drained []ForumWake
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		claimed := claimedPath(path)
		if err := os.Rename(path, claimed); err != nil {
			continue
		}
		data, err := os.ReadFile(claimed)
		if rmErr := os.Remove(claimed); rmErr != nil && !os.IsNotExist(rmErr) {
			return nil, fmt.Errorf("removing delivered forum wake %s: %w", entry.Name(), rmErr)
		}
		if err != nil {
			continue
		}
		var w ForumWake
		if err := json.Unmarshal(data, &w); err != nil || w.Session == "" {
			continue
		}
		sum, err := inbox.Unread(projectRoot, w.Session)
		if err == nil {
			if sum.Count == 0 {
				continue
			}
			w.Count, w.Ended = sum.Count, sum.Ended
			if sum.File != "" {
				w.File = sum.File
			}
		}
		drained = append(drained, w)
	}
	sort.Slice(drained, func(i, j int) bool { return drained[i].DetectedAt.Before(drained[j].DetectedAt) })
	return drained, nil
}
