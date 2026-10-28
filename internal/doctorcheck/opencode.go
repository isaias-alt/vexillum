package doctorcheck

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/models"
	"github.com/isaias-alt/vexillum/internal/soldier"
)

// OpenCodeChecks reports on OpenCode, the agent CLI a model profile can select
// with "harness": "opencode": whether the binary is on PATH, whether its
// version is one vexillum drives (1.x from 1.18.29, or 2.x) and which, whether
// an older install shadows a newer one in PATH, and whether herdr's
// integration for it is installed and current. Nothing is reported, so a setup
// without OpenCode sees no new line, unless a profile in models.json names the
// opencode harness or an opencode binary is on PATH anyway. The binary is
// Required only when commander is true, that is when OpenCode is one of the
// project's commander harnesses (see Commanders); then it is always reported.
// The rest is never Required: a missing piece fails an OpenCode dispatch with
// its own message, it does not make the environment unusable for the other
// harnesses.
func OpenCodeChecks(projectDir, vexillumHome string, commander bool, probes *Probes) []Result {
	return openCodeChecks(projectDir, vexillumHome, os.Getenv("PATH"), commander, probes)
}

func openCodeChecks(projectDir, vexillumHome, pathEnv string, commander bool, probes *Probes) []Result {
	usesOpenCode := false
	if t, err := models.Load(projectDir, vexillumHome); err == nil {
		for _, p := range t.Profiles {
			if p.Harness == soldier.HarnessOpenCode {
				usesOpenCode = true
			}
		}
	}
	installs := probes.openCodeInstalls(pathEnv)
	if !usesOpenCode && len(installs) == 0 && !commander {
		return nil
	}
	if len(installs) == 0 && commander {
		return []Result{{Name: "OpenCode", Required: true, Detail: "opencode not found in PATH - " + installHints["opencode"]}}
	}
	if len(installs) == 0 {
		return []Result{{Name: "OpenCode", Warn: true, Detail: "a model profile uses harness opencode but opencode is not on PATH - " + installHints["opencode"]}}
	}

	out := []Result{{Name: "OpenCode", OK: true, Required: commander}, openCodeVersion(installs[0])}
	if shadow, ok := openCodeShadow(installs); ok {
		out = append(out, shadow)
	}
	if integration, ok := openCodeIntegration(probes); ok {
		out = append(out, integration)
	}
	return out
}

// openCodeInstall is one opencode executable found in PATH, in PATH order.
type openCodeInstall struct {
	Path    string // as found in PATH
	Real    string // symlinks resolved
	Version soldier.OpenCodeVersion
	Err     error // from reading the version
}

// scanOpenCodeInstalls lists every distinct opencode executable in pathEnv's
// directories, first to win first, with its version. A path that resolves to
// one already listed (a brew link and the Cellar file) is one install.
func scanOpenCodeInstalls(pathEnv string) []openCodeInstall {
	var out []openCodeInstall
	seen := map[string]bool{}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, "opencode")
		info, err := os.Stat(candidate)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		real := realPath(candidate)
		if seen[real] {
			continue
		}
		seen[real] = true
		out = append(out, openCodeInstall{Path: candidate, Real: real})
	}
	// Each version is a process of its own (about 60 ms for opencode), and the
	// installs are independent: read them together.
	var wg sync.WaitGroup
	for i := range out {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i].Version, out[i].Err = soldier.OpenCodeVersionAt(out[i].Path)
		}()
	}
	wg.Wait()
	return out
}

// openCodeVersion reports the version of the opencode that wins in PATH, the
// one vexillum will run.
func openCodeVersion(in openCodeInstall) Result {
	const name = "OpenCode version"
	if in.Err != nil {
		return Result{Name: name, Warn: true, Detail: "could not determine the version of " + in.Path + ": " + in.Err.Error()}
	}
	if err := in.Version.Supported(); err != nil {
		return Result{Name: name, Warn: true, Detail: err.Error()}
	}
	return Result{Name: name, OK: true, Detail: fmt.Sprintf("%s (V%d)", in.Version, in.Version.Major)}
}

// openCodeShadow warns when the opencode that wins in PATH is a 1.x and a 2.x
// is installed further along: the V1 install, usually left over under
// ~/.opencode/bin, answers every dispatch and the V2 one never runs. ok is
// false when there is nothing to say.
func openCodeShadow(installs []openCodeInstall) (Result, bool) {
	const name = "OpenCode V1 shadowing V2 in PATH"
	if len(installs) < 2 || installs[0].Err != nil || installs[0].Version.Major != 1 {
		return Result{}, false
	}
	for _, later := range installs[1:] {
		if later.Err == nil && later.Version.Major == 2 {
			return Result{Name: name, Warn: true, Detail: fmt.Sprintf("%s (%s) comes first in PATH, so it runs instead of %s (%s) - put the 2.x directory first, or remove the 1.x install",
				installs[0].Path, installs[0].Version, later.Path, later.Version)}, true
		}
	}
	return Result{}, false
}

// openCodeIntegration reports herdr's integration for opencode, the plugin that
// lets an OpenCode soldier tell herdr whether it is working, idle or blocked
// instead of herdr reading the screen. ok is false when herdr is not on PATH,
// which the "herdr" check already reports.
func openCodeIntegration(probes *Probes) (Result, bool) {
	if _, err := exec.LookPath("herdr"); err != nil {
		return Result{}, false
	}
	const name = "herdr integration for opencode"
	state, detail, err := probes.IntegrationStatus("opencode")
	if err != nil {
		return Result{Name: name, Warn: true, Detail: "could not read the integration status: " + err.Error()}, true
	}
	switch state {
	case herdr.IntegrationNotInstalled:
		return Result{Name: name, Warn: true, Detail: "not installed, so herdr reads an OpenCode soldier's state off the screen; run 'herdr integration install opencode'"}, true
	case herdr.IntegrationOutdated:
		return Result{Name: name, Warn: true, Detail: detail + "; run 'herdr integration install opencode' to update it, then restart OpenCode's shared background service and any open OpenCode TUI so an old one cannot overwrite the new reporter"}, true
	}
	return Result{Name: name, OK: true, Detail: strings.TrimSpace(detail)}, true
}
