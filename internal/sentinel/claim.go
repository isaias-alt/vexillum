package sentinel

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// A wake is only worth anything if the commander hears it, and the process that
// takes it off the queue (the Stop hook's await, the OpenCode injector) can
// fail to hand it on: its hook may be gone by the time it prints, the API may
// refuse. Consuming the wake first and putting it back after a failure loses
// it when the process dies in between, so a delivery claims the wakes
// instead: each file is renamed aside (the claim, atomic with one winner) and
// stays on disk until the caller reports the outcome, Commit when the wakes
// were delivered, Release when they were not. A claim whose holder died is
// found again after staleClaimAfter and goes back to the queue.

// staleClaimAfter is how long a claim may be held before it counts as
// abandoned. A delivery holds its claim for the time of one print or one API
// call, well under this.
const staleClaimAfter = 2 * time.Minute

// heldFile is one claimed wake file: where it waits when pending and where the
// claim keeps it.
type heldFile struct {
	pending string
	claimed string
}

// Claimed is a batch of wakes claimed from a project's queue and not yet
// consumed. Wakes and ForumWakes are what to tell the commander. The caller
// must end the claim with Commit or Release.
type Claimed struct {
	Wakes      []Wake
	ForumWakes []ForumWake
	held       []heldFile
}

// Claim claims every wake of the project that is still news, both kinds, and
// holds them. A wake that is not news any more is consumed on the spot (see
// wakeIsCurrent). Whatever one kind returned is kept even if the other failed.
func Claim(projectRoot string) (*Claimed, error) {
	wakes, wheld, werr := claimWakes(projectRoot)
	forumWakes, fheld, ferr := claimForum(projectRoot)
	return &Claimed{Wakes: wakes, ForumWakes: forumWakes, held: append(wheld, fheld...)}, errors.Join(werr, ferr)
}

// Len is how many wakes are held, of both kinds.
func (c *Claimed) Len() int { return len(c.Wakes) + len(c.ForumWakes) }

// Reason is the text to hand the commander; see WakeReason.
func (c *Claimed) Reason() string { return WakeReason(c.Wakes, c.ForumWakes) }

// Commit consumes the claimed wakes: they were delivered.
func (c *Claimed) Commit() error {
	err := commitHeld(c.held)
	c.held = nil
	return err
}

// Release puts the claimed wakes back in the queue: they were not delivered, so
// the next claim finds them again and judges them anew (a wake that went stale
// in the meantime is dropped then, like any other). A wake of the same task or
// forum session that was recorded since is newer and stays; the claimed copy is
// dropped.
func (c *Claimed) Release() error {
	err := releaseHeld(c.held)
	c.held = nil
	return err
}

func commitHeld(held []heldFile) error {
	var errs []error
	for _, h := range held {
		if err := os.Remove(h.claimed); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("removing delivered wake %s: %w", filepath.Base(h.pending), err))
		}
	}
	return errors.Join(errs...)
}

func releaseHeld(held []heldFile) error {
	var errs []error
	for _, h := range held {
		if err := restore(h.claimed, h.pending); err != nil {
			errs = append(errs, fmt.Errorf("putting back wake %s: %w", filepath.Base(h.pending), err))
		}
	}
	return errors.Join(errs...)
}

// restore moves a claimed file back to its pending name, unless a newer file
// already has it, in which case the claimed copy is dropped.
func restore(claimed, pending string) error {
	if _, err := os.Stat(pending); err == nil {
		discard(claimed)
		return nil
	}
	if err := os.Rename(claimed, pending); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// discard removes a claimed file that is not worth delivering. A failure leaves
// it to be found as an abandoned claim, so it is not reported.
func discard(claimed string) {
	_ = os.Remove(claimed)
}

// listPending lists the names of the pending wake files directly in dir. On the
// way it puts back the claims abandoned by a holder that died (see
// staleClaimAfter), so they are pending again. A missing directory is an empty
// queue.
func listPending(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) == ".json" {
			names = append(names, name)
			continue
		}
		if base, ok := abandonedClaim(name, time.Now()); ok {
			if err := restore(filepath.Join(dir, name), filepath.Join(dir, base)); err == nil {
				names = append(names, base)
			}
		}
	}
	return names, nil
}

// abandonedClaim reports whether name is a claimed wake file whose claim is
// older than staleClaimAfter at now, and the name the wake had when pending.
// Claims in any other shape are left alone.
func abandonedClaim(name string, now time.Time) (pending string, ok bool) {
	base, suffix, found := strings.Cut(name, claimMarker)
	if !found || filepath.Ext(base) != ".json" {
		return "", false
	}
	millis, _, _ := strings.Cut(suffix, "-")
	ms, err := strconv.ParseInt(millis, 10, 64)
	if err != nil {
		return "", false
	}
	return base, now.Sub(time.UnixMilli(ms)) >= staleClaimAfter
}
