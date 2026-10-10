package cli

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/doctorcheck"
	"github.com/isaias-alt/vexillum/internal/sentinel"
)

// TestMain keeps doctor's sentinel count from depending on whichever
// sentinels happen to be running on the machine the tests run on, and keeps a
// settling sentinel from waiting for a pane that fakeHerdr does not have. It
// also gives the whole run a throwaway HOME, so a test that forgets to pass its
// own home or project directory can never reach the real ~/.vexillum or a
// project's .vexillum/forum/ (see TestHomeIsIsolated).
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "vx-cli-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "creating the test home:", err)
		os.Exit(1)
	}
	os.Setenv("HOME", home)
	doctorcheck.SetLiveSentinelPIDs(func() ([]int, error) { return nil, nil })
	sentinel.SetSettleReadGap(time.Millisecond)
	code := m.Run()
	_ = os.RemoveAll(home)
	os.Exit(code)
}

// The suite runs with a throwaway HOME (TestMain), so nothing it does with
// "the default vexillum home" lands in the maintainer's real one.
func TestHomeIsIsolated(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(home, os.TempDir()) && !strings.Contains(home, "vx-cli-test-home-") {
		t.Errorf("tests must not run against a real home, got %s", home)
	}
}
