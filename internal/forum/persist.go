package forum

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
)

// sessionDir is where everything durable about one session lives, next to
// its whiteboards: <home>/forums/<key>/. session.json (queue + outbox +
// status) is small and rewritten on every change; transcript.json grows
// and is rewritten only when a message is added.
func sessionDir(home, key string) string {
	return filepath.Join(home, "forums", key)
}

func sessionFile(home, key string) string {
	return filepath.Join(sessionDir(home, key), "session.json")
}
func transcriptFile(home, key string) string {
	return filepath.Join(sessionDir(home, key), "transcript.json")
}

// loadRecord reads a session's durable record without its transcript, or
// returns (nil, nil) if the session has never been persisted.
func loadRecord(home, key string) (*sessionRecord, error) {
	if !ValidSessionKey(key) {
		return nil, fmt.Errorf("invalid forum session key: %q", key)
	}
	data, err := os.ReadFile(sessionFile(home, key))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading session %s: %w", key, err)
	}
	var rec sessionRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("parsing session %s: %w", key, err)
	}
	if rec.Key != key {
		return nil, fmt.Errorf("session file for %s names key %q", key, rec.Key)
	}
	return &rec, nil
}

// loadSession reads a session's durable state, or returns (nil, nil, nil)
// if the session has never been persisted. A transcript that is missing
// (never written) is empty; one that is unreadable is an error rather than
// silently dropped.
func loadSession(home, key string) (*sessionRecord, []Message, error) {
	rec, err := loadRecord(home, key)
	if err != nil || rec == nil {
		return nil, nil, err
	}

	var transcript []Message
	tdata, err := os.ReadFile(transcriptFile(home, key))
	switch {
	case err == nil:
		if err := json.Unmarshal(tdata, &transcript); err != nil {
			return nil, nil, fmt.Errorf("parsing transcript %s: %w", key, err)
		}
	case !errors.Is(err, os.ErrNotExist):
		return nil, nil, fmt.Errorf("reading transcript %s: %w", key, err)
	}
	return rec, transcript, nil
}

func saveRecord(home string, rec *sessionRecord) error {
	if err := os.MkdirAll(sessionDir(home, rec.Key), 0o755); err != nil {
		return fmt.Errorf("creating session dir for %s: %w", rec.Key, err)
	}
	if err := atomicfile.WriteJSON(sessionFile(home, rec.Key), rec); err != nil {
		return fmt.Errorf("saving session %s: %w", rec.Key, err)
	}
	return nil
}

func saveTranscript(home, key string, transcript []Message) error {
	if err := os.MkdirAll(sessionDir(home, key), 0o755); err != nil {
		return fmt.Errorf("creating session dir for %s: %w", key, err)
	}
	if transcript == nil {
		transcript = []Message{}
	}
	if err := atomicfile.WriteJSON(transcriptFile(home, key), transcript); err != nil {
		return fmt.Errorf("saving transcript %s: %w", key, err)
	}
	return nil
}

func layoutFile(home, key string) string {
	return filepath.Join(sessionDir(home, key), "layout.json")
}

// loadLayout reads a session's layout inbox; a missing file is an empty inbox,
// and so is a file written by another format version (see decodeLayoutInbox).
func loadLayout(home, key string) (layoutInbox, error) {
	data, err := os.ReadFile(layoutFile(home, key))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return newLayoutInbox(), nil
		}
		return layoutInbox{}, fmt.Errorf("reading layout %s: %w", key, err)
	}
	in, err := decodeLayoutInbox(data)
	if err != nil {
		return layoutInbox{}, fmt.Errorf("parsing layout %s: %w", key, err)
	}
	return in, nil
}

func saveLayout(home, key string, in *layoutInbox) error {
	if err := os.MkdirAll(sessionDir(home, key), 0o755); err != nil {
		return fmt.Errorf("creating session dir for %s: %w", key, err)
	}
	in.Version = layoutFileVersion
	if in.Stamps == nil {
		in.Stamps = []versionStamp{}
	}
	if in.Issues == nil {
		in.Issues = []layoutIssue{}
	}
	if err := atomicfile.WriteJSON(layoutFile(home, key), in); err != nil {
		return fmt.Errorf("saving layout %s: %w", key, err)
	}
	return nil
}
