package forum

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// ErrServerRunning is returned by AcquireLock when another forum server
// process is alive for the same home.
type ErrServerRunning struct{ PID int }

func (e *ErrServerRunning) Error() string {
	return fmt.Sprintf("a forum server is already running (pid %d) - not starting a second one", e.PID)
}

// serverDir holds the forum server's own files: the lock (server.pid), the
// discovery file (server.json), the last-port hint and the log.
func serverDir(home string) string { return filepath.Join(home, "forum") }

func lockPath(home string) string { return filepath.Join(serverDir(home), "server.pid") }

// AcquireLock claims the one-forum-server-per-home lock. It is
// sentinel.AcquireLock's pattern (see internal/sentinel): the claim is
// atomic, not a read-then-write, so two servers racing at the same instant
// cannot both win - the pid is written in full to a temp file and published
// with a hard link, which fails with EEXIST if the lock already exists. The
// loser reports the winner if its pid is alive, or reclaims the lock if it
// is stale (unparseable, or a dead pid left by a crashed server). Call the
// returned release func on clean shutdown.
func AcquireLock(home string) (release func(), err error) {
	dir := serverDir(home)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating %s: %w", dir, err)
	}
	path := lockPath(home)

	for {
		claimed, err := claimLock(dir, path)
		if err != nil {
			return nil, err
		}
		if claimed {
			return func() { _ = os.Remove(path) }, nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				// Reclaimed by someone else between our failed claim and
				// this read; retry our own claim.
				continue
			}
			return nil, fmt.Errorf("reading %s: %w", path, readErr)
		}
		if pid, convErr := strconv.Atoi(strings.TrimSpace(string(data))); convErr == nil && processAlive(pid) {
			return nil, &ErrServerRunning{PID: pid}
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("removing stale forum server lock: %w", err)
		}
	}
}

func claimLock(dir, path string) (claimed bool, err error) {
	tmp, err := os.CreateTemp(dir, "server.pid.tmp-*")
	if err != nil {
		return false, fmt.Errorf("creating lock temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.WriteString(strconv.Itoa(os.Getpid())); err != nil {
		_ = tmp.Close()
		return false, fmt.Errorf("writing lock temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return false, fmt.Errorf("closing lock temp file: %w", err)
	}
	if err := os.Link(tmpPath, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, nil
		}
		return false, fmt.Errorf("publishing lock: %w", err)
	}
	return true, nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix FindProcess always succeeds; signal 0 is the existence probe.
	return proc.Signal(syscall.Signal(0)) == nil
}
