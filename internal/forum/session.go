package forum

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Session lifecycle states.
const (
	StatusOpen  = "open"
	StatusEnded = "ended"
)

// Who ended a session. A session the user ended from the browser is not
// reopened by a plain `vexillum forum <file>` - see Hub.Open.
const (
	EndedByUser  = "user"
	EndedByAgent = "agent"
)

// Transcript roles.
const (
	RoleUser  = "user"
	RoleAgent = "agent"
)

// Bounds on everything a browser (or the artifact running inside it) can
// push into a session. Whatever lands in a prompt reaches the agent as the
// user's own instructions, and it is persisted to disk wholesale, so every
// field is capped instead of trusted.
const (
	maxPromptChars     = 20000
	maxSelectorChars   = 512
	maxTextChars       = 500
	maxTagChars        = 64
	maxQueueKeyChars   = 512
	maxTargetBytes     = 8192
	maxQueuedPrompts   = 200
	maxReplyChars      = 64000
	maxTranscriptItems = 500
	// maxTranscriptBytes bounds the transcript file (forum-tool's chat cap).
	maxTranscriptBytes = 5 << 20
	// attachmentOnlyPrompt is the prompt text of a message that is only images.
	attachmentOnlyPrompt = "(see the attached image)"
	defaultPromptTag     = "feedback"
)

var tagPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)

// Prompt is one piece of feedback from the browser for the agent: free text
// typed in the composer, or whatever the artifact queued through
// window.forum.queuePrompt. UID is assigned by the server and unique within
// a session. QueueKey is browser-facing only: a later unsent prompt with
// the same key replaces the earlier one, and it is never shown to the agent.
type Prompt struct {
	UID      string          `json:"uid"`
	Prompt   string          `json:"prompt"`
	Tag      string          `json:"tag"`
	Selector string          `json:"selector,omitempty"`
	Text     string          `json:"text,omitempty"`
	Target   json.RawMessage `json:"target,omitempty"`
	QueueKey string          `json:"queue_key,omitempty"`
	QueuedAt time.Time       `json:"queued_at"`
	// Attachments are images stored under the session (see attachments.go).
	Attachments []Attachment `json:"attachments,omitempty"`
	// LayoutIDs links a prompt to the layout warnings it queued. Only
	// QueueLayoutWarnings sets it (PromptInput has no such field), so nothing
	// the artifact queues can claim, or release, a warning. Never delivered to
	// the agent.
	LayoutIDs []string `json:"layout_ids,omitempty"`
}

// PromptInput is the client-controllable part of a Prompt.
type PromptInput struct {
	Prompt   string          `json:"prompt"`
	Tag      string          `json:"tag"`
	Selector string          `json:"selector"`
	Text     string          `json:"text"`
	Target   json.RawMessage `json:"target"`
	QueueKey string          `json:"queue_key"`
	// Attachments are the ids of images already uploaded for this session.
	Attachments []string `json:"attachments"`
}

// Message is one transcript entry: a prompt the user sent, or a reply the
// agent posted. Agent text is markdown (rendered sanitized in the browser);
// user text is always plain.
type Message struct {
	ID       string    `json:"id"`
	Role     string    `json:"role"`
	Text     string    `json:"text"`
	Tag      string    `json:"tag,omitempty"`
	Selector string    `json:"selector,omitempty"`
	At       time.Time `json:"at"`
	// Attachments mirror the images the prompt carried.
	Attachments []Attachment `json:"attachments,omitempty"`
}

// sessionRecord is the durable, restart-proof part of a session
// (<state>/forums/<key>/session.json). Queued holds prompts the user has
// drafted but not sent; Outbox holds prompts the user sent that no poll has
// consumed yet. Both survive a server restart.
type sessionRecord struct {
	Version   int       `json:"version"`
	Key       string    `json:"key"`
	File      string    `json:"file"`
	Token     string    `json:"token"`
	Status    string    `json:"status"`
	EndedBy   string    `json:"ended_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Queued    []Prompt  `json:"queued"`
	Outbox    []Prompt  `json:"outbox"`
	// DeliveredAt is when a poll last took prompts from the outbox and the
	// agent has not polled again or replied since. It is what lets the browser
	// say "your agent received your message and is working" instead of "not
	// listening" in the gap between the delivering poll returning and the
	// next one starting. Persisted so it survives a server restart.
	DeliveredAt time.Time `json:"delivered_at,omitempty"`
}

const sessionRecordVersion = 1

func newID(prefix string) (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating id: %w", err)
	}
	return prefix + hex.EncodeToString(b[:]), nil
}

func newToken() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func clip(s string, max int) string {
	if r := []rune(s); len(r) > max {
		return string(r[:max])
	}
	return s
}

// errBadPrompt marks a prompt the server refuses as malformed (a client
// error, unlike the hub's state errors).
type errBadPrompt struct{ reason string }

func (e *errBadPrompt) Error() string { return e.reason }

func isBadPrompt(err error) bool {
	var bad *errBadPrompt
	return errors.As(err, &bad)
}

// normalizePrompt validates and bounds in, returning the prompt to store
// (without uid/queued_at, which the hub assigns). An empty prompt is an
// error; an unusable tag falls back to the default instead of failing.
func normalizePrompt(in PromptInput) (Prompt, error) {
	text := strings.TrimSpace(in.Prompt)
	if text == "" && len(in.Attachments) > 0 {
		text = attachmentOnlyPrompt
	}
	if text == "" {
		return Prompt{}, &errBadPrompt{reason: "prompt is empty"}
	}
	for _, id := range in.Attachments {
		if !ValidAttachmentID(id) {
			return Prompt{}, &errBadPrompt{reason: "invalid attachment id"}
		}
	}
	tag := strings.TrimSpace(in.Tag)
	if tag == "" || len(tag) > maxTagChars || !tagPattern.MatchString(tag) {
		tag = defaultPromptTag
	}
	p := Prompt{
		Prompt:   clip(text, maxPromptChars),
		Tag:      tag,
		Selector: clip(strings.TrimSpace(in.Selector), maxSelectorChars),
		Text:     clip(strings.Join(strings.Fields(in.Text), " "), maxTextChars),
		QueueKey: clip(strings.TrimSpace(in.QueueKey), maxQueueKeyChars),
	}
	if len(in.Target) > 0 && len(in.Target) <= maxTargetBytes && string(in.Target) != "null" {
		var compact bytes.Buffer
		if err := json.Compact(&compact, in.Target); err == nil {
			p.Target = json.RawMessage(compact.Bytes())
		}
	}
	return p, nil
}
