package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/isaias-alt/vexillum/internal/cmdname"
)

// pushShipBranch pushes the camp branch to origin and returns the commit it
// left there. recorded is the tip of branch that a previous ship pushed
// ("" when none is known).
//
// A push rejected as non-fast-forward is retried with a lease only when that
// is provably vexillum's own doing: the tip origin holds must be exactly
// recorded, so the only thing the retry overwrites is what ship itself
// pushed before the soldier rewrote it (a rebase, an amend). The lease pins
// that same SHA, names the one branch ref and nothing else, so neither the
// base branch nor any other ref can be touched, and a push that landed on
// origin in between makes git refuse the lease. Any other remote tip, or no
// record at all, is refused with what is on the remote and how to decide.
func pushShipBranch(campPath, branch, base, recorded string, stdout io.Writer) (string, error) {
	if branch == "" || strings.HasPrefix(branch, "-") || branch == base {
		return "", fmt.Errorf("refusing to push %q: it is not a mission branch distinct from the base branch %q", branch, base)
	}
	ref := "refs/heads/" + branch

	head, err := gitOutput(campPath, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("reading the tip of %s: %w", branch, err)
	}

	out, err := gitOutput(campPath, "push", "origin", ref+":"+ref)
	if err == nil {
		return head, nil
	}
	if !nonFastForwardRejection(out) {
		return "", fmt.Errorf("pushing %s to origin: %w\n%s", branch, err, out)
	}

	tip, err := remoteBranchTip(campPath, ref)
	if err != nil {
		return "", fmt.Errorf("pushing %s to origin was rejected as non-fast-forward, and reading origin's tip to decide whether to rewrite it failed: %w", branch, err)
	}
	if tip == "" {
		return "", fmt.Errorf("pushing %s to origin: %w\n%s", branch, err, out)
	}
	if recorded == "" {
		return "", fmt.Errorf("pushing %s to origin was rejected as non-fast-forward and %s will not force it: origin's %s is at %s, but no push of this task is recorded, so it cannot be told apart from somebody else's work.\n%s",
			branch, cmdname.Name, branch, tip, decideHint(branch, tip))
	}
	if tip != recorded {
		return "", fmt.Errorf("pushing %s to origin was rejected as non-fast-forward and %s will not force it: origin's %s is at %s, not %s, the tip %s last pushed, so somebody else pushed to it since.\n%s",
			branch, cmdname.Name, branch, tip, recorded, cmdname.Name, decideHint(branch, tip))
	}

	lease := "--force-with-lease=" + ref + ":" + recorded
	if out, err := gitOutput(campPath, "push", lease, "origin", ref+":"+ref); err != nil {
		return "", fmt.Errorf("rewriting %s on origin (lease %s) failed: %w\n%s", branch, recorded, err, out)
	}
	fmt.Fprintf(stdout, "rewrote the PR branch %s on origin (force-with-lease) from %s to %s\n", branch, recorded, head)
	return head, nil
}

// decideHint tells the general how to look at what origin holds and how to
// authorize the overwrite by hand.
func decideHint(branch, tip string) string {
	return fmt.Sprintf("To decide: run 'git fetch origin %s' in the camp and inspect it (git log, git diff). If those commits can be overwritten, push once by hand with 'git push --force-with-lease=%s:%s origin %s' and ship again; otherwise integrate them first.",
		branch, branch, tip, branch)
}

// nonFastForwardRejection reports whether git's push output says the branch
// was rejected because origin's tip is not an ancestor of what was pushed.
func nonFastForwardRejection(out string) bool {
	return strings.Contains(out, "[rejected]") &&
		(strings.Contains(out, "non-fast-forward") || strings.Contains(out, "fetch first"))
}

// remoteBranchTip returns the commit origin holds for ref, or "" when origin
// has no such ref. It reads the remote's advertised refs, so it never moves
// anything in the camp.
func remoteBranchTip(campPath, ref string) (string, error) {
	out, err := gitOutput(campPath, "ls-remote", "--exit-code", "origin", ref)
	if err != nil {
		if exitCode(err) == 2 {
			return "", nil
		}
		return "", fmt.Errorf("%w\n%s", err, out)
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == ref {
			return fields[0], nil
		}
	}
	return "", nil
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

// gitOutput runs git in dir with a stable message language, returning its
// combined output trimmed.
func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
