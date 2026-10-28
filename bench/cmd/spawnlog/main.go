// Command spawnlog stands in for a tool on PATH. It runs the real tool with
// the same arguments and streams, then appends one line to the file named by
// VXBENCH_SPAWN_LOG: the start time, the duration of the real tool, the pid
// of the process that spawned it, the tool, its exit code and its arguments.
// The benchmark harness symlinks it under the name of each tool it traces and
// reads the log back to count the subprocesses a vx command starts.
//
// VXBENCH_REAL_PATH is the PATH to look the real tool up in (the traced PATH
// minus the directory holding these symlinks).
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	os.Exit(run())
}

func run() int {
	name := filepath.Base(os.Args[0])
	real, err := lookPathIn(name, os.Getenv("VXBENCH_REAL_PATH"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "spawnlog: %v\n", err)
		return 127
	}

	cmd := exec.Command(real, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	start := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(start)

	code := 0
	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
	case errors.As(runErr, &exitErr):
		code = exitErr.ExitCode()
	default:
		fmt.Fprintf(os.Stderr, "spawnlog: running %s: %v\n", real, runErr)
		code = 126
	}

	if logPath := os.Getenv("VXBENCH_SPAWN_LOG"); logPath != "" {
		// One write per line, so concurrent tools do not interleave.
		line := fmt.Sprintf("%d\t%d\t%d\t%s\t%d\t%s\n", start.UnixNano(), elapsed.Nanoseconds(), os.Getppid(), name, code, strings.Join(os.Args[1:], " "))
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			_, _ = f.WriteString(line)
			_ = f.Close()
		}
	}
	return code
}

func lookPathIn(name, pathEnv string) (string, error) {
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s not found in VXBENCH_REAL_PATH", name)
}
