package doctorcheck

import (
	"fmt"
	"os"
	"strings"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/commanderreg"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/install"
	"github.com/isaias-alt/vexillum/internal/opencodeapi"
	"github.com/isaias-alt/vexillum/internal/project"
)

// OpenCodeCommanderChecks reports on what an OpenCode commander needs beyond
// the binary (see OpenCodeChecks): the sentinel wakes it from outside, through
// the OpenCode V2 HTTP API, so it must be registered with a live pane
// (commanders.json), the API must answer in this environment, and the
// vexillum block must be in a file OpenCode reads. It is for a project whose
// commander harnesses include opencode, and reports nothing when opencode is
// not on PATH (the binary line says so). Every check is read-only: it never
// starts the OpenCode service and never sends a prompt.
//
// The API check asks "opencode service status" and GET /api/info with this
// process's HOME and XDG directories, the way the sentinel's own calls find
// the service, so it speaks for the sentinel only when both run with the same
// ones.
func OpenCodeCommanderChecks(projectDir, vexillumHome string) []Result {
	return openCodeCommanderChecks(projectDir, vexillumHome, os.Getenv("PATH"), herdr.CLI{})
}

func openCodeCommanderChecks(projectDir, vexillumHome, pathEnv string, h herdr.Locator) []Result {
	installs := openCodeInstalls(pathEnv)
	if len(installs) == 0 {
		return nil
	}
	binary := installs[0]

	reg, live := openCodeRegistry(projectDir, vexillumHome, h)
	out := []Result{reg}
	out = append(out, openCodeWakeAPI(binary, live))
	if block, ok := OpenCodeBlockVisible(projectDir); ok {
		out = append(out, block)
	}
	if skills, ok := openCodeSkillsHidden(); ok {
		out = append(out, skills)
	}
	return out
}

// openCodeRegistry reports whether the project has a live OpenCode commander
// in its registry, and whether it was found (so the API check knows a TUI is
// supposed to be running).
func openCodeRegistry(projectDir, vexillumHome string, h herdr.Locator) (Result, bool) {
	const name = "OpenCode commander registered"
	root, err := project.Root(vexillumHome, projectDir)
	if err != nil {
		return Result{Name: name, Warn: true, Detail: err.Error()}, false
	}
	entries, err := commanderreg.Load(root)
	if err != nil {
		return Result{Name: name, Warn: true, Detail: err.Error()}, false
	}
	if _, ok := entries["opencode"]; !ok {
		return Result{Name: name, Detail: "no OpenCode commander has registered in this project yet; it does so the first time it runs '" + cmdname.Name +
			" dispatch' or '" + cmdname.Name + " forum open' here, and until then the sentinel cannot wake it"}, false
	}

	live, err := commanderreg.Resolve(root, vexillumHome, h)
	for _, c := range live {
		if c.Harness != "opencode" {
			continue
		}
		detail := "pane " + c.PaneID
		if c.Recomputed {
			detail += " (found again by directory, herdr gave the registered pane's id to something else)"
		}
		info, getErr := h.AgentGet(c.PaneID)
		switch {
		case getErr != nil:
		case info.Session == "":
			detail += ", no session yet: the sentinel holds a wake until a message is sent in it"
		default:
			detail += ", session " + info.Session
		}
		return Result{Name: name, OK: true, Detail: detail}, true
	}
	detail := "registered, but its pane no longer runs an opencode in this project; run '" + cmdname.Name + " dispatch' or '" + cmdname.Name + " forum open' from the open OpenCode commander to register it again"
	if err != nil {
		detail += " (herdr: " + err.Error() + ")"
	}
	return Result{Name: name, Warn: true, Detail: detail}, false
}

// openCodeWakeAPI reports whether the sentinel's wake route, the V2 HTTP API,
// can work: V2 only, the background service answering. liveCommander says an
// OpenCode TUI is supposed to be open, which is when a stopped service is a
// problem.
func openCodeWakeAPI(binary openCodeInstall, liveCommander bool) Result {
	const name = "OpenCode wake API"
	if binary.Err != nil {
		return Result{Name: name, Warn: true, Detail: "skipped, the version of " + binary.Path + " is unknown"}
	}
	if binary.Version.Major == 1 {
		return Result{Name: name, Warn: true, Detail: fmt.Sprintf("OpenCode %s is a V1: it has no 'opencode api', so the sentinel cannot put a wake in the commander's session and falls back to "+
			"'herdr agent prompt' when the commander is idle, which types into the editor and can glue the wake onto text you typed but did not send; use OpenCode 2.x for the commander", binary.Version)}
	}
	if binary.Version.Major != 2 {
		return Result{Name: name, Warn: true, Detail: fmt.Sprintf("not checked, OpenCode %s is not a version the wake API was verified against", binary.Version)}
	}

	api := newOpenCodeAPI(binary.Path)
	running, err := api.ServiceRunning()
	if err != nil {
		return Result{Name: name, Warn: true, Detail: "could not ask 'opencode service status': " + err.Error()}
	}
	if !running {
		detail := "the OpenCode background service is not running in this environment ('opencode service status' does not start it)"
		if liveCommander {
			return Result{Name: name, Warn: true, Detail: detail + ", yet a commander is open: the sentinel would not reach its session if it runs with another HOME or XDG_* than this shell"}
		}
		return Result{Name: name, OK: true, Detail: detail + "; it starts with the first OpenCode TUI"}
	}
	info, err := api.Info()
	if err != nil {
		return Result{Name: name, Warn: true, Detail: "GET /api/info failed: " + err.Error()}
	}
	if info.Version != "" && info.Version != binary.Version.String() {
		return Result{Name: name, Warn: true, Detail: fmt.Sprintf("the service runs OpenCode %s but the binary in PATH is %s; run 'opencode service restart' (open TUIs reconnect) so the API the sentinel calls is the one this doctor read", info.Version, binary.Version)}
	}
	return Result{Name: name, OK: true, Detail: fmt.Sprintf("the service (OpenCode %s) answers GET /api/info; the wake itself is POST /api/session/<id>/prompt, which doctor does not call because it would add a message to the session (verified live on 2.0.32 only)", info.Version)}
}

// newOpenCodeAPI is the OpenCode API client for the opencode at binary. A
// variable so tests can answer without a service.
var newOpenCodeAPI = func(binary string) opencodeapi.Client { return opencodeapi.CLI{Binary: binary} }

// OpenCodeBlockVisible reports whether the vexillum block is in a file
// OpenCode reads. OpenCode reads the first of AGENTS.md and CLAUDE.md that
// exists and ignores the other, so a block in CLAUDE.md is invisible to it
// when an AGENTS.md exists. ok is false when the project has no block at all,
// which the block's own check reports.
func OpenCodeBlockVisible(projectDir string) (res Result, ok bool) {
	const name = "OpenCode reads the vexillum block"
	shadowed, file, err := install.BlockShadowedByAgents(projectDir)
	switch {
	case err != nil:
		return Result{Name: name, Warn: true, Detail: err.Error()}, true
	case file == "":
		return Result{}, false
	case shadowed:
		return Result{Name: name, Warn: true, Detail: "the block is in " + install.ClaudeFile + ", but " + install.AgentsFile +
			" exists and OpenCode reads that one and ignores " + install.ClaudeFile + "; delete the block from " + install.ClaudeFile + " and run '" + cmdname.Name + " init' to put it in " + install.AgentsFile}, true
	case file == install.ClaudeFile:
		return Result{Name: name, OK: true, Detail: install.ClaudeFile + " holds it and the project has no " + install.AgentsFile + ", so OpenCode falls back to it (an " + install.AgentsFile + " added later would hide it)"}, true
	}
	return Result{Name: name, OK: true, Detail: install.AgentsFile + " holds it"}, true
}

// openCodeSkillsHidden warns when the environment switches off OpenCode's
// reading of .claude/skills, where vexillum installs its skills. ok is false
// when there is nothing to say. (Per OpenCode's rules documentation; the
// switches were not exercised.)
func openCodeSkillsHidden() (Result, bool) {
	for _, env := range []string{"OPENCODE_DISABLE_CLAUDE_CODE", "OPENCODE_DISABLE_CLAUDE_CODE_SKILLS"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" && v != "0" && v != "false" {
			return Result{Name: "OpenCode reads .claude/skills", Warn: true, Detail: env + " is set, so OpenCode will not see the vexillum skills in .claude/skills (per OpenCode's documentation); unset it"}, true
		}
	}
	return Result{}, false
}
