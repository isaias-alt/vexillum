package doctorcheck

import (
	"sync"

	"github.com/isaias-alt/vexillum/internal/commanderreg"
	"github.com/isaias-alt/vexillum/internal/herdr"
)

// Probes shares, between the checks of one report, the external processes more
// than one of them needs, so the report starts each once: `herdr integration
// status` (the Pi and OpenCode checks both read it), the `opencode
// --version` of every opencode in PATH (the OpenCode binary check and the
// OpenCode commander checks both list them) and the herdr lookups that tell
// which registered commanders are live (the OpenCode commander check and the
// shared wake queue warning both need them). Safe for concurrent use: the
// first check to ask runs the process and the others wait for its answer.
//
// A Probes is for one report, with the same PATH throughout. A nil *Probes
// shares nothing, every call runs its own process; the zero value is ready.
type Probes struct {
	integrations herdr.IntegrationReader

	mu           sync.Mutex
	opencodePath string
	opencode     []openCodeInstall
	opencodeDone bool

	live     liveCommanders
	liveDone bool
}

// liveCommanders is one commanderreg.Resolve and the arguments it was asked
// with.
type liveCommanders struct {
	root, vexillumHome string
	commanders         []commanderreg.Commander
	err                error
}

// IntegrationStatus is herdr.IntegrationStatus, run once per Probes.
func (p *Probes) IntegrationStatus(name string) (herdr.IntegrationState, string, error) {
	if p == nil {
		return herdr.IntegrationStatus(name)
	}
	return p.integrations.Status(name)
}

// openCodeInstalls is scanOpenCodeInstalls, run once per Probes (and per
// pathEnv, should it ever differ).
func (p *Probes) openCodeInstalls(pathEnv string) []openCodeInstall {
	if p == nil {
		return scanOpenCodeInstalls(pathEnv)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.opencodeDone || p.opencodePath != pathEnv {
		p.opencode, p.opencodePath, p.opencodeDone = scanOpenCodeInstalls(pathEnv), pathEnv, true
	}
	return p.opencode
}

// resolveCommanders is commanderreg.Resolve, run once per Probes (and per
// project and home, should they ever differ).
func (p *Probes) resolveCommanders(root, vexillumHome string, h herdr.Locator) ([]commanderreg.Commander, error) {
	if p == nil {
		return commanderreg.Resolve(root, vexillumHome, h)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.liveDone || p.live.root != root || p.live.vexillumHome != vexillumHome {
		live, err := commanderreg.Resolve(root, vexillumHome, h)
		p.live, p.liveDone = liveCommanders{root: root, vexillumHome: vexillumHome, commanders: live, err: err}, true
	}
	return p.live.commanders, p.live.err
}
