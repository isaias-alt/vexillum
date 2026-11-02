package herdr

import (
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

// Select is a table over every combination of VX_BACKEND (unset, hc, the TUI
// value, an unknown one) and the three terminal variables.
func TestSelect(t *testing.T) {
	const (
		hc   = BackendHC
		tui  = BackendHerdr
		env  = SourceEnv
		det  = SourceDetected
		dflt = SourceDefault
	)
	// term is HC_TERMINAL_ID, pane HERDR_PANE_ID, ws HERDR_WORKSPACE_ID.
	for _, c := range []struct {
		name         string
		vx           string
		term, pane   bool
		ws           bool
		wantName     string
		wantSource   string
		wantErrNamed string
	}{
		// VX_BACKEND unset: only a bare app terminal selects hc.
		{name: "nowhere", wantName: tui, wantSource: dflt},
		{name: "app terminal", term: true, wantName: hc, wantSource: det},
		{name: "tui pane", pane: true, ws: true, wantName: tui, wantSource: dflt},
		{name: "pane id only", pane: true, wantName: tui, wantSource: dflt},
		{name: "workspace id only", ws: true, wantName: tui, wantSource: dflt},
		// Both HC_* and HERDR_* set: not guessed, the TUI stays what it was.
		{name: "both, full pane", term: true, pane: true, ws: true, wantName: tui, wantSource: dflt},
		{name: "both, pane id", term: true, pane: true, wantName: tui, wantSource: dflt},
		{name: "both, workspace id", term: true, ws: true, wantName: tui, wantSource: dflt},

		// VX_BACKEND=hc always wins.
		{name: "hc nowhere", vx: "hc", wantName: hc, wantSource: env},
		{name: "hc app terminal", vx: "hc", term: true, wantName: hc, wantSource: env},
		{name: "hc tui pane", vx: "hc", pane: true, ws: true, wantName: hc, wantSource: env},
		{name: "hc both", vx: "hc", term: true, pane: true, ws: true, wantName: hc, wantSource: env},
		{name: "hc is case and space insensitive", vx: " HC ", wantName: hc, wantSource: env},

		// VX_BACKEND set to the TUI value stays the TUI, inside an app terminal too.
		{name: "herdr nowhere", vx: "herdr", wantName: tui, wantSource: env},
		{name: "herdr app terminal", vx: "herdr", term: true, wantName: tui, wantSource: env},
		{name: "herdr tui pane", vx: "herdr", pane: true, ws: true, wantName: tui, wantSource: env},
		{name: "herdr both", vx: "herdr", term: true, pane: true, ws: true, wantName: tui, wantSource: env},
		{name: "herdr is case insensitive", vx: "HERDR", term: true, wantName: tui, wantSource: env},

		// A blank value is unset.
		{name: "blank app terminal", vx: "  ", term: true, wantName: hc, wantSource: det},
		{name: "blank nowhere", vx: "  ", wantName: tui, wantSource: dflt},

		// Unknown values keep failing clearly, whatever the terminal.
		{name: "unknown nowhere", vx: "tmux", wantErrNamed: `"tmux"`},
		{name: "unknown app terminal", vx: "tmux", term: true, wantErrNamed: `"tmux"`},
		{name: "unknown tui pane", vx: "zellij", pane: true, ws: true, wantErrNamed: `"zellij"`},
	} {
		m := map[string]string{"VX_BACKEND": c.vx}
		if c.term {
			m["HC_TERMINAL_ID"] = "t1"
		}
		if c.pane {
			m["HERDR_PANE_ID"] = "w1:p1"
		}
		if c.ws {
			m["HERDR_WORKSPACE_ID"] = "w1"
		}
		got, err := Select(envOf(m))
		if c.wantErrNamed != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErrNamed) || !strings.Contains(err.Error(), `want "herdr" or "hc"`) {
				t.Errorf("%s: err = %v, want one naming %s and the accepted values", c.name, err, c.wantErrNamed)
			}
			if WantsHC(envOf(m)) {
				t.Errorf("%s: an unusable value must not count as hc", c.name)
			}
			if _, err := BackendName(envOf(m)); err == nil {
				t.Errorf("%s: BackendName must report the error too", c.name)
			}
			continue
		}
		if err != nil || got.Name != c.wantName || got.Source != c.wantSource {
			t.Errorf("%s: Select = %+v, %v; want %s from %s", c.name, got, err, c.wantName, c.wantSource)
		}
		if name, err := BackendName(envOf(m)); err != nil || name != c.wantName {
			t.Errorf("%s: BackendName = %q, %v; want %q", c.name, name, err, c.wantName)
		}
		if WantsHC(envOf(m)) != (c.wantName == hc) {
			t.Errorf("%s: WantsHC = %v, want %v", c.name, WantsHC(envOf(m)), c.wantName == hc)
		}
	}
}

// Default and UsingHC read the process environment through the same selection.
func TestDefaultFollowsSelect(t *testing.T) {
	t.Setenv("VX_BACKEND", "")
	t.Setenv("HERDR_PANE_ID", "")
	t.Setenv("HERDR_WORKSPACE_ID", "")
	t.Setenv("HC_TERMINAL_ID", "t1")
	if !UsingHC() || NameOf(Default()) != BackendHC {
		t.Errorf("an app terminal without VX_BACKEND must select hc, got %s", NameOf(Default()))
	}
	t.Setenv("VX_BACKEND", "herdr")
	if UsingHC() || NameOf(Default()) != BackendHerdr {
		t.Errorf("VX_BACKEND=herdr must stay on the TUI in an app terminal, got %s", NameOf(Default()))
	}
}
