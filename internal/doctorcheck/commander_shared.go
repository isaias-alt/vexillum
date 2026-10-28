package doctorcheck

import (
	"fmt"
	"strings"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/project"
)

// SharedWakeQueue warns when the project has more than one live commander (for
// instance a Claude Code one and a Pi one). Every commander of a project reads
// the same wake queue and a wake is delivered once, to whichever commander
// asks first, so the other does not hear about a soldier that finished or about
// forum feedback. It reports nothing when at most one commander is live, and
// when the registry or herdr cannot be read (no commander can be proven live).
// Read-only. A registry entry only counts while its pane still runs that
// harness in the project (see commanderreg.Resolve), so a commander that was
// closed long ago does not trigger it.
func SharedWakeQueue(projectDir, vexillumHome string, probes *Probes) []Result {
	return sharedWakeQueue(projectDir, vexillumHome, herdr.CLI{}, probes)
}

func sharedWakeQueue(projectDir, vexillumHome string, h herdr.Locator, probes *Probes) []Result {
	root, err := project.Root(vexillumHome, projectDir)
	if err != nil {
		return nil
	}
	// The error is ignored on purpose: Resolve still returns the commanders it
	// did find, and a herdr that cannot be asked proves nothing either way.
	live, _ := probes.resolveCommanders(root, vexillumHome, h)
	if len(live) < 2 {
		return nil
	}
	kinds := make([]string, len(live))
	for i, c := range live {
		kinds[i] = c.Harness
	}
	return []Result{{
		Name: "commanders share one wake queue",
		Warn: true,
		Detail: fmt.Sprintf("%d commanders are live in this project (%s) and a wake is delivered once, to whichever asks first, so the others may never hear about a soldier that finished or about forum feedback; "+
			"keep one commander per project (the others can catch up with '%s status' and '%s forum inbox')", len(live), strings.Join(kinds, ", "), cmdname.Name, cmdname.Name),
	}}
}
