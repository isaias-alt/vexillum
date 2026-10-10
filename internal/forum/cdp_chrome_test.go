//go:build unix

package forum_test

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The forum shows the artifact in an iframe with an opaque origin (sandbox
// without allow-same-origin), so nothing running in the chrome page, and no
// tool that reaches into the page's DOM, can click an element inside it. The
// artifact can only click itself, which is what the other real-Chrome tests
// do: they have the artifact report what it sees. A click a user would make is
// different: it is a trusted event that crosses the chrome, the SDK and the
// hub. The Chrome DevTools Protocol (CDP) can send it, because Input events
// are dispatched by the browser at viewport coordinates and routed to
// whichever frame is under that point, same-origin or not. The helper below
// finds an element's coordinates from a selector by running a script inside the
// sandboxed frame: Chrome puts it in its own process, which CDP exposes as an
// "iframe" target to attach to (with Page.createIsolatedWorld as the fallback
// when the frame shares the page's process).
//
// Standard library only, like every test here: cdpBrowser speaks just enough of
// the WebSocket protocol (RFC 6455) to talk to Chrome's --remote-debugging-port.

// cdpBrowser is headless Chrome with a DevTools connection and one attached page.
type cdpBrowser struct {
	t       *testing.T
	conn    net.Conn
	reader  *bufio.Reader
	nextID  int
	session string
	pageID  string // target id of the page, which is also its main frame's id
}

type cdpMessage struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Method string `json:"method"`
}

// startCDPChrome launches headless Chrome on a throwaway profile with the
// DevTools port on an ephemeral port, connects to it and opens url in a page.
func startCDPChrome(t *testing.T, chrome, url string) *cdpBrowser {
	t.Helper()
	profile := t.TempDir()
	cmd := exec.Command(chrome, "--headless=new", "--disable-gpu", "--no-sandbox", "--window-size=1100,800",
		"--remote-debugging-port=0", "--user-data-dir="+profile, "about:blank")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("chrome: %v", err)
	}
	t.Cleanup(func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); _ = cmd.Wait() })

	// Chrome writes "<port>\n<browser websocket path>" here once it listens.
	var port, path string
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		data, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort"))
		if err != nil {
			continue
		}
		if lines := strings.Split(strings.TrimSpace(string(data)), "\n"); len(lines) == 2 {
			port, path = lines[0], lines[1]
			break
		}
	}
	if port == "" {
		t.Fatal("Chrome never reported its DevTools port")
	}

	b := &cdpBrowser{t: t}
	b.dial("127.0.0.1:"+port, path)
	t.Cleanup(func() { _ = b.conn.Close() })

	var target struct {
		TargetID string `json:"targetId"`
	}
	b.call("", "Target.createTarget", map[string]any{"url": url}, &target)
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	b.call("", "Target.attachToTarget", map[string]any{"targetId": target.TargetID, "flatten": true}, &attached)
	b.session = attached.SessionID
	b.pageID = target.TargetID
	b.call(b.session, "Page.enable", nil, nil)
	return b
}

// dial opens the DevTools WebSocket.
func (b *cdpBrowser) dial(addr, path string) {
	b.t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		b.t.Fatalf("dialing DevTools: %v", err)
	}
	key := make([]byte, 16)
	_, _ = rand.Read(key)
	fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		path, addr, base64.StdEncoding.EncodeToString(key))
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		b.t.Fatalf("DevTools handshake: %v", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		b.t.Fatalf("DevTools handshake = %s", resp.Status)
	}
	b.conn, b.reader = conn, reader
}

// call sends one command (to the page session, or the browser when session is
// empty), reads until its response arrives (events in between are dropped) and
// decodes the result into out.
func (b *cdpBrowser) call(session, method string, params any, out any) {
	b.t.Helper()
	if err := b.try(session, method, params, out); err != nil {
		b.t.Fatalf("%s: %v", method, err)
	}
}

func (b *cdpBrowser) try(session, method string, params any, out any) error {
	b.nextID++
	id := b.nextID
	msg := map[string]any{"id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if session != "" {
		msg["sessionId"] = session
	}
	data, _ := json.Marshal(msg)
	_ = b.conn.SetDeadline(time.Now().Add(30 * time.Second))
	if err := writeWSFrame(b.conn, 0x1, data); err != nil {
		return err
	}
	for {
		payload, err := readWSMessage(b.reader, b.conn)
		if err != nil {
			return err
		}
		var m cdpMessage
		if json.Unmarshal(payload, &m) != nil || m.ID != id {
			continue
		}
		if m.Error != nil {
			return fmt.Errorf("%s (%d)", m.Error.Message, m.Error.Code)
		}
		if out != nil {
			return json.Unmarshal(m.Result, out)
		}
		return nil
	}
}

// evaluate runs expression in session, in the context contextID (0 is the
// session's main world), and decodes the JSON value it returns.
func (b *cdpBrowser) evaluate(session string, contextID int, expression string, out any) error {
	params := map[string]any{"expression": "JSON.stringify(" + expression + ")", "returnByValue": true, "awaitPromise": true}
	if contextID != 0 {
		params["contextId"] = contextID
	}
	var res struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
		Exception *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := b.try(session, "Runtime.evaluate", params, &res); err != nil {
		return err
	}
	if res.Exception != nil {
		return fmt.Errorf("%s %s", res.Exception.Text, res.Exception.Exception.Description)
	}
	if res.Result.Value == "" {
		return errors.New("expression returned nothing")
	}
	return json.Unmarshal([]byte(res.Result.Value), out)
}

type cdpRect struct{ X, Y, Width, Height float64 }

// frameScope says where to run a script that sees the artifact's DOM: a CDP
// session and, for the isolated-world case, a context id inside it.
type frameScope struct {
	session string
	context int
}

// artifactScope finds the artifact's iframe. Chrome gives a sandboxed
// (opaque-origin) frame its own process, and CDP lists it as a target of type
// "iframe" whose own session runs scripts in the artifact itself. When no such
// target exists the frame lives in the page's process and shows up in the
// page's frame tree instead, where an isolated world sees its DOM.
func (b *cdpBrowser) artifactScope() (frameScope, error) {
	var targets struct {
		TargetInfos []struct {
			TargetID      string `json:"targetId"`
			Type          string `json:"type"`
			ParentFrameID string `json:"parentFrameId"`
		} `json:"targetInfos"`
	}
	if err := b.try("", "Target.getTargets", nil, &targets); err != nil {
		return frameScope{}, err
	}
	for _, info := range targets.TargetInfos {
		if info.Type != "iframe" || info.ParentFrameID != b.pageID {
			continue
		}
		var attached struct {
			SessionID string `json:"sessionId"`
		}
		if err := b.try("", "Target.attachToTarget", map[string]any{"targetId": info.TargetID, "flatten": true}, &attached); err != nil {
			return frameScope{}, err
		}
		return frameScope{session: attached.SessionID}, nil
	}

	var tree struct {
		FrameTree struct {
			ChildFrames []struct {
				Frame struct {
					ID string `json:"id"`
				} `json:"frame"`
			} `json:"childFrames"`
		} `json:"frameTree"`
	}
	if err := b.try(b.session, "Page.getFrameTree", nil, &tree); err != nil {
		return frameScope{}, err
	}
	if len(tree.FrameTree.ChildFrames) != 1 {
		return frameScope{}, fmt.Errorf("no iframe target and %d child frames in the page", len(tree.FrameTree.ChildFrames))
	}
	var world struct {
		ExecutionContextID int `json:"executionContextId"`
	}
	if err := b.try(b.session, "Page.createIsolatedWorld", map[string]any{"frameId": tree.FrameTree.ChildFrames[0].Frame.ID, "worldName": "vx-test"}, &world); err != nil {
		return frameScope{}, err
	}
	return frameScope{session: b.session, context: world.ExecutionContextID}, nil
}

// click clicks the element matching selector inside the iframe matched by
// iframeSelector, the way a user would: a real mouse move, press and release
// at the element's center, trusted events that the browser routes into the
// sandboxed frame. It first waits for the layout to hold still, because the
// frame starts loading before the chrome around it has laid out and a click at
// coordinates read from a half-built page lands elsewhere: the element's
// position on the page must be non-empty and identical across two reads
// settleGap apart.
func (b *cdpBrowser) click(iframeSelector, selector string) {
	b.t.Helper()
	const settleGap = 250 * time.Millisecond
	var previous cdpRect
	var last error
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(settleGap) {
		rect, err := b.pageRect(iframeSelector, selector)
		if last = err; err != nil {
			previous = cdpRect{}
			continue
		}
		if rect != previous {
			previous = rect
			last = fmt.Errorf("layout still moving: %+v", rect)
			continue
		}
		x, y := rect.X+rect.Width/2, rect.Y+rect.Height/2
		for _, ev := range []map[string]any{
			{"type": "mouseMoved", "x": x, "y": y},
			{"type": "mousePressed", "x": x, "y": y, "button": "left", "buttons": 1, "clickCount": 1},
			{"type": "mouseReleased", "x": x, "y": y, "button": "left", "buttons": 0, "clickCount": 1},
		} {
			b.call(b.session, "Input.dispatchMouseEvent", ev, nil)
		}
		return
	}
	b.t.Fatalf("could not click %s in %s: %v", selector, iframeSelector, last)
}

// pageRect is the box of selector inside the artifact in the page's own
// viewport coordinates: the iframe's box (read from the chrome page) plus the
// element's box inside the frame (read from a script running in the frame).
// It fails while either is missing or empty.
func (b *cdpBrowser) pageRect(iframeSelector, selector string) (cdpRect, error) {
	const rectOf = `(() => { const r = document.querySelector(%s).getBoundingClientRect(); return {x: r.x, y: r.y, width: r.width, height: r.height}; })()`
	var frame, el cdpRect
	if err := b.evaluate(b.session, 0, fmt.Sprintf(rectOf, jsString(iframeSelector)), &frame); err != nil {
		return cdpRect{}, err
	}
	scope, err := b.artifactScope()
	if err != nil {
		return cdpRect{}, err
	}
	if err := b.evaluate(scope.session, scope.context, fmt.Sprintf(rectOf, jsString(selector)), &el); err != nil {
		return cdpRect{}, err
	}
	if frame.Width == 0 || frame.Height == 0 || el.Width == 0 || el.Height == 0 {
		return cdpRect{}, errors.New("not laid out yet")
	}
	return cdpRect{frame.X + el.X, frame.Y + el.Y, el.Width, el.Height}, nil
}

func jsString(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}

// writeWSFrame writes one masked client frame (RFC 6455 section 5).
func writeWSFrame(w io.Writer, opcode byte, payload []byte) error {
	header := []byte{0x80 | opcode}
	switch n := len(payload); {
	case n < 126:
		header = append(header, 0x80|byte(n))
	case n < 1<<16:
		header = append(header, 0x80|126, byte(n>>8), byte(n))
	default:
		header = append(header, 0x80|127)
		header = binary.BigEndian.AppendUint64(header, uint64(n))
	}
	mask := make([]byte, 4)
	_, _ = rand.Read(mask)
	header = append(header, mask...)
	masked := make([]byte, len(payload))
	for i, c := range payload {
		masked[i] = c ^ mask[i%4]
	}
	_, err := w.Write(append(header, masked...))
	return err
}

// readWSMessage reads one complete message, joining fragments and answering
// pings, and returns its payload.
func readWSMessage(r *bufio.Reader, conn net.Conn) ([]byte, error) {
	var message []byte
	for {
		head := make([]byte, 2)
		if _, err := io.ReadFull(r, head); err != nil {
			return nil, err
		}
		fin, opcode := head[0]&0x80 != 0, head[0]&0x0f
		length := uint64(head[1] & 0x7f)
		switch length {
		case 126:
			ext := make([]byte, 2)
			if _, err := io.ReadFull(r, ext); err != nil {
				return nil, err
			}
			length = uint64(binary.BigEndian.Uint16(ext))
		case 127:
			ext := make([]byte, 8)
			if _, err := io.ReadFull(r, ext); err != nil {
				return nil, err
			}
			length = binary.BigEndian.Uint64(ext)
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, err
		}
		switch opcode {
		case 0x8:
			return nil, io.EOF
		case 0x9:
			if err := writeWSFrame(conn, 0xA, payload); err != nil {
				return nil, err
			}
			continue
		case 0xA:
			continue
		}
		message = append(message, payload...)
		if fin {
			return message, nil
		}
	}
}

// An artifact whose button queues a prompt from its own click handler. The
// prompt records whether the click was a trusted (user) event, which a click
// the artifact makes on itself never is.
const clickArtifact = `<!doctype html><html><head><meta charset="utf-8"><title>c</title></head><body style="margin:0">
<button id="pick" style="position:absolute;left:30px;top:30px;width:140px;height:44px">Pick A</button>
<script>
document.getElementById("pick").addEventListener("click", (event) => {
  window.forum.queuePrompt("Go with A trusted=" + event.isTrusted, { tag: "choice" });
});
</script></body></html>`

// The reliable way to click inside the sandboxed artifact: CDP mouse input at
// the element's coordinates. The click reaches the artifact as a trusted event
// and travels through the SDK and the chrome to the hub's queue, which the
// test reads over the browser API. Real Chrome: chrome > sandboxed artifact.
func TestCDP_RealChrome_ClickInsideTheSandboxedArtifactQueuesAPrompt(t *testing.T) {
	chrome := headlessChrome(t)
	env := newEnv(t, time.Minute)
	key := env.open().Key
	env.setArtifact(clickArtifact)

	browser := startCDPChrome(t, chrome, env.ts.URL+"/session/"+key)
	browser.click("#artifact", "#pick")

	var queued []string
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		_, data := env.browser("GET", "/api/s/"+key+"/state", key, nil)
		var snap struct {
			Queued []struct {
				Prompt string `json:"prompt"`
			} `json:"queued"`
		}
		if err := json.Unmarshal(data, &snap); err != nil {
			t.Fatalf("state: %v (%s)", err, data)
		}
		queued = queued[:0]
		for _, q := range snap.Queued {
			queued = append(queued, q.Prompt)
		}
		if len(queued) > 0 {
			break
		}
	}
	if len(queued) != 1 || queued[0] != "Go with A trusted=true" {
		t.Fatalf("want one prompt queued by a trusted click, got %q", queued)
	}
}
