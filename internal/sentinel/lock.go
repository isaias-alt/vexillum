package sentinel

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
)

// The sentinel is a singleton per home. The guarantee comes from an flock(2)
// on sentinel.lock, held for the life of the process: the kernel arbitrates
// the claim, drops it the instant the process dies however it dies, and there
// is no stale-file reclaim step for two starters to race on (an earlier
// pid-file-only lock let a reclaiming starter delete a lock another had just
// taken, and a finishing sentinel delete its successor's).
//
// sentinel.pid (the holder's pid) is kept next to it as information and so
// that a sentinel started before the flock existed, which only knows the pid
// file, still sees a live holder; sentinel.json records the build it runs from
// (see Build). Neither file decides who holds the lock.

func lockPath(vexillumHome string) string {
	return filepath.Join(vexillumHome, "sentinel.pid")
}

func flockPath(vexillumHome string) string {
	return filepath.Join(vexillumHome, "sentinel.lock")
}

// ErrRunning is returned by AcquireLock when another sentinel holds the lock.
type ErrRunning struct {
	// PID is the holder's pid, 0 when it could not be read.
	PID  int
	Home string
}

func (e *ErrRunning) Error() string {
	if e.PID == 0 {
		return fmt.Sprintf("a sentinel is already running for %s - not starting a second one", e.Home)
	}
	return fmt.Sprintf("a sentinel is already running (pid %d) for %s - not starting a second one", e.PID, e.Home)
}

// AcquireLock claims the single-sentinel-per-home lock, refusing if another
// sentinel process is already alive - a race-proof singleton lock, because
// vexillum should never end up with two sentinels racing to reconcile the
// same tasks. Call the returned release func (e.g. via defer) to release the
// lock on clean shutdown; a process that dies without releasing frees it anyway.
func AcquireLock(vexillumHome string) (release func(), err error) {
	if err := os.MkdirAll(vexillumHome, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(flockPath(vexillumHome), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening sentinel lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			pid, _ := readPID(vexillumHome)
			return nil, &ErrRunning{PID: pid, Home: vexillumHome}
		}
		return nil, fmt.Errorf("locking sentinel lock: %w", err)
	}

	// No sentinel of this generation is alive now. One that predates the
	// flock holds only the pid file.
	if pid, ok := legacyHolder(vexillumHome); ok {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, &ErrRunning{PID: pid, Home: vexillumHome}
	}

	self := os.Getpid()
	if err := atomicfile.Write(lockPath(vexillumHome), []byte(strconv.Itoa(self))); err != nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		return nil, fmt.Errorf("recording sentinel pid: %w", err)
	}
	// Whatever build record is on disk belongs to a sentinel that is gone.
	_ = os.Remove(infoPath(vexillumHome))

	var once sync.Once
	return func() {
		once.Do(func() {
			// Still holding the flock: nobody else can have written these.
			if pid, ok := readPID(vexillumHome); ok && pid == self {
				_ = os.Remove(lockPath(vexillumHome))
			}
			if info, ok := readInfo(vexillumHome); ok && info.PID == self {
				_ = os.Remove(infoPath(vexillumHome))
			}
			_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			_ = f.Close()
		})
	}, nil
}

// AcquireLockRetiring is AcquireLock for a starter that is the replacement of
// a holder whose binary was replaced on disk (HolderStale): that holder
// retires within a poll interval, so rather than refusing at once this waits
// up to wait for it. Any other refusal is returned immediately.
func AcquireLockRetiring(vexillumHome string, wait time.Duration) (release func(), err error) {
	deadline := time.Now().Add(wait)
	retiring := false
	for {
		// A retiring holder removes its build record just before it drops the
		// flock, so a holder seen stale once stays stale: judging it again
		// could find no record while the flock is still held and give up on
		// a holder that is only moments from gone. The judgement also comes
		// before the attempt, never after a refusal, for the same reason.
		retiring = retiring || HolderStale(vexillumHome)
		release, err := tryAcquireLock(vexillumHome)
		var running *ErrRunning
		if err == nil || !errors.As(err, &running) || !retiring || !time.Now().Before(deadline) {
			return release, err
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// tryAcquireLock is AcquireLock; a variable so tests can interleave a holder's
// exit with a refused attempt.
var tryAcquireLock = AcquireLock

// readPID reads the pid file.
func readPID(vexillumHome string) (int, bool) {
	data, err := os.ReadFile(lockPath(vexillumHome))
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		return 0, false
	}
	return pid, true
}

// legacyHolder reports a live sentinel process named by the pid file that is
// not this process: the only way to tell a sentinel that never took the flock.
// A pid that is alive but no longer a "vx sentinel" was recycled and is not a
// holder.
func legacyHolder(vexillumHome string) (pid int, ok bool) {
	pid, ok = readPID(vexillumHome)
	if !ok || pid == os.Getpid() || !processAlive(pid) {
		return 0, false
	}
	if _, command, err := inspectProcess(pid); err != nil || !isSentinelCommand(command) {
		return 0, false
	}
	return pid, true
}

// flockHeld reports whether some process holds the sentinel flock. It probes
// with a shared lock, which never blocks another probe.
func flockHeld(vexillumHome string) bool {
	f, err := os.OpenFile(flockPath(vexillumHome), os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err != nil {
		return errors.Is(err, syscall.EWOULDBLOCK)
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

// IsRunning reports whether a sentinel process is currently alive for
// vexillumHome, without claiming the lock itself - internal/cli uses this to
// decide whether dispatch needs to auto-start one.
func IsRunning(vexillumHome string) bool {
	if flockHeld(vexillumHome) {
		return true
	}
	_, ok := legacyHolder(vexillumHome)
	return ok
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds regardless of whether the pid
	// is live; signal 0 is the standard existence probe (sends nothing,
	// just checks permission/existence).
	return proc.Signal(syscall.Signal(0)) == nil
}

// HolderPID returns the pid the sentinel pid file names, for reporting. It
// says nothing about whether that process is alive.
func HolderPID(vexillumHome string) (int, bool) {
	return readPID(vexillumHome)
}
