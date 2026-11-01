package doctorcheck

import (
	"os"
	"testing"
)

// TestMain keeps the checks from seeing the terminal the suite happens to run
// in: a herdr pane or an hc terminal (with VX_BACKEND=hc) changes which backend
// and integration the checks probe. Tests that need these set them with
// t.Setenv.
func TestMain(m *testing.M) {
	for _, name := range []string{
		"HERDR_PANE_ID", "HERDR_WORKSPACE_ID", "HERDR_ENV", "HERDR_SOCKET_PATH",
		"HC_TERMINAL_ID", "HC_SESSION", "HC_SOCKET", "HC_SCOPE", "HC_TERMINAL_SOCKET",
		"VX_BACKEND",
	} {
		os.Unsetenv(name)
	}
	os.Exit(m.Run())
}
