package doctorcheck

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/commanderreg"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/sentinel"
)

// pendingWakeGrace is how long a wake may wait before it counts as undelivered:
// a live await or the sentinel's injector looks every few seconds, so a wake
// younger than this is just in flight.
const pendingWakeGrace = time.Minute

// PendingWakes warns about every project whose wake queue holds wakes that
// nobody is listening for: no live "vx sentinel await" (the Stop hook of a
// Claude Code or Pi commander) and no live registered commander (an OpenCode
// one, woken by the sentinel itself). Such wakes are not lost, they wait for
// the next commander turn in that project, but the commander that dispatched
// the soldiers may well be somewhere else, or idle past the await's 55
// minutes, and not hear about them. It reports nothing when every queue is
// empty or has a listener. Read-only; it starts processes only for a project
// that has wakes waiting.
func PendingWakes(vexillumHome string) []Result {
	return pendingWakes(vexillumHome, herdr.CLI{}, time.Now(), sentinel.HasLiveAwaiter)
}

func pendingWakes(vexillumHome string, h herdr.Locator, now time.Time, hasAwaiter func(vexillumHome, projectRoot string) bool) []Result {
	var out []Result
	for _, q := range sentinel.PendingQueues(vexillumHome) {
		if now.Sub(q.Oldest) < pendingWakeGrace {
			continue
		}
		if hasAwaiter(vexillumHome, q.ProjectRoot) || commanderreg.Live(q.ProjectRoot, vexillumHome, h) {
			continue
		}
		noun := "wakes"
		if q.Count == 1 {
			noun = "wake"
		}
		out = append(out, Result{
			Name: "pending wakes in " + filepath.Base(q.ProjectRoot),
			Warn: true,
			Detail: fmt.Sprintf("%d %s waiting since %s and nothing to deliver them (no live '%s sentinel await', no live commander registered there): "+
				"the commander that dispatched the soldiers does not hear about them until a commander takes a turn in that project, or '%s sentinel drain' is run from it; "+
				"a Claude Code commander idle for more than an hour has no await left, and a soldier dispatched before this version recorded no commander",
				q.Count, noun, q.Oldest.Format("15:04"), cmdname.Name, cmdname.Name),
		})
	}
	return out
}
