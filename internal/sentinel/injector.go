package sentinel

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/isaias-alt/vexillum/internal/commanderreg"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/opencodeapi"
	"github.com/isaias-alt/vexillum/internal/project"
)

// The Claude Code commander is woken from inside: its Stop hook runs
// "vx sentinel await", which exits 2 with the wake text. An OpenCode V2
// commander has no such hook, and OpenCode's server plugins run in a shared
// background service with no pane identity, so it is woken from outside: after
// the sentinel has recorded a wake for a project whose commander is a live
// OpenCode TUI, the OpenCodeInjector hands the same text (WakeReason) to the
// commander's session through the service's own HTTP API.
//
// Why not "herdr agent prompt", which would need no API: it types into the
// TUI's editor, so a wake arriving while the person has an unsent draft is
// glued onto the draft ("HUMANReply with only the word ping." was observed on
// OpenCode 2.0.32). The API delivers a user turn without touching the editor.
// "herdr agent prompt" stays as the fallback for when the API cannot be used.

// DefaultFallbackAfter is how long the API route must have been failing for a
// project before the injector types the wake into the commander's pane
// instead, when the commander is idle.
const DefaultFallbackAfter = 30 * time.Second

// herdrFallbackTimeoutMS bounds the fallback's "herdr agent prompt --wait": it
// returns once the pane started working (the text was taken) or after this
// long, which is not a failure, since the commander is then busy with it.
const herdrFallbackTimeoutMS = 10000

// OpenCodeAPI is what the injector needs from a running OpenCode V2: opencodeapi.CLI
// is the real one. Prompt must fail rather than start a service when none runs
// (opencodeapi.ErrServiceStopped), since a service the sentinel started would
// not be the commander's.
type OpenCodeAPI interface {
	Prompt(sessionID, text string) error
}

// InjectorHerdr is the herdr the injector asks about the commander's pane and
// falls back to prompting; herdr.CLI satisfies it.
type InjectorHerdr interface {
	herdr.Locator
	AgentPrompt(name, text string, timeoutMS int) (status string, err error)
}

// OpenCodeInjector delivers wakes to OpenCode commanders. One value serves the
// whole sentinel and is used from its single polling goroutine.
type OpenCodeInjector struct {
	VexillumHome string
	Herdr        InjectorHerdr
	API          OpenCodeAPI
	Log          io.Writer
	// FallbackAfter is DefaultFallbackAfter when zero.
	FallbackAfter time.Duration
	// Now is time.Now when nil.
	Now func() time.Time

	// failing remembers, per project root, since when delivery has been
	// failing and the last problem logged, so a persistent one is logged once.
	failing map[string]failure
}

type failure struct {
	since  time.Time
	logged string
}

// NewOpenCodeInjector returns an injector writing its problems to log.
func NewOpenCodeInjector(vexillumHome string, h InjectorHerdr, api OpenCodeAPI, log io.Writer) *OpenCodeInjector {
	return &OpenCodeInjector{VexillumHome: vexillumHome, Herdr: h, API: api, Log: log}
}

func (i *OpenCodeInjector) now() time.Time {
	if i.Now != nil {
		return i.Now()
	}
	return time.Now()
}

func (i *OpenCodeInjector) fallbackAfter() time.Duration {
	if i.FallbackAfter > 0 {
		return i.FallbackAfter
	}
	return DefaultFallbackAfter
}

// Sweep delivers the pending wakes of every project that has a live OpenCode
// commander and no live "vx sentinel await", and returns how many projects it
// woke. A project with nothing pending costs two directory reads and no herdr
// call.
func (i *OpenCodeInjector) Sweep() int {
	roots, err := project.AllRoots(i.VexillumHome)
	if err != nil {
		return 0
	}
	woke := 0
	for _, root := range roots {
		woke += i.sweepProject(root)
	}
	return woke
}

const harnessOpenCode = "opencode"

func (i *OpenCodeInjector) sweepProject(root string) int {
	if !hasPendingWakes(root) {
		delete(i.failing, root)
		return 0
	}
	entries, err := commanderreg.Load(root)
	if err != nil {
		i.problem(root, "cannot read the commander registry", err)
		return 0
	}
	if _, ok := entries[harnessOpenCode]; !ok {
		return 0
	}
	// An await is alive: some commander of the project has a Stop hook that
	// delivers the wake itself, and two deliverers would race for it.
	if HasLiveAwaiter(i.VexillumHome, root) {
		return 0
	}

	pane, found, err := i.commanderPane(root)
	if err != nil {
		i.problem(root, "cannot find the commander", err)
	}
	if !found {
		// No live OpenCode commander: the wake waits for whoever listens next.
		delete(i.failing, root)
		return 0
	}
	info, err := i.Herdr.AgentGet(pane)
	if err != nil {
		i.problem(root, "cannot read the commander's pane "+pane, err)
		return 0
	}

	viaHerdr := i.fallbackDue(root) && (info.Status == "idle" || info.Status == "done")
	if !viaHerdr && info.Session == "" {
		i.problem(root, "the commander's pane "+pane+" has no OpenCode session yet (nothing was sent in it), so the wake waits", nil)
		return 0
	}

	wakes, forumWakes, err := DrainAll(root)
	if len(wakes)+len(forumWakes) == 0 {
		if err != nil {
			i.problem(root, "cannot read the wakes", err)
		}
		// Everything pending was stale and is gone now.
		delete(i.failing, root)
		return 0
	}

	text := WakeReason(wakes, forumWakes)
	route := "the OpenCode API"
	if viaHerdr {
		route = "herdr agent prompt"
		err = i.promptViaHerdr(pane, text)
	} else {
		err = i.API.Prompt(info.Session, text)
	}
	if err != nil {
		if rerr := Requeue(root, wakes, forumWakes); rerr != nil {
			fmt.Fprintf(i.Log, "sentinel: warning: LOST a wake for the opencode commander of %s: %v\n", filepath.Base(root), rerr)
		}
		i.problem(root, "cannot deliver the wake through "+route+", it stays queued", err)
		return 0
	}
	delete(i.failing, root)
	fmt.Fprintf(i.Log, "sentinel: woke the opencode commander of %s (pane %s) through %s\n", filepath.Base(root), pane, route)
	return 1
}

// commanderPane is the pane of the project's live OpenCode commander.
func (i *OpenCodeInjector) commanderPane(root string) (pane string, found bool, err error) {
	live, err := commanderreg.Resolve(root, i.VexillumHome, i.Herdr)
	for _, c := range live {
		if c.Harness == harnessOpenCode {
			return c.PaneID, true, err
		}
	}
	return "", false, err
}

func (i *OpenCodeInjector) promptViaHerdr(pane, text string) error {
	_, err := i.Herdr.AgentPrompt(pane, text, herdrFallbackTimeoutMS)
	if err == nil || herdr.IsTimeout(err) {
		// Accepted: the commander is working on it.
		return nil
	}
	return err
}

// fallbackDue reports whether delivery has been failing for root long enough
// to type the wake into the pane instead.
func (i *OpenCodeInjector) fallbackDue(root string) bool {
	f, ok := i.failing[root]
	return ok && !f.since.IsZero() && i.now().Sub(f.since) >= i.fallbackAfter()
}

// problem records that delivery for root is failing and logs it once per
// distinct problem. err may be nil.
func (i *OpenCodeInjector) problem(root, what string, err error) {
	if i.failing == nil {
		i.failing = map[string]failure{}
	}
	f := i.failing[root]
	if f.since.IsZero() {
		f.since = i.now()
	}
	line := "sentinel: warning: opencode commander of " + filepath.Base(root) + ": " + what
	if err != nil {
		line += ": " + describe(err)
	}
	if f.logged != line {
		fmt.Fprintln(i.Log, line)
		f.logged = line
	}
	i.failing[root] = f
}

// describe says what an error from the API route most likely means.
func describe(err error) string {
	switch {
	case errors.Is(err, opencodeapi.ErrServiceStopped):
		return err.Error() + " (does the sentinel run with the same HOME and XDG directories as the commander's OpenCode?)"
	case opencodeapi.IsSessionNotFound(err):
		return err.Error() + " (the session belongs to another OpenCode service: the sentinel and the commander may not share HOME and XDG directories)"
	}
	return err.Error()
}

// hasPendingWakes reports whether the project has a wake file waiting, of
// either kind.
func hasPendingWakes(projectRoot string) bool {
	for _, dir := range []string{wakesDir(projectRoot), forumWakesDir(projectRoot)} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".json" {
				return true
			}
		}
	}
	return false
}
