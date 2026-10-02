package forum

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
)

// Images the reviewer pastes or drops into the conversation are stored under
// the session's own state directory (<home>/forums/<key>/attachments/) and
// handed to the agent as local file paths. Everything is bounded and nothing
// about the on-disk name comes from the client: the id is server-generated,
// the extension is derived from the image's own bytes (never from a filename
// or Content-Type the client claims), and only a fixed set of raster formats
// is accepted - notably not SVG, which can carry script.
//
// The size, count and total-disk limits follow forum-tool's attachment store
// (upstream, MIT, v0.1.80; see THIRD-PARTY-NOTICES.md).
const (
	maxAttachmentBytes      = 10 << 20  // per image
	maxAttachmentsPerPrompt = 4         // images on one prompt
	maxStagedAttachments    = 16        // uploaded, not yet part of a prompt
	maxAttachmentDiskBytes  = 256 << 20 // per session
	// stagedTTL is how long an attachment that no prompt or transcript
	// message references survives before a sweep removes it.
	stagedTTL = time.Hour
)

// Attachment errors the HTTP layer maps to statuses.
var (
	ErrUnsupportedImage   = errors.New("unsupported image: send a PNG, JPEG, GIF or WebP")
	ErrAttachmentsFull    = errors.New("this session's attachment storage is full")
	ErrTooManyStaged      = errors.New("too many images waiting to be sent")
	ErrNoAttachment       = errors.New("no such attachment")
	ErrAttachmentInUse    = errors.New("attachment already belongs to a message")
	ErrTooManyAttachments = errors.New("too many images on one message")
)

// Attachment describes one stored image. Path is the agent-facing local
// file path; it is filled in only on the copy delivered by a poll, never
// persisted.
type Attachment struct {
	ID    string `json:"id"`
	Mime  string `json:"mime"`
	Bytes int64  `json:"bytes"`
	Path  string `json:"path,omitempty"`
}

var attachmentIDPattern = regexp.MustCompile(`^at_[0-9a-f]{16}$`)

// ValidAttachmentID reports whether id has the shape newID("at_") produces.
func ValidAttachmentID(id string) bool { return attachmentIDPattern.MatchString(id) }

// imageTypes maps the accepted formats to the extension they are stored under.
var imageTypes = []struct{ mime, ext string }{
	{"image/png", ".png"},
	{"image/jpeg", ".jpg"},
	{"image/gif", ".gif"},
	{"image/webp", ".webp"},
}

// sniffImage detects the format from the leading bytes alone. It returns
// ("", "") for anything that is not one of the accepted raster formats.
func sniffImage(data []byte) (mime, ext string) {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png", ".png"
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return "image/jpeg", ".jpg"
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "image/gif", ".gif"
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "image/webp", ".webp"
	}
	return "", ""
}

func attachmentsDir(home, key string) string {
	return filepath.Join(sessionDir(home, key), "attachments")
}

// findAttachment resolves id to its file, trying each accepted extension.
func findAttachment(home, key, id string) (path, mime string, size int64, err error) {
	if !ValidSessionKey(key) || !ValidAttachmentID(id) {
		return "", "", 0, ErrNoAttachment
	}
	for _, t := range imageTypes {
		p := filepath.Join(attachmentsDir(home, key), id+t.ext)
		if info, serr := os.Stat(p); serr == nil && info.Mode().IsRegular() {
			return p, t.mime, info.Size(), nil
		}
	}
	return "", "", 0, ErrNoAttachment
}

// referencedAttachments is every attachment id a queued prompt, an
// undelivered prompt or a transcript message still points at.
func (l *liveSession) referencedAttachments() map[string]bool {
	refs := map[string]bool{}
	add := func(list []Attachment) {
		for _, a := range list {
			refs[a.ID] = true
		}
	}
	for _, p := range l.rec.Queued {
		add(p.Attachments)
	}
	for _, p := range l.rec.Outbox {
		add(p.Attachments)
	}
	for _, p := range l.rec.Inflight {
		add(p.Attachments)
	}
	for _, m := range l.transcript {
		add(m.Attachments)
	}
	return refs
}

// attachmentFiles lists the stored attachments of l as id -> file info.
func (h *Hub) attachmentFiles(l *liveSession) map[string]os.FileInfo {
	out := map[string]os.FileInfo{}
	entries, err := os.ReadDir(attachmentsDir(h.home, l.rec.Key))
	if err != nil {
		return out
	}
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if !ValidAttachmentID(id) || e.IsDir() {
			continue
		}
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() {
			out[id] = info
		}
	}
	return out
}

// sweepAttachments removes stored images that nothing references and that
// are older than stagedTTL. Anything a queued prompt, an undelivered prompt
// or the transcript still points at is kept. Callers hold h.mu.
func (h *Hub) sweepAttachments(l *liveSession) {
	refs := l.referencedAttachments()
	now := h.opts.Now()
	for id, info := range h.attachmentFiles(l) {
		if refs[id] || now.Sub(info.ModTime()) < stagedTTL {
			continue
		}
		h.removeAttachmentFile(l.rec.Key, id)
	}
}

// dropAttachments deletes ids right away unless something still references
// them (the user removed a queued prompt, or discarded a staged image).
// Callers hold h.mu and have already updated the state.
func (h *Hub) dropAttachments(l *liveSession, ids []string) {
	if len(ids) == 0 {
		return
	}
	refs := l.referencedAttachments()
	for _, id := range ids {
		if !refs[id] {
			h.removeAttachmentFile(l.rec.Key, id)
		}
	}
}

func (h *Hub) removeAttachmentFile(key, id string) {
	if p, _, _, err := findAttachment(h.home, key, id); err == nil {
		if rerr := os.Remove(p); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
			h.logf("forum: removing attachment %s: %v", id, rerr)
		}
	}
}

func attachmentIDs(list []Attachment) []string {
	ids := make([]string, 0, len(list))
	for _, a := range list {
		ids = append(ids, a.ID)
	}
	return ids
}

// AddAttachment validates data as an image by its content and stores it as a
// staged attachment of key's session, ready to be attached to a prompt.
func (h *Hub) AddAttachment(key string, data []byte) (Attachment, error) {
	mime, ext := sniffImage(data)
	if mime == "" {
		return Attachment{}, ErrUnsupportedImage
	}
	if len(data) > maxAttachmentBytes {
		return Attachment{}, errors.New("image is too large")
	}
	id, err := newID("at_")
	if err != nil {
		return Attachment{}, err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	l, err := h.get(key)
	if err != nil {
		return Attachment{}, err
	}
	if err := l.endedErr(); err != nil {
		return Attachment{}, err
	}
	h.sweepAttachments(l)
	refs := l.referencedAttachments()
	var total int64
	staged := 0
	for fid, info := range h.attachmentFiles(l) {
		total += info.Size()
		if !refs[fid] {
			staged++
		}
	}
	if staged >= maxStagedAttachments {
		return Attachment{}, ErrTooManyStaged
	}
	if total+int64(len(data)) > maxAttachmentDiskBytes {
		return Attachment{}, ErrAttachmentsFull
	}
	if err := os.MkdirAll(attachmentsDir(h.home, key), 0o755); err != nil {
		return Attachment{}, err
	}
	if err := atomicfile.Write(filepath.Join(attachmentsDir(h.home, key), id+ext), data); err != nil {
		return Attachment{}, err
	}
	h.notify()
	return Attachment{ID: id, Mime: mime, Bytes: int64(len(data))}, nil
}

// AttachmentFile returns the stored file for one of key's attachments.
func (h *Hub) AttachmentFile(key, id string) (path, mime string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, err := h.get(key); err != nil {
		return "", "", err
	}
	p, m, _, err := findAttachment(h.home, key, id)
	return p, m, err
}

// RemoveAttachment discards a staged attachment the user took back before
// sending. One that already belongs to a prompt or message is refused.
func (h *Hub) RemoveAttachment(key, id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	l, err := h.get(key)
	if err != nil {
		return err
	}
	if _, _, _, err := findAttachment(h.home, key, id); err != nil {
		return err
	}
	if l.referencedAttachments()[id] {
		return ErrAttachmentInUse
	}
	h.removeAttachmentFile(key, id)
	return nil
}

// resolveAttachments turns the ids a prompt names into stored attachments,
// refusing unknown ids, repeats, ones already used by another message, and
// more than maxAttachmentsPerPrompt. Callers hold h.mu.
func (h *Hub) resolveAttachments(l *liveSession, ids []string, replacing []Attachment) ([]Attachment, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > maxAttachmentsPerPrompt {
		return nil, ErrTooManyAttachments
	}
	refs := l.referencedAttachments()
	// A prompt that replaces another (same queue key) may keep its images.
	for _, a := range replacing {
		delete(refs, a.ID)
	}
	seen := map[string]bool{}
	out := make([]Attachment, 0, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		_, mime, size, err := findAttachment(h.home, l.rec.Key, id)
		if err != nil {
			return nil, err
		}
		if refs[id] {
			return nil, ErrAttachmentInUse
		}
		out = append(out, Attachment{ID: id, Mime: mime, Bytes: size})
	}
	return out, nil
}

// AttachmentPath is the absolute local path of one of key's attachments, for
// the agent. It returns "" if the attachment no longer exists.
func (h *Hub) AttachmentPath(key, id string) string {
	p, _, _, err := findAttachment(h.home, key, id)
	if err != nil {
		return ""
	}
	return p
}
