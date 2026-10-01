package forum_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/forum"
)

// The annotation bodies below have the exact shape the chrome builds in
// buildAnnotationPrompt (forum-chrome.js): the note as prompt, plus the
// tag, selector and text the artifact side reported.
func TestAnnotation_ElementAndTextSelectionReachThePollWithSelectorAndText(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	base := "/api/s/" + open.Key

	element := map[string]any{"prompt": "Make this button larger", "tag": "button", "selector": `button[data-testid="save"]`, "text": "Save changes"}
	selection := map[string]any{"prompt": "Reword this", "tag": "text", "selector": "main > p:nth-of-type(2)", "text": "the selected sentence"}
	for _, body := range []map[string]any{element, selection} {
		if resp, data := env.browser("POST", base+"/queue", open.Key, body); resp.StatusCode != 200 {
			t.Fatalf("queue = %d %s", resp.StatusCode, data)
		}
	}

	// Both show in the queue the user can edit, and can be removed from it.
	_, data := env.browser("GET", base+"/state", open.Key, nil)
	var snap forum.Snapshot
	if err := json.Unmarshal(data, &snap); err != nil || len(snap.Queued) != 2 {
		t.Fatalf("state = %s (%v)", data, err)
	}
	if snap.Queued[0].Selector != `button[data-testid="save"]` || snap.Queued[1].Tag != "text" {
		t.Errorf("queued = %+v", snap.Queued)
	}
	extra := map[string]any{"prompt": "never mind", "tag": "div", "selector": "div", "text": "x"}
	_, data = env.browser("POST", base+"/queue", open.Key, extra)
	var added struct {
		Prompt forum.Prompt `json:"prompt"`
	}
	if err := json.Unmarshal(data, &added); err != nil || added.Prompt.UID == "" {
		t.Fatalf("queue response = %s", data)
	}
	if resp, data := env.browser("DELETE", base+"/queue/"+added.Prompt.UID, open.Key, nil); resp.StatusCode != 200 {
		t.Fatalf("remove = %d %s", resp.StatusCode, data)
	}

	if resp, data := env.browser("POST", base+"/send", open.Key, map[string]any{}); resp.StatusCode != 200 {
		t.Fatalf("send = %d %s", resp.StatusCode, data)
	}
	_, data = env.agent("POST", "/api/agent/poll", map[string]any{"file": env.file, "timeout_ms": 2000})
	var polled forum.PollResponse
	if err := json.Unmarshal(data, &polled); err != nil || len(polled.Prompts) != 2 {
		t.Fatalf("poll = %s (%v)", data, err)
	}

	out := forum.FormatPoll(env.file, polled)
	for _, want := range []string{
		"    tag: button\n", "    selector: button[data-testid=\"save\"]\n", "    text: Save changes\n",
		"    tag: text\n", "    selector: main > p:nth-of-type(2)\n", "    text: the selected sentence\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("poll output missing %q:\n%s", want, out)
		}
	}
}

func TestAnnotation_QueueRefusesWithoutTheSessionGuarantees(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	body := map[string]any{"prompt": "x", "tag": "p", "selector": "p", "text": "x"}
	path := "/api/s/" + open.Key + "/queue"

	if resp, _ := env.browserWith("POST", path, body, func(r *http.Request) { r.Header.Set("Origin", env.ts.URL) }); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token = %d, want 401", resp.StatusCode)
	}
	token, _ := env.hub.Token(open.Key)
	if resp, _ := env.browserWith("POST", path, body, func(r *http.Request) {
		r.Header.Set("X-Forum-Token", token)
		r.Header.Set("Origin", "http://evil.example")
	}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin = %d, want 403", resp.StatusCode)
	}
}

func TestAnnotation_OversizedContextIsBounded(t *testing.T) {
	env := newEnv(t, time.Minute)
	open := env.open()
	long := strings.Repeat("a ", 1000)
	resp, data := env.browser("POST", "/api/s/"+open.Key+"/queue", open.Key, map[string]any{"prompt": "x", "tag": "text", "selector": strings.Repeat("div > ", 300), "text": long})
	if resp.StatusCode != 200 {
		t.Fatalf("queue = %d %s", resp.StatusCode, data)
	}
	var out struct {
		Prompt forum.Prompt `json:"prompt"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Prompt.Selector) > 512 || len(out.Prompt.Text) > 500 {
		t.Errorf("selector %d / text %d chars, want <= 512 / 500", len(out.Prompt.Selector), len(out.Prompt.Text))
	}
}
