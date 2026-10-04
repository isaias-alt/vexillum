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

// SetExecutablePath swaps what CurrentBuild treats as the running executable
// for the duration of a test and returns a function that restores it.
func SetExecutablePath(f func() (string, error)) (restore func()) {
	old := executablePath
	executablePath = f
	return func() { executablePath = old }
}

// SetListProcesses swaps the process lister LivePIDs reads for the duration of
// a test and returns a function that restores it.
func SetListProcesses(f func() (string, error)) (restore func()) {
	old := listProcesses
	listProcesses = f
	return func() { listProcesses = old }
}

// SetTryAcquireLock swaps the lock attempt AcquireLockRetiring makes for the
// duration of a test and returns a function that restores it.
func SetTryAcquireLock(f func(string) (func(), error)) (restore func()) {
	old := tryAcquireLock
	tryAcquireLock = f
	return func() { tryAcquireLock = old }
}
