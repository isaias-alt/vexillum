package forum

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
)

// Image attachments: pictures the reviewer pastes, drops or picks in the
// conversation panel. Each one is written under the owning session's state
// directory and later handed to the agent as an absolute local path.
//
// The reviewer's browser is not trusted with any part of the stored name. The
// id is minted here, the extension comes from sniffing the bytes (a filename or
// Content-Type from the client is never consulted), and only four raster
// formats are accepted. SVG is deliberately absent: it can carry script.
const (
	maxAttachmentBytes     = 10 << 20  // largest single image
	maxImagesPerPrompt     = 4         // images one prompt may carry
	maxPendingImages       = 16        // uploaded but not yet on any prompt
	maxSessionImageBytes   = 256 << 20 // everything stored for one session
	orphanImageGracePeriod = time.Hour // an unreferenced image survives this long
)

// Errors the HTTP layer translates into status codes.
var (
	ErrUnsupportedImage   = errors.New("unsupported image: send a PNG, JPEG, GIF or WebP")
	ErrAttachmentsFull    = errors.New("this session's attachment storage is full")
	ErrTooManyStaged      = errors.New("too many images waiting to be sent")
	ErrNoAttachment       = errors.New("no such attachment")
	ErrAttachmentInUse    = errors.New("attachment already belongs to a message")
	ErrTooManyAttachments = errors.New("too many images on one message")
)

// Attachment is the record of one stored image. Path is the agent-facing file
// location; it is only filled in on the copy a poll delivers and is never
// written to disk with the session.
type Attachment struct {
	ID    string `json:"id"`
	Mime  string `json:"mime"`
	Bytes int64  `json:"bytes"`
	Path  string `json:"path,omitempty"`
}

var attachmentIDShape = regexp.MustCompile(`^at_[0-9a-f]{16}$`)

// ValidAttachmentID reports whether id looks like something newID("at_") made.
func ValidAttachmentID(id string) bool { return attachmentIDShape.MatchString(id) }

// imageFormat ties a MIME type to the extension it is stored under and to the
// test that recognises its leading bytes.
type imageFormat struct {
	mime  string
	ext   string
	match func(head []byte) bool
}

func hasPrefix(prefix ...byte) func([]byte) bool {
	return func(head []byte) bool { return bytes.HasPrefix(head, prefix) }
}

var imageFormats = []imageFormat{
	{"image/png", ".png", hasPrefix(0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n')},
	{"image/jpeg", ".jpg", hasPrefix(0xff, 0xd8, 0xff)},
	{"image/gif", ".gif", func(head []byte) bool {
		return bytes.HasPrefix(head, []byte("GIF87a")) || bytes.HasPrefix(head, []byte("GIF89a"))
	}},
	{"image/webp", ".webp", func(head []byte) bool {
		return len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP"
	}},
}

// detectImage identifies data by content alone.
func detectImage(data []byte) (imageFormat, bool) {
	for _, f := range imageFormats {
		if f.match(data) {
			return f, true
		}
	}
	return imageFormat{}, false
}

// storedImage is a located attachment file.
type storedImage struct {
	path string
	mime string
	size int64
}

func attachmentsDir(home, key string) string {
	return filepath.Join(sessionDir(home, key), "attachments")
}

// locateAttachment finds the file behind id for a session, whichever of the
// accepted extensions it was stored with. Malformed keys and ids never touch
// the filesystem.
func locateAttachment(home, key, id string) (storedImage, error) {
	if !ValidSessionKey(key) || !ValidAttachmentID(id) {
		return storedImage{}, ErrNoAttachment
	}
	for _, f := range imageFormats {
		path := filepath.Join(attachmentsDir(home, key), id+f.ext)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return storedImage{path: path, mime: f.mime, size: info.Size()}, nil
		}
	}
	return storedImage{}, ErrNoAttachment
}

// claimedAttachments is the set of ids some message still points at: a queued
// prompt, one waiting to be delivered (outbox or in flight), or a transcript
// entry.
func (l *liveSession) claimedAttachments() map[string]bool {
	claimed := map[string]bool{}
	claim := func(list []Attachment) {
		for _, a := range list {
			claimed[a.ID] = true
		}
	}
	for _, p := range l.rec.Queued {
		claim(p.Attachments)
	}
	for _, p := range l.rec.Outbox {
		claim(p.Attachments)
	}
	for _, p := range l.rec.Inflight {
		claim(p.Attachments)
	}
	for _, m := range l.transcript {
		claim(m.Attachments)
	}
	return claimed
}

// storedAttachmentInfo maps id to file info for everything on disk for l.
func (h *Hub) storedAttachmentInfo(l *liveSession) map[string]os.FileInfo {
	found := map[string]os.FileInfo{}
	entries, err := os.ReadDir(attachmentsDir(h.home, l.rec.Key))
	if err != nil {
		return found
	}
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		if e.IsDir() || !ValidAttachmentID(id) {
			continue
		}
		if info, err := e.Info(); err == nil && info.Mode().IsRegular() {
			found[id] = info
		}
	}
	return found
}

// sweepAttachments deletes images no message claims once they are older than
// the grace period. Callers hold h.mu.
func (h *Hub) sweepAttachments(l *liveSession) {
	claimed := l.claimedAttachments()
	now := h.opts.Now()
	for id, info := range h.storedAttachmentInfo(l) {
		if !claimed[id] && now.Sub(info.ModTime()) >= orphanImageGracePeriod {
			h.deleteAttachmentFile(l.rec.Key, id)
		}
	}
}

// dropAttachments deletes the given images immediately, sparing any that a
// message still claims (the user removed a queued prompt or discarded a staged
// image). Callers hold h.mu and have already updated the session state.
func (h *Hub) dropAttachments(l *liveSession, ids []string) {
	if len(ids) == 0 {
		return
	}
	claimed := l.claimedAttachments()
	for _, id := range ids {
		if !claimed[id] {
			h.deleteAttachmentFile(l.rec.Key, id)
		}
	}
}

func (h *Hub) deleteAttachmentFile(key, id string) {
	img, err := locateAttachment(h.home, key, id)
	if err != nil {
		return
	}
	if err := os.Remove(img.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		h.logf("forum: removing attachment %s: %v", id, err)
	}
}

func attachmentIDs(list []Attachment) []string {
	ids := make([]string, len(list))
	for i, a := range list {
		ids[i] = a.ID
	}
	return ids
}

// AddAttachment checks that data really is an accepted image and stores it as
// a pending attachment of the session, ready to be put on a prompt.
func (h *Hub) AddAttachment(key string, data []byte) (Attachment, error) {
	format, ok := detectImage(data)
	if !ok {
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

	claimed := l.claimedAttachments()
	var used int64
	pending := 0
	for storedID, info := range h.storedAttachmentInfo(l) {
		used += info.Size()
		if !claimed[storedID] {
			pending++
		}
	}
	if pending >= maxPendingImages {
		return Attachment{}, ErrTooManyStaged
	}
	if used+int64(len(data)) > maxSessionImageBytes {
		return Attachment{}, ErrAttachmentsFull
	}

	dir := attachmentsDir(h.home, key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Attachment{}, fmt.Errorf("creating attachments dir: %w", err)
	}
	if err := atomicfile.Write(filepath.Join(dir, id+format.ext), data); err != nil {
		return Attachment{}, fmt.Errorf("storing attachment: %w", err)
	}
	h.notify()
	return Attachment{ID: id, Mime: format.mime, Bytes: int64(len(data))}, nil
}

// AttachmentFile returns where one of the session's attachments lives and
// what type it is.
func (h *Hub) AttachmentFile(key, id string) (path, mime string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, err := h.get(key); err != nil {
		return "", "", err
	}
	img, err := locateAttachment(h.home, key, id)
	return img.path, img.mime, err
}

// AttachmentPath is the absolute local path the agent reads an attachment
// from, or "" when it no longer exists.
func (h *Hub) AttachmentPath(key, id string) string {
	img, err := locateAttachment(h.home, key, id)
	if err != nil {
		return ""
	}
	return img.path
}

// RemoveAttachment discards a pending attachment the user took back before
// sending. One that already belongs to a prompt or message is refused.
func (h *Hub) RemoveAttachment(key, id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	l, err := h.get(key)
	if err != nil {
		return err
	}
	if _, err := locateAttachment(h.home, key, id); err != nil {
		return err
	}
	if l.claimedAttachments()[id] {
		return ErrAttachmentInUse
	}
	h.deleteAttachmentFile(key, id)
	return nil
}

// resolveAttachments turns the ids a prompt names into stored attachments. It
// refuses unknown ids, ids another message already owns and prompts with more
// than maxImagesPerPrompt images; repeated ids are collapsed. A prompt that
// replaces an earlier one (same queue key) may keep that one's images, so
// those are passed as replacing. Callers hold h.mu.
func (h *Hub) resolveAttachments(l *liveSession, ids []string, replacing []Attachment) ([]Attachment, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > maxImagesPerPrompt {
		return nil, ErrTooManyAttachments
	}
	claimed := l.claimedAttachments()
	for _, a := range replacing {
		delete(claimed, a.ID)
	}
	resolved := make([]Attachment, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		img, err := locateAttachment(h.home, l.rec.Key, id)
		if err != nil {
			return nil, err
		}
		if claimed[id] {
			return nil, ErrAttachmentInUse
		}
		resolved = append(resolved, Attachment{ID: id, Mime: img.mime, Bytes: img.size})
	}
	return resolved, nil
}
