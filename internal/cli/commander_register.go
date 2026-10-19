package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/commanderreg"
	"github.com/isaias-alt/vexillum/internal/herdr"
)

// commanderLocator is the herdr that registerCommander asks which agent runs in
// the pane. A variable so tests can answer without a herdr server.
var commanderLocator herdr.Locator = herdr.CLI{}

// registerCommander records the herdr pane the running command was typed in
// (HERDR_PANE_ID) in the project's commander registry, under the harness herdr
// reports for it. The commands a commander runs call it ("dispatch",
// "forum open"), so the sentinel and the forum listener can find a commander
// that has no Stop hook await to find. It is best effort: a command run outside
// a herdr pane, or in a pane herdr does not know as an agent, registers
// nothing silently, and a real failure is a warning, never an error of the
// command that called it.
func registerCommander(projectRoot, cwd string, stderr io.Writer) {
	dir := cwd
	if top, err := gitToplevel(cwd); err == nil {
		dir = top
	}
	_, err := commanderreg.Register(projectRoot, dir, os.Getenv("HERDR_PANE_ID"), commanderLocator, time.Now())
	if err == nil || errors.Is(err, commanderreg.ErrNoPane) || errors.Is(err, commanderreg.ErrNotAnAgent) {
		return
	}
	fmt.Fprintf(stderr, cmdname.Name+": warning: could not register this commander: %v\n", err)
}
