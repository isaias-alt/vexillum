package cli

import (
	"os"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/doctorcheck"
	"github.com/isaias-alt/vexillum/internal/sentinel"
)

// TestMain keeps doctor's sentinel count from depending on whichever
// sentinels happen to be running on the machine the tests run on, and keeps a
// settling sentinel from waiting for a pane that fakeHerdr does not have.
func TestMain(m *testing.M) {
	doctorcheck.SetLiveSentinelPIDs(func() ([]int, error) { return nil, nil })
	sentinel.SetSettleReadGap(time.Millisecond)
	os.Exit(m.Run())
}
