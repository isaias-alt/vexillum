package herdr

import (
	"fmt"
	"os"
	"strings"
)

// BackendEnv is the environment variable that picks the terminal backend
// vexillum drives. Unset (or "herdr") is the herdr TUI, the default; "hc" is the
// experimental herdr-core backend, which lets vexillum run from a Herdr desktop
// app terminal.
const BackendEnv = "VX_BACKEND"

const (
	BackendHerdr = "herdr"
	BackendHC    = "hc"
)

// Backend is a terminal backend: both what internal/soldier drives (Client) and
// what the commander registry asks (Locator). CLI is the herdr TUI one and HC
// the herdr-core one.
type Backend interface {
	Client
	Locator
}

// BackendName returns the backend getenv selects, or an error naming the
// values that are accepted. It is a pure function of the environment so a
// command can reject a bad value before it touches anything.
func BackendName(getenv func(string) string) (string, error) {
	switch v := strings.ToLower(strings.TrimSpace(getenv(BackendEnv))); v {
	case "", BackendHerdr:
		return BackendHerdr, nil
	case BackendHC:
		return BackendHC, nil
	default:
		return "", fmt.Errorf("unknown %s %q (want %q or %q)", BackendEnv, getenv(BackendEnv), BackendHerdr, BackendHC)
	}
}

// WantsHC reports whether getenv selects the hc backend. Any other value,
// including an unusable one, counts as not selecting it: the commands validate
// the variable up front (BackendName) and refuse to run on a bad one, so a
// helper deep inside one need not.
func WantsHC(getenv func(string) string) bool {
	return strings.EqualFold(strings.TrimSpace(getenv(BackendEnv)), BackendHC)
}

// UsingHC is WantsHC for the process environment.
func UsingHC() bool { return WantsHC(os.Getenv) }

// Default is the backend the process environment selects: the herdr TUI client
// unless VX_BACKEND=hc.
func Default() Backend {
	if UsingHC() {
		return NewHC()
	}
	return CLI{}
}
