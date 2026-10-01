package forum_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/forum"
)

var (
	pngBytes  = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 64)...)
	jpgBytes  = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0}, 64)...)
	gifBytes  = append([]byte("GIF89a"), bytes.Repeat([]byte{0}, 64)...)
	webpBytes = append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), bytes.Repeat([]byte{0}, 64)...)
)

// upload sends raw bytes the way the chrome does, optionally lying about the
// type and name.
func (e *testEnv) upload(key string, data []byte, contentType string) (*http.Response, []byte) {
	e.t.Helper()
	token, _ := e.hub.Token(key)
	req, _ := http.NewRequest("POST", e.ts.URL+"/api/s/"+key+"/attachments", bytes.NewReader(data))
	req.Header.Set("X-Forum-Token", token)
	req.Header.Set("Origin", e.ts.URL)
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("upload: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, body
}

func (e *testEnv) uploadOK(key string, data []byte) forum.Attachment {
	e.t.Helper()
	resp, body := e.upload(key, data, "application/octet-stream")
	if resp.StatusCode != 200 {
		e.t.Fatalf("upload = %d %s", resp.StatusCode, body)
	}
	var out struct {
		Attachment forum.Attachment `json:"attachment"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		e.t.Fatal(err)
	}
	return out.Attachment
}

func TestAttachments_TypeIsDecidedByContentNotByWhatTheClientClaims(t *testing.T) {
	env := newEnv(t, time.Minute)
	key := env.open().Key

	for name, tc := range map[string]struct {
		data []byte
		mime string
		ext  string
	}{"png": {pngBytes, "image/png", ".png"}, "jpeg": {jpgBytes, "image/jpeg", ".jpg"}, "gif": {gifBytes, "image/gif", ".gif"}, "webp": {webpBytes, "image/webp", ".webp"}} {
		// Claims to be a script; the bytes say image.
		resp, body := env.upload(key, tc.data, "text/html")
		if resp.StatusCode != 200 {
			t.Fatalf("%s: upload = %d %s", name, resp.StatusCode, body)
		}
		var out struct{ Attachment forum.Attachment }
		_ = json.Unmarshal(body, &out)
		if out.Attachment.Mime != tc.mime || !forum.ValidAttachmentID(out.Attachment.ID) {
			t.Errorf("%s: attachment = %+v", name, out.Attachment)
		}
		path := env.hub.AttachmentPath(key, out.Attachment.ID)
		if !strings.HasSuffix(path, tc.ext) || !strings.HasPrefix(path, filepath.Join(env.home, "forums", key, "attachments")) {
			t.Errorf("%s: stored at %q, want the session's attachments dir with %s", name, path, tc.ext)
		}
	}

	for name, data := range map[string][]byte{
		"svg with script": []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"html":            []byte("<html><script>alert(1)</script></html>"),
		"empty":           {},
		"text":            []byte("hello"),
		"png-named-text":  []byte("png"),
	} {
		resp, body := env.upload(key, data, "image/png") // claims to be a PNG
		if resp.StatusCode != http.StatusUnsupportedMediaType || !strings.Contains(string(body), "unsupported_type") {
			t.Errorf("%s: upload = %d %s, want 415", name, resp.StatusCode, body)
		}
	}
	entries, _ := os.ReadDir(filepath.Join(env.home, "forums", key, "attachments"))
	if len(entries) != 4 {
		t.Errorf("only the four real images should be on disk, got %d files", len(entries))
	}
}

func TestAttachments_SizeLimitIsEnforcedWhileReading(t *testing.T) {
	env := newEnv(t, time.Minute)
	key := env.open().Key
	big := append(append([]byte(nil), pngBytes...), bytes.Repeat([]byte{1}, 10<<20)...)
	resp, body := env.upload(key, big, "image/png")
	if resp.StatusCode != http.StatusRequestEntityTooLarge || !strings.Contains(string(body), "too_large") {
		t.Fatalf("oversized upload = %d %s", resp.StatusCode, body)
	}
	exact := append(append([]byte(nil), pngBytes...), bytes.Repeat([]byte{1}, 10<<20-len(pngBytes))...)
	if resp, body := env.upload(key, exact, "image/png"); resp.StatusCode != 200 {
		t.Fatalf("an image exactly at the limit should be accepted: %d %s", resp.StatusCode, body)
	}
}

func TestAttachments_CountLimits(t *testing.T) {
	env := newEnv(t, time.Minute)
	key := env.open().Key

	// At most 16 uploaded images waiting to be sent.
	var ids []string
	for i := 0; i < 16; i++ {
		ids = append(ids, env.uploadOK(key, pngBytes).ID)
	}
	if resp, body := env.upload(key, pngBytes, "image/png"); resp.StatusCode != http.StatusConflict || !strings.Contains(string(body), "attachments_full") {
		t.Errorf("the 17th staged upload = %d %s", resp.StatusCode, body)
	}

	// At most 4 per message.
	resp, body := env.browser("POST", "/api/s/"+key+"/queue", key, map[string]any{"prompt": "x", "attachments": ids[:5]})
	if resp.StatusCode != 400 || !strings.Contains(string(body), "too_many_attachments") {
		t.Errorf("5 attachments on one prompt = %d %s", resp.StatusCode, body)
	}
	if resp, body = env.browser("POST", "/api/s/"+key+"/queue", key, map[string]any{"prompt": "x", "attachments": ids[:4]}); resp.StatusCode != 200 {
		t.Fatalf("4 attachments = %d %s", resp.StatusCode, body)
	}
	// An attachment belongs to one message.
	if resp, body = env.browser("POST", "/api/s/"+key+"/queue", key, map[string]any{"prompt": "again", "attachments": ids[:1]}); resp.StatusCode != 409 || !strings.Contains(string(body), "attachment_in_use") {
		t.Errorf("reusing an attachment = %d %s", resp.StatusCode, body)
	}
	// Made-up and malformed ids.
	for _, bad := range []string{"at_0000000000000000", "../../etc/passwd", "at_zz", ""} {
		resp, _ = env.browser("POST", "/api/s/"+key+"/queue", key, map[string]any{"prompt": "y", "attachments": []string{bad}})
		if resp.StatusCode != 400 && resp.StatusCode != 404 {
			t.Errorf("attachment id %q = %d, want a client error", bad, resp.StatusCode)
		}
	}
}

func TestAttachments_SessionDiskCap(t *testing.T) {
	env := newEnv(t, time.Minute)
	key := env.open().Key
	chunk := append(append([]byte(nil), pngBytes...), bytes.Repeat([]byte{2}, 9<<20)...)
	// 256 MiB per session: referencing each upload from a prompt frees the staged slots.
	var hit bool
	for i := 0; i < 40 && !hit; i++ {
		att := env.uploadOK2(key, chunk)
		if att == nil {
			hit = true
			break
		}
		if resp, body := env.browser("POST", "/api/s/"+key+"/queue", key, map[string]any{"prompt": "p", "attachments": []string{att.ID}}); resp.StatusCode != 200 {
			t.Fatalf("queue = %d %s", resp.StatusCode, body)
		}
	}
	if !hit {
		t.Error("the session's disk cap was never reached")
	}
}

func (e *testEnv) uploadOK2(key string, data []byte) *forum.Attachment {
	e.t.Helper()
	resp, body := e.upload(key, data, "image/png")
	if resp.StatusCode == http.StatusConflict {
		return nil
	}
	if resp.StatusCode != 200 {
		e.t.Fatalf("upload = %d %s", resp.StatusCode, body)
	}
	var out struct{ Attachment forum.Attachment }
	_ = json.Unmarshal(body, &out)
	return &out.Attachment
}

func TestAttachments_QueueSendPollDeliversLocalPaths(t *testing.T) {
	env := newEnv(t, time.Minute)
	key := env.open().Key
	keepBrowserOn(t, env, key)

	a := env.uploadOK(key, pngBytes)
	b := env.uploadOK(key, jpgBytes)
	if resp, body := env.browser("POST", "/api/s/"+key+"/queue", key, map[string]any{"prompt": "see these", "tag": "message", "attachments": []string{a.ID, b.ID}}); resp.StatusCode != 200 {
		t.Fatalf("queue = %d %s", resp.StatusCode, body)
	}
	// An attachments-only message is allowed.
	c := env.uploadOK(key, gifBytes)
	if resp, body := env.browser("POST", "/api/s/"+key+"/queue", key, map[string]any{"prompt": "  ", "attachments": []string{c.ID}}); resp.StatusCode != 200 {
		t.Fatalf("attachment-only queue = %d %s", resp.StatusCode, body)
	}

	// The queue snapshot shows them (no paths: those are for the agent only).
	_, data := env.browser("GET", "/api/s/"+key+"/state", key, nil)
	if strings.Contains(string(data), env.home) {
		t.Errorf("the browser snapshot must not expose local paths:\n%s", data)
	}
	var snap forum.Snapshot
	_ = json.Unmarshal(data, &snap)
	if len(snap.Queued) != 2 || len(snap.Queued[0].Attachments) != 2 || snap.Queued[0].Attachments[0].Mime != "image/png" {
		t.Fatalf("queued = %+v", snap.Queued)
	}

	if resp, body := env.browser("POST", "/api/s/"+key+"/send", key, map[string]any{}); resp.StatusCode != 200 {
		t.Fatalf("send = %d %s", resp.StatusCode, body)
	}
	_, data = env.agent("POST", "/api/agent/poll", map[string]any{"file": env.file, "timeout_ms": 2000})
	var poll forum.PollResponse
	if err := json.Unmarshal(data, &poll); err != nil || poll.Status != forum.PollFeedback || len(poll.Prompts) != 2 {
		t.Fatalf("poll = %s", data)
	}
	got := poll.Prompts[0].Attachments
	if len(got) != 2 || got[0].Path != env.hub.AttachmentPath(key, a.ID) {
		t.Fatalf("delivered attachments = %+v", got)
	}
	for _, att := range got {
		raw, err := os.ReadFile(att.Path)
		if err != nil || len(raw) == 0 {
			t.Errorf("the agent cannot read %s: %v", att.Path, err)
		}
	}

	text := forum.FormatPoll(env.file, poll)
	for _, want := range []string{"attachments[2]:", "- path: " + got[0].Path, "type: image/png", "type: image/jpeg", "bytes: ", "read each image from its path"} {
		if !strings.Contains(text, want) {
			t.Errorf("poll text missing %q:\n%s", want, text)
		}
	}
	if poll.Prompts[1].Prompt == "" {
		t.Error("an attachment-only message should still carry a prompt line")
	}
	// The transcript mirrors the attachments for the thumbnails.
	_, data = env.browser("GET", "/api/s/"+key+"/state", key, nil)
	_ = json.Unmarshal(data, &snap)
	if len(snap.Transcript) != 2 || len(snap.Transcript[0].Attachments) != 2 {
		t.Errorf("transcript = %+v", snap.Transcript)
	}
}

func TestAttachments_ServeRemoveAndSecurity(t *testing.T) {
	env := newEnv(t, time.Minute)
	key := env.open().Key
	att := env.uploadOK(key, pngBytes)
	path := "/api/s/" + key + "/attachments/" + att.ID

	resp, body := env.browser("GET", path, key, nil)
	if resp.StatusCode != 200 || !bytes.Equal(body, pngBytes) || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("GET = %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("served attachment lacks hardening headers: %v", resp.Header)
	}

	// Same guarantees as every browser route: token, same origin.
	for name, mutate := range map[string]func(*http.Request){
		"no token":     func(r *http.Request) { r.Header.Set("Origin", env.ts.URL) },
		"wrong token":  func(r *http.Request) { r.Header.Set("X-Forum-Token", "nope"); r.Header.Set("Origin", env.ts.URL) },
		"cross origin": func(r *http.Request) { tok, _ := env.hub.Token(key); r.Header.Set("X-Forum-Token", tok); r.Header.Set("Origin", "http://evil.example") },
	} {
		for _, method := range []string{"GET", "DELETE"} {
			if resp, _ := env.browserWith(method, path, nil, mutate); resp.StatusCode != 401 && resp.StatusCode != 403 {
				t.Errorf("%s %s = %d, want rejection", method, name, resp.StatusCode)
			}
		}
		req, _ := http.NewRequest("POST", env.ts.URL+"/api/s/"+key+"/attachments", bytes.NewReader(pngBytes))
		mutate(req)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != 401 && r.StatusCode != 403 {
			t.Errorf("upload with %s = %d, want rejection", name, r.StatusCode)
		}
	}
	// The agent token is not a way in.
	if resp, _ := env.agent("POST", "/api/s/"+key+"/attachments", nil); resp.StatusCode == 200 {
		t.Error("agent token must not open the browser API")
	}
	// Traversal and unknown ids never reach the filesystem.
	for _, id := range []string{"..%2f..%2fsession.json", "at_ffffffffffffffff", "x"} {
		if resp, _ := env.browser("GET", "/api/s/"+key+"/attachments/"+id, key, nil); resp.StatusCode != 404 {
			t.Errorf("GET attachment %q = %d, want 404", id, resp.StatusCode)
		}
	}

	// A staged image can be taken back; the file goes with it.
	disk := env.hub.AttachmentPath(key, att.ID)
	if resp, _ := env.browser("DELETE", path, key, nil); resp.StatusCode != 200 {
		t.Fatalf("DELETE = %d", resp.StatusCode)
	}
	if _, err := os.Stat(disk); !os.IsNotExist(err) {
		t.Error("removed attachment still on disk")
	}
	if resp, _ := env.browser("GET", path, key, nil); resp.StatusCode != 404 {
		t.Errorf("GET after DELETE = %d", resp.StatusCode)
	}

	// One that belongs to a queued message cannot be pulled out from under it,
	// but removing the queued message frees it.
	att = env.uploadOK(key, pngBytes)
	_, data := env.browser("POST", "/api/s/"+key+"/queue", key, map[string]any{"prompt": "m", "attachments": []string{att.ID}})
	var q struct{ Prompt forum.Prompt }
	_ = json.Unmarshal(data, &q)
	path = "/api/s/" + key + "/attachments/" + att.ID
	if resp, _ := env.browser("DELETE", path, key, nil); resp.StatusCode != 409 {
		t.Errorf("DELETE of a queued message's image = %d, want 409", resp.StatusCode)
	}
	disk = env.hub.AttachmentPath(key, att.ID)
	env.browser("DELETE", "/api/s/"+key+"/queue/"+q.Prompt.UID, key, nil)
	if _, err := os.Stat(disk); !os.IsNotExist(err) {
		t.Error("removing the queued message should delete its image")
	}
}

func TestAttachments_CannotUploadToAnEndedSession(t *testing.T) {
	env := newEnv(t, time.Minute)
	key := env.open().Key
	env.browser("POST", "/api/s/"+key+"/end", key, nil)
	if resp, body := env.upload(key, pngBytes, "image/png"); resp.StatusCode != http.StatusConflict || !strings.Contains(string(body), "ended") {
		t.Errorf("upload after end = %d %s", resp.StatusCode, body)
	}
}

// Queued prompts, their images and the transcript survive a server restart
// (a new Hub over the same state directory).
func TestAttachments_SurviveARestart(t *testing.T) {
	env := newEnv(t, time.Minute)
	key := env.open().Key
	att := env.uploadOK(key, pngBytes)
	if resp, body := env.browser("POST", "/api/s/"+key+"/queue", key, map[string]any{"prompt": "keep", "attachments": []string{att.ID}}); resp.StatusCode != 200 {
		t.Fatalf("queue = %d %s", resp.StatusCode, body)
	}
	env.browser("POST", "/api/s/"+key+"/send", key, map[string]any{})
	staged := env.uploadOK(key, jpgBytes)

	again := forum.NewHub(env.home, forum.HubOptions{})
	if _, err := again.Open(env.file, false); err != nil {
		t.Fatal(err)
	}
	res, err := again.Poll(t.Context(), key, time.Second)
	if err != nil || res.Status != forum.PollFeedback || len(res.Prompts) != 1 || len(res.Prompts[0].Attachments) != 1 || res.Prompts[0].Attachments[0].ID != att.ID {
		t.Fatalf("after restart poll = %+v, err %v", res, err)
	}
	if p := again.AttachmentPath(key, att.ID); p == "" {
		t.Error("delivered attachment lost across restart")
	}
	if p := again.AttachmentPath(key, staged.ID); p == "" {
		t.Error("staged attachment lost across restart")
	}
}

// keepBrowserOn marks the session's browser as connected for the test.
func keepBrowserOn(t *testing.T, env *testEnv, key string) { keepBrowser(t, env.hub, key) }
