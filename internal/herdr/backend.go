package herdr

import (
	"fmt"
	"os"
	"strings"
)

// BackendEnv is the environment variable that picks the terminal backend
// vexillum drives. Set to "herdr" it is the herdr TUI, set to "hc" the
// experimental herdr-core backend, which lets vexillum run from a Herdr desktop
// app terminal. Unset, Select decides from the terminal the process runs in.
const BackendEnv = "VX_BACKEND"

const (
	BackendHerdr = "herdr"
	BackendHC    = "hc"
)

// The variables Select reads to recognize the terminal. The Herdr app (hc) sets
// HCTerminalEnv and none of the HERDR_* ones; a herdr TUI pane sets the latter,
// and these two are the ones the commands already use to tell a pane apart
// (the workspace dispatch needs, the pane a commander registers).
const (
	HCTerminalEnv   = "HC_TERMINAL_ID"
	TUIPaneEnv      = "HERDR_PANE_ID"
	TUIWorkspaceEnv = "HERDR_WORKSPACE_ID"
)

// How a backend was chosen (Selection.Source).
const (
	// SourceEnv: VX_BACKEND was set.
	SourceEnv = "env"
	// SourceDetected: VX_BACKEND was unset and the process runs in a Herdr app
	// terminal.
	SourceDetected = "detected"
	// SourceDefault: neither, so the herdr TUI.
	SourceDefault = "default"
)

// Backend is a terminal backend: both what internal/soldier drives (Client) and
// what the commander registry asks (Locator). CLI is the herdr TUI one and HC
// the herdr-core one.
type Backend interface {
	Client
	Locator
}

// Selection is the backend a process uses and how it was chosen.
type Selection struct {
	Name   string
	Source string
}

// Select is the one place the backend is chosen. In order:
//
//  1. VX_BACKEND set: it wins ("hc" or "herdr"; any other value is an error
//     naming the accepted ones).
//  2. VX_BACKEND unset and the process runs in a Herdr app terminal, that is
//     HC_TERMINAL_ID is set and neither HERDR_PANE_ID nor HERDR_WORKSPACE_ID is:
//     hc. A core restarted with the app carries HC_TERMINAL_ID and no VX_BACKEND,
//     and exporting VX_BACKEND=hc in a shell rc would break the TUI.
//  3. Anything else: the herdr TUI. That includes an environment with both HC_*
//     and HERDR_* variables set: it is not guessed, the TUI stays what it was.
//
// It is a pure function of the environment so a command can reject a bad value
// before it touches anything.
func Select(getenv func(string) string) (Selection, error) {
	switch v := strings.ToLower(strings.TrimSpace(getenv(BackendEnv))); v {
	case BackendHC:
		return Selection{BackendHC, SourceEnv}, nil
	case BackendHerdr:
		return Selection{BackendHerdr, SourceEnv}, nil
	case "":
		if getenv(HCTerminalEnv) != "" && getenv(TUIPaneEnv) == "" && getenv(TUIWorkspaceEnv) == "" {
			return Selection{BackendHC, SourceDetected}, nil
		}
		return Selection{BackendHerdr, SourceDefault}, nil
	default:
		return Selection{}, fmt.Errorf("unknown %s %q (want %q or %q)", BackendEnv, getenv(BackendEnv), BackendHerdr, BackendHC)
	}
}

// BackendName is the name of the backend Select picks, or its error.
func BackendName(getenv func(string) string) (string, error) {
	sel, err := Select(getenv)
	return sel.Name, err
}

// WantsHC reports whether getenv selects the hc backend. An unusable VX_BACKEND
// counts as not selecting it: the commands validate the variable up front
// (BackendName) and refuse to run on a bad one, so a helper deep inside one
// need not.
func WantsHC(getenv func(string) string) bool {
	sel, err := Select(getenv)
	return err == nil && sel.Name == BackendHC
}

// UsingHC is WantsHC for the process environment.
func UsingHC() bool { return WantsHC(os.Getenv) }

// Namer is implemented by a Backend that can say which one it is.
type Namer interface {
	BackendName() string
}

func (CLI) BackendName() string { return BackendHerdr }
func (*HC) BackendName() string { return BackendHC }

// NameOf is the backend c is: BackendHerdr for anything that does not say, which
// is every client but HC (and the fakes tests use).
func NameOf(c any) string {
	if n, ok := c.(Namer); ok {
		return n.BackendName()
	}
	return BackendHerdr
}

// RecordedBackend is what a task dispatched through c records as its backend:
// empty for the herdr TUI, which keeps task files written before backends
// existed valid as they are.
func RecordedBackend(c any) string {
	if name := NameOf(c); name != BackendHerdr {
		return name
	}
	return ""
}

// SameBackend reports whether a task that recorded taskBackend (see
// RecordedBackend) belongs to the backend c is.
func SameBackend(taskBackend string, c any) bool {
	if taskBackend == "" {
		taskBackend = BackendHerdr
	}
	return taskBackend == NameOf(c)
}

// Default is the backend the process environment selects (see Select): the
// herdr TUI client unless that is hc.
func Default() Backend {
	if UsingHC() {
		return NewHC()
	}
	return CLI{}
}
