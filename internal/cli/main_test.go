package cli

import (
	"os"
	"testing"

	"github.com/isaias-alt/vexillum/internal/doctorcheck"
)

// TestMain keeps doctor's sentinel count from depending on whichever
// sentinels happen to be running on the machine the tests run on.
func TestMain(m *testing.M) {
	doctorcheck.SetLiveSentinelPIDs(func() ([]int, error) { return nil, nil })
	os.Exit(m.Run())
}
