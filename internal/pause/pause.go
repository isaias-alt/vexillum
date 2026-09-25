// Package pause resolves where a soldier (mission or scout) declares that
// it's deliberately pausing its own turn to wait on something of its own -
// a background job it started, a validation run still in flight - instead
// of blocking on it with a polling loop. Mirrors internal/report's own
// convention: one file per soldier, under
// ~/.vexillum/projects/<key>/pauses/<agent-name>.md.
//
// internal/sentinel reads this before ever trusting an idle turn as
// "done": a live herdr status of idle/done only means the turn stopped
// responding, never why. Background-and-pause is a legitimate, useful
// pattern (it avoids burning a soldier's own turn/context on a polling
// loop) - but without an explicit, persisted declaration like this one, a
// soldier that paused on purpose is indistinguishable from one that
// actually finished. This file resolves that ambiguity as a durable fact
// on disk, never as an inference from pane/turn state, and never as
// something said only in the turn's own prose (which the sentinel never
// reads).
//
// File format, chosen to be trivial for a soldier to write correctly with
// a single tool call, the same way internal/report's is:
//
//	paused: <reason>
//	until: <optional - free text, e.g. "the e2e run finishes">
//
// The "until" line is never parsed as a real deadline - see Active's own
// doc comment for why a pause's actual validity window is judged from the
// file's own mtime instead.
package pause

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const dirName = "pauses"

// ValidityWindow bounds how long a declared pause is trusted before
// internal/sentinel stops treating an idle turn as "known, waiting" and
// falls back to its ambiguous-idle handling instead - mirroring
// firstmate's own bounded "recheck on a long cadence" rather than
// indefinite trust. Without this bound, a soldier that declared a pause
// and then genuinely got stuck (its background job crashed silently,
// say) would be trusted forever and never surface to the commander -
// trading the original false-positive-done bug for a new
// false-negative-stuck one.
const ValidityWindow = 30 * time.Minute

// Pause is a soldier's declared reason for deliberately pausing its turn.
type Pause struct {
	Reason string
	Until  string // free text, informational only - see package doc.
}

// Dir returns <project root>/pauses.
func Dir(projectRoot string) string {
	return filepath.Join(projectRoot, dirName)
}

// Path returns the pause file a soldier named agentName should write to.
func Path(projectRoot, agentName string) string {
	return filepath.Join(Dir(projectRoot), agentName+".md")
}

// Exists reports whether agentName currently has a pause file on disk,
// regardless of age - matches internal/report.Exists. Most callers want
// Active instead, which also accounts for ValidityWindow.
func Exists(projectRoot, agentName string) bool {
	_, err := os.Stat(Path(projectRoot, agentName))
	return err == nil
}

// Active reports whether agentName currently has a declared pause in
// effect: its file exists and was last written within ValidityWindow of
// now. Checked live at the moment it's asked, matching
// internal/report.Exists's own reasoning - internal/sentinel needs the
// true, current state of the filesystem on every tick, not a cached
// snapshot.
//
// A pause older than ValidityWindow reports inactive (ok=false) rather
// than an error - it's not that anything is wrong, just that it's no
// longer trusted on its own; internal/sentinel's own ambiguous-idle
// confirm window takes over from there.
func Active(projectRoot, agentName string, now time.Time) (Pause, bool, error) {
	path := Path(projectRoot, agentName)
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Pause{}, false, nil
		}
		return Pause{}, false, fmt.Errorf("checking declared pause %s: %w", path, err)
	}
	if now.Sub(info.ModTime()) >= ValidityWindow {
		return Pause{}, false, nil
	}

	p, err := read(path)
	if err != nil {
		return Pause{}, false, err
	}
	return p, true, nil
}

// read parses a pause file's "paused:"/"until:" lines (see package doc).
// Lenient by design: the file's mere presence is the actual signal (see
// package doc's "never something said only in the turn's own prose") -
// missing or reordered lines still count as a declared pause, just with
// an emptier Reason, rather than being rejected outright over a format
// slip.
func read(path string) (Pause, error) {
	f, err := os.Open(path)
	if err != nil {
		return Pause{}, fmt.Errorf("reading declared pause %s: %w", path, err)
	}
	defer f.Close()

	var p Pause
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "paused:"):
			p.Reason = strings.TrimSpace(line[len("paused:"):])
		case strings.HasPrefix(lower, "until:"):
			p.Until = strings.TrimSpace(line[len("until:"):])
		}
	}
	if err := scanner.Err(); err != nil {
		return Pause{}, fmt.Errorf("reading declared pause %s: %w", path, err)
	}
	return p, nil
}

// Remove deletes agentName's pause file, if any. A missing file is not an
// error - most agent names never had one. internal/cli.runRedispatch
// calls this before relaunching a discarded attempt, for the exact same
// reason it already calls internal/report.Remove: the fresh run's
// slug-derived candidate agent name will very likely collide with the
// dead soldier's, and a leftover pause file at that path could otherwise
// be mistaken for the new run's own declaration.
func Remove(projectRoot, agentName string) error {
	path := Path(projectRoot, agentName)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing stale pause %s: %w", path, err)
	}
	return nil
}
