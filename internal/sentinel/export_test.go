package sentinel

// SetInspectProcess swaps the process inspector for the duration of a test
// and returns a function that restores it.
func SetInspectProcess(f func(pid int) (ppid int, command string, err error)) (restore func()) {
	old := inspectProcess
	inspectProcess = f
	return func() { inspectProcess = old }
}

// ErrProcessGone is what a fake inspector returns for a missing pid.
var ErrProcessGone = errProcessGone

var (
	InspectProcess = func(pid int) (int, string, error) { return inspectProcess(pid) }
	IsAwaitCommand = isAwaitCommand
	ParsePSLine    = parsePSLine
)
