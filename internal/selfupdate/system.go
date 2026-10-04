package selfupdate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func (u *Updater) platform() (goos, goarch string) {
	goos, goarch = u.OS, u.Arch
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	return goos, goarch
}

func (u *Updater) checkWritable(dir string) error {
	if u.CheckWritable != nil {
		return u.CheckWritable(dir)
	}
	return probeWritable(dir)
}

// probeWritable creates and removes a temp file in dir: the only check that
// answers "can the new binary be written here" for sure (mode bits, ACLs,
// read-only mounts all land in the same place).
func probeWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".vx-write-probe-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

// runVersion asks the vx at exe for its version: "vx <version>" on --version.
func runVersion(ctx context.Context, exe string) (string, error) {
	out, err := exec.CommandContext(ctx, exe, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("running %s --version: %w", exe, err)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return "", fmt.Errorf("unexpected output of %s --version: %q", exe, strings.TrimSpace(string(out)))
	}
	return fields[1], nil
}

// ExecBrew is the real Brew: it runs the brew command on the user's terminal.
type ExecBrew struct{}

// Upgrade implements Brew.
func (ExecBrew) Upgrade(ctx context.Context, formula string) error {
	cmd := exec.CommandContext(ctx, "brew", "upgrade", formula)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
