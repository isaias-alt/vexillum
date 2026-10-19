package soldier

import "time"

// SetNotReadyPollWindow shortens how long startAgent polls an agent that
// failed to start with agent_not_ready, and returns a func that restores it.
func SetNotReadyPollWindow(d time.Duration) (restore func()) {
	old := notReadyPollWindow
	notReadyPollWindow = d
	return func() { notReadyPollWindow = old }
}

// SetReservedHarnesses replaces the reserved harness names, and returns a
// func that restores them.
func SetReservedHarnesses(names []string) (restore func()) {
	old := reservedHarnesses
	reservedHarnesses = names
	return func() { reservedHarnesses = old }
}
