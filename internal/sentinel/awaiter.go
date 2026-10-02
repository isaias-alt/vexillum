package sentinel

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
	"github.com/isaias-alt/vexillum/internal/cmdname"
)

// Every Stop hook invocation launches a "vx sentinel await" process that
// blocks for up to ~55 minutes waiting for a wake. Claude Code fires the
// hook on every turn end and does not reliably reap the previous one, so
// before this registry they piled up (a dozen seen at once, from sessions
// long dead). Each await process now records itself in
// <vexillumHome>/sentinel-awaiters/<pid>.json and:
//
//   - exits on its own once the process that launched it is gone
//     (OwnerGone), and
//   - on start, supersedes an earlier await of the same owner and project
//     (the previous turn's, now redundant: the newest hook invocation is
//     the one Claude Code is certainly still listening to) and reaps
//     orphans left behind by owners that died.
//
// Nothing here ever signals a process by name or by a bare pid read from a
// file: a pid is only signaled after inspectProcess confirms it is still a
// "vx sentinel await" whose parent is what its record says (a recycled pid
// belongs to something else and is left alone, only its stale record goes).

const awaitersDirName = "sentinel-awaiters"

// legacyExecutableName is the executable's name before cmdname.Name; an
// await launched by an old hook may still be running under it.
const legacyExecutableName = "vexillum"

// AwaiterRecord is what an await process writes about itself.
type AwaiterRecord struct {
	PID         int       `json:"pid"`
	OwnerPID    int       `json:"owner_pid"`
	ProjectRoot string    `json:"project_root"`
	StartedAt   time.Time `json:"started_at"`
}

func awaitersDir(vexillumHome string) string {
	return filepath.Join(vexillumHome, awaitersDirName)
}

func awaiterPath(vexillumHome string, pid int) string {
	return filepath.Join(awaitersDir(vexillumHome), strconv.Itoa(pid)+".json")
}

// errProcessGone is inspectProcess's answer for a pid that does not exist.
var errProcessGone = errors.New("no such process")

// inspectProcess reports pid's parent pid and full command line. It is a
// variable so tests can stand in for ps.
var inspectProcess = func(pid int) (ppid int, command string, err error) {
	out, err := exec.Command("ps", "-o", "ppid=,command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		// ps exits non-zero for a pid that does not exist.
		return 0, "", errProcessGone
	}
	return parsePSLine(string(out))
}

// parsePSLine parses one "ps -o ppid=,command=" line.
func parsePSLine(line string) (ppid int, command string, err error) {
	line = strings.TrimSpace(line)
	field, rest, found := strings.Cut(line, " ")
	if !found {
		return 0, "", errProcessGone
	}
	ppid, convErr := strconv.Atoi(field)
	if convErr != nil {
		return 0, "", fmt.Errorf("unexpected ps output %q", line)
	}
	return ppid, strings.TrimSpace(rest), nil
}

// isAwaitCommand reports whether command is a "vx sentinel await"
// invocation: the executable (under its current or legacy name, any path)
// followed by exactly those two arguments. The main polling sentinel ("vx
// sentinel") and anything else never match.
func isAwaitCommand(command string) bool {
	exe, ok := strings.CutSuffix(strings.TrimSpace(command), " sentinel await")
	if !ok {
		return false
	}
	// An executable path may contain spaces only when it is absolute; a
	// relative command with spaces is some other program's arguments that
	// merely end in the same words.
	if strings.ContainsAny(exe, " \t") && !strings.HasPrefix(exe, "/") {
		return false
	}
	base := filepath.Base(exe)
	return base == cmdname.Name || base == legacyExecutableName
}

// OwnerGone reports whether the process that launched this one (ownerPID,
// the parent recorded at start) is gone. A process whose parent dies is
// reparented, so a changed parent pid also covers a recycled owner pid,
// which a plain "is that pid alive" probe would miss.
func OwnerGone(ownerPID int) bool {
	return os.Getppid() != ownerPID
}

// RegisterAwaiter records the calling await process (owned by ownerPID,
// waiting on projectRoot's wakes) and cleans up around it: it signals to
// exit any other live await of the same owner and project, and any await
// whose owner is gone, and drops the records of processes that no longer
// exist. The returned release removes this process's own record; call it on
// every exit path.
func RegisterAwaiter(vexillumHome, projectRoot string, ownerPID int) (release func(), err error) {
	dir := awaitersDir(vexillumHome)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating awaiters directory: %w", err)
	}

	self := os.Getpid()
	rec := AwaiterRecord{PID: self, OwnerPID: ownerPID, ProjectRoot: projectRoot, StartedAt: time.Now().UTC()}
	path := awaiterPath(vexillumHome, self)
	if err := atomicfile.WriteJSON(path, rec); err != nil {
		return nil, fmt.Errorf("recording awaiter: %w", err)
	}

	// Sweep only after our own record is on disk, and supersede only
	// strictly older awaits: two awaits starting at the same instant each
	// see the other's record, and without an ordering both would signal
	// each other and both would die. With it exactly the newest survives.
	sweepAwaiters(vexillumHome, func(r AwaiterRecord) bool {
		return r.OwnerPID == ownerPID && r.ProjectRoot == projectRoot && startedBefore(r, rec)
	})
	return func() { _ = os.Remove(path) }, nil
}

// startedBefore orders two await records by start time, breaking a tie by
// pid so the order is total.
func startedBefore(a, b AwaiterRecord) bool {
	if a.StartedAt.Equal(b.StartedAt) {
		return a.PID < b.PID
	}
	return a.StartedAt.Before(b.StartedAt)
}

// ReapAwaiters signals to exit every await process whose owner is gone and
// drops stale records, returning how many processes it signaled. The
// polling sentinel runs it at startup; RegisterAwaiter does the same sweep
// whenever a new await starts.
func ReapAwaiters(vexillumHome string) int {
	return sweepAwaiters(vexillumHome, nil)
}

// sweepAwaiters walks the awaiter records. A record whose process is gone
// or is no longer a "vx sentinel await" (its pid was recycled) is deleted
// without signaling anything. A verified await is sent SIGTERM when its
// real parent no longer matches the record (an orphan) or when superseded
// says so; its own handler exits it quietly with status 0. Returns the
// number of processes signaled.
func sweepAwaiters(vexillumHome string, superseded func(AwaiterRecord) bool) int {
	entries, err := os.ReadDir(awaitersDir(vexillumHome))
	if err != nil {
		return 0
	}

	signaled := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(awaitersDir(vexillumHome), entry.Name())

		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var rec AwaiterRecord
		if err := json.Unmarshal(data, &rec); err != nil || rec.PID <= 1 || entry.Name() != strconv.Itoa(rec.PID)+".json" {
			_ = os.Remove(path)
			continue
		}
		if rec.PID == os.Getpid() {
			continue
		}

		ppid, command, err := inspectProcess(rec.PID)
		if err != nil || !isAwaitCommand(command) {
			_ = os.Remove(path)
			continue
		}

		orphaned := ppid != rec.OwnerPID
		if !orphaned && (superseded == nil || !superseded(rec)) {
			continue
		}
		if err := syscall.Kill(rec.PID, syscall.SIGTERM); err == nil {
			signaled++
		}
		_ = os.Remove(path)
	}
	return signaled
}

// LiveAwaiters counts the "vx sentinel await" processes currently alive
// under vexillumHome: the Stop hooks that are blocked waiting to wake a
// commander. It only reads - it signals nothing and deletes no record.
func LiveAwaiters(vexillumHome string) int {
	entries, err := os.ReadDir(awaitersDir(vexillumHome))
	if err != nil {
		return 0
	}
	live := 0
	for _, entry := range entries {
		pid, convErr := strconv.Atoi(strings.TrimSuffix(entry.Name(), ".json"))
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || convErr != nil || pid <= 1 {
			continue
		}
		if _, command, err := inspectProcess(pid); err == nil && isAwaitCommand(command) {
			live++
		}
	}
	return live
}
