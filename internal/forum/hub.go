package forum

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
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
)

// HubOptions tune timing; zero values mean the production defaults. Tests
// shrink them so the grace and long-poll paths run in milliseconds.
type HubOptions struct {
	// BrowserGrace is how long a session's browser may be gone (no open
	// state request) before an agent poll reports browser_disconnected.
	BrowserGrace time.Duration
	// Now overrides the clock.
	Now func() time.Time
	// Logf receives non-fatal persistence problems.
	Logf func(format string, args ...any)
}

const defaultBrowserGrace = 30 * time.Second

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
	rec         sessionRecord
	transcript  []Message
	version     int64
	pollers     int
	browsers    int
	lastBrowser time.Time
}

// NewHub returns a Hub persisting under home (the ~/.vexillum directory).
func NewHub(home string, opts HubOptions) *Hub {
	if opts.BrowserGrace <= 0 {
		opts.BrowserGrace = defaultBrowserGrace
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
	l := &liveSession{
		rec:         *rec,
		transcript:  transcript,
		version:     h.opts.Now().UnixNano(),
		lastBrowser: h.opts.Now(),
	}
	h.sessions[key] = l
	return l, nil
}

// commit clones l's record, applies fn, persists the result, and only then
// replaces the in-memory copy - a failed write leaves memory and disk both
// at the previous state. Callers hold h.mu.
func (h *Hub) commit(l *liveSession, fn func(rec *sessionRecord) error) error {
	next := l.rec
	next.Queued = append([]Prompt(nil), l.rec.Queued...)
	next.Outbox = append([]Prompt(nil), l.rec.Outbox...)
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

// appendTranscript adds msgs to l's transcript, keeping the newest
// maxTranscriptItems. A failed write is logged, not returned: the prompts
// the messages mirror are already safely queued, and the next successful
// write persists the full transcript anyway. Callers hold h.mu.
func (h *Hub) appendTranscript(l *liveSession, msgs ...Message) error {
	next := append(append([]Message(nil), l.transcript...), msgs...)
	if over := len(next) - maxTranscriptItems; over > 0 {
		next = next[over:]
	}
	err := saveTranscript(h.home, l.rec.Key, next)
	l.transcript = next
	if err != nil {
		h.logf("forum: %v", err)
	}
	return err
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
	// A fresh open starts the browser's grace period: the page is about to
	// load (or already is), and a poll must not call it disconnected before
	// it ever had the chance to connect.
	l.lastBrowser = h.opts.Now()
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
	if err := h.commit(l, func(rec *sessionRecord) error {
		for i := range rec.Queued {
			if rec.Queued[i].UID == uid {
				rec.Queued = append(rec.Queued[:i], rec.Queued[i+1:]...)
				return nil
			}
		}
		return ErrNoPrompt
	}); err != nil {
		return err
	}
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
		if end {
			rec.Status = StatusEnded
			rec.EndedBy = EndedByUser
		}
		return nil
	}); err != nil {
		return 0, err
	}
	if len(sent) > 0 {
		msgs := make([]Message, 0, len(sent))
		for _, p := range sent {
			id, _ := newID("m_")
			msgs = append(msgs, Message{ID: id, Role: RoleUser, Text: clip(p.Prompt, 4000), Tag: p.Tag, Selector: p.Selector, At: h.opts.Now().UTC()})
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
	if err := h.appendTranscript(l, Message{ID: id, Role: RoleAgent, Text: text, At: h.opts.Now().UTC()}); err != nil {
		return err
	}
	h.bump(l)
	return nil
}

// PollResult is what one agent poll delivers.
type PollResult struct {
	Key     string
	File    string
	Status  string
	EndedBy string
	Prompts []Prompt
}

// Poll blocks until key's user has sent feedback, the session ended, the
// browser has been gone past the grace period, or timeout elapsed (0 means
// wait indefinitely). Delivered prompts are consumed. While it waits the
// session counts as "listening" in the browser. A canceled ctx consumes
// nothing.
func (h *Hub) Poll(ctx context.Context, key string, timeout time.Duration) (PollResult, error) {
	h.mu.Lock()
	l, err := h.get(key)
	if err != nil {
		h.mu.Unlock()
		return PollResult{}, err
	}
	l.pollers++
	h.bump(l)
	var deadline time.Time
	if timeout > 0 {
		deadline = h.opts.Now().Add(timeout)
	}
	defer func() {
		h.mu.Lock()
		l.pollers--
		h.bump(l)
		h.mu.Unlock()
	}()

	for {
		if err := ctx.Err(); err != nil {
			h.mu.Unlock()
			return PollResult{}, err
		}
		now := h.opts.Now()
		res := PollResult{Key: key, File: l.rec.File, EndedBy: l.rec.EndedBy}

		if len(l.rec.Outbox) > 0 {
			taken := append([]Prompt(nil), l.rec.Outbox...)
			if err := h.commit(l, func(rec *sessionRecord) error {
				rec.Outbox = []Prompt{}
				return nil
			}); err != nil {
				h.mu.Unlock()
				return PollResult{}, err
			}
			res.Prompts = taken
			res.Status = PollFeedback
			if l.rec.Status == StatusEnded {
				res.Status = PollEnded
			}
			h.bump(l)
			h.mu.Unlock()
			return res, nil
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
}

// Restore puts prompts a poll consumed back at the front of key's outbox,
// for a poll whose client went away before it could read the response.
func (h *Hub) Restore(key string, prompts []Prompt) error {
	if len(prompts) == 0 {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	l, err := h.get(key)
	if err != nil {
		return err
	}
	if err := h.commit(l, func(rec *sessionRecord) error {
		rec.Outbox = append(append([]Prompt(nil), prompts...), rec.Outbox...)
		return nil
	}); err != nil {
		return err
	}
	h.bump(l)
	return nil
}

// Snapshot is the browser's view of a session.
type Snapshot struct {
	Version    int64     `json:"version"`
	Key        string    `json:"key"`
	File       string    `json:"file"`
	Status     string    `json:"status"`
	EndedBy    string    `json:"ended_by,omitempty"`
	Listening  bool      `json:"listening"`
	Pending    int       `json:"pending"`
	Queued     []Prompt  `json:"queued"`
	Transcript []Message `json:"transcript"`
}

func (l *liveSession) snapshot() Snapshot {
	return Snapshot{
		Version:    l.version,
		Key:        l.rec.Key,
		File:       l.rec.File,
		Status:     l.rec.Status,
		EndedBy:    l.rec.EndedBy,
		Listening:  l.pollers > 0,
		Pending:    len(l.rec.Outbox),
		Queued:     append([]Prompt{}, l.rec.Queued...),
		Transcript: append([]Message{}, l.transcript...),
	}
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
			snap := l.snapshot()
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
			snap := l.snapshot()
			h.mu.Unlock()
			return snap, nil
		}
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
