// Package commanderreg is the commander registry: which herdr pane each
// commander of a project runs in, one entry per harness kind.
//
// Nothing else in vexillum knows where a commander lives. A Claude Code
// commander is reached through its Stop hook's "vx sentinel await", which
// needs no address; a Pi or OpenCode commander is woken from outside, so the
// sentinel and the forum listener need its pane. The pane is the one
// HERDR_PANE_ID names in the shell of whoever runs a commander command
// ("vx dispatch", "vx forum open"), which registers it here as a side effect.
//
// The file is <projectRoot>/commanders.json. Registering a kind overwrites that
// kind's entry and leaves the others alone, so one project can have a Claude
// and a Pi commander at once. Entries are never deleted by a reader: Resolve
// only judges whether one is still alive, and a stale entry is simply absent.
package commanderreg

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/schemaver"
)

const (
	fileName     = "commanders.json"
	lockFileName = "commanders.lock"
)

var (
	// ErrNoPane means there is no pane id to register (the command did not run
	// inside a herdr pane).
	ErrNoPane = errors.New("no herdr pane id")
	// ErrNotAnAgent means herdr knows no agent in the pane, or does not say
	// which harness it runs, so there is no kind to register it under.
	ErrNotAnAgent = errors.New("the pane is not a recognized agent")
)

// Entry is one registered commander.
type Entry struct {
	PaneID string `json:"pane_id"`
	// Dir is the project directory the commander ran its command from (the git
	// top level). A pane id can be reused after herdr restarts, so an entry only
	// counts while the agent in the pane still works inside Dir.
	Dir          string    `json:"dir"`
	RegisteredAt time.Time `json:"registered_at"`
}

type registryFile struct {
	SchemaVersion int `json:"schema_version"`
	// Commanders maps a harness kind ("claude", "pi", "opencode") to its entry.
	Commanders map[string]Entry `json:"commanders"`
}

// Commander is a live commander: where it is right now.
type Commander struct {
	// Harness is the kind the commander was registered under.
	Harness string
	PaneID  string
	// Recomputed is true when the registered pane no longer answered and the
	// pane was found again by working directory instead.
	Recomputed bool
}

// Path is the registry file of a project.
func Path(projectRoot string) string {
	return filepath.Join(projectRoot, fileName)
}

// Load reads the registry; a project that never registered a commander has an
// empty one.
func Load(projectRoot string) (map[string]Entry, error) {
	data, err := os.ReadFile(Path(projectRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading the commander registry: %w", err)
	}
	var f registryFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", Path(projectRoot), err)
	}
	if !schemaver.Supported(f.SchemaVersion) {
		return nil, fmt.Errorf("unsupported commander registry schema version %d (expected %d)", f.SchemaVersion, schemaver.Current)
	}
	return f.Commanders, nil
}

// Register records the commander running in paneID under the harness herdr
// reports for it, replacing that kind's earlier entry, and returns the kind.
// dir is the project directory the command ran from. It returns ErrNoPane for
// an empty paneID and ErrNotAnAgent when herdr has no agent there.
func Register(projectRoot, dir, paneID string, h herdr.Locator, now time.Time) (string, error) {
	if paneID == "" {
		return "", ErrNoPane
	}
	info, err := h.AgentGet(paneID)
	if err != nil {
		if herdr.IsNotFound(err) {
			return "", fmt.Errorf("%w: %s", ErrNotAnAgent, paneID)
		}
		return "", fmt.Errorf("looking up pane %s: %w", paneID, err)
	}
	if info.Kind == "" {
		return "", fmt.Errorf("%w: %s", ErrNotAnAgent, paneID)
	}

	unlock, err := lock(projectRoot)
	if err != nil {
		return "", err
	}
	defer unlock()
	commanders, err := Load(projectRoot)
	if err != nil {
		return "", err
	}
	if commanders == nil {
		commanders = map[string]Entry{}
	}
	commanders[info.Kind] = Entry{PaneID: paneID, Dir: dir, RegisteredAt: now.UTC()}
	if err := atomicfile.WriteJSON(Path(projectRoot), registryFile{SchemaVersion: schemaver.Current, Commanders: commanders}); err != nil {
		return "", fmt.Errorf("writing the commander registry: %w", err)
	}
	return info.Kind, nil
}

// lock serializes registrations of one project, so two commanders registering
// at once cannot lose each other's entry. flock releases itself if the holder
// dies.
func lock(projectRoot string) (unlock func(), err error) {
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		return nil, fmt.Errorf("creating the project root: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(projectRoot, lockFileName), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening the commander registry lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("locking the commander registry: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// Resolve returns the live commanders of the project, ordered by harness kind.
// An entry is live when herdr still answers for its pane with the same kind and
// the agent works inside the entry's directory. Otherwise the pane is looked up
// again in `herdr agent list` (same kind, working inside the directory, never a
// soldier's camp under vexillumHome, the focused one first), and the entry is
// absent when that finds nothing either. It reads and signals nothing.
//
// A project with no registry costs no herdr call. The error reports a registry
// that could not be read or a herdr that could not be asked; the commanders
// found anyway are still returned.
func Resolve(projectRoot, vexillumHome string, h herdr.Locator) ([]Commander, error) {
	entries, err := Load(projectRoot)
	if err != nil || len(entries) == 0 {
		return nil, err
	}
	kinds := make([]string, 0, len(entries))
	for kind := range entries {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)

	var (
		live    []Commander
		errs    []error
		listed  []herdr.AgentInfo
		listErr error
		didList bool
	)
	for _, kind := range kinds {
		e := entries[kind]
		info, getErr := h.AgentGet(e.PaneID)
		if getErr == nil && info.Kind == kind && inside(info, e.Dir) {
			live = append(live, Commander{Harness: kind, PaneID: e.PaneID})
			continue
		}
		if getErr != nil && !herdr.IsNotFound(getErr) {
			errs = append(errs, fmt.Errorf("looking up pane %s: %w", e.PaneID, getErr))
		}
		if !didList {
			listed, listErr = h.AgentList()
			didList = true
			if listErr != nil {
				listed = nil
				errs = append(errs, fmt.Errorf("listing agents: %w", listErr))
			}
		}
		if pane := findByDir(listed, kind, e.Dir, vexillumHome); pane != "" {
			live = append(live, Commander{Harness: kind, PaneID: pane, Recomputed: true})
		}
	}
	return live, errors.Join(errs...)
}

// Live reports whether any commander of the project is live (see Resolve).
func Live(projectRoot, vexillumHome string, h herdr.Locator) bool {
	live, _ := Resolve(projectRoot, vexillumHome, h)
	return len(live) > 0
}

// findByDir is the pane of the agent of kind working inside dir and outside
// vexillumHome (soldiers work in camps under it), the focused agent first.
func findByDir(agents []herdr.AgentInfo, kind, dir, vexillumHome string) string {
	pane := ""
	for _, a := range agents {
		if a.Kind != kind || a.PaneID == "" || !inside(a, dir) || inside(a, vexillumHome) {
			continue
		}
		if a.Focused {
			return a.PaneID
		}
		if pane == "" {
			pane = a.PaneID
		}
	}
	return pane
}

// inside reports whether the agent works in dir or below it, by either of its
// reported directories. Symlinks are resolved on both sides, so a project
// reached through one (macOS's /var) still matches.
func inside(a herdr.AgentInfo, dir string) bool {
	if dir == "" {
		return false
	}
	roots := forms(dir)
	for _, cwd := range []string{a.Cwd, a.ForegroundCwd} {
		if cwd == "" {
			continue
		}
		for _, c := range forms(cwd) {
			for _, root := range roots {
				if within(c, root) {
					return true
				}
			}
		}
	}
	return false
}

// forms is path as given and, when it differs, with symlinks resolved.
func forms(path string) []string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != path {
		return []string{path, resolved}
	}
	return []string{path}
}

func within(path, root string) bool {
	path, root = filepath.Clean(path), filepath.Clean(root)
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}
