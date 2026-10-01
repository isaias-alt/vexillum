package forum_test

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// The selector and context logic is browser JS with no Go counterpart, so it
// is exercised under node against a fake DOM. Skipped where node is not
// installed: node is a test-time convenience, never a runtime dependency.
func TestSDK_SelectorAndContextHelpers(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	out, err := exec.Command(node, "testdata/sdk_dom_test.js", "assets/chrome/forum-sdk.js").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "ok") {
		t.Fatalf("sdk dom test failed: %v\n%s", err, out)
	}
}

// The chrome and the artifact SDK talk only through postMessage, in separate
// files and separate documents, so nothing but this test notices when one
// side posts a message the other never handles (the failure mode where
// annotating "does nothing"). Every message type one side sends must be
// handled by the other.
func TestAnnotationProtocol_BothSidesAgree(t *testing.T) {
	read := func(name string) string {
		data, err := os.ReadFile("assets/chrome/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	sdk, chrome := read("forum-sdk.js"), read("forum-chrome.js")

	sentBySDK := regexp.MustCompile(`post\("(forum:[a-z-]+)"`).FindAllStringSubmatch(sdk, -1)
	handledByChrome := regexp.MustCompile(`case "(forum:[a-z-]+)"`).FindAllStringSubmatch(chrome, -1)
	sentByChrome := regexp.MustCompile(`type: "(forum:[a-z-]+)"`).FindAllStringSubmatch(chrome, -1)
	handledBySDK := regexp.MustCompile(`message\.type === "(forum:[a-z-]+)"`).FindAllStringSubmatch(sdk, -1)

	set := func(matches [][]string) map[string]bool {
		out := map[string]bool{}
		for _, m := range matches {
			if strings.HasPrefix(m[1], "forum:rpc") { // F1's request/response channel, tested with the server
				continue
			}
			out[m[1]] = true
		}
		return out
	}
	check := func(sent, handled map[string]bool, from, to string) {
		if len(sent) == 0 {
			t.Fatalf("found no messages sent by the %s: the pattern in this test is stale", from)
		}
		for msg := range sent {
			if !handled[msg] {
				t.Errorf("the %s sends %s but the %s never handles it", from, msg, to)
			}
		}
	}
	check(set(sentBySDK), set(handledByChrome), "SDK", "chrome")
	check(set(sentByChrome), set(handledBySDK), "chrome", "SDK")
	for _, want := range []string{"forum:annotate", "forum:selection", "forum:mode", "forum:hold"} {
		if !set(sentBySDK)[want] && !set(sentByChrome)[want] {
			t.Errorf("%s is no longer part of the protocol", want)
		}
	}
}
