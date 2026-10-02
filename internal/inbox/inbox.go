// Package inbox is the durable, per-project store of forum feedback waiting
// for the commander. The forum listener (a deterministic process, no LLM)
// writes every prompt the user sends from a review panel here before it
// acknowledges the forum server, and the commander reads and confirms them with
// "vx forum inbox". The inbox is the source of truth; a forum wake
// (internal/sentinel) is only the doorbell.
//
// Layout, under <project root>/forum-inbox/:
//
//	<session key>/<uid>.json         unread
//	<session key>/read/<uid>.json    confirmed by the commander (newest kept)
//
// One file per prompt, named by the prompt's uid, so a redelivery from the
// forum server (a listener that crashed between delivery and ack) rewrites the
// same file and can never duplicate a message, and a prompt the commander
// already confirmed is not resurrected by it.
package inbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
)

const (
	dirName     = "forum-inbox"
	readDirName = "read"
	// maxRead bounds the confirmed prompts kept per session.
	maxRead = 200
)

// Attachment is an image the user attached; only its local path travels.
type Attachment struct {
	Path  string `json:"path"`
	Type  string `json:"type"`
	Bytes int64  `json:"bytes"`
}

// Entry is one prompt as the listener received it.
type Entry struct {
	UID      string          `json:"uid"`
	Session  string          `json:"session"`
	File     string          `json:"file"`
	Tag      string          `json:"tag"`
	Prompt   string          `json:"prompt"`
	Selector string          `json:"selector,omitempty"`
	Text     string          `json:"text,omitempty"`
	Target   json.RawMessage `json:"target,omitempty"`
	// Attachments carry absolute local paths, never contents.
	Attachments []Attachment `json:"attachments,omitempty"`
	// Ended is true when the session ended with this delivery (Send & End: the
	// final prompts); EndedBy says who ended it.
	Ended   bool   `json:"ended,omitempty"`
	EndedBy string `json:"ended_by,omitempty"`
	// QueuedAt is when the user queued the prompt; ReceivedAt when the
	// listener stored it.
	QueuedAt   time.Time `json:"queued_at"`
	ReceivedAt time.Time `json:"received_at"`
}

// Summary is the unread state of one session.
type Summary struct {
	Session string
	File    string
	Count   int
	// Ended is true if any unread entry came with the session ending.
	Ended bool
}

var namePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func validName(s string) bool {
	return namePattern.MatchString(s) && s != "." && s != ".." && !strings.Contains(s, "..")
}

// Dir is the inbox directory of the project rooted at projectRoot.
func Dir(projectRoot string) string { return filepath.Join(projectRoot, dirName) }

func sessionDir(projectRoot, session string) string { return filepath.Join(Dir(projectRoot), session) }

// EntryPath is where an unread entry lives (it moves under read/ once
// confirmed).
func EntryPath(projectRoot, session, uid string) string {
	return filepath.Join(sessionDir(projectRoot, session), uid+".json")
}

func readPath(projectRoot, session, uid string) string {
	return filepath.Join(sessionDir(projectRoot, session), readDirName, uid+".json")
}

// Write stores e atomically and reports whether the entry is now unread. It is
// idempotent: the same uid rewrites the same file (the redelivery may know more,
// for instance that the session ended since), and a prompt the commander
// already confirmed is left confirmed (written is false).
func Write(projectRoot string, e Entry) (written bool, err error) {
	if projectRoot == "" {
		return false, errors.New("inbox: no project root")
	}
	if !validName(e.Session) || !validName(e.UID) {
		return false, fmt.Errorf("inbox: invalid session key or uid (%q, %q)", e.Session, e.UID)
	}
	if _, err := os.Stat(readPath(projectRoot, e.Session, e.UID)); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inbox: %w", err)
	}
	if e.ReceivedAt.IsZero() {
		e.ReceivedAt = time.Now().UTC()
	}
	path := EntryPath(projectRoot, e.Session, e.UID)
	// Keep the first receipt time on a rewrite so the order stays stable.
	if prev, err := readEntry(path); err == nil && !prev.ReceivedAt.IsZero() {
		e.ReceivedAt = prev.ReceivedAt
		e.Ended = e.Ended || prev.Ended
		if e.EndedBy == "" {
			e.EndedBy = prev.EndedBy
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("inbox: %w", err)
	}
	if err := atomicfile.WriteJSON(path, e); err != nil {
		return false, fmt.Errorf("inbox: writing %s: %w", e.UID, err)
	}
	return true, nil
}

func readEntry(path string) (Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, err
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// Pending returns every unread entry of the project, oldest first (then by uid,
// so the order is total). A damaged file is skipped, not fatal.
func Pending(projectRoot string) ([]Entry, error) {
	sessions, err := os.ReadDir(Dir(projectRoot))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("inbox: %w", err)
	}
	var out []Entry
	for _, s := range sessions {
		if !s.IsDir() || !validName(s.Name()) {
			continue
		}
		es, err := PendingFor(projectRoot, s.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, es...)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ReceivedAt.Equal(out[j].ReceivedAt) {
			return out[i].ReceivedAt.Before(out[j].ReceivedAt)
		}
		return out[i].UID < out[j].UID
	})
	return out, nil
}

// PendingFor returns the unread entries of one session, oldest first.
func PendingFor(projectRoot, session string) ([]Entry, error) {
	if !validName(session) {
		return nil, fmt.Errorf("inbox: invalid session key %q", session)
	}
	files, err := os.ReadDir(sessionDir(projectRoot, session))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("inbox: %w", err)
	}
	var out []Entry
	for _, f := range files {
		if f.IsDir() || filepath.Ext(f.Name()) != ".json" {
			continue
		}
		e, err := readEntry(filepath.Join(sessionDir(projectRoot, session), f.Name()))
		if err != nil || e.UID == "" {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ReceivedAt.Equal(out[j].ReceivedAt) {
			return out[i].ReceivedAt.Before(out[j].ReceivedAt)
		}
		return out[i].UID < out[j].UID
	})
	return out, nil
}

// Unread summarizes session's unread entries (Count 0 when there are none).
func Unread(projectRoot, session string) (Summary, error) {
	es, err := PendingFor(projectRoot, session)
	if err != nil {
		return Summary{}, err
	}
	return summarize(session, es), nil
}

func summarize(session string, es []Entry) Summary {
	s := Summary{Session: session, Count: len(es)}
	for _, e := range es {
		s.File = e.File
		s.Ended = s.Ended || e.Ended
	}
	return s
}

// MarkRead confirms the unread entries named by uids (any session), moving them
// under read/. Unknown or already confirmed uids are ignored, so confirming
// twice is harmless. It returns how many entries it confirmed.
func MarkRead(projectRoot string, uids []string) (int, error) {
	want := map[string]bool{}
	for _, u := range uids {
		if validName(u) {
			want[u] = true
		}
	}
	sessions, err := os.ReadDir(Dir(projectRoot))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, fmt.Errorf("inbox: %w", err)
	}
	moved := 0
	for _, s := range sessions {
		if !s.IsDir() || !validName(s.Name()) {
			continue
		}
		touched := false
		for uid := range want {
			from := EntryPath(projectRoot, s.Name(), uid)
			if _, err := os.Stat(from); err != nil {
				continue
			}
			to := readPath(projectRoot, s.Name(), uid)
			if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
				return moved, fmt.Errorf("inbox: %w", err)
			}
			if err := os.Rename(from, to); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return moved, fmt.Errorf("inbox: confirming %s: %w", uid, err)
			}
			moved++
			touched = true
		}
		if touched {
			pruneRead(filepath.Join(sessionDir(projectRoot, s.Name()), readDirName))
		}
	}
	return moved, nil
}

// pruneRead keeps the newest maxRead confirmed entries of one session.
func pruneRead(dir string) {
	files, err := os.ReadDir(dir)
	if err != nil || len(files) <= maxRead {
		return
	}
	type aged struct {
		name string
		at   time.Time
	}
	all := make([]aged, 0, len(files))
	for _, f := range files {
		info, err := f.Info()
		if err != nil {
			continue
		}
		all = append(all, aged{f.Name(), info.ModTime()})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].at.Before(all[j].at) })
	for _, a := range all[:len(all)-maxRead] {
		_ = os.Remove(filepath.Join(dir, a.name))
	}
}

// Sessions summarizes every session with unread entries, ordered by key.
func Sessions(projectRoot string) ([]Summary, error) {
	es, err := Pending(projectRoot)
	if err != nil {
		return nil, err
	}
	by := map[string][]Entry{}
	for _, e := range es {
		by[e.Session] = append(by[e.Session], e)
	}
	keys := make([]string, 0, len(by))
	for k := range by {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Summary, 0, len(keys))
	for _, k := range keys {
		out = append(out, summarize(k, by[k]))
	}
	return out, nil
}
