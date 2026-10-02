package forum

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Errors a caller can distinguish (the HTTP layer maps them to statuses).
var (
	ErrNoSession     = errors.New("no such forum session")
	ErrNoPrompt      = errors.New("no such queued prompt")
	ErrNothingToSend = errors.New("nothing to send")
	ErrQueueFull     = errors.New("too many queued prompts")
)

// ErrSessionEnded reports an operation refused because the session was
// already ended; EndedBy says by whom.
type ErrSessionEnded struct{ EndedBy string }

func (e *ErrSessionEnded) Error() string {
	if e.EndedBy == "" {
		return "session already ended"
	}
	return "session already ended by " + e.EndedBy
}

// Poll result statuses.
const (
	PollFeedback            = "feedback"
	PollEnded               = "ended"
	PollBrowserDisconnected = "browser_disconnected"
	PollTimeout             = "timeout"
	// PollNoSessions is only reported by a multiplexed poll: no session is
	// open, so there is nothing left to listen to.
	PollNoSessions = "no_sessions"
)

// HubOptions tune timing; zero values mean the production defaults. Tests
// shrink them so the grace and long-poll paths run in milliseconds.
type HubOptions struct {
	// BrowserGrace is how long a session's browser may be gone (no open
	// state request) before an agent poll reports browser_disconnected.
	BrowserGrace time.Duration
	// ListenerGrace is how long after an agent's last sign of life (a poll
	// starting or ending, a reply, an open, an ack) the panel keeps saying the
	// agent is about to listen again instead of warning that nobody is.
	ListenerGrace time.Duration
	// Now overrides the clock.
	Now func() time.Time
	// Logf receives non-fatal persistence problems.
	Logf func(format string, args ...any)
}

const (
	defaultBrowserGrace  = 30 * time.Second
	defaultListenerGrace = 30 * time.Second
)

// pollAllRecentWindow bounds which on-disk sessions a multiplexed poll
// adopts when it starts: abandoned sessions from long ago stay unloaded until
// their browser reconnects (which loads them and wakes the poll) or the agent
// opens them again.
const pollAllRecentWindow = 7 * 24 * time.Hour

// AgentWorkingWindow is how long after delivering prompts to a poll the
// browser keeps saying the agent is working. The agent has no way to say "I am
// busy", and a real task (a mission, a review) easily runs for many minutes
// between polls; past this without a poll or a reply the honest answer is
// "not listening" again.
const AgentWorkingWindow = 15 * time.Minute

// Hub owns every live session of one server process. All durable state goes
// through atomicfile (see persist.go) before it is committed to memory, so
// a crash or restart never loses a queued or sent prompt; waiters (agent
// polls and browser state requests) block on a broadcast channel that every
// change closes and replaces.
type Hub struct {
	home string
	opts HubOptions

	mu           sync.Mutex
	sessions     map[string]*liveSession
	changed      chan struct{}
	lastActivity time.Time
}

type liveSession struct {
	rec        sessionRecord
	transcript []Message
	layout     layoutState
	version    int64
	pollers    int
	// relayPollers counts the pollers that are the listener (a subset of
	// pollers): they forward feedback to the commander, so they are not "the
	// agent listening".
	relayPollers int
	browsers     int
	lastBrowser  time.Time
	// lastListener is the agent's heartbeat: the last moment something proved
	// it is there (see Hub.beat). In memory only; a loaded session starts with a
	// fresh one so a restarted server gives the poll time to reconnect.
	lastListener time.Time
}

// NewHub returns a Hub persisting under home (the ~/.vexillum directory).
func NewHub(home string, opts HubOptions) *Hub {
	if opts.BrowserGrace <= 0 {
		opts.BrowserGrace = defaultBrowserGrace
	}
	if opts.ListenerGrace <= 0 {
		opts.ListenerGrace = defaultListenerGrace
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	h := &Hub{
		home:     home,
		opts:     opts,
		sessions: make(map[string]*liveSession),
		changed:  make(chan struct{}),
	}
	h.lastActivity = opts.Now()
	return h
}

func (h *Hub) logf(format string, args ...any) {
	if h.opts.Logf != nil {
		h.opts.Logf(format, args...)
	}
}

// notify wakes every waiter without changing any session's visible state.
// Callers hold h.mu.
func (h *Hub) notify() {
	h.lastActivity = h.opts.Now()
	close(h.changed)
	h.changed = make(chan struct{})
}

// bump records a visible change to l and wakes every waiter. Callers hold
// h.mu.
func (h *Hub) bump(l *liveSession) {
	l.version++
	h.notify()
}

// get returns the live session for key, loading it from disk the first time
// it is needed (so a restarted server resumes whatever was persisted).
// Callers hold h.mu.
func (h *Hub) get(key string) (*liveSession, error) {
	if l, ok := h.sessions[key]; ok {
		return l, nil
	}
	rec, transcript, err := loadSession(h.home, key)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, ErrNoSession
	}
	// A transcript written under an older or looser cap is trimmed on load (and
	// the trim persisted and its orphaned images swept, below).
	transcript, trimmed := boundTranscript(transcript)
	// The layout inbox is advisory: a damaged file starts an empty one rather
	// than blocking the session.
	layout, err := loadLayout(h.home, key)
	if err != nil {
		h.logf("forum: %v", err)
		layout = layoutState{Version: layoutStateVersion}
	}
	// Whether the agent is still working on the last feedback is a fact about a
	// process this server no longer knows about: a restarted server never
	// blocks the review surface on it.
	rec.AwaitingSince = time.Time{}
	l := &liveSession{
		rec:          *rec,
		transcript:   transcript,
		layout:       layout,
		version:      h.opts.Now().UnixNano(),
		lastBrowser:  h.opts.Now(),
		lastListener: h.opts.Now(),
	}
	h.sessions[key] = l
	if len(trimmed) > 0 {
		if err := saveTranscript(h.home, key, l.transcript); err != nil {
			h.logf("forum: %v", err)
		}
		h.sweepAttachments(l)
	}
	return l, nil
}

// commit clones l's record, applies fn, persists the result, and only then
// replaces the in-memory copy - a failed write leaves memory and disk both
// at the previous state. Callers hold h.mu.
func (h *Hub) commit(l *liveSession, fn func(rec *sessionRecord) error) error {
	next := l.rec
	next.Queued = append([]Prompt(nil), l.rec.Queued...)
	next.Outbox = append([]Prompt(nil), l.rec.Outbox...)
	next.Inflight = append([]Prompt(nil), l.rec.Inflight...)
	if err := fn(&next); err != nil {
		return err
	}
	next.UpdatedAt = h.opts.Now().UTC()
	if err := saveRecord(h.home, &next); err != nil {
		return err
	}
	l.rec = next
	return nil
}

// beat records a sign of life from the agent on l. Callers hold h.mu.
func (h *Hub) beat(l *liveSession) {
	l.lastListener = h.opts.Now()
}

// appendTranscript adds msgs to l's transcript, keeping the newest messages
// that fit maxTranscriptItems and maxTranscriptBytes (see boundTranscript).
// Evicting a message only frees its images once nothing else references
// them: a prompt still queued or waiting in the outbox keeps its files. A
// failed write is logged, not returned: the prompts the messages mirror are
// already safely queued, and the next successful write persists the full
// transcript anyway. Callers hold h.mu.
func (h *Hub) appendTranscript(l *liveSession, msgs ...Message) error {
	next, evicted := boundTranscript(append(append([]Message(nil), l.transcript...), msgs...))
	err := saveTranscript(h.home, l.rec.Key, next)
	l.transcript = next
	if err != nil {
		h.logf("forum: %v", err)
	}
	if len(evicted) > 0 {
		h.sweepAttachments(l)
	}
	return err
}

// boundTranscript keeps the newest suffix of msgs with at most
// maxTranscriptItems entries whose JSON fits maxTranscriptBytes, and reports
// what it evicted. The newest message is always kept, however large. Sizes
// are measured the way the file is written (indented, one element per
// array slot), so the cap bounds the file on disk, not just the payload.
func boundTranscript(msgs []Message) (kept, evicted []Message) {
	cut := len(msgs)
	size := 3
	for i := len(msgs) - 1; i >= 0 && len(msgs)-i <= maxTranscriptItems; i-- {
		data, err := json.MarshalIndent(msgs[i], "  ", "  ")
		if err != nil {
			break
		}
		size += len(data) + 4 // indent, separator and newline
		if size > maxTranscriptBytes && cut < len(msgs) {
			break
		}
		cut = i
	}
	return msgs[cut:], msgs[:cut]
}

func (l *liveSession) endedErr() error {
	if l.rec.Status == StatusEnded {
		return &ErrSessionEnded{EndedBy: l.rec.EndedBy}
	}
	return nil
}

// OpenResult is what Open reports back to the agent-facing command.
type OpenResult struct {
	Key              string
	File             string
	Status           string // StatusOpen, or "user_ended" when a reopen was refused
	Created          bool
	BrowserConnected bool
	// ProjectRoot is the project root the session is now recorded under.
	ProjectRoot string
	// Pending counts prompts the user already sent that the next poll will
	// deliver.
	Pending int
}

// OpenUserEnded is OpenResult.Status when the user ended the session from
// the browser and the caller did not pass reopen.
const OpenUserEnded = "user_ended"

// Open creates or resumes the session for file (an absolute path). A session
// the user ended from the browser is refused unless reopen is set: reviving
// it silently would reopen a surface the user deliberately closed.
func (h *Hub) Open(file string, reopen bool) (OpenResult, error) {
	return h.OpenFor(file, reopen, "")
}

// OpenFor is Open that also records projectRoot (when not empty) as the
// project whose commander owns the session, so the listener can wake it.
func (h *Hub) OpenFor(file string, reopen bool, projectRoot string) (OpenResult, error) {
	file = filepath.Clean(file)
	key := SessionKey(file)

	h.mu.Lock()
	defer h.mu.Unlock()

	l, err := h.get(key)
	created := false
	if errors.Is(err, ErrNoSession) {
		token, terr := newToken()
		if terr != nil {
			return OpenResult{}, terr
		}
		now := h.opts.Now().UTC()
		l = &liveSession{
			rec: sessionRecord{
				Version:   sessionRecordVersion,
				Key:       key,
				File:      file,
				Token:     token,
				Status:    StatusOpen,
				CreatedAt: now,
				UpdatedAt: now,
				Queued:    []Prompt{},
				Outbox:    []Prompt{},
			},
			version: h.opts.Now().UnixNano(),
		}
		if err := saveRecord(h.home, &l.rec); err != nil {
			return OpenResult{}, err
		}
		h.sessions[key] = l
		created = true
	} else if err != nil {
		return OpenResult{}, err
	}

	if l.rec.Status == StatusEnded {
		if l.rec.EndedBy == EndedByUser && !reopen {
			return h.openResult(l, OpenUserEnded, created), nil
		}
		if err := h.commit(l, func(rec *sessionRecord) error {
			rec.Status = StatusOpen
			rec.EndedBy = ""
			return nil
		}); err != nil {
			return OpenResult{}, err
		}
	}
	if projectRoot != "" && l.rec.ProjectRoot != projectRoot {
		if err := h.commit(l, func(rec *sessionRecord) error {
			rec.ProjectRoot = projectRoot
			return nil
		}); err != nil {
			return OpenResult{}, err
		}
	}
	// A fresh open starts the browser's grace period: the page is about to
	// load (or already is), and a poll must not call it disconnected before
	// it ever had the chance to connect.
	l.lastBrowser = h.opts.Now()
	h.beat(l)
	h.bump(l)
	return h.openResult(l, StatusOpen, created), nil
}

func (h *Hub) openResult(l *liveSession, status string, created bool) OpenResult {
	return OpenResult{
		Key:              l.rec.Key,
		File:             l.rec.File,
		Status:           status,
		Created:          created,
		BrowserConnected: l.browsers > 0,
		ProjectRoot:      l.rec.ProjectRoot,
		Pending:          len(l.rec.Outbox),
	}
}

// Token returns key's session token (what the browser must present to queue
// prompts).
func (h *Hub) Token(key string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	l, err := h.get(key)
	if err != nil {
		return "", err
	}
	return l.rec.Token, nil
}

// File returns the artifact path for key.
func (h *Hub) File(key string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	l, err := h.get(key)
	if err != nil {
		return "", err
	}
	return l.rec.File, nil
}

// RequireOpen returns *ErrSessionEnded if key's session has ended (and
// ErrNoSession if it does not exist), nil if it still accepts prompts.
func (h *Hub) RequireOpen(key string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	l, err := h.get(key)
	if err != nil {
		return err
	}
	return l.endedErr()
}

// QueuePrompt adds a prompt to key's unsent queue. A non-empty QueueKey
// replaces the earlier unsent prompt with the same key, in place.
func (h *Hub) QueuePrompt(key string, in PromptInput) (Prompt, error) {
	p, err := normalizePrompt(in)
	if err != nil {
		return Prompt{}, err
	}
	uid, err := newID("pr_")
	if err != nil {
		return Prompt{}, err
	}
	p.UID = uid
	p.QueuedAt = h.opts.Now().UTC()

	h.mu.Lock()
	defer h.mu.Unlock()
	l, err := h.get(key)
	if err != nil {
		return Prompt{}, err
	}
	if err := l.endedErr(); err != nil {
		return Prompt{}, err
	}
	var replaced []Attachment
	if p.QueueKey != "" {
		for _, q := range l.rec.Queued {
			if q.QueueKey == p.QueueKey {
				replaced = q.Attachments
			}
		}
	}
	if p.Attachments, err = h.resolveAttachments(l, in.Attachments, replaced); err != nil {
		return Prompt{}, err
	}
	if err := h.commit(l, func(rec *sessionRecord) error {
		if p.QueueKey != "" {
			for i := range rec.Queued {
				if rec.Queued[i].QueueKey == p.QueueKey {
					rec.Queued[i] = p
					return nil
				}
			}
		}
		if len(rec.Queued) >= maxQueuedPrompts {
			return ErrQueueFull
		}
		rec.Queued = append(rec.Queued, p)
		return nil
	}); err != nil {
		return Prompt{}, err
	}
	// Images only the replaced prompt carried are no longer anyone's.
	h.dropAttachments(l, attachmentIDs(replaced))
	h.bump(l)
	return p, nil
}

// RemoveQueued drops one unsent prompt.
func (h *Hub) RemoveQueued(key, uid string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	l, err := h.get(key)
	if err != nil {
		return err
	}
	if err := l.endedErr(); err != nil {
		return err
	}
	var removed []Attachment
	var released []string
	if err := h.commit(l, func(rec *sessionRecord) error {
		for i := range rec.Queued {
			if rec.Queued[i].UID == uid {
				removed = rec.Queued[i].Attachments
				released = rec.Queued[i].LayoutIDs
				rec.Queued = append(rec.Queued[:i], rec.Queued[i+1:]...)
				return nil
			}
		}
		return ErrNoPrompt
	}); err != nil {
		return err
	}
	h.dropAttachments(l, attachmentIDs(removed))
	h.releaseLayout(l, l.unreferencedLayoutIDs(released))
	h.bump(l)
	return nil
}

// Send moves the unsent queue to the outbox, where the next agent poll
// picks it up, and records the prompts in the transcript. With end set it
// also ends the session on the user's behalf ("Send & End"): the outbox is
// still delivered once, then polls report ended. Returns how many prompts
// were sent.
func (h *Hub) Send(key string, end bool) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	l, err := h.get(key)
	if err != nil {
		return 0, err
	}
	if err := l.endedErr(); err != nil {
		return 0, err
	}
	sent := append([]Prompt(nil), l.rec.Queued...)
	if len(sent) == 0 && !end {
		return 0, ErrNothingToSend
	}
	if err := h.commit(l, func(rec *sessionRecord) error {
		rec.Outbox = append(rec.Outbox, rec.Queued...)
		rec.Queued = []Prompt{}
		if len(sent) > 0 {
			// Sending feedback starts a round, and until the agent answers
			// (or the artifact changes) the review surface waits for it.
			rec.Round++
			rec.RoundArtifact = artifactVersion(rec.File)
			rec.AwaitingSince = h.opts.Now().UTC()
		}
		if end {
			rec.Status = StatusEnded
			rec.EndedBy = EndedByUser
			rec.AwaitingSince = time.Time{}
		}
		return nil
	}); err != nil {
		return 0, err
	}
	if len(sent) > 0 {
		msgs := make([]Message, 0, len(sent))
		for _, p := range sent {
			id, _ := newID("m_")
			msgs = append(msgs, Message{ID: id, Role: RoleUser, Text: clip(p.Prompt, 4000), Tag: p.Tag, Selector: p.Selector, At: h.opts.Now().UTC(), Attachments: p.Attachments, Round: l.rec.Round, QueueKey: p.QueueKey})
		}
		_ = h.appendTranscript(l, msgs...)
	}
	h.bump(l)
	return len(sent), nil
}

// End ends key's session. Ending an already-ended session is a no-op that
// keeps the original EndedBy.
func (h *Hub) End(key, by string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	l, err := h.get(key)
	if err != nil {
		return err
	}
	if l.rec.Status == StatusEnded {
		return nil
	}
	if err := h.commit(l, func(rec *sessionRecord) error {
		rec.Status = StatusEnded
		rec.EndedBy = by
		rec.AwaitingSince = time.Time{}
		return nil
	}); err != nil {
		return err
	}
	h.bump(l)
	return nil
}

// Reply posts an agent message (markdown) to key's conversation panel.
func (h *Hub) Reply(key, text string) error {
	text = clip(strings.TrimSpace(text), maxReplyChars)
	if text == "" {
		return fmt.Errorf("reply is empty")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	l, err := h.get(key)
	if err != nil {
		return err
	}
	if err := l.endedErr(); err != nil {
		return err
	}
	id, err := newID("m_")
	if err != nil {
		return err
	}
	h.beat(l)
	// A reply answers the round in progress: nothing is awaited any more.
	if err := h.commit(l, func(rec *sessionRecord) error {
		rec.DeliveredAt = time.Time{}
		rec.AwaitingSince = time.Time{}
		rec.AnsweredThrough = rec.Round
		rec.RelayedAt = time.Time{}
		rec.Commander = ""
		return nil
	}); err != nil {
		return err
	}
	if err := h.appendTranscript(l, Message{ID: id, Role: RoleAgent, Text: text, At: h.opts.Now().UTC(), Round: l.rec.Round}); err != nil {
		return err
	}
	h.bump(l)
	return nil
}

// RelayResult is what Relay reports back to the listener.
type RelayResult struct {
	// Active is true while the latest round is still waiting for the commander
	// (the listener keeps refreshing the commander status until it is not).
	Active bool
	// Posted is true when this call added the notice to the transcript.
	Posted bool
}

// Relay records that the listener forwarded key's latest round to the
// commander. It posts notice (a fixed line, never derived from the user's text)
// at most once per round, as a non-answering message, and marks the round
// relayed with the commander's presence. It does NOT answer the round: the
// review surface keeps waiting until a real reply, an artifact change or the
// user's Stop waiting. Calling it again for the same round only refreshes
// commander, so it is safe on redelivery. An ended or already answered session
// is a no-op (Active false).
func (h *Hub) Relay(key, commander, notice string) (RelayResult, error) {
	notice = clip(strings.TrimSpace(notice), maxReplyChars)
	if commander != CommanderConnected {
		commander = CommanderNone
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	l, err := h.get(key)
	if err != nil {
		return RelayResult{}, err
	}
	if l.rec.Status == StatusEnded || l.rec.Round <= l.rec.AnsweredThrough {
		return RelayResult{}, nil
	}
	h.beat(l)
	first := l.rec.RelayedRound < l.rec.Round || l.rec.RelayedAt.IsZero()
	if !first && l.rec.Commander == commander {
		return RelayResult{Active: true}, nil
	}
	if err := h.commit(l, func(rec *sessionRecord) error {
		if first {
			rec.RelayedAt = h.opts.Now().UTC()
			rec.RelayedRound = rec.Round
		}
		rec.Commander = commander
		return nil
	}); err != nil {
		return RelayResult{}, err
	}
	posted := false
	if first && notice != "" {
		id, err := newID("m_")
		if err != nil {
			return RelayResult{}, err
		}
		if err := h.appendTranscript(l, Message{ID: id, Role: RoleAgent, Kind: MessageKindNotice, Text: notice, At: h.opts.Now().UTC(), Round: l.rec.Round}); err != nil {
			return RelayResult{}, err
		}
		posted = true
	}
	h.bump(l)
	return RelayResult{Active: true, Posted: posted}, nil
}

// PollResult is what one agent poll delivers.
type PollResult struct {
	Key     string
	File    string
	Status  string
	EndedBy string
	Prompts []Prompt
	// ProjectRoot is the project root recorded for the session (empty when it
	// was opened outside any project).
	ProjectRoot string
	// Delivery names the lease on Prompts: the agent confirms it with Hub.Ack
	// once it has read them, and until then they stay on disk (see
	// sessionRecord.Inflight). Empty when nothing was delivered.
	Delivery string
	// OtherPending counts the other sessions a multiplexed poll left with
	// feedback still waiting; it is 0 for a poll of one session.
	OtherPending int
}

// hasUndelivered reports whether l holds prompts no acknowledged poll has
// delivered: sent and waiting, or handed out and never confirmed.
func (l *liveSession) hasUndelivered() bool {
	return len(l.rec.Outbox) > 0 || len(l.rec.Inflight) > 0
}

// oldestUndelivered is when the oldest prompt waiting for delivery was queued.
func (l *liveSession) oldestUndelivered() time.Time {
	if len(l.rec.Inflight) > 0 {
		return l.rec.Inflight[0].QueuedAt
	}
	if len(l.rec.Outbox) > 0 {
		return l.rec.Outbox[0].QueuedAt
	}
	return time.Time{}
}

// beginPoll registers one poller on l. Callers hold h.mu.
//
// A relay poll (the listener's) is not the agent: it forwards feedback and
// re-polls at once, so it never ends the wait for the commander's answer.
func (h *Hub) beginPoll(l *liveSession, relay bool) {
	l.pollers++
	if relay {
		l.relayPollers++
	}
	h.beat(l)
	if !relay {
		// Polling again: the agent is listening, not merely working. A poll that
		// starts with feedback already waiting is the one about to take it, so only
		// a poll with nothing to deliver means the agent is done with the last round.
		idle := !l.hasUndelivered()
		if (idle && (!l.rec.AwaitingSince.IsZero() || !l.rec.RelayedAt.IsZero())) || !l.rec.DeliveredAt.IsZero() {
			if err := h.commit(l, func(rec *sessionRecord) error {
				rec.DeliveredAt = time.Time{}
				if idle {
					rec.AwaitingSince = time.Time{}
					rec.RelayedAt = time.Time{}
					rec.Commander = ""
				}
				return nil
			}); err != nil {
				h.logf("clearing delivered_at: %v", err)
			}
		}
	}
	h.bump(l)
}

// endPoll drops one poller from l. Callers hold h.mu.
func (h *Hub) endPoll(l *liveSession, relay bool) {
	l.pollers--
	if relay {
		l.relayPollers--
	}
	h.beat(l)
	h.bump(l)
}

// deliver leases everything waiting on l to the caller: unconfirmed prompts of
// an earlier delivery first (marked redelivered), then the outbox. The prompts
// move to Inflight and stay there until Ack, so a poll that dies before its
// output is read costs nothing. Callers hold h.mu.
func (h *Hub) deliver(l *liveSession, relay bool) (PollResult, error) {
	redelivered := len(l.rec.Inflight)
	all := append(append([]Prompt(nil), l.rec.Inflight...), l.rec.Outbox...)
	id, err := newID("dl_")
	if err != nil {
		return PollResult{}, err
	}
	if err := h.commit(l, func(rec *sessionRecord) error {
		rec.Inflight = all
		rec.InflightID = id
		rec.Outbox = []Prompt{}
		if !relay {
			rec.DeliveredAt = h.opts.Now().UTC()
		}
		return nil
	}); err != nil {
		return PollResult{}, err
	}
	out := append([]Prompt(nil), all...)
	for i := 0; i < redelivered; i++ {
		out[i].Redelivered = true
	}
	res := PollResult{Key: l.rec.Key, File: l.rec.File, EndedBy: l.rec.EndedBy, ProjectRoot: l.rec.ProjectRoot, Status: PollFeedback, Prompts: out, Delivery: id}
	if l.rec.Status == StatusEnded {
		res.Status = PollEnded
	}
	h.bump(l)
	return res, nil
}

// Ack confirms that the agent read delivery's prompts, releasing them. An
// unknown or stale delivery (a newer one replaced it, or it was already
// acknowledged) is ignored, so acknowledging twice is harmless. A poll that is
// never acknowledged is not an error: its prompts come back with the next poll.
func (h *Hub) Ack(key, delivery string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	l, err := h.get(key)
	if err != nil {
		return err
	}
	h.beat(l)
	if delivery == "" || l.rec.InflightID != delivery {
		return nil
	}
	if err := h.commit(l, func(rec *sessionRecord) error {
		rec.Inflight = []Prompt{}
		rec.InflightID = ""
		return nil
	}); err != nil {
		return err
	}
	h.notify()
	return nil
}

// Poll blocks until key's user has sent feedback, the session ended, the
// browser has been gone past the grace period, or timeout elapsed (0 means
// wait indefinitely). Delivered prompts are leased, not dropped: see Ack. While
// it waits the session counts as "listening" in the browser. A canceled ctx
// consumes nothing.
func (h *Hub) Poll(ctx context.Context, key string, timeout time.Duration) (PollResult, error) {
	h.mu.Lock()
	l, err := h.get(key)
	if err != nil {
		h.mu.Unlock()
		return PollResult{}, err
	}
	h.beginPoll(l, false)
	var deadline time.Time
	if timeout > 0 {
		deadline = h.opts.Now().Add(timeout)
	}
	defer func() {
		h.mu.Lock()
		h.endPoll(l, false)
		h.mu.Unlock()
	}()

	for {
		if err := ctx.Err(); err != nil {
			h.mu.Unlock()
			return PollResult{}, err
		}
		now := h.opts.Now()
		res := PollResult{Key: key, File: l.rec.File, EndedBy: l.rec.EndedBy, ProjectRoot: l.rec.ProjectRoot}

		if l.hasUndelivered() {
			res, err := h.deliver(l, false)
			h.mu.Unlock()
			return res, err
		}
		if l.rec.Status == StatusEnded {
			res.Status = PollEnded
			h.mu.Unlock()
			return res, nil
		}
		wake := time.Duration(-1)
		if l.browsers == 0 {
			remaining := l.lastBrowser.Add(h.opts.BrowserGrace).Sub(now)
			if remaining <= 0 {
				// Reporting it restarts the grace period, so an agent that
				// ignores the status and polls again waits a full grace
				// period instead of spinning.
				l.lastBrowser = now
				res.Status = PollBrowserDisconnected
				h.mu.Unlock()
				return res, nil
			}
			wake = remaining
		}
		if !deadline.IsZero() {
			remaining := deadline.Sub(now)
			if remaining <= 0 {
				res.Status = PollTimeout
				h.mu.Unlock()
				return res, nil
			}
			if wake < 0 || remaining < wake {
				wake = remaining
			}
		}
		h.wait(ctx, wake)
	}
}

// wait releases h.mu until something changes, wake elapses (negative means
// never) or ctx ends, then takes h.mu back. Callers hold h.mu.
func (h *Hub) wait(ctx context.Context, wake time.Duration) {
	changed := h.changed
	h.mu.Unlock()
	var timer <-chan time.Time
	var t *time.Timer
	if wake >= 0 {
		t = time.NewTimer(wake)
		timer = t.C
	}
	select {
	case <-changed:
	case <-timer:
	case <-ctx.Done():
	}
	if t != nil {
		t.Stop()
	}
	h.mu.Lock()
}

// PollAll is Poll over every open session at once, so one agent listener
// covers N review windows. It delivers the feedback of one session per call
// (the one waiting longest; OtherPending says how many more are waiting), and
// each session it covers counts as "listening" while it runs. A session that
// ends while it waits is reported once, as ended. It reports no_sessions when
// nothing is open, and browser_disconnected only when no covered session has a
// browser left. Sessions opened while it waits join it. Delivery is leased per
// session exactly as in Poll.
func (h *Hub) PollAll(ctx context.Context, timeout time.Duration) (PollResult, error) {
	return h.PollAllWith(ctx, timeout, PollAllOptions{})
}

// PollAllOptions tune a multiplexed poll.
type PollAllOptions struct {
	// Relay marks the poll as the listener's. It covers only sessions that
	// have a project root (a session opened outside any project has no
	// commander to forward to, so it stays undelivered for a manual poll), it
	// does not count as "the agent listening" in the panel, and it never ends
	// the wait for the commander's answer.
	Relay bool
}

// PollAllWith is PollAll with options.
func (h *Hub) PollAllWith(ctx context.Context, timeout time.Duration, opts PollAllOptions) (PollResult, error) {
	relay := opts.Relay
	h.mu.Lock()
	h.adoptRecentSessions()
	tracked := map[string]*liveSession{}
	defer func() {
		h.mu.Lock()
		for _, l := range tracked {
			h.endPoll(l, relay)
		}
		h.mu.Unlock()
	}()
	var deadline time.Time
	if timeout > 0 {
		deadline = h.opts.Now().Add(timeout)
	}

	for {
		if err := ctx.Err(); err != nil {
			h.mu.Unlock()
			return PollResult{}, err
		}
		now := h.opts.Now()
		// An ended session still holding undelivered feedback (Send & End) stays
		// covered until the agent has taken it.
		for key, l := range h.sessions {
			if relay && l.rec.ProjectRoot == "" {
				continue
			}
			if tracked[key] == nil && (l.rec.Status == StatusOpen || l.hasUndelivered()) {
				tracked[key] = l
				h.beginPoll(l, relay)
			}
		}
		if len(tracked) == 0 {
			h.mu.Unlock()
			return PollResult{Status: PollNoSessions}, nil
		}

		keys := make([]string, 0, len(tracked))
		for key := range tracked {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		var pick *liveSession
		waiting := 0
		for _, key := range keys {
			l := tracked[key]
			if !l.hasUndelivered() {
				continue
			}
			waiting++
			if pick == nil || l.oldestUndelivered().Before(pick.oldestUndelivered()) {
				pick = l
			}
		}
		if pick != nil {
			res, err := h.deliver(pick, relay)
			res.OtherPending = waiting - 1
			h.mu.Unlock()
			return res, err
		}
		for _, key := range keys {
			if l := tracked[key]; l.rec.Status == StatusEnded {
				res := PollResult{Key: key, File: l.rec.File, EndedBy: l.rec.EndedBy, ProjectRoot: l.rec.ProjectRoot, Status: PollEnded}
				h.mu.Unlock()
				return res, nil
			}
		}

		wake := time.Duration(-1)
		connected := false
		var allGone time.Time
		for _, l := range tracked {
			if l.browsers > 0 {
				connected = true
				break
			}
			if gone := l.lastBrowser.Add(h.opts.BrowserGrace); gone.After(allGone) {
				allGone = gone
			}
		}
		if !connected {
			remaining := allGone.Sub(now)
			if remaining <= 0 {
				// As in Poll, reporting it restarts every grace period.
				for _, l := range tracked {
					l.lastBrowser = now
				}
				h.mu.Unlock()
				return PollResult{Status: PollBrowserDisconnected}, nil
			}
			wake = remaining
		}
		if !deadline.IsZero() {
			remaining := deadline.Sub(now)
			if remaining <= 0 {
				h.mu.Unlock()
				return PollResult{Status: PollTimeout}, nil
			}
			if wake < 0 || remaining < wake {
				wake = remaining
			}
		}
		h.wait(ctx, wake)
	}
}

// adoptRecentSessions loads the sessions persisted on disk that a multiplexed
// poll should cover although nothing has touched them since the server
// started: recently updated open ones, and any still holding undelivered
// feedback. Callers hold h.mu.
func (h *Hub) adoptRecentSessions() {
	entries, err := os.ReadDir(filepath.Join(h.home, "forums"))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			h.logf("forum: listing sessions: %v", err)
		}
		return
	}
	for _, e := range entries {
		key := e.Name()
		if !e.IsDir() || !ValidSessionKey(key) || h.sessions[key] != nil {
			continue
		}
		rec, err := loadRecord(h.home, key)
		if err != nil {
			h.logf("forum: %v", err)
			continue
		}
		if rec == nil {
			continue
		}
		undelivered := len(rec.Outbox) > 0 || len(rec.Inflight) > 0
		recent := h.opts.Now().Sub(rec.UpdatedAt) <= pollAllRecentWindow
		if !undelivered && (rec.Status != StatusOpen || !recent) {
			continue
		}
		if _, err := h.get(key); err != nil {
			h.logf("forum: %v", err)
		}
	}
}

// Snapshot is the browser's view of a session.
type Snapshot struct {
	Version   int64  `json:"version"`
	Key       string `json:"key"`
	File      string `json:"file"`
	Status    string `json:"status"`
	EndedBy   string `json:"ended_by,omitempty"`
	Listening bool   `json:"listening"`
	// Listener is the agent's presence for the panel: "listening" (a poll is
	// open), "working" (it took the user's prompts and has not polled or replied
	// since), "waiting" (no poll right now but the agent was there a moment ago,
	// so a new poll is expected: no warning yet) or "none".
	Listener string `json:"listener"`
	// ListenerUntil is when "waiting" runs out and becomes "none".
	ListenerUntil *time.Time `json:"listener_until,omitempty"`
	// WorkingUntil is set while the agent is not polling but took the user's
	// prompts and has neither polled again nor replied: the browser shows
	// "received your message and is working" until then.
	WorkingUntil *time.Time `json:"working_until,omitempty"`
	// Relayed is set from the moment the listener forwarded the latest round to
	// the commander until a real answer, an artifact change or the user stopping
	// the wait: the honest state for "somebody got it, nobody has answered".
	Relayed *RelayView `json:"relayed,omitempty"`
	// Forwarding is true while the listener (not an agent poll) is what is
	// waiting for the user's feedback.
	Forwarding bool `json:"forwarding,omitempty"`
	// AwaitingSince is set from the user's Send until the agent answers, polls
	// again, the artifact changes, or the user stops waiting: the browser blocks
	// the review surface meanwhile, in every tab.
	AwaitingSince *time.Time `json:"awaiting_since,omitempty"`
	// Round is how many times the user has sent feedback; AnsweredThrough is the
	// newest round the agent answered.
	Round           int       `json:"round"`
	AnsweredThrough int       `json:"answered_through"`
	Pending         int       `json:"pending"`
	Queued          []Prompt  `json:"queued"`
	Transcript      []Message `json:"transcript"`
	// LayoutWarnings is the passive layout inbox. It is browser-only: a poll
	// never carries it and nothing in it reaches the agent until the user
	// queues it as a prompt.
	LayoutWarnings []LayoutWarningView `json:"layout_warnings"`
}

// RelayView is the browser's view of a relayed round.
type RelayView struct {
	Since time.Time `json:"since"`
	Round int       `json:"round"`
	// Commander is CommanderConnected or CommanderNone: whether a commander
	// session was known when the round was forwarded.
	Commander string `json:"commander"`
}

func (l *liveSession) snapshot(now time.Time, listenerGrace time.Duration) Snapshot {
	var workingUntil *time.Time
	if until := l.rec.DeliveredAt.Add(AgentWorkingWindow); l.pollers-l.relayPollers == 0 && l.rec.Status != StatusEnded &&
		!l.rec.DeliveredAt.IsZero() && now.Before(until) {
		workingUntil = &until
	}
	var awaitingSince *time.Time
	if since := l.rec.AwaitingSince; !since.IsZero() && l.rec.Status != StatusEnded {
		awaitingSince = &since
	}
	var relayed *RelayView
	if !l.rec.RelayedAt.IsZero() && l.rec.Status != StatusEnded && l.rec.Round > l.rec.AnsweredThrough {
		relayed = &RelayView{Since: l.rec.RelayedAt, Round: l.rec.RelayedRound, Commander: l.rec.Commander}
	}
	agentPollers := l.pollers - l.relayPollers
	forwarding := l.relayPollers > 0 && l.rec.Status != StatusEnded
	listener, listenerUntil := "none", (*time.Time)(nil)
	if l.rec.Status != StatusEnded {
		switch grace := l.lastListener.Add(listenerGrace); {
		case relayed != nil:
			listener = "relayed"
		case agentPollers == 0 && forwarding:
			listener = "forwarding"
		case l.pollers > 0:
			listener = "listening"
		case workingUntil != nil:
			listener = "working"
		case !l.lastListener.IsZero() && now.Before(grace):
			listener, listenerUntil = "waiting", &grace
		}
	}
	return Snapshot{
		Listener:        listener,
		ListenerUntil:   listenerUntil,
		WorkingUntil:    workingUntil,
		AwaitingSince:   awaitingSince,
		Round:           l.rec.Round,
		AnsweredThrough: l.rec.AnsweredThrough,
		Version:         l.version,
		Key:             l.rec.Key,
		File:            l.rec.File,
		Status:          l.rec.Status,
		EndedBy:         l.rec.EndedBy,
		Listening:       agentPollers > 0,
		Relayed:         relayed,
		Forwarding:      forwarding,
		Pending:         len(l.rec.Outbox),
		Queued:          append([]Prompt{}, l.rec.Queued...),
		Transcript:      append([]Message{}, l.transcript...),

		LayoutWarnings: layoutViews(l.layout.Warnings),
	}
}

// snapshotOf is l's snapshot after folding in the one thing the hub does not
// hear about on its own: the artifact file changing. A changed file answers the
// round in progress (the agent's reply to feedback is often just the edit), so
// it ends the wait and marks the round answered. Callers hold h.mu.
func (h *Hub) snapshotOf(l *liveSession) Snapshot {
	if l.rec.Round > l.rec.AnsweredThrough && l.rec.RoundArtifact != "" && artifactVersion(l.rec.File) != l.rec.RoundArtifact {
		if err := h.commit(l, func(rec *sessionRecord) error {
			rec.AnsweredThrough = rec.Round
			rec.AwaitingSince = time.Time{}
			rec.RelayedAt = time.Time{}
			rec.Commander = ""
			return nil
		}); err != nil {
			h.logf("recording the artifact change: %v", err)
		}
	}
	return l.snapshot(h.opts.Now(), h.opts.ListenerGrace)
}

// StopWaiting ends the wait for the agent on the user's say-so (the overlay's
// "Stop waiting"): the review surface is usable again in every tab, and the
// panel goes back to saying the agent is not listening. It reports whether
// there was anything to stop.
func (h *Hub) StopWaiting(key string) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	l, err := h.get(key)
	if err != nil {
		return false, err
	}
	if l.rec.AwaitingSince.IsZero() && l.rec.DeliveredAt.IsZero() && l.rec.RelayedAt.IsZero() {
		return false, nil
	}
	if err := h.commit(l, func(rec *sessionRecord) error {
		rec.AwaitingSince = time.Time{}
		rec.DeliveredAt = time.Time{}
		rec.RelayedAt = time.Time{}
		rec.Commander = ""
		return nil
	}); err != nil {
		return false, err
	}
	h.bump(l)
	return true, nil
}

// State returns key's snapshot. A request carrying the version it already
// has (since) blocks up to wait for something to change, which is how the
// browser stays live without polling on a timer; while it blocks, the
// browser counts as connected. A since the session does not know (0, or one
// from before a server restart) answers immediately.
func (h *Hub) State(ctx context.Context, key string, since int64, wait time.Duration) (Snapshot, error) {
	h.mu.Lock()
	l, err := h.get(key)
	if err != nil {
		h.mu.Unlock()
		return Snapshot{}, err
	}
	l.browsers++
	l.lastBrowser = h.opts.Now()
	h.notify()
	defer func() {
		h.mu.Lock()
		l.browsers--
		l.lastBrowser = h.opts.Now()
		h.notify()
		h.mu.Unlock()
	}()

	var timeout <-chan time.Time
	if wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		timeout = t.C
	}
	for {
		if l.version != since || wait <= 0 || ctx.Err() != nil {
			snap := h.snapshotOf(l)
			h.mu.Unlock()
			return snap, nil
		}
		changed := h.changed
		h.mu.Unlock()
		expired := false
		select {
		case <-changed:
		case <-timeout:
			expired = true
		case <-ctx.Done():
			expired = true
		}
		h.mu.Lock()
		if expired {
			snap := h.snapshotOf(l)
			h.mu.Unlock()
			return snap, nil
		}
	}
}

// Watch streams key's snapshots to fn: once right away, then again each time
// the session changes or tick elapses, until ctx is done or fn fails. fn sees
// every wake-up (not only real changes) so the caller can heartbeat and check
// things the hub does not own, like the artifact file; it dedupes on
// Snapshot.Version. While it runs the browser counts as connected, exactly as
// during a State long-poll. It returns nil when ctx ends.
func (h *Hub) Watch(ctx context.Context, key string, tick time.Duration, fn func(Snapshot) error) error {
	h.mu.Lock()
	l, err := h.get(key)
	if err != nil {
		h.mu.Unlock()
		return err
	}
	l.browsers++
	l.lastBrowser = h.opts.Now()
	h.notify()
	defer func() {
		h.mu.Lock()
		l.browsers--
		l.lastBrowser = h.opts.Now()
		h.notify()
		h.mu.Unlock()
	}()

	timer := time.NewTicker(tick)
	defer timer.Stop()
	for {
		snap := h.snapshotOf(l)
		changed := h.changed
		h.mu.Unlock()
		if err := fn(snap); err != nil {
			return err
		}
		select {
		case <-changed:
		case <-timer.C:
		case <-ctx.Done():
			return nil
		}
		h.mu.Lock()
	}
}

// Idle reports whether nothing has been connected (no poll, no browser) to
// any session for at least idleFor.
func (h *Hub) Idle(idleFor time.Duration) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, l := range h.sessions {
		if l.pollers > 0 || l.browsers > 0 {
			return false
		}
	}
	return h.opts.Now().Sub(h.lastActivity) >= idleFor
}

// Touch marks the server as having just done something useful, restarting
// its idle countdown.
func (h *Hub) Touch() {
	h.mu.Lock()
	h.lastActivity = h.opts.Now()
	h.mu.Unlock()
}

// artifactRevision resolves the revision of a reported artifact version,
// recording the version if it is new. Callers hold h.mu.
func (l *liveSession) artifactRevision(version string) (rev int, changed bool) {
	return l.layout.revisionOf(clip(version, 128))
}

func (h *Hub) saveLayoutState(l *liveSession) {
	if err := saveLayout(h.home, l.rec.Key, &l.layout); err != nil {
		h.logf("forum: %v", err)
	}
}

// RecordLayoutPass folds one browser diagnostic pass into key's layout inbox.
// It is passive by construction: it wakes browser tabs (the inbox changed)
// but never touches the queue or the outbox, so no poll can return because of
// it. A pass for an ended session is ignored.
func (h *Hub) RecordLayoutPass(key string, pass LayoutPass) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	l, err := h.get(key)
	if err != nil {
		return err
	}
	if l.rec.Status == StatusEnded {
		return nil
	}
	revision, versionsChanged := l.artifactRevision(pass.ArtifactVersion)
	next, changed := applyLayoutPass(l.layout.Warnings, pass, revision, h.opts.Now().UTC())
	if !changed && !versionsChanged {
		return nil
	}
	l.layout.Warnings = next
	h.saveLayoutState(l)
	if changed {
		h.bump(l)
	}
	return nil
}

// QueueLayoutWarnings turns the user's selection of layout issues into one
// ordinary queued prompt tagged "layout-warnings" (the user still has to send
// it) and marks those issues queued. Issues that can no longer be queued are
// skipped; if none can, it fails with ErrNothingToQueue.
func (h *Hub) QueueLayoutWarnings(key string, ids []string) (Prompt, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	l, err := h.get(key)
	if err != nil {
		return Prompt{}, err
	}
	if err := l.endedErr(); err != nil {
		return Prompt{}, err
	}
	selected := selectableLayoutWarnings(l.layout.Warnings, ids)
	if len(selected) == 0 {
		return Prompt{}, ErrNothingToQueue
	}
	selected, text, label, target, err := layoutPrompt(selected)
	if err != nil {
		return Prompt{}, err
	}
	p, err := normalizePrompt(PromptInput{Prompt: text, Tag: LayoutWarningsTag, Text: label, Target: target})
	if err != nil {
		return Prompt{}, err
	}
	// normalizePrompt drops an over-limit target silently; layoutPrompt fits it
	// already, so a missing one is a bug to surface, not to store.
	if len(p.Target) == 0 {
		return Prompt{}, ErrLayoutTargetTooLarge
	}
	for _, w := range selected {
		p.LayoutIDs = append(p.LayoutIDs, w.ID)
	}
	if p.UID, err = newID("pr_"); err != nil {
		return Prompt{}, err
	}
	now := h.opts.Now().UTC()
	p.QueuedAt = now
	if err := h.commit(l, func(rec *sessionRecord) error {
		if len(rec.Queued) >= maxQueuedPrompts {
			return ErrQueueFull
		}
		rec.Queued = append(rec.Queued, p)
		return nil
	}); err != nil {
		return Prompt{}, err
	}
	l.layout.Warnings = markLayoutQueued(l.layout.Warnings, selected, l.currentRevision(), now)
	h.saveLayoutState(l)
	h.bump(l)
	return p, nil
}

// currentRevision is the newest artifact revision recorded so far.
func (l *liveSession) currentRevision() int {
	if len(l.layout.Versions) == 0 {
		return l.layout.Base
	}
	return l.layout.Base + len(l.layout.Versions) - 1
}

// DismissLayoutWarning dismisses one issue for the current artifact revision.
func (h *Hub) DismissLayoutWarning(key, id string) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	l, err := h.get(key)
	if err != nil {
		return false, err
	}
	if err := l.endedErr(); err != nil {
		return false, err
	}
	next, changed := dismissLayoutWarning(l.layout.Warnings, id, l.currentRevision(), h.opts.Now().UTC())
	if !changed {
		return false, nil
	}
	l.layout.Warnings = next
	h.saveLayoutState(l)
	h.bump(l)
	return true, nil
}

// unreferencedLayoutIDs filters ids down to those no pending prompt (queued or
// waiting in the outbox) still carries, so removing one prompt never frees a
// warning another one is about.
func (l *liveSession) unreferencedLayoutIDs(ids []string) []string {
	held := map[string]bool{}
	for _, list := range [][]Prompt{l.rec.Queued, l.rec.Outbox, l.rec.Inflight} {
		for _, p := range list {
			for _, id := range p.LayoutIDs {
				held[id] = true
			}
		}
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !held[id] {
			out = append(out, id)
		}
	}
	return out
}

// releaseLayout returns warnings whose queued prompt was removed unsent to
// their previous state. Callers hold h.mu.
func (h *Hub) releaseLayout(l *liveSession, ids []string) {
	if len(ids) == 0 {
		return
	}
	next, changed := releaseLayoutQueued(l.layout.Warnings, ids, l.currentRevision(), h.opts.Now().UTC())
	if !changed {
		return
	}
	l.layout.Warnings = next
	h.saveLayoutState(l)
}
