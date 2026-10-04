package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/isaias-alt/vexillum/internal/buildinfo"
	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/scaffold"
	"github.com/isaias-alt/vexillum/internal/selfupdate"
)

const (
	githubOwner = "isaias-alt"
	githubRepo  = "vexillum"
)

// binaryEnv is what the binary-update step of "vx upgrade" needs from the
// outside, so tests can run it with fakes.
type binaryEnv struct {
	// updater builds the Updater, writing its progress to out.
	updater func(out io.Writer) (*selfupdate.Updater, error)
	// exec replaces the current process with argv[0]. It only returns when it
	// failed (a test fake may return nil).
	exec func(path string, argv, envv []string) error
	// projectReady says whether the scaffold step has something to work on.
	projectReady func() bool
	stdout       io.Writer
	stderr       io.Writer
}

// osBinaryEnv wires the step to the real process: the running executable,
// GitHub Releases, brew and exec(2).
func osBinaryEnv() *binaryEnv {
	return &binaryEnv{
		updater: func(out io.Writer) (*selfupdate.Updater, error) {
			exe, err := os.Executable()
			if err != nil {
				return nil, fmt.Errorf("cannot locate the running executable: %w", err)
			}
			if exe, err = filepath.EvalSymlinks(exe); err != nil {
				return nil, fmt.Errorf("cannot resolve the running executable: %w", err)
			}
			return &selfupdate.Updater{
				Source:    selfupdate.NewGitHubSource(githubOwner, githubRepo),
				Brew:      selfupdate.ExecBrew{},
				Current:   buildinfo.Version,
				Exe:       exe,
				GoBinDirs: goBinDirs(),
				Out:       out,
			}, nil
		},
		exec: syscall.Exec,
		projectReady: func() bool {
			cwd, err := os.Getwd()
			return err == nil && scaffold.ProjectInitialized(cwd)
		},
		stdout: os.Stdout,
		stderr: os.Stderr,
	}
}

// goBinDirs are the directories "go install" writes to.
func goBinDirs() []string {
	var dirs []string
	if d := os.Getenv("GOBIN"); d != "" {
		dirs = append(dirs, d)
	}
	for _, p := range filepath.SplitList(os.Getenv("GOPATH")) {
		dirs = append(dirs, filepath.Join(p, "bin"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "go", "bin"))
	}
	return dirs
}

// upgradeBinary is the first half of "vx upgrade": bring the vx binary to the
// newest release of the channel. done is true when the run is over, with code
// as its exit code; false means go on and refresh the scaffold with the
// binary that is running.
//
// After a replaced binary the scaffold is refreshed by the new one, exec-ed
// in this process's place with --scaffold-only, so its templates are the
// ones that apply. A sentinel started from the old binary needs nothing
// here: it notices the replaced executable on its own and retires (see
// sentinel.Build.Stale).
func upgradeBinary(opts setupOptions, args []string, env *binaryEnv) (done bool, code int) {
	fail := func(format string, a ...any) (bool, int) {
		fmt.Fprintf(env.stderr, cmdname.Name+": "+format+"\n", a...)
		return true, 1
	}

	u, err := env.updater(env.stdout)
	if err != nil {
		return fail("%v", err)
	}
	ctx := context.Background()
	ch := opts.Channel
	if ch == "" {
		ch = selfupdate.Stable
	}

	if opts.Check {
		st, err := u.Check(ctx, ch)
		if err != nil {
			return fail("%v", err)
		}
		if st.Newer {
			fmt.Fprintf(env.stdout, "Update available: %s -> %s (%s channel). Run '%s' to install it.\n",
				st.Current, st.Latest.Version(), ch, upgradeCommand(ch))
		} else {
			fmt.Fprintf(env.stdout, "%s %s is up to date (latest on the %s channel: %s).\n",
				cmdname.Name, st.Current, ch, st.Latest.Version())
		}
		return true, 0
	}

	res, err := u.Upgrade(ctx, ch)
	if err != nil {
		return fail("%v", err)
	}
	switch {
	case res.BrewBehind:
		fmt.Fprintf(env.stdout, "Homebrew did not install %s: its formula has not caught up with the release yet, so %s stays at %s. Try again later.\n",
			res.Latest.Version(), cmdname.Name, res.Current)
		return false, 0
	case !res.Updated:
		fmt.Fprintf(env.stdout, "%s %s is up to date (latest on the %s channel: %s).\n",
			cmdname.Name, res.Current, ch, res.Latest.Version())
		return false, 0
	}

	fmt.Fprintf(env.stdout, "Updated %s to %s.\n", cmdname.Name, res.Latest.Version())
	if !env.projectReady() {
		fmt.Fprintf(env.stdout, "No initialized vexillum project here, so there is no scaffold to refresh. Run '%s init' in a project.\n", cmdname.Name)
		return true, 0
	}
	if res.Exe == "" {
		return fail("updated, but could not find the new binary to refresh the scaffold with - run '%s upgrade' again in the project", cmdname.Name)
	}

	fmt.Fprintf(env.stdout, "Refreshing the project's scaffold with %s %s...\n", cmdname.Name, res.Latest.Version())
	argv := append([]string{res.Exe, "upgrade", "--scaffold-only"}, args...)
	if err := env.exec(res.Exe, argv, os.Environ()); err != nil {
		return fail("updated, but could not start %s to refresh the scaffold (%v) - run '%s upgrade' again in the project", res.Exe, err, cmdname.Name)
	}
	return true, 0
}

// upgradeCommand is the command that installs the newest release of ch.
func upgradeCommand(ch selfupdate.Channel) string {
	if ch == selfupdate.Canary {
		return cmdname.Name + " upgrade --channel canary"
	}
	return cmdname.Name + " upgrade"
}
