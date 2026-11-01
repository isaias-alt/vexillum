package doctorcheck

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/models"
	"github.com/isaias-alt/vexillum/internal/soldier"
)

// HCBackend reports on the experimental hc (herdr-core) backend, selected with
// VX_BACKEND=hc, in place of the herdr TUI lines: the core binary and its
// "hc x.y.z" version (which also rejects the herdr TUI), the protocol hello over
// "hc connect", and whether doctor runs in an app terminal. It costs the
// --version run and one short "hc connect" round trip, nothing slower.
func HCBackend() []Result {
	return hcBackend(os.Getenv, exec.LookPath, herdr.HCVersionOf, func(bin string) error {
		return (&herdr.HC{Bin: bin}).HelloOK()
	})
}

// hcBackend is HCBackend with everything it touches injected, so tests control it.
func hcBackend(getenv func(string) string, lookPath func(string) (string, error), version func(string) (string, error), hello func(bin string) error) []Result {
	results := []Result{{
		Name:   "terminal backend",
		OK:     true,
		Detail: herdr.BackendEnv + "=hc - herdr-core, experimental (the herdr TUI is not used)",
	}}

	bin, ver, err := herdr.ResolveHC(lookPath, version)
	if err != nil {
		return append(results, Result{Name: "herdr-core", Required: true, Detail: err.Error() + " - install it with 'curl -fsSL https://releases.herdr.dev/core/install.sh | sh'"})
	}
	results = append(results, Result{Name: "herdr-core", OK: true, Required: true, Detail: fmt.Sprintf("%s (hc %s)", bin, ver)})

	if err := hello(bin); err != nil {
		results = append(results, Result{Name: "herdr-core protocol", Warn: true, Detail: "hello failed: " + err.Error()})
	} else {
		results = append(results, Result{Name: "herdr-core protocol", OK: true, Detail: "hello answered generation 1"})
	}

	session := herdr.PinnedSession(getenv)
	if id := getenv("HC_TERMINAL_ID"); id != "" {
		results = append(results, Result{Name: "Herdr app terminal", OK: true, Detail: fmt.Sprintf("terminal %s, session %s", id, session)})
	} else {
		results = append(results, Result{Name: "Herdr app terminal", Warn: true, Detail: "HC_TERMINAL_ID is not set - dispatch, redispatch and the Stop hook await need a Herdr app terminal (session " + session + ")"})
	}
	if path, err := lookPath("herdr-app"); err == nil {
		results = append(results, Result{Name: "herdr-app", OK: true, Detail: path + " - names each soldier's tab vx-<slug> in the app (default session only)"})
	} else {
		results = append(results, Result{Name: "herdr-app", OK: true, Detail: "not on PATH - soldier tabs show as 1, 2... in the Herdr app (optional)"})
	}
	return results
}

// BackendValue reports a VX_BACKEND value vexillum does not know.
func BackendValue() Result {
	if err := herdrBackendError(); err != nil {
		return Result{Name: herdr.BackendEnv, Required: true, Detail: err.Error()}
	}
	return Result{}
}

func herdrBackendError() error {
	_, err := herdr.BackendName(os.Getenv)
	return err
}

// HCOpenCodeProfiles warns, under VX_BACKEND=hc, about each configured model
// profile (built-in, global or project models.json) that uses the opencode
// harness: OpenCode reports no status to herdr-core, so dispatch refuses it.
// Nothing is reported when no profile uses opencode.
func HCOpenCodeProfiles(projectDir, vexillumHome string) []Result {
	t, err := models.Load(projectDir, vexillumHome)
	if err != nil {
		return nil
	}
	var names []string
	for _, p := range t.Profiles {
		if p.Harness == soldier.HarnessOpenCode {
			names = append(names, p.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return []Result{{
		Name:   "opencode profiles",
		Warn:   true,
		Detail: "profile(s) " + strings.Join(names, ", ") + " use the opencode harness, which reports no status to herdr-core - dispatch refuses them under " + herdr.BackendEnv + "=hc; use a claude or pi profile, or the herdr TUI backend",
	}}
}
