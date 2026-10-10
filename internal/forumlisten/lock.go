package forumlisten

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/isaias-alt/vexillum/internal/cmdname"
)

// The listener is a singleton per home, by pid lock: the same pattern as
// sentinel.AcquireLock and forum.AcquireLock (the claim is a hard link, so two
// racing starters cannot both win). On top of it, a pid is only ever trusted or
// signaled after inspectProcess confirms it is still "vx forum listen": a
// recycled pid belongs to something else, so a lock naming it is stale and
// signaling it is never done. Nothing here kills by name.

// ErrRunning is returned by AcquireLock when another listener is alive.
type ErrRunning struct{ PID int }

func (e *ErrRunning) Error() string {
	return fmt.Sprintf("a forum listener is already running (pid %d) - not starting a second one", e.PID)
}

func dir(home string) string { return filepath.Join(home, "forum") }

func lockPath(home string) string { return filepath.Join(dir(home), "listener.pid") }

// LogPath is where a background-started listener writes its log.
func LogPath(home string) string { return filepath.Join(dir(home), "listener.log") }

var errProcessGone = errors.New("no such process")

// inspectProcess reports pid's full command line. A variable so tests can stand
// in for ps.
var inspectProcess = func(pid int) (command string, err error) {
	out, err := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", errProcessGone
	}
	command = strings.TrimSpace(string(out))
	if command == "" {
		return "", errProcessGone
	}
	return command, nil
}

// isListenCommand reports whether command is a "vx forum listen" invocation: the
// executable (any path) followed by exactly those two
// arguments.
func isListenCommand(command string) bool {
	exe, ok := strings.CutSuffix(strings.TrimSpace(command), " forum listen")
	if !ok {
		return false
	}
	if strings.ContainsAny(exe, " \t") && !strings.HasPrefix(exe, "/") {
		return false
	}
	base := filepath.Base(exe)
	return base == cmdname.Name
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// verifiedListener reports whether pid is alive and really is a listener.
func verifiedListener(pid int) bool {
	if !processAlive(pid) {
		return false
	}
	command, err := inspectProcess(pid)
	return err == nil && isListenCommand(command)
}

func readLockPID(home string) (int, error) {
	data, err := os.ReadFile(lockPath(home))
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

// AcquireLock claims the one-listener-per-home lock. The loser reports the
// winner if it is a live, verified listener, and reclaims the lock when it is
// stale (unparseable, a dead pid, or a pid that is no longer a listener). Call
// the returned release on clean shutdown.
func AcquireLock(home string) (release func(), err error) {
	if err := os.MkdirAll(dir(home), 0o755); err != nil {
		return nil, fmt.Errorf("creating %s: %w", dir(home), err)
	}
	path := lockPath(home)
	for {
		claimed, err := claimLock(dir(home), path)
		if err != nil {
			return nil, err
		}
		if claimed {
			return func() { _ = os.Remove(path) }, nil
		}
		pid, readErr := readLockPID(home)
		if readErr != nil && errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr == nil && verifiedListener(pid) {
			return nil, &ErrRunning{PID: pid}
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("removing stale forum listener lock: %w", err)
		}
	}
}

func claimLock(d, path string) (claimed bool, err error) {
	tmp, err := os.CreateTemp(d, "listener.pid.tmp-*")
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

// Running reports whether a verified listener holds the lock for home.
func Running(home string) bool {
	pid, err := readLockPID(home)
	return err == nil && verifiedListener(pid)
}

// Stop asks the listener of home to exit (SIGTERM, which its loop handles as a
// clean shutdown) and waits briefly for it to go. Only a verified listener is
// ever signaled. It reports whether there was one.
func Stop(home string) (bool, error) {
	pid, err := readLockPID(home)
	if err != nil || !verifiedListener(pid) {
		return false, nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return true, fmt.Errorf("stopping the forum listener (pid %d): %w", pid, err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return true, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return true, fmt.Errorf("the forum listener (pid %d) did not stop", pid)
}

// Ensure starts the listener through spawn unless a verified one is running,
// and waits (up to wait) for one to hold the lock. A listener that was exiting
// at the instant ours started still held the lock, so ours exited as "already
// running" and then the old one went away; spawning again once closes that
// window, as forum.EnsureServer does.
func Ensure(home string, spawn func() error, wait time.Duration) error {
	if Running(home) {
		return nil
	}
	if err := spawn(); err != nil {
		return fmt.Errorf("starting the forum listener: %w", err)
	}
	deadline := time.Now().Add(wait)
	respawnAt := time.Now().Add(1500 * time.Millisecond)
	respawned := false
	for {
		if Running(home) {
			return nil
		}
		now := time.Now()
		if now.After(deadline) {
			return fmt.Errorf("the forum listener did not start within %s (see %s)", wait, LogPath(home))
		}
		if !respawned && now.After(respawnAt) {
			respawned = true
			if err := spawn(); err != nil {
				return fmt.Errorf("starting the forum listener: %w", err)
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}
