package doctorcheck

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/models"
	"github.com/isaias-alt/vexillum/internal/opencodeosc"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/soldier"
)

// HCBackend reports on the experimental hc (herdr-core) backend, selected with
// VX_BACKEND=hc or detected in a Herdr app terminal, in place of the herdr TUI lines: the core binary and its
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
	var results []Result

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

// BackendChoice reports which terminal backend vexillum uses and how it was
// chosen (an explicit VX_BACKEND, or detected from the Herdr app terminal this
// runs in), or a VX_BACKEND value vexillum does not know.
func BackendChoice() Result { return backendChoice(os.Getenv) }

func backendChoice(getenv func(string) string) Result {
	sel, err := herdr.Select(getenv)
	if err != nil {
		return Result{Name: herdr.BackendEnv, Required: true, Detail: err.Error()}
	}
	product := "herdr TUI"
	if sel.Name == herdr.BackendHC {
		product = "herdr-core, experimental (the herdr TUI is not used)"
	}
	var how string
	switch sel.Source {
	case herdr.SourceEnv:
		how = "chosen by " + herdr.BackendEnv + "=" + sel.Name
	case herdr.SourceDetected:
		how = "detected from the Herdr app terminal (" + herdr.HCTerminalEnv + " is set, " + herdr.BackendEnv + " is not)"
	default:
		how = "default (" + herdr.BackendEnv + " is not set)"
	}
	return Result{Name: "terminal backend", OK: true, Detail: product + " - " + how}
}

// HCOpenCodePlugin reports, under VX_BACKEND=hc, on what an OpenCode soldier
// needs to be watched there. herdr-core reads a status from the OSC 7501 an agent
// prints and OpenCode prints none, so vexillum launches an OpenCode 2.x soldier
// with a plugin of its own that does (internal/opencodeosc). Nothing is reported
// when no model profile (built-in, global or project models.json) uses the
// opencode harness. Otherwise: a warning when the opencode that wins in PATH is
// a 1.x, which has no plugin here and is refused at dispatch, and for 2.x
// whether the plugin file is installed and current. A missing or stale file is
// not a problem to fix: every dispatch (re)writes it, a running soldier keeps
// the copy it loaded. Never Required. A missing or unreadable opencode is
// reported by OpenCodeChecks, not repeated here.
func HCOpenCodePlugin(projectDir, vexillumHome string, probes *Probes) []Result {
	return hcOpenCodePlugin(projectDir, vexillumHome, os.Getenv("PATH"), probes)
}

func hcOpenCodePlugin(projectDir, vexillumHome, pathEnv string, probes *Probes) []Result {
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

	const name = "opencode status plugin"
	installs := probes.openCodeInstalls(pathEnv)
	if len(installs) == 0 || installs[0].Err != nil {
		return nil
	}
	if v := installs[0].Version; v.Major != 2 {
		return []Result{{Name: name, Warn: true, Detail: fmt.Sprintf("profile(s) %s use the opencode harness, but opencode %s is not 2.x - under %s=hc only OpenCode 2.x reports a status (through vexillum's plugin), so dispatch refuses it; install 2.x or use the herdr TUI backend",
			strings.Join(names, ", "), v, herdr.BackendEnv)}}
	}

	root, err := project.Root(vexillumHome, projectDir)
	if err != nil {
		return []Result{{Name: name, Warn: true, Detail: "could not resolve the project root: " + err.Error()}}
	}
	state, err := opencodeosc.Inspect(root)
	if err != nil {
		return []Result{{Name: name, Warn: true, Detail: "could not read the plugin: " + err.Error()}}
	}
	switch state {
	case opencodeosc.Missing:
		return []Result{{Name: name, OK: true, Detail: "not installed yet - written to " + opencodeosc.Dir(root) + " at the next opencode dispatch"}}
	case opencodeosc.Stale:
		return []Result{{Name: name, OK: true, Detail: opencodeosc.Path(root) + " differs from this vx's plugin - the next opencode dispatch rewrites it (a running soldier keeps the copy it loaded)"}}
	}
	return []Result{{Name: name, OK: true, Detail: opencodeosc.Path(root) + " (current); it replaces the TUI plugins in your cli.json for hc soldiers"}}
}
