package doctorcheck

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/isaias-alt/vexillum/internal/herdr"
)

// countingBin is a PATH holding a herdr that logs each `integration status` to
// a file and prints out for it, or fails when out is empty, plus the scripts
// given. It returns the PATH and a function that counts the calls.
func countingBin(t *testing.T, out string, scripts map[string]string) (string, func() int) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "calls")
	scripts["herdr"] = `[ "$1 $2" = "integration status" ] && echo x >> '` + log + `'
[ -n '` + out + `' ] || exit 1
printf '%s\n' '` + out + `'`
	calls := func() int {
		b, _ := os.ReadFile(log)
		return strings.Count(string(b), "x")
	}
	return binDir(t, scripts), calls
}

// The Pi and OpenCode checks read one `herdr integration status` between them,
// and each still gets its own agent's line.
func TestProbes_PiAndOpenCodeShareOneIntegrationStatus(t *testing.T) {
	bin, calls := countingBin(t, "pi: installed (v9) (/p)\nopencode: not installed (/o)", map[string]string{
		"pi":       "echo 1.3.0",
		"opencode": "echo 2.0.32",
	})
	t.Setenv("PATH", bin)
	probes := &Probes{}

	var pi, oc []Result
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); pi = PiChecks(projectWithProfiles(t, ""), t.TempDir(), false, probes) }()
	go func() { defer wg.Done(); oc = OpenCodeChecks(projectWithProfiles(t, ""), t.TempDir(), false, probes) }()
	wg.Wait()

	if got := calls(); got != 1 {
		t.Errorf("herdr integration status ran %d times, want 1", got)
	}
	if r, ok := result(pi, "herdr integration for pi"); !ok || !r.OK {
		t.Errorf("pi integration = %+v, %v", r, ok)
	}
	if r, ok := result(oc, "herdr integration for opencode"); !ok || !r.Warn || !strings.Contains(r.Detail, "not installed") {
		t.Errorf("opencode integration = %+v, %v", r, ok)
	}
}

// Without Probes every check runs its own process, as it always did.
func TestProbes_NilSharesNothing(t *testing.T) {
	bin, calls := countingBin(t, "pi: installed (v9) (/p)\nopencode: installed (v9) (/o)", map[string]string{
		"pi":       "echo 1.3.0",
		"opencode": "echo 2.0.32",
	})
	t.Setenv("PATH", bin)
	PiChecks(projectWithProfiles(t, ""), t.TempDir(), false, nil)
	OpenCodeChecks(projectWithProfiles(t, ""), t.TempDir(), false, nil)
	if got := calls(); got != 2 {
		t.Errorf("herdr integration status ran %d times, want 2", got)
	}
}

// A failed run is the answer for everyone who asks, not a reason to run again.
func TestProbes_IntegrationFailureIsSharedToo(t *testing.T) {
	bin, calls := countingBin(t, "", map[string]string{})
	t.Setenv("PATH", bin)
	probes := &Probes{}
	for _, name := range []string{"pi", "opencode"} {
		if _, _, err := probes.IntegrationStatus(name); err == nil {
			t.Errorf("%s: want the error of the failed run", name)
		}
	}
	if got := calls(); got != 1 {
		t.Errorf("herdr integration status ran %d times, want 1", got)
	}
}

// The opencodes in PATH are listed, and asked their version, once per Probes.
func TestProbes_OpenCodeInstallsAreScannedOnce(t *testing.T) {
	log := filepath.Join(t.TempDir(), "calls")
	bin := binDir(t, map[string]string{"opencode": "echo x >> '" + log + "'; echo 2.0.32"})
	probes := &Probes{}
	first := probes.openCodeInstalls(bin)
	second := probes.openCodeInstalls(bin)
	if len(first) != 1 || len(second) != 1 || first[0].Version.String() != "2.0.32" {
		t.Fatalf("installs = %+v, %+v", first, second)
	}
	if b, _ := os.ReadFile(log); strings.Count(string(b), "x") != 1 {
		t.Errorf("opencode --version ran %d times, want 1", strings.Count(string(b), "x"))
	}

	// Another PATH is another question.
	other := binDir(t, map[string]string{"opencode": "echo 1.18.29"})
	if got := probes.openCodeInstalls(other); len(got) != 1 || got[0].Version.String() != "1.18.29" {
		t.Errorf("installs for another PATH = %+v", got)
	}
}

// Several opencodes in PATH are asked at the same time, and come back in PATH
// order.
func TestScanOpenCodeInstalls_AsksEveryInstallAtOnce(t *testing.T) {
	// Each opencode waits for the other's marker, so serial reads would time out.
	dir := t.TempDir()
	wait := func(mine, other string) string {
		return "touch '" + filepath.Join(dir, mine) + "'; i=0; while [ ! -e '" + filepath.Join(dir, other) + "' ] && [ $i -lt 100 ]; do sleep 0.1; i=$((i+1)); done; " +
			"if [ -e '" + filepath.Join(dir, other) + "' ]; then echo " + mine + "; else echo 0.0.0; fi"
	}
	first := binDir(t, map[string]string{"opencode": wait("1.18.29", "2.0.32")})
	second := binDir(t, map[string]string{"opencode": wait("2.0.32", "1.18.29")})

	got := scanOpenCodeInstalls(first + string(os.PathListSeparator) + second)
	if len(got) != 2 {
		t.Fatalf("installs = %+v", got)
	}
	if got[0].Err != nil || got[1].Err != nil || got[0].Version.String() != "1.18.29" || got[1].Version.String() != "2.0.32" {
		t.Errorf("versions = %v (%v), %v (%v); want 1.18.29 then 2.0.32 (they did not overlap, or came back out of order)", got[0].Version, got[0].Err, got[1].Version, got[1].Err)
	}
}

type countingLocator struct {
	fakeLocator
	mu    sync.Mutex
	calls int
}

func (c *countingLocator) AgentGet(target string) (herdr.AgentInfo, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	return c.fakeLocator.AgentGet(target)
}

// The OpenCode commander check and the shared wake queue warning ask which
// commanders are live once between them.
func TestProbes_CommandersAreResolvedOnce(t *testing.T) {
	home := t.TempDir()
	dir, h := registerCommanders(t, home, func(dir string) []herdr.AgentInfo {
		return []herdr.AgentInfo{
			{Kind: "claude", PaneID: "w1:p1", Cwd: dir},
			{Kind: "opencode", PaneID: "w1:p2", Cwd: dir},
		}
	})
	loc := &countingLocator{fakeLocator: h}
	probes := &Probes{}

	reg, _ := openCodeRegistry(dir, home, loc, probes)
	afterRegistry := loc.calls
	warns := sharedWakeQueue(dir, home, loc, probes)

	if !reg.OK {
		t.Errorf("registry = %+v", reg)
	}
	if len(warns) != 1 {
		t.Errorf("shared wake queue = %+v, want its warning", warns)
	}
	// Two commanders, one lookup each by Resolve, plus the registry check's own
	// lookup of the OpenCode pane's session: nothing more once it was resolved.
	if loc.calls != afterRegistry {
		t.Errorf("the shared wake queue asked herdr %d more times after the registry check", loc.calls-afterRegistry)
	}
}
