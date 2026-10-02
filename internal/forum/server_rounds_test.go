package forum_test

import (
	"net/http"
	"testing"
	"time"
)

// "Stop waiting" is a browser route behind the session token, and it gives the
// review surface back for the whole session.
func TestServer_StopWaitingClearsTheWaitAndNeedsTheToken(t *testing.T) {
	env := newEnv(t, time.Minute)
	key := env.open().Key
	env.browser("POST", "/api/s/"+key+"/queue", key, map[string]any{"prompt": "go"})
	if resp, data := env.browser("POST", "/api/s/"+key+"/send", key, map[string]any{}); resp.StatusCode != 200 {
		t.Fatalf("send = %d %s", resp.StatusCode, data)
	}
	snap := env.snapshot(key)
	if snap.AwaitingSince == nil || snap.Round != 1 {
		t.Fatalf("after send: awaiting %v round %d", snap.AwaitingSince, snap.Round)
	}
	if resp, _ := env.browserWith("POST", "/api/s/"+key+"/stop-waiting", nil, func(*http.Request) {}); resp.StatusCode == 200 {
		t.Fatal("stop-waiting without the session token must be refused")
	}
	if resp, data := env.browser("POST", "/api/s/"+key+"/stop-waiting", key, nil); resp.StatusCode != 200 {
		t.Fatalf("stop-waiting = %d %s", resp.StatusCode, data)
	}
	if snap := env.snapshot(key); snap.AwaitingSince != nil || snap.Round != 1 {
		t.Fatalf("after stop: awaiting %v round %d", snap.AwaitingSince, snap.Round)
	}
}
