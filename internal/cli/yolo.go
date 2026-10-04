package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/scaffold"
	"github.com/isaias-alt/vexillum/internal/yolo"
)

const yoloUsage = `Turn yolo mode on or off for this project, or print whether it is on.

Usage:
  ` + cmdname.Name + ` yolo [on|off|status]

With yolo on, the commander lands a finished, verified mission right away with
'` + cmdname.Name + ` land' and strikes its camp in the same turn, instead of asking the
general first. It is off by default, and with no argument the command prints
the current state like 'status' does.

'status' prints "on" or "off" and exits 0. A missing setting is off. A
.vexillum/yolo.json that is not valid is an error, never a silent "off".

Yolo never covers '` + cmdname.Name + ` ship' (opening a real pull request stays the
general's decision), pushing, a scout, or a land that is refused or diverged:
those are always reported to the general. It never discards work either.

The setting is stored in .vexillum/yolo.json and is committed with the project
like models.json. '` + cmdname.Name + ` land' refuses a dirty checkout, so commit the file after
changing it. Turning it on needs a project prepared with '` + cmdname.Name + ` init'.
`

// Yolo runs the "vx yolo" command.
func Yolo(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(yoloUsage)
		return 0
	}
	if len(args) > 1 {
		fmt.Fprintf(os.Stderr, cmdname.Name+": yolo takes at most one argument, got %d\n", len(args))
		return 1
	}
	action := "status"
	if len(args) == 1 {
		action = args[0]
	}

	projectDir, _, err := resolveDirs()
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}
	return runYolo(projectDir, action, os.Stdout, os.Stderr)
}

func runYolo(projectDir, action string, stdout, stderr io.Writer) int {
	switch action {
	case "status":
		on, err := yolo.Enabled(projectDir)
		if err != nil {
			fmt.Fprintln(stderr, cmdname.Name+":", err)
			return 1
		}
		fmt.Fprintln(stdout, onOff(on))
		return 0
	case "on", "off":
		on := action == "on"
		if !scaffold.ProjectInitialized(projectDir) {
			if !on {
				// Nothing was ever turned on here, so it is already off.
				fmt.Fprintln(stdout, "off")
				return 0
			}
			fmt.Fprintln(stderr, cmdname.Name+": project not initialized here (no .vexillum/config.json), run '"+cmdname.Name+" init' first")
			return 1
		}
		if err := yolo.Set(projectDir, on); err != nil {
			fmt.Fprintln(stderr, cmdname.Name+":", err)
			return 1
		}
		fmt.Fprintln(stdout, onOff(on))
		fmt.Fprintln(stderr, "Wrote .vexillum/"+yolo.FileName+". Commit it: '"+cmdname.Name+" land' refuses a dirty checkout.")
		return 0
	}
	fmt.Fprintf(stderr, cmdname.Name+": unknown yolo action %q (use on, off or status)\n", action)
	return 1
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
