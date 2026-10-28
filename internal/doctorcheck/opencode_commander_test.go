package doctorcheck

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/commanderreg"
	"github.com/isaias-alt/vexillum/internal/herdr"
	"github.com/isaias-alt/vexillum/internal/install"
	"github.com/isaias-alt/vexillum/internal/project"
	"github.com/isaias-alt/vexillum/internal/slot"
)

// fakeLocator knows a fixed set of agents.
type fakeLocator struct {
	agents []herdr.AgentInfo
	err    error
}

func (f fakeLocator) AgentGet(target string) (herdr.AgentInfo, error) {
	if f.err != nil {
		return herdr.AgentInfo{}, f.err
	}
	for _, a := range f.agents {
		if a.PaneID == target {
			return a, nil
		}
	}
	return herdr.AgentInfo{}, &herdr.APIError{Code: "agent_not_found", Message: target}
}

func (f fakeLocator) AgentList() ([]herdr.AgentInfo, error) { return f.agents, f.err }

// ocService is an opencode that prints version, answers "service status" with
// status ("http://..." or "stopped") and GET /api/info with serviceVersion.
func ocService(t *testing.T, version, status, serviceVersion string) string {
	t.Helper()
	return binDir(t, map[string]string{"opencode": `case "$1 $2" in
  "service status") echo '` + status + `';;
  "api GET") echo '{"version":"` + serviceVersion + `","pid":1,"urls":["http://127.0.0.1:49778"]}';;
  *) echo '` + version + `';;
esac`})
}

// registered is a project directory whose OpenCode commander is registered in
// pane w1:p3, and the locator that knows that pane (with session, "" for none).
func registered(t *testing.T, home, session string) (dir string, h fakeLocator) {
	t.Helper()
	dir = t.TempDir()
	h = fakeLocator{agents: []herdr.AgentInfo{{Kind: "opencode", PaneID: "w1:p3", Cwd: dir, Session: session}}}
	root, err := project.Root(home, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := commanderreg.Register(root, dir, "w1:p3", h, time.Now()); err != nil {
		t.Fatal(err)
	}
	return dir, h
}

func withBlock(t *testing.T, dir string, files ...string) {
	t.Helper()
	tmpl, err := install.SlotTemplate(slot.LangEN)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("# rules\n\n"+slot.Render(tmpl)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOpenCodeCommanderChecks_NothingWithoutTheBinary(t *testing.T) {
	empty := t.TempDir()
	if got := openCodeCommanderChecks(t.TempDir(), t.TempDir(), empty, fakeLocator{}, nil); len(got) != 0 {
		t.Errorf("got %s", names(got))
	}
}

func TestOpenCodeCommanderChecks_ALiveCommanderOnAnAnsweringServiceIsAllOK(t *testing.T) {
	home := t.TempDir()
	dir, h := registered(t, home, "ses_abc")
	withBlock(t, dir, "AGENTS.md")
	bin := ocService(t, "opencode v2.0.32", "http://127.0.0.1:49778", "2.0.32")

	got := openCodeCommanderChecks(dir, home, bin, h, nil)
	for _, name := range []string{"OpenCode commander registered", "OpenCode wake API", "OpenCode reads the vexillum block"} {
		r, ok := result(got, name)
		if !ok || !r.OK || r.Warn || r.Required {
			t.Errorf("%s = %+v (found %v), want ok", name, r, ok)
		}
	}
	reg, _ := result(got, "OpenCode commander registered")
	if !strings.Contains(reg.Detail, "pane w1:p3") || !strings.Contains(reg.Detail, "session ses_abc") {
		t.Errorf("registered detail = %q", reg.Detail)
	}
	api, _ := result(got, "OpenCode wake API")
	for _, want := range []string{"2.0.32", "GET /api/info", "POST /api/session/<id>/prompt", "verified live on 2.0.32 only"} {
		if !strings.Contains(api.Detail, want) {
			t.Errorf("wake API detail %q lacks %q", api.Detail, want)
		}
	}
}

func TestOpenCodeCommanderChecks_Registration(t *testing.T) {
	bin := ocService(t, "opencode v2.0.32", "http://127.0.0.1:49778", "2.0.32")

	t.Run("not yet", func(t *testing.T) {
		got := openCodeCommanderChecks(t.TempDir(), t.TempDir(), bin, fakeLocator{}, nil)
		r, _ := result(got, "OpenCode commander registered")
		if r.OK || r.Warn || !strings.Contains(r.Detail, "vx dispatch") {
			t.Errorf("result = %+v, want an optional missing that names the commands that register", r)
		}
	})

	t.Run("no session yet", func(t *testing.T) {
		home := t.TempDir()
		dir, h := registered(t, home, "")
		r, _ := result(openCodeCommanderChecks(dir, home, bin, h, nil), "OpenCode commander registered")
		if !r.OK || !strings.Contains(r.Detail, "no session yet") {
			t.Errorf("result = %+v", r)
		}
	})

	t.Run("pane gone", func(t *testing.T) {
		home := t.TempDir()
		dir, _ := registered(t, home, "ses_abc")
		r, _ := result(openCodeCommanderChecks(dir, home, bin, fakeLocator{}, nil), "OpenCode commander registered")
		if r.OK || !r.Warn || !strings.Contains(r.Detail, "no longer runs an opencode") {
			t.Errorf("result = %+v", r)
		}
	})

	t.Run("herdr failing", func(t *testing.T) {
		home := t.TempDir()
		dir, _ := registered(t, home, "ses_abc")
		r, _ := result(openCodeCommanderChecks(dir, home, bin, fakeLocator{err: errors.New("herdr is down")}, nil), "OpenCode commander registered")
		if !r.Warn || !strings.Contains(r.Detail, "herdr is down") {
			t.Errorf("result = %+v", r)
		}
	})
}

func TestOpenCodeCommanderChecks_WakeAPI(t *testing.T) {
	cases := map[string]struct {
		version, status, svc string
		live                 bool
		wantOK               bool
		want                 string
	}{
		"v1 has no api":        {"1.19.3", "stopped", "", true, false, "V1: it has no 'opencode api'"},
		"v1 names the caveat":  {"1.19.3", "stopped", "", true, false, "glue the wake onto text you typed"},
		"stopped, tui open":    {"opencode v2.0.32", "stopped", "", true, false, "yet a commander is open"},
		"stopped, nobody home": {"opencode v2.0.32", "stopped", "", false, true, "does not start it"},
		"stale service":        {"opencode v2.0.32", "http://127.0.0.1:1", "2.0.30", true, false, "opencode service restart"},
		"unknown major":        {"opencode v3.0.1", "stopped", "", true, false, "not a version the wake API was verified against"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			dir := t.TempDir()
			var h herdr.Locator = fakeLocator{}
			if tc.live {
				var live fakeLocator
				dir, live = registered(t, home, "ses_abc")
				h = live
			}
			bin := ocService(t, tc.version, tc.status, tc.svc)
			r, ok := result(openCodeCommanderChecks(dir, home, bin, h, nil), "OpenCode wake API")
			if !ok || r.OK != tc.wantOK || r.Warn == tc.wantOK || r.Required || !strings.Contains(r.Detail, tc.want) {
				t.Errorf("wake API = %+v (found %v), want ok=%v mentioning %q", r, ok, tc.wantOK, tc.want)
			}
		})
	}
}

// Doctor never starts the service: opencode api would, so it is never run
// against a stopped one.
func TestOpenCodeCommanderChecks_NeverRunsAPIAgainstAStoppedService(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls")
	bin := binDir(t, map[string]string{"opencode": `printf '%s\n' "$*" >> '` + log + `'
case "$1 $2" in
  "service status") echo stopped;;
  *) echo 'opencode v2.0.32';;
esac`})
	openCodeCommanderChecks(t.TempDir(), t.TempDir(), bin, fakeLocator{}, nil)
	data, _ := os.ReadFile(log)
	if strings.Contains(string(data), "api ") {
		t.Errorf("opencode api ran against a stopped service:\n%s", data)
	}
}

func TestOpenCodeBlockVisible(t *testing.T) {
	t.Run("in AGENTS.md", func(t *testing.T) {
		dir := t.TempDir()
		withBlock(t, dir, "AGENTS.md")
		r, ok := OpenCodeBlockVisible(dir)
		if !ok || !r.OK || !strings.Contains(r.Detail, "AGENTS.md holds it") {
			t.Errorf("%+v %v", r, ok)
		}
	})
	t.Run("in AGENTS.md and CLAUDE.md imports it", func(t *testing.T) {
		dir := t.TempDir()
		withBlock(t, dir, "AGENTS.md")
		if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("@AGENTS.md\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if r, _ := OpenCodeBlockVisible(dir); !r.OK {
			t.Errorf("%+v", r)
		}
	})
	t.Run("in CLAUDE.md alone", func(t *testing.T) {
		dir := t.TempDir()
		withBlock(t, dir, "CLAUDE.md")
		r, ok := OpenCodeBlockVisible(dir)
		if !ok || !r.OK || !strings.Contains(r.Detail, "falls back") {
			t.Errorf("%+v %v", r, ok)
		}
	})
	t.Run("shadowed: block in CLAUDE.md, AGENTS.md without one", func(t *testing.T) {
		dir := t.TempDir()
		withBlock(t, dir, "CLAUDE.md")
		if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# my rules\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		r, ok := OpenCodeBlockVisible(dir)
		if !ok || r.OK || !r.Warn || r.Required {
			t.Fatalf("%+v %v, want a warning", r, ok)
		}
		for _, want := range []string{"ignores CLAUDE.md", "delete the block from CLAUDE.md", "vx init"} {
			if !strings.Contains(r.Detail, want) {
				t.Errorf("detail %q lacks %q", r.Detail, want)
			}
		}
	})
	t.Run("no block anywhere says nothing", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# my rules\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if r, ok := OpenCodeBlockVisible(dir); ok {
			t.Errorf("a line was reported for a project with no block: %+v", r)
		}
	})
}

func TestOpenCodeCommanderChecks_SkillsSwitchedOff(t *testing.T) {
	bin := ocService(t, "opencode v2.0.32", "stopped", "")
	run := func() (Result, bool) {
		return result(openCodeCommanderChecks(t.TempDir(), t.TempDir(), bin, fakeLocator{}, nil), "OpenCode reads .claude/skills")
	}
	if _, ok := run(); ok {
		t.Error("a line without the switch set")
	}
	t.Setenv("OPENCODE_DISABLE_CLAUDE_CODE_SKILLS", "1")
	if r, ok := run(); !ok || !r.Warn || !strings.Contains(r.Detail, "OPENCODE_DISABLE_CLAUDE_CODE_SKILLS") {
		t.Errorf("%+v %v", r, ok)
	}
	t.Setenv("OPENCODE_DISABLE_CLAUDE_CODE_SKILLS", "0")
	if _, ok := run(); ok {
		t.Error("a line with the switch set to 0")
	}
}
