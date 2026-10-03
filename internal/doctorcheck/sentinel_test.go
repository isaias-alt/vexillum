package doctorcheck

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/buildinfo"
	"github.com/isaias-alt/vexillum/internal/sentinel"
)

// holdSentinel makes this test process the home's sentinel and publishes b as
// its build; pass the zero value's pointer as nil to publish none.
func holdSentinel(t *testing.T, b *sentinel.Build) string {
	t.Helper()
	home := t.TempDir()
	release, err := sentinel.AcquireLock(home)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	t.Cleanup(release)
	if b != nil {
		if err := sentinel.PublishInfo(home, *b); err != nil {
			t.Fatalf("PublishInfo: %v", err)
		}
	}
	return home
}

func stubSentinelPIDs(t *testing.T, pids []int, err error) {
	t.Helper()
	t.Cleanup(SetLiveSentinelPIDs(func() ([]int, error) { return pids, err }))
}

func currentBuild(t *testing.T) sentinel.Build {
	t.Helper()
	b, err := sentinel.CurrentBuild(buildinfo.Version)
	if err != nil {
		t.Fatalf("CurrentBuild: %v", err)
	}
	return b
}

func TestSentinelRunning_NotRunningIsFine(t *testing.T) {
	stubSentinelPIDs(t, nil, nil)
	r := SentinelRunning(t.TempDir())
	if !r.OK || r.Warn || !strings.Contains(r.Detail, "not running") {
		t.Errorf("got %+v, want ok and not running", r)
	}
}

func TestSentinelRunning_CurrentSentinelIsFine(t *testing.T) {
	b := currentBuild(t)
	home := holdSentinel(t, &b)
	stubSentinelPIDs(t, []int{os.Getpid()}, nil)

	r := SentinelRunning(home)
	if !r.OK || r.Warn || !strings.HasPrefix(r.Detail, "running") {
		t.Errorf("got %+v, want ok and running", r)
	}
}

func TestSentinelRunning_WarnsWhenMoreThanOneIsAlive(t *testing.T) {
	b := currentBuild(t)
	home := holdSentinel(t, &b)
	stubSentinelPIDs(t, []int{4242, 4343}, nil)

	r := SentinelRunning(home)
	if !r.Warn || r.OK {
		t.Fatalf("got %+v, want a warning", r)
	}
	for _, want := range []string{"2 sentinel processes", "4242, 4343", "kill"} {
		if !strings.Contains(r.Detail, want) {
			t.Errorf("detail %q lacks %q", r.Detail, want)
		}
	}
}

func TestSentinelRunning_WarnsAboutDuplicatesEvenWhenNoneHoldsTheLock(t *testing.T) {
	stubSentinelPIDs(t, []int{11, 12}, nil)
	r := SentinelRunning(t.TempDir())
	if !r.Warn || !strings.Contains(r.Detail, "2 sentinel processes") {
		t.Errorf("got %+v, want the duplicate warning", r)
	}
}

func TestSentinelRunning_OneProcessOrAnUnreadableListingIsFine(t *testing.T) {
	b := currentBuild(t)
	home := holdSentinel(t, &b)

	stubSentinelPIDs(t, []int{os.Getpid()}, nil)
	if r := SentinelRunning(home); r.Warn {
		t.Errorf("one sentinel warned: %+v", r)
	}
	stubSentinelPIDs(t, nil, errors.New("ps is not here"))
	if r := SentinelRunning(home); r.Warn {
		t.Errorf("a failed listing warned: %+v", r)
	}
}

func TestSentinelRunning_WarnsWhenTheVersionDiffers(t *testing.T) {
	b := currentBuild(t)
	b.Version = "v0.0.1-old"
	home := holdSentinel(t, &b)
	stubSentinelPIDs(t, []int{os.Getpid()}, nil)

	r := SentinelRunning(home)
	if !r.Warn {
		t.Fatalf("got %+v, want a warning", r)
	}
	for _, want := range []string{"v0.0.1-old", buildinfo.Version, "kill"} {
		if !strings.Contains(r.Detail, want) {
			t.Errorf("detail %q lacks %q", r.Detail, want)
		}
	}
}

// Same version string, different file: every plain "go build" says "dev", so
// the version alone cannot tell a rebuilt binary from the one still running.
func TestSentinelRunning_WarnsWhenTheBinaryWasReplacedUnderTheSameVersion(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "vx")
	if err := os.WriteFile(exe, []byte("new build"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	b := sentinel.Build{Version: buildinfo.Version, Executable: exe, Resolved: resolved, Size: 1, ModTimeUnixNano: 1}
	home := holdSentinel(t, &b)
	stubSentinelPIDs(t, []int{os.Getpid()}, nil)

	r := SentinelRunning(home)
	if !r.Warn || !strings.Contains(r.Detail, "changed") || !strings.Contains(r.Detail, "replaced on disk") {
		t.Errorf("got %+v, want a replaced-binary warning", r)
	}
}

func TestSentinelRunning_WarnsAboutASentinelThatNeverRecordedItsBuild(t *testing.T) {
	home := holdSentinel(t, nil)
	stubSentinelPIDs(t, []int{os.Getpid()}, nil)

	r := SentinelRunning(home)
	if !r.Warn || !strings.Contains(r.Detail, "predates build tracking") {
		t.Errorf("got %+v, want the old-sentinel warning", r)
	}
}
