package sentinel

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
)

// A sentinel is a long-lived process, so it can outlive the binary it started
// from: a rebuild, a "brew upgrade". An old sentinel keeps running its old
// logic against state newer builds write. To bound that, it records the build
// it runs from (Build, in sentinel.json) and, after each pass, checks that the
// executable on disk is still the file it started from; when it is not, it
// retires (see Run), and the next "vx dispatch", "vx prompt" or Stop hook
// await starts a fresh sentinel from the new binary.
//
// Identity is the executable file itself (resolved path, size, modification
// time), not just the version string: every plain "go build" reports "dev".

// Build identifies the executable a sentinel started from.
type Build struct {
	// Version is what the binary reported (buildinfo.Version).
	Version string `json:"version"`
	// Executable is the path os.Executable gave at start.
	Executable string `json:"executable"`
	// Resolved is Executable with symlinks followed, at start.
	Resolved string `json:"resolved"`
	// Size and ModTimeUnixNano are the resolved file's, at start.
	Size            int64 `json:"size"`
	ModTimeUnixNano int64 `json:"mod_time_unix_nano"`
}

// executablePath is os.Executable, a variable so tests can point it elsewhere.
var executablePath = os.Executable

// CurrentBuild describes the running executable.
func CurrentBuild(version string) (Build, error) {
	exe, err := executablePath()
	if err != nil {
		return Build{}, fmt.Errorf("locating the running executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return Build{}, fmt.Errorf("resolving %s: %w", exe, err)
	}
	fi, err := os.Stat(resolved)
	if err != nil {
		return Build{}, fmt.Errorf("reading %s: %w", resolved, err)
	}
	return Build{
		Version:         version,
		Executable:      exe,
		Resolved:        resolved,
		Size:            fi.Size(),
		ModTimeUnixNano: fi.ModTime().UnixNano(),
	}, nil
}

// Stale reports why the executable on disk is no longer the one b describes,
// or "" while it still is. A missing file counts as changed: whoever replaced
// or removed it is not running this build anymore.
func (b Build) Stale() string {
	resolved, err := filepath.EvalSymlinks(b.Executable)
	if err != nil {
		return fmt.Sprintf("%s is gone", b.Executable)
	}
	if resolved != b.Resolved {
		return fmt.Sprintf("%s now points to %s, not %s", b.Executable, resolved, b.Resolved)
	}
	fi, err := os.Stat(resolved)
	if err != nil {
		return fmt.Sprintf("%s is gone", resolved)
	}
	if fi.Size() != b.Size || fi.ModTime().UnixNano() != b.ModTimeUnixNano {
		return fmt.Sprintf("%s was replaced on disk", resolved)
	}
	return ""
}

// Info is the record a running sentinel keeps in sentinel.json.
type Info struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	Build     Build     `json:"build"`
}

func infoPath(vexillumHome string) string {
	return filepath.Join(vexillumHome, "sentinel.json")
}

// PublishInfo records, for the calling sentinel (which must hold the lock),
// the build it runs from.
func PublishInfo(vexillumHome string, b Build) error {
	info := Info{PID: os.Getpid(), StartedAt: time.Now().UTC(), Build: b}
	if err := atomicfile.WriteJSON(infoPath(vexillumHome), info); err != nil {
		return fmt.Errorf("recording sentinel build: %w", err)
	}
	return nil
}

func readInfo(vexillumHome string) (Info, bool) {
	data, err := os.ReadFile(infoPath(vexillumHome))
	if err != nil {
		return Info{}, false
	}
	var info Info
	if err := json.Unmarshal(data, &info); err != nil || info.PID <= 1 {
		return Info{}, false
	}
	return info, true
}

// LiveInfo returns the build record of the sentinel currently holding the
// lock. It reports false when none runs, and also for a sentinel that never
// wrote one (started before builds were recorded).
func LiveInfo(vexillumHome string) (Info, bool) {
	info, ok := readInfo(vexillumHome)
	if !ok || !processAlive(info.PID) || !IsRunning(vexillumHome) {
		return Info{}, false
	}
	return info, true
}

// HolderStale reports whether the running sentinel's own executable has been
// replaced on disk, i.e. it is about to retire and a fresh one should take
// over.
func HolderStale(vexillumHome string) bool {
	info, ok := LiveInfo(vexillumHome)
	return ok && info.Build.Stale() != ""
}

// listProcesses returns "ps" output of the current user's processes, one
// "<pid> <command>" per line. A variable so tests can stand in for ps.
var listProcesses = func() (string, error) {
	out, err := exec.Command("ps", "-x", "-o", "pid=,command=").Output()
	return string(out), err
}

// LivePIDs returns the pid of every "vx sentinel" process of the current user,
// whichever home or build it belongs to and whether or not it holds the lock.
// More than one means sentinels are competing for the same tasks.
func LivePIDs() ([]int, error) {
	out, err := listProcesses()
	if err != nil {
		return nil, fmt.Errorf("listing processes: %w", err)
	}
	var pids []int
	for _, line := range strings.Split(out, "\n") {
		field, command, found := strings.Cut(strings.TrimSpace(line), " ")
		pid, convErr := strconv.Atoi(field)
		if !found || convErr != nil {
			continue
		}
		if isSentinelCommand(command) {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}
