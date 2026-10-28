package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/commanderreg"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/sentinel"
	"github.com/isaias-alt/vexillum/internal/state"
)

// commanderLocator is the herdr that registerCommander asks which agent runs in
// the pane. A variable so tests can answer without a herdr server.
var commanderLocator herdr.Locator = herdr.CLI{}

// registerCommander records the herdr pane the running command was typed in
// (HERDR_PANE_ID) in the project's commander registry, under the harness herdr
// reports for it. The commands a commander runs ("dispatch", "forum open") call
// it, so the sentinel and the forum listener can find a commander that has no
// Stop hook await to find. It is best effort: a command run outside a herdr
// pane, or in a pane herdr does not know as an agent, registers nothing
// silently, and a real failure is a warning, never an error of the command that
// called it.
func registerCommander(projectRoot, cwd string, stderr io.Writer) {
	dir := cwd
	if top, err := gitToplevel(cwd); err == nil {
		dir = top
	}
	registerCommanderAt(projectRoot, dir, commanderLocator, stderr)
}

// registerCommanderAt is registerCommander for a project directory already
// known, asking loc which agent the pane runs.
func registerCommanderAt(projectRoot, dir string, loc herdr.Locator, stderr io.Writer) {
	_, err := commanderreg.Register(projectRoot, dir, os.Getenv("HERDR_PANE_ID"), loc, time.Now())
	if err == nil || errors.Is(err, commanderreg.ErrNoPane) || errors.Is(err, commanderreg.ErrNotAnAgent) {
		return
	}
	fmt.Fprintf(stderr, cmdname.Name+": warning: could not register this commander: %v\n", err)
}

// commanderHome is where the commander that runs a command lives, as far as it
// can be told: the project whose wake queue its Stop hook await drains. It is
// not always the project the command runs in, because a commander can work in
// one repository and dispatch into another.
type commanderHome struct {
	// Pane is the herdr pane the command was typed in (HERDR_PANE_ID); empty
	// outside a herdr pane, which means nothing is known about the commander.
	Pane string
	// Root is the commander's own project root under ~/.vexillum/projects,
	// empty when it could not be told.
	Root string
	// Dir is the git top level of the commander's own project, empty when
	// unknown.
	Dir string
}

// memoLocator remembers what herdr said about each pane, so finding the
// commander's project and registering it cost one "herdr agent get", not two.
type memoLocator struct {
	inner herdr.Locator
	got   map[string]memoGet
}

type memoGet struct {
	info herdr.AgentInfo
	err  error
}

func (m *memoLocator) AgentGet(target string) (herdr.AgentInfo, error) {
	if g, ok := m.got[target]; ok {
		return g.info, g.err
	}
	info, err := m.inner.AgentGet(target)
	if m.got == nil {
		m.got = map[string]memoGet{}
	}
	m.got[target] = memoGet{info: info, err: err}
	return info, err
}

func (m *memoLocator) AgentList() ([]herdr.AgentInfo, error) { return m.inner.AgentList() }

// resolveCommanderHome finds the project the commander running this command
// waits for wakes in. dispatchRoot and dispatchDir are the project the command
// itself runs in (its root, and its git top level).
//
//  1. If the commander's pane has a live "vx sentinel await", that await's project
//     is the answer: it is exactly the queue the Stop hook drains, and it costs
//     no process.
//  2. Otherwise the project is the one the commander's agent works in, per
//     "herdr agent get" (the same fact the commander registry is built on). An
//     agent working inside dispatchDir is in the dispatch project, with no
//     further process; any other directory is resolved with git.
//  3. Otherwise, with a pane but nothing else known, the commander is taken to
//     live in the dispatch project, which is what every command assumed before
//     the commander was looked up at all.
func resolveCommanderHome(vexillumHome, dispatchRoot, dispatchDir string, loc herdr.Locator) commanderHome {
	pane := os.Getenv("HERDR_PANE_ID")
	if pane == "" {
		return commanderHome{}
	}
	home := commanderHome{Pane: pane}
	if root, ok := sentinel.AwaiterProject(vexillumHome, pane); ok {
		home.Root = root
	}
	if info, err := loc.AgentGet(pane); err == nil && info.Kind != "" {
		for _, cwd := range []string{info.Cwd, info.ForegroundCwd} {
			if cwd == "" {
				continue
			}
			dir, root := dispatchDir, dispatchRoot
			if !dirWithin(cwd, dispatchDir) {
				top, err := gitToplevel(cwd)
				if err != nil || refuseInsideVexillumHome(top, vexillumHome) != nil {
					continue
				}
				r, err := project.Root(vexillumHome, top)
				if err != nil {
					continue
				}
				dir, root = top, r
			}
			if home.Root == "" || home.Root == root {
				home.Root, home.Dir = root, dir
			}
			break
		}
	}
	if home.Root == "" {
		home.Root, home.Dir = dispatchRoot, dispatchDir
	}
	return home
}

// dirWithin reports whether path is dir or below it, symlinks resolved on both
// sides.
func dirWithin(path, dir string) bool {
	if dir == "" {
		return false
	}
	resolve := func(p string) string {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return r
		}
		return filepath.Clean(p)
	}
	path, dir = resolve(path), resolve(dir)
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// announceCommander is what a command that starts a soldier ("dispatch",
// "redispatch") does about the commander that ran it: it finds the commander's
// project, registers the commander there (so a Pi or OpenCode commander can be
// found by the sentinel), records on task which commander to wake, and warns
// when that project is not the one being dispatched into. Nothing is written
// outside a herdr pane. It is best effort: a commander that cannot be told
// leaves task as it is.
func announceCommander(task *state.Task, vexillumHome, projectRoot, projectDir string, stderr io.Writer) {
	dir := projectDir
	if top, err := gitToplevel(projectDir); err == nil {
		dir = top
	}
	loc := &memoLocator{inner: commanderLocator}
	home := resolveCommanderHome(vexillumHome, projectRoot, dir, loc)
	if home.Pane == "" {
		return
	}
	if home.Dir != "" {
		registerCommanderAt(home.Root, home.Dir, loc, stderr)
	}
	applyCommander(task, home)
	if home.Root != projectRoot {
		where := home.Dir
		if where == "" {
			where = filepath.Base(home.Root)
		}
		fmt.Fprintf(stderr, cmdname.Name+": warning: dispatching into %s from a commander that lives in %s: wakes for this soldier go to that commander's project, but status, land, strike and prompt still run from %s\n", dir, where, dir)
	}
}

// applyCommander records on task the commander to wake. A commander whose
// project is not known leaves what the task already carries.
func applyCommander(task *state.Task, home commanderHome) {
	if home.Pane == "" || home.Root == "" {
		return
	}
	task.CommanderProject = home.Root
	task.CommanderPane = home.Pane
}

// recordCommander is announceCommander for the commands that send a settled or
// blocked soldier new work ("prompt", "decide"): the commander that does so is
// the one to hear when the soldier settles again, which may not be the one that
// dispatched it. It stores that commander on the task file, before the soldier
// is set running, and only when it differs from what the task carries. It
// registers and warns about nothing, and a failure is a warning: the soldier
// still gets its work, and its wake goes where the task already pointed.
func recordCommander(projectRoot, projectDir, vexillumHome string, task state.Task, stderr io.Writer) {
	dir := projectDir
	if top, err := gitToplevel(projectDir); err == nil {
		dir = top
	}
	home := resolveCommanderHome(vexillumHome, projectRoot, dir, &memoLocator{inner: commanderLocator})
	if home.Pane == "" || home.Root == "" || (task.CommanderProject == home.Root && task.CommanderPane == home.Pane) {
		return
	}
	_, err := state.Update(projectRoot, task.ID, func(current *state.Task) (bool, error) {
		applyCommander(current, home)
		return true, nil
	})
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": warning: could not record this commander on task %s: %v\n", task.ID, err)
	}
}
