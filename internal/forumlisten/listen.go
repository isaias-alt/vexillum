// Package forumlisten is the forum listener: a deterministic process (no LLM)
// that holds one multiplexed poll on the forum server so no commander has to.
// For every prompt the server delivers it
//
//  1. writes the prompt, all of its fields, to the project's durable inbox
//     (internal/inbox), one atomic file per uid,
//  2. rings a forum wake (internal/sentinel) for the commander's Stop hook,
//  3. tells the server the round was relayed, which posts one fixed,
//     non-answering notice and keeps the browser waiting for the commander,
//  4. and only then acknowledges the delivery.
//
// That order means a crash anywhere loses nothing: the server redelivers
// whatever was not acknowledged, and every step is idempotent for a repeated
// uid. The listener never edits an artifact, ends a session, answers, or
// decides: it has no judgement to apply, so everything is forwarded.
package forumlisten

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/isaias-alt/vexillum/internal/forum"
	"github.com/isaias-alt/vexillum/internal/inbox"
	"github.com/isaias-alt/vexillum/internal/sentinel"
)

// Notice is the one line the listener posts per round. It is fixed text, never
// derived from what the user wrote.
const Notice = "Received. Forwarded to the commander, who will answer here."

// ErrServerTooOld means the forum server does not speak the listener's part of
// the API (it ignored the relay flag and delivered a session with no project).
var ErrServerTooOld = errors.New("the running forum server predates the forum listener; run `forum stop` and retry")

// Options configure Run. Only Home and Connect are required.
type Options struct {
	// Home is the vexillum home (~/.vexillum).
	Home string
	// Connect returns a client for the running forum server, starting one if
	// needed (the CLI passes forum.EnsureServer).
	Connect func(ctx context.Context) (*forum.Client, error)
	// PollTimeout bounds one multiplexed poll; between polls the listener
	// refreshes what it knows about commanders. Default 30s.
	PollTimeout time.Duration
	// NoSessionsGrace is how long the listener lingers after the server reports
	// no open session before it exits, so a session opened a moment later is
	// still covered. Default 10s.
	NoSessionsGrace time.Duration
	// RetryDelay is the pause before retrying after a failure. Default 500ms.
	RetryDelay time.Duration
	// MaxFailures is how many consecutive failures end the loop with an error.
	// Default 8.
	MaxFailures int
	// RefreshEvery is the least time between two looks at whether commander
	// sessions came or went. Default 5s.
	RefreshEvery time.Duration
	// Commander reports whether a commander session is known for projectRoot.
	// Default: a live "vx sentinel await" for it (sentinel.HasLiveAwaiter).
	Commander func(projectRoot string) bool
	// Log receives one line per event; nil discards.
	Log io.Writer
	// Now overrides the clock.
	Now func() time.Time
}

func (o *Options) defaults() {
	if o.PollTimeout <= 0 {
		o.PollTimeout = 30 * time.Second
	}
	if o.NoSessionsGrace <= 0 {
		o.NoSessionsGrace = 10 * time.Second
	}
	if o.RetryDelay <= 0 {
		o.RetryDelay = 500 * time.Millisecond
	}
	if o.MaxFailures <= 0 {
		o.MaxFailures = 8
	}
	if o.RefreshEvery <= 0 {
		o.RefreshEvery = 5 * time.Second
	}
	if o.Commander == nil {
		home := o.Home
		o.Commander = func(root string) bool { return sentinel.HasLiveAwaiter(home, root) }
	}
	if o.Now == nil {
		o.Now = time.Now
	}
}

// relayState is what the listener last told the server about a relayed round,
// kept so it can refresh the commander's presence while the round is open.
type relayState struct {
	file, root, commander string
}

type listener struct {
	opts    Options
	client  *forum.Client
	relayed map[string]*relayState
	// lastRefresh rate-limits refresh, which inspects processes.
	lastRefresh time.Time
}

func (l *listener) logf(format string, args ...any) {
	if l.opts.Log != nil {
		fmt.Fprintf(l.opts.Log, l.opts.Now().Format(time.RFC3339)+" "+format+"\n", args...)
	}
}

func commanderStatus(known bool) string {
	if known {
		return forum.CommanderConnected
	}
	return forum.CommanderNone
}

// Run polls until ctx ends (nil), nothing is left to listen to (nil), or it
// keeps failing (an error). The caller holds the singleton lock (AcquireLock).
func Run(ctx context.Context, opts Options) error {
	opts.defaults()
	l := &listener{opts: opts, relayed: map[string]*relayState{}}
	var err error
	if l.client, err = opts.Connect(ctx); err != nil {
		return fmt.Errorf("connecting to the forum server: %w", err)
	}
	l.logf("forum listener started")

	failures := 0
	var noSessionsSince time.Time
	for {
		if ctx.Err() != nil {
			return nil
		}
		res, err := l.client.PollRelay(ctx, opts.PollTimeout)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if failures++; failures > opts.MaxFailures {
				return fmt.Errorf("lost the forum server and could not reach it again: %w", err)
			}
			l.logf("poll failed (%d/%d): %v", failures, opts.MaxFailures, err)
			if !sleep(ctx, opts.RetryDelay) {
				return nil
			}
			var ae *forum.APIError
			if errors.As(err, &ae) {
				continue // the server answered; reconnecting would not change it
			}
			if c, cerr := opts.Connect(ctx); cerr == nil {
				l.client = c
			} else {
				l.logf("reconnect failed: %v", cerr)
			}
			continue
		}

		switch res.Status {
		case forum.PollNoSessions:
			if noSessionsSince.IsZero() {
				noSessionsSince = opts.Now()
			}
			if opts.Now().Sub(noSessionsSince) >= opts.NoSessionsGrace {
				l.logf("no sessions left, exiting")
				return nil
			}
			failures = 0
			if !sleep(ctx, minDuration(opts.NoSessionsGrace, time.Second)) {
				return nil
			}
			continue
		case forum.PollBrowserDisconnected:
			// Every review window went away past the server's grace: nobody is
			// reviewing. Exit so the server can idle out; `forum <file>`
			// starts a listener again.
			l.logf("every review window is gone, exiting")
			return nil
		}
		noSessionsSince = time.Time{}

		if res.Status == forum.PollFeedback || res.Status == forum.PollEnded {
			if err := l.handle(ctx, res); err != nil {
				if errors.Is(err, ErrServerTooOld) {
					return err
				}
				if failures++; failures > opts.MaxFailures {
					return fmt.Errorf("could not store forum feedback: %w", err)
				}
				l.logf("handling delivery failed (the server will redeliver it): %v", err)
				if !sleep(ctx, opts.RetryDelay) {
					return nil
				}
				continue
			}
		}
		failures = 0
		l.refresh(ctx)
	}
}

// handle stores one delivery and acknowledges it, in the crash-safe order.
func (l *listener) handle(ctx context.Context, res forum.PollResponse) error {
	if len(res.Prompts) == 0 {
		// A session that ended with nothing in flight: there is no feedback to
		// forward, and the server reports it only once.
		l.logf("session %s ended with no feedback", res.Session)
		l.ack(res)
		return nil
	}
	root := res.ProjectRoot
	if root == "" {
		return ErrServerTooOld
	}
	ended := res.Status == forum.PollEnded
	for _, p := range res.Prompts {
		e := inbox.Entry{
			UID:      p.UID,
			Session:  res.Session,
			File:     res.File,
			Tag:      p.Tag,
			Prompt:   p.Prompt,
			Selector: p.Selector,
			Text:     p.Text,
			Target:   p.Target,
			Ended:    ended,
			QueuedAt: p.QueuedAt,
		}
		if ended {
			e.EndedBy = res.EndedBy
		}
		for _, a := range p.Attachments {
			e.Attachments = append(e.Attachments, inbox.Attachment{Path: a.Path, Type: a.Mime, Bytes: a.Bytes})
		}
		if _, err := inbox.Write(root, e); err != nil {
			return err
		}
	}

	sum, err := inbox.Unread(root, res.Session)
	if err != nil {
		return err
	}
	if sum.Count > 0 {
		if err := sentinel.RecordForumWake(root, sentinel.ForumWake{Session: res.Session, File: res.File, Count: sum.Count, Ended: sum.Ended || ended, DetectedAt: l.opts.Now().UTC()}); err != nil {
			return err
		}
	}
	if !ended {
		l.relay(ctx, res.Session, res.File, root)
	}
	l.logf("stored %d prompt(s) of session %s (%d unread, ended: %v)", len(res.Prompts), res.Session, sum.Count, ended)
	l.ack(res)
	return nil
}

// relay tells the server the round was forwarded. A failure is logged, not
// fatal: the inbox and the wake already hold the feedback, and the browser keeps
// waiting on its own (the overlay does not depend on the notice).
func (l *listener) relay(ctx context.Context, session, file, root string) {
	commander := commanderStatus(l.opts.Commander(root))
	out, err := l.client.Relay(ctx, file, commander, Notice)
	if err != nil {
		var ae *forum.APIError
		if errors.As(err, &ae) && (ae.Status == 404 && ae.Code == "") {
			l.logf("the server has no relay route (an older build); the browser will not show the relayed state")
			return
		}
		l.logf("relay of session %s failed: %v", session, err)
		return
	}
	if out.Active {
		l.relayed[session] = &relayState{file: file, root: root, commander: commander}
	} else {
		delete(l.relayed, session)
	}
}

// ack confirms the delivery with its own deadline: ctx may already be canceled
// by the signal that is ending the process. A failure only means the server
// redelivers, and every step above is idempotent.
func (l *listener) ack(res forum.PollResponse) {
	if res.Delivery == "" || res.File == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := l.client.Ack(ctx, res.File, res.Delivery); err != nil {
		l.logf("could not acknowledge delivery of session %s (it will be redelivered): %v", res.Session, err)
	}
}

// refresh re-reads whether a commander session is known for every relayed
// round still open and tells the server when that changed, so the browser's
// "no commander session is known" is honest as sessions come and go. Rounds the
// server says are no longer waiting are forgotten.
func (l *listener) refresh(ctx context.Context) {
	if len(l.relayed) == 0 || l.opts.Now().Sub(l.lastRefresh) < l.opts.RefreshEvery {
		return
	}
	l.lastRefresh = l.opts.Now()
	for session, st := range l.relayed {
		commander := commanderStatus(l.opts.Commander(st.root))
		if commander == st.commander {
			// Still ask now and then whether the round is still open, so the map
			// does not grow: Relay is a no-op when nothing changed.
			if out, err := l.client.Relay(ctx, st.file, commander, Notice); err == nil && !out.Active {
				delete(l.relayed, session)
			}
			continue
		}
		out, err := l.client.Relay(ctx, st.file, commander, Notice)
		if err != nil {
			l.logf("refreshing the commander status of session %s failed: %v", session, err)
			continue
		}
		if !out.Active {
			delete(l.relayed, session)
			continue
		}
		st.commander = commander
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
