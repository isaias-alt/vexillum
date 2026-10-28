package doctorcheck

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/models"
	"github.com/isaias-alt/vexillum/internal/soldier"
)

// PiSupportedVersionPrefix is the Pi version series vexillum's Pi harness was
// checked against (the docs and CLI of Pi 1.3.0). Pi's flags have moved
// between majors: the fullscreen TUI default, MCP handling under --tools and
// the --no-mcp flag all arrived in 1.x, so doctor warns on any other series.
const PiSupportedVersionPrefix = "1."

// PiChecks reports on Pi, the agent CLI a model profile can select with
// "harness": "pi": whether the binary is on PATH, its version, and whether
// herdr's integration for it is installed. Nothing is reported, so a
// Claude-only setup sees no new line, unless a profile in models.json names
// the pi harness or a pi binary is on PATH anyway. The binary is Required only
// when commander is true, that is when Pi is one of the project's commander
// harnesses (see Commanders); then it is always reported. The rest is never
// Required: a missing piece fails a Pi dispatch with its own message, it does
// not make the environment unusable for Claude.
func PiChecks(projectDir, vexillumHome string, commander bool, probes *Probes) []Result {
	usesPi := false
	if t, err := models.Load(projectDir, vexillumHome); err == nil {
		for _, p := range t.Profiles {
			if p.Harness == soldier.HarnessPi {
				usesPi = true
			}
		}
	}
	_, lookErr := exec.LookPath("pi")
	present := lookErr == nil
	if !usesPi && !present && !commander {
		return nil
	}

	if !present && commander {
		return []Result{{Name: "Pi", Required: true, Detail: "pi not found in PATH - " + installHints["pi"]}}
	}
	if !present {
		return []Result{{Name: "Pi", Warn: true, Detail: "a model profile uses harness pi but pi is not on PATH - " + installHints["pi"]}}
	}
	out := []Result{{Name: "Pi", OK: true, Required: commander}, piVersion()}
	if integration, ok := piIntegration(probes); ok {
		out = append(out, integration)
	}
	return out
}

// piVersion runs `pi --version`, which prints the bare version number.
func piVersion() Result {
	const name = "Pi version"
	raw, err := exec.Command("pi", "--version").Output()
	if err != nil {
		return Result{Name: name, Warn: true, Detail: "could not determine pi version: " + err.Error()}
	}
	version := strings.TrimSpace(string(raw))
	if version == "" {
		return Result{Name: name, Warn: true, Detail: "pi --version printed nothing"}
	}
	if !strings.HasPrefix(version, PiSupportedVersionPrefix) {
		return Result{Name: name, Warn: true, Detail: fmt.Sprintf("%s is not a %s version - vexillum's Pi harness was verified against %sx, this version may behave differently", version, PiSupportedVersionPrefix, PiSupportedVersionPrefix)}
	}
	return Result{Name: name, OK: true, Detail: version}
}

// piIntegration reports herdr's integration for pi, the extension that lets a
// Pi soldier tell herdr whether it is working or idle instead of herdr
// reading the screen. ok is false when herdr is not on PATH, which the
// "herdr" check already reports.
func piIntegration(probes *Probes) (Result, bool) {
	if _, err := exec.LookPath("herdr"); err != nil {
		return Result{}, false
	}
	const name = "herdr integration for pi"
	state, detail, err := probes.IntegrationStatus("pi")
	if err != nil {
		return Result{Name: name, Warn: true, Detail: "could not read the integration status: " + err.Error()}, true
	}
	switch state {
	case herdr.IntegrationNotInstalled:
		return Result{Name: name, Warn: true, Detail: "not installed, so herdr reads a Pi soldier's state off the screen; run 'herdr integration install pi'"}, true
	case herdr.IntegrationOutdated:
		return Result{Name: name, Warn: true, Detail: detail + "; run 'herdr integration install pi' to update it"}, true
	}
	return Result{Name: name, OK: true, Detail: detail}, true
}
