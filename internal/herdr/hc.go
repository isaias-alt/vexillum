package herdr

// The hc backend: an experimental implementation of Client and Locator on
// herdr-core's "hc" CLI, so vexillum can run from a Herdr desktop app terminal.
// Selected with VX_BACKEND=hc (see backend.go); the herdr TUI path (CLI) is
// untouched.
//
// hc has terminals and nothing else: no workspaces, tabs or panes, and an agent
// is just a program a terminal runs. The mapping onto Client is therefore:
//
//   - CreateTab starts one shell terminal (hc run, no argv) per soldier. Its
//     tab id and pane id are both the terminal's address (t7).
//   - AgentStart types the agent's launch line into that shell (hc send), with
//     the environment as an "env K=V" prefix, because hc run has no --env and a
//     terminal inherits the core's environment, not the caller's.
//   - A soldier's agent name is a label (vexillum.agent), not an hc name: hc
//     names are unique but cannot be set after the terminal exists, while the
//     soldier chooses (and may have to change) its name after CreateTab.
//   - Ownership is a set of labels (vexillum.managed, vexillum.project,
//     vexillum.task) checked before anything is sent to or killed in a terminal
//     addressed by name or tab id.
//   - Status is the OSC 7501 word the program reported: idle, working, blocked,
//     done (not idle), error (new), and "unknown" for a program that never
//     reported (hc says null). Claude Code and pi report; opencode does not.
//
// See the experimental-backend notes in the commit history for the probes this
// was built on; the facts that matter are repeated next to the code that uses
// them.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Labels vexillum puts on the hc terminals it starts. herdr.space and
// herdr.tab are the app's own: a terminal that carries herdr.space shows up in
// the Herdr app, in that space (and tab).
const (
	LabelManaged  = "vexillum.managed"
	LabelProject  = "vexillum.project"
	LabelTask     = "vexillum.task"
	LabelAgent    = "vexillum.agent"
	LabelLaunched = "vexillum.launched"

	labelSpace = "herdr.space"
	labelTab   = "herdr.tab"

	// SpacePrefix starts the space label vexillum picks for a project
	// (vx-<project>): the Herdr app creates a space for a label it does not know.
	SpacePrefix = "vx-"
)

// HCSpace is the space label (herdr.space) soldiers of the project at
// projectRoot are shown under. The project label is the same string without the
// prefix.
func HCSpace(projectRoot string) string {
	return SpacePrefix + filepath.Base(projectRoot)
}

var (
	// hcPollInterval is how often the hc backend looks at a terminal while it
	// waits for a status (hc has no wait for "working").
	hcPollInterval = 250 * time.Millisecond
	// hcStallWindow is how long a prompt may go without any sign the agent took
	// it (no working status, no finished turn) before it is reported as
	// agent_prompt_stalled, the same startup race herdr reports.
	hcStallWindow = 5 * time.Second
	// hcStartWindow is how long AgentStart waits for the agent to report its
	// first status before it gives up with agent_not_ready.
	hcStartWindow = 10 * time.Second
	// hcShellWindow is how long AgentStart waits for the new shell to draw its
	// prompt before typing the launch line anyway.
	hcShellWindow = 8 * time.Second
	// hcBlockedGrace is how long AgentWait ignores a "blocked" reading it sees
	// right away: a modal answered with keys stays blocked for a moment.
	hcBlockedGrace = 600 * time.Millisecond
)

// HC is the hc (herdr-core) backend. The zero value works: it finds the hc
// binary on PATH and pins the session of the terminal it runs in. NewHC is the
// same thing; use a pointer, since CreateTab remembers the environment
// AgentStart needs.
type HC struct {
	// Bin is the hc binary. Empty means find one (ResolveHC) on first use.
	Bin string
	// Session is the hc session every call is pinned to. Empty means the one
	// the calling terminal runs in (PinnedSession).
	Session string

	mu    sync.Mutex
	bin   string
	names map[string]string   // agent name -> terminal address
	envs  map[string][]string // terminal address -> env AgentStart sets
}

// NewHC returns the hc backend.
func NewHC() *HC { return &HC{} }

// TaskBinder is implemented by a backend that marks the terminal it started for
// a task with that task's id, so a later call can tell the terminal is the
// task's. internal/soldier calls it right after CreateTab when the client has it.
type TaskBinder interface {
	BindTask(paneID, taskID string) error
}

var _ Backend = (*HC)(nil)
var _ TaskBinder = (*HC)(nil)

// ---- binary, session and environment ----

var hcVersionRE = regexp.MustCompile(`^hc (\d+\.\d+\.\d+\S*)$`)

// HCVersionOf runs "<bin> --version" and returns the hc version it prints. The
// herdr TUI prints "herdr x.y.z" and is rejected: only "hc x.y.z" is the core.
func HCVersionOf(bin string) (string, error) {
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("running %s --version: %w", bin, err)
	}
	return ParseHCVersion(string(out))
}

// ParseHCVersion extracts x.y.z from "hc x.y.z".
func ParseHCVersion(out string) (string, error) {
	trimmed := strings.TrimSpace(out)
	m := hcVersionRE.FindStringSubmatch(trimmed)
	if m == nil {
		return "", fmt.Errorf("--version printed %q, not \"hc x.y.z\" (is this the herdr TUI?)", trimmed)
	}
	return m[1], nil
}

// ResolveHC finds the hc binary the way First Mate does: "herdr-core" first,
// then "hc", and only a binary whose --version prints "hc x.y.z" counts (the
// herdr TUI answers to neither name but another tool might). lookPath and
// version are injected so tests control them; HCVersionOf is the real one.
func ResolveHC(lookPath func(string) (string, error), version func(bin string) (string, error)) (bin, ver string, err error) {
	var rejected []string
	for _, name := range []string{"herdr-core", "hc"} {
		path, lookErr := lookPath(name)
		if lookErr != nil {
			continue
		}
		v, verErr := version(path)
		if verErr != nil {
			rejected = append(rejected, fmt.Sprintf("%s: %v", path, verErr))
			continue
		}
		return path, v, nil
	}
	if len(rejected) > 0 {
		return "", "", fmt.Errorf("no usable herdr-core binary: %s", strings.Join(rejected, "; "))
	}
	return "", "", errors.New("herdr-core is not on PATH (looked for herdr-core and hc)")
}

// binary returns the hc binary, resolving it once.
func (h *HC) binary() (string, error) {
	if h.Bin != "" {
		return h.Bin, nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.bin != "" {
		return h.bin, nil
	}
	bin, _, err := ResolveHC(exec.LookPath, HCVersionOf)
	if err != nil {
		return "", err
	}
	h.bin = bin
	return bin, nil
}

// PinnedSession is the hc session a process running in an hc terminal talks
// to: HC_SESSION when set, else the session its HC_TERMINAL_SOCKET belongs to
// (<dir>/<session>/<session>.sock), else "default". Pinning it explicitly means
// an inherited HC_SOCKET or terminal socket can never send a call somewhere
// else.
func PinnedSession(getenv func(string) string) string {
	if s := getenv("HC_SESSION"); s != "" {
		return s
	}
	if sock := getenv("HC_TERMINAL_SOCKET"); sock != "" {
		dir := filepath.Base(filepath.Dir(sock))
		if base := strings.TrimSuffix(filepath.Base(sock), ".sock"); base == dir && base != "" {
			return base
		}
	}
	return "default"
}

// hcStripped are the variables removed from the environment of every hc call:
// they each override or shadow the session pin (HC_SOCKET beats HC_SESSION), or
// make hc treat the call as made from inside one of the terminals.
var hcStripped = map[string]bool{
	"HC_SOCKET":          true,
	"HC_SESSION":         true,
	"HC_TERMINAL_ID":     true,
	"HC_TERMINAL_SOCKET": true,
	"HC_SCOPE":           true,
}

// hcEnv is environ with the stripped variables removed and HC_SESSION pinned.
func hcEnv(environ []string, session string) []string {
	out := make([]string, 0, len(environ)+1)
	for _, kv := range environ {
		key, _, _ := strings.Cut(kv, "=")
		if hcStripped[key] {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "HC_SESSION="+session)
}

func (h *HC) session() string {
	if h.Session != "" {
		return h.Session
	}
	return PinnedSession(os.Getenv)
}

// ---- running hc ----

// hcErrorCodes maps hc's error kinds to the codes the Is* predicates of this
// package recognize, so the callers that handle a herdr error handle the hc one
// the same way.
var hcErrorCodes = map[string]string{
	"not_found":       "agent_not_found",
	"name_taken":      "agent_name_taken",
	"blocked":         "agent_blocked",
	"finished":        "agent_not_running",
	"status_mismatch": "status_mismatch",
}

// hcExitCodes are the exit codes that identify an error whatever its JSON says.
var hcExitCodes = map[int]string{
	3:  "not_found",
	9:  "blocked",
	10: "status_mismatch",
	11: "finished",
}

// hcError builds the error of a failed hc call. The exit code is read first and
// the JSON body, which hc prints on stderr, only adds the message.
func hcError(args []string, exitCode int, stderr []byte) error {
	var body struct {
		Error struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
			Hint    string `json:"hint"`
		} `json:"error"`
	}
	_ = json.Unmarshal(stderr, &body)
	kind := hcExitCodes[exitCode]
	if kind == "" {
		kind = body.Error.Kind
	}
	if kind == "" {
		return fmt.Errorf("hc %v failed (exit %d): %s", args, exitCode, strings.TrimSpace(string(stderr)))
	}
	code := hcErrorCodes[kind]
	if code == "" {
		code = kind
	}
	msg := body.Error.Message
	if msg == "" {
		msg = kind
	}
	if body.Error.Hint != "" {
		msg += " (" + body.Error.Hint + ")"
	}
	return &APIError{Code: code, Message: msg}
}

// exec runs hc with args in the pinned session and returns its stdout.
func (h *HC) exec(args ...string) ([]byte, error) {
	bin, err := h.binary()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = hcEnv(os.Environ(), h.session())
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return nil, fmt.Errorf("running hc %v: %w", args, err)
		}
		return nil, hcError(args, exitErr.ExitCode(), stderr.Bytes())
	}
	return stdout.Bytes(), nil
}

// execJSON is exec with stdout decoded into v.
func (h *HC) execJSON(v any, args ...string) error {
	out, err := h.exec(args...)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(out, v); err != nil {
		return fmt.Errorf("parsing hc %v response: %w (raw: %s)", args, err, bytes.TrimSpace(out))
	}
	return nil
}

// ---- terminal records ----

type hcStatus struct {
	State string `json:"state"`
	App   string `json:"app"`
	Kind  string `json:"kind"`
	Msg   string `json:"msg"`
}

type hcForeground struct {
	Argv []string `json:"argv"`
	Cwd  string   `json:"cwd"`
	Name string   `json:"name"`
	PID  int      `json:"pid"`
}

// hcTerminal is the part of "hc get" vexillum reads.
type hcTerminal struct {
	Address    string            `json:"address"`
	Argv       []string          `json:"argv"`
	Cwd        string            `json:"cwd"`
	Labels     map[string]string `json:"labels"`
	PID        int               `json:"pid"`
	State      string            `json:"state"`
	Reported   bool              `json:"reported"`
	Status     *hcStatus         `json:"status"`
	Turn       int               `json:"turn"`
	Foreground *hcForeground     `json:"foreground"`
}

// hcListed is one entry of "hc ls": brief, with the status as a plain string.
type hcListed struct {
	Address string `json:"address"`
	Agent   string `json:"agent"`
	Cwd     string `json:"cwd"`
	State   string `json:"state"`
	Status  string `json:"status"`
	Turn    int    `json:"turn"`
}

var shellNames = map[string]bool{"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true, "ksh": true, "nu": true, "elvish": true, "xonsh": true, "pwsh": true}

func isShellArgv0(argv0 string) bool {
	return shellNames[strings.TrimPrefix(filepath.Base(argv0), "-")]
}

// atShellPrompt reports whether the terminal's own shell is its foreground
// program, i.e. nothing launched from it is running. A terminal whose root
// program is not a shell never is: when that program ends the terminal ends.
func (t hcTerminal) atShellPrompt() bool {
	if len(t.Argv) == 0 || !isShellArgv0(t.Argv[0]) {
		return false
	}
	return t.Foreground == nil || t.Foreground.PID == t.PID
}

// agentGone reports whether an agent launched in this terminal (AgentStart set
// vexillum.launched) has exited and left the shell: hc keeps the last status the
// program reported, so without this a dead agent would look idle or done forever.
func (t hcTerminal) agentGone() bool {
	return t.Labels[LabelLaunched] == "1" && t.atShellPrompt()
}

// owned reports whether vexillum started this terminal.
func (t hcTerminal) owned() bool {
	return t.Labels[LabelManaged] == "1" && t.Labels[LabelProject] != ""
}

// statusWord is the agent_status vexillum sees: the word the program reported,
// or "unknown" when it never reported (hc says null).
func (t hcTerminal) statusWord() string {
	if t.Status == nil || t.Status.State == "" {
		return "unknown"
	}
	return strings.ToLower(t.Status.State)
}

// harnessKinds maps what a terminal says it runs (the OSC app name, or a
// program's name) to the harness name vexillum uses.
var harnessKinds = map[string]string{
	"claude":      "claude",
	"claude-code": "claude",
	"pi":          "pi",
	"opencode":    "opencode",
}

// kind is the harness the terminal runs, or "" when none is running or it is
// not one vexillum knows. The OSC app name wins once the program has reported;
// before that, the foreground program's argv names it ("node" runs pi, so the
// process name alone is not enough).
func (t hcTerminal) kind() string {
	if t.atShellPrompt() {
		return ""
	}
	if t.Status != nil {
		if k := harnessKinds[strings.ToLower(t.Status.App)]; k != "" {
			return k
		}
	}
	if t.Foreground != nil {
		for _, a := range append([]string{t.Foreground.Name}, t.Foreground.Argv...) {
			if k := harnessKinds[filepath.Base(a)]; k != "" {
				return k
			}
		}
	}
	if len(t.Argv) > 0 {
		if k := harnessKinds[filepath.Base(t.Argv[0])]; k != "" {
			return k
		}
	}
	return ""
}

func (t hcTerminal) info() AgentInfo {
	info := AgentInfo{
		Kind:   t.kind(),
		PaneID: t.Address,
		Name:   t.Labels[LabelAgent],
		Cwd:    t.Cwd,
		Status: t.statusWord(),
	}
	if t.Foreground != nil {
		info.ForegroundCwd = t.Foreground.Cwd
	}
	return info
}

// ---- target resolution ----

var hcAddressRE = regexp.MustCompile(`^([a-z0-9._-]+/)?t[0-9bcdfghjkmnpqrstvwxyz]+$`)

func hcNotFound(target string) error {
	return &APIError{Code: "agent_not_found", Message: fmt.Sprintf("no terminal %q", target)}
}

// listLabelled returns the live terminals carrying every label (k=v).
func (h *HC) listLabelled(labels ...string) ([]hcListed, error) {
	args := []string{"ls", "--json"}
	for _, l := range labels {
		args = append(args, "--label", l)
	}
	var resp struct {
		Terminals []hcListed `json:"terminals"`
	}
	if err := h.execJSON(&resp, args...); err != nil {
		return nil, err
	}
	return resp.Terminals, nil
}

func (h *HC) getAddr(addr string) (hcTerminal, error) {
	var t hcTerminal
	if err := h.execJSON(&t, "get", addr); err != nil {
		return hcTerminal{}, err
	}
	if t.Address == "" {
		t.Address = addr
	}
	return t, nil
}

func (h *HC) remember(name, addr string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.names == nil {
		h.names = map[string]string{}
	}
	h.names[name] = addr
}

func (h *HC) forget(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.names, name)
}

func (h *HC) cached(name string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.names[name]
}

// terminal returns the terminal target names: a tab or pane id (an hc address)
// or an agent name. A name only ever resolves to a terminal that carries the
// vexillum labels and that name, so a call made by name cannot reach anything
// else; an address is returned as it is and judged by the caller.
func (h *HC) terminal(target string) (hcTerminal, error) {
	if hcAddressRE.MatchString(target) {
		return h.getAddr(target)
	}
	if addr := h.cached(target); addr != "" {
		t, err := h.getAddr(addr)
		if err == nil && t.owned() && t.Labels[LabelAgent] == target && t.State != "finished" {
			return t, nil
		}
		h.forget(target)
	}
	found, err := h.listLabelled(LabelAgent+"="+target, LabelManaged+"=1")
	if err != nil {
		return hcTerminal{}, err
	}
	if len(found) == 0 {
		return hcTerminal{}, hcNotFound(target)
	}
	t, err := h.getAddr(found[0].Address)
	if err != nil {
		return hcTerminal{}, err
	}
	h.remember(target, t.Address)
	return t, nil
}

// writable is terminal for a call that types into the terminal: a terminal
// reached by address must be one vexillum started or one that runs an agent
// (the commander's own pane, which the sentinel prompts), never a plain shell
// or another program of the user's.
func (h *HC) writable(target string) (hcTerminal, error) {
	t, err := h.terminal(target)
	if err != nil {
		return t, err
	}
	if t.State == "finished" {
		return t, &APIError{Code: "agent_not_running", Message: fmt.Sprintf("terminal %s has finished", t.Address)}
	}
	if !t.owned() && t.kind() == "" {
		return t, fmt.Errorf("refusing to type into terminal %s: vexillum did not start it and it runs no agent", t.Address)
	}
	return t, nil
}

// ---- Client ----

func (h *HC) CreateTab(workspaceID, cwd, label string, env ...string) (string, string, error) {
	for _, kv := range env {
		if key, _, ok := strings.Cut(kv, "="); !ok || !envKeyRE.MatchString(key) {
			return "", "", fmt.Errorf("invalid environment entry %q (want KEY=VALUE)", kv)
		}
	}
	project := strings.TrimPrefix(workspaceID, SpacePrefix)
	var resp struct {
		Address string `json:"address"`
	}
	// No parent and no notes: the soldier must outlive the commander's terminal
	// and the core must type nothing into it. No --name: see the package comment.
	err := h.execJSON(&resp, "run", "--bg", "--json", "--no-parent", "--outlive-parent", "--no-notify",
		"--cwd", cwd,
		"--label", LabelManaged+"=1",
		"--label", LabelProject+"="+project,
		"--label", labelSpace+"="+workspaceID,
		"--label", labelTab+"="+label)
	if err != nil {
		return "", "", err
	}
	if resp.Address == "" {
		return "", "", fmt.Errorf("unexpected hc run response: no address")
	}
	h.mu.Lock()
	if h.envs == nil {
		h.envs = map[string][]string{}
	}
	h.envs[resp.Address] = append([]string(nil), env...)
	h.mu.Unlock()
	return resp.Address, resp.Address, nil
}

// BindTask labels the terminal with the task that owns it.
func (h *HC) BindTask(paneID, taskID string) error {
	t, err := h.getAddr(paneID)
	if err != nil {
		return err
	}
	if !t.owned() {
		return fmt.Errorf("refusing to label terminal %s: vexillum did not start it", paneID)
	}
	_, err = h.exec("label", paneID, LabelTask+"="+taskID)
	return err
}

var (
	envKeyRE   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	shellSafeR = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)
)

// shellQuote quotes s for a POSIX-style shell: sh, bash, zsh and fish all read a
// single-quoted word, with an embedded quote written as quote, backslash, quote,
// quote, the same way.
func shellQuote(s string) string {
	if s != "" && shellSafeR.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// launchLine is the command typed into the shell to start the agent: the
// environment as an "env K=V" prefix, then the program and its arguments.
func launchLine(env []string, kind string, args []string) string {
	parts := make([]string, 0, len(env)+len(args)+2)
	if len(env) > 0 {
		parts = append(parts, "env")
		for _, kv := range env {
			k, v, _ := strings.Cut(kv, "=")
			parts = append(parts, k+"="+shellQuote(v))
		}
	}
	parts = append(parts, shellQuote(kind))
	for _, a := range args {
		parts = append(parts, shellQuote(a))
	}
	return strings.Join(parts, " ")
}

func (h *HC) AgentStart(name, kind, paneID string, agentArgs ...string) error {
	t, err := h.getAddr(paneID)
	if err != nil {
		return err
	}
	if !t.owned() {
		return fmt.Errorf("refusing to start an agent in terminal %s: vexillum did not start it", paneID)
	}
	taken, err := h.listLabelled(LabelAgent+"="+name, LabelManaged+"=1")
	if err != nil {
		return err
	}
	for _, other := range taken {
		if other.Address != paneID {
			return &APIError{Code: "agent_name_taken", Message: fmt.Sprintf("agent name %q is already in use by terminal %s", name, other.Address)}
		}
	}
	if _, err := h.exec("label", paneID, LabelAgent+"="+name, LabelLaunched+"=1"); err != nil {
		return err
	}
	h.remember(name, paneID)

	h.mu.Lock()
	env := h.envs[paneID]
	h.mu.Unlock()

	h.waitShellPrompt(paneID)
	if _, err := h.exec("send", paneID, "--", launchLine(env, kind, agentArgs)); err != nil {
		return err
	}

	started := time.Now()
	deadline := started.Add(hcStartWindow)
	for {
		t, err := h.getAddr(paneID)
		if err != nil {
			return err
		}
		switch {
		case t.State == "finished":
			return &APIError{Code: "agent_not_running", Message: fmt.Sprintf("terminal %s finished while %s was starting", paneID, kind)}
		case t.statusWord() != "unknown":
			return nil
		case t.atShellPrompt() && time.Since(started) > 2*time.Second:
			// The shell is back at its prompt: the program exited (or never
			// started). The screen says why.
			screen, _ := h.exec("read", paneID)
			return &APIError{Code: "agent_not_running", Message: fmt.Sprintf("%s exited right after it was started: %s", kind, lastLines(string(screen), 5))}
		}
		if time.Now().After(deadline) {
			// Still no status: a first-run dialog (Claude's trust prompt reports
			// nothing) or a program that never reports. Same verdict herdr gives,
			// and startAgent looks at the screen from here.
			return &APIError{Code: "agent_not_ready", Message: fmt.Sprintf("%s reported no status within %s", kind, hcStartWindow)}
		}
		time.Sleep(hcPollInterval)
	}
}

// waitShellPrompt waits for the new shell to draw something, so the launch line
// is not typed before it reads input. Best effort: after hcShellWindow the line
// is typed anyway.
func (h *HC) waitShellPrompt(addr string) {
	deadline := time.Now().Add(hcShellWindow)
	for time.Now().Before(deadline) {
		if out, err := h.exec("read", addr); err == nil && strings.TrimSpace(string(out)) != "" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n "), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, " | "))
}

func (h *HC) AgentSendKeys(name string, keys ...string) error {
	t, err := h.writable(name)
	if err != nil {
		return err
	}
	_, err = h.exec(append([]string{"keys", t.Address, "--"}, keys...)...)
	return err
}

func (h *HC) AgentReady(name string) (bool, error) {
	t, err := h.terminal(name)
	if err != nil {
		return false, err
	}
	if t.agentGone() {
		return false, hcNotFound(name)
	}
	return t.Reported && t.Status != nil, nil
}

func (h *HC) AgentStatus(name string) (string, error) {
	t, err := h.terminal(name)
	if err != nil {
		return "", err
	}
	if t.agentGone() {
		return "", &APIError{Code: "agent_not_found", Message: fmt.Sprintf("the agent in terminal %s exited: it is back at its shell", t.Address)}
	}
	return t.statusWord(), nil
}

// hcSendReply is what "hc send" answers: the state before the text went in.
type hcSendReply struct {
	Started *bool   `json:"started"`
	Status  *string `json:"status"`
	Turn    int     `json:"turn"`
}

func timeoutSeconds(timeoutMS int) int {
	s := (timeoutMS + 999) / 1000
	if s < 1 {
		return 1
	}
	return s
}

func untilSet(until []string) map[string]bool {
	set := map[string]bool{}
	for _, u := range until {
		set[strings.ToLower(u)] = true
	}
	return set
}

// AgentPrompt sends text and waits for the agent to take it. hc "send --wait"
// returns once the turn is over, which would make every dispatch wait for the
// soldier's whole first turn; herdr's "--until working" returns as soon as the
// agent starts. So the text goes in with a plain send, and the terminal is
// then watched: a "working" status (when asked for), or a turn that ended
// since the send, settles it. done is not idle: both are reported as they come.
func (h *HC) AgentPrompt(name, text string, until []string, timeoutMS int) (string, error) {
	t, err := h.writable(name)
	if err != nil {
		return "", err
	}
	var reply hcSendReply
	if err := h.execJSON(&reply, "send", t.Address, "--", text); err != nil {
		return "", err
	}
	before := "unknown"
	if reply.Status != nil {
		before = strings.ToLower(*reply.Status)
	}
	want := untilSet(until)

	start := time.Now()
	deadline := start.Add(time.Duration(timeoutMS) * time.Millisecond)
	seenWorking := false
	for {
		cur, err := h.getAddr(t.Address)
		if err != nil {
			return "", err
		}
		if cur.State == "finished" {
			return "", &APIError{Code: "agent_not_running", Message: fmt.Sprintf("terminal %s finished while it was prompted", t.Address)}
		}
		st := cur.statusWord()
		switch {
		case st == "working":
			seenWorking = true
			if want["working"] {
				return st, nil
			}
		case st != "unknown" && (cur.Turn > reply.Turn || st != before):
			// A turn ended since the send, or the status moved to something
			// that is not "working": the agent took the text and has settled.
			return st, nil
		}
		now := time.Now()
		if !seenWorking && now.Sub(start) > hcStallWindow && st == before && cur.Turn == reply.Turn {
			return "", &APIError{Code: "agent_prompt_stalled", Message: fmt.Sprintf("terminal %s showed no sign of taking the prompt within %s", t.Address, hcStallWindow)}
		}
		if now.After(deadline) {
			return "", &APIError{Code: "timeout", Message: fmt.Sprintf("terminal %s did not settle within %dms", t.Address, timeoutMS)}
		}
		time.Sleep(hcPollInterval)
	}
}

// AgentRead returns the screen and lines of history above it as plain text.
// hc prints no ANSI; ParseAskUserQuestionModal strips it anyway.
func (h *HC) AgentRead(name string, lines int) (string, error) {
	t, err := h.terminal(name)
	if err != nil {
		return "", err
	}
	out, err := h.exec("read", t.Address, "--lines", strconv.Itoa(lines))
	if err != nil {
		return string(out), fmt.Errorf("reading soldier transcript: %w", err)
	}
	return string(out), nil
}

// AgentReadVisible returns the visible screen only (hc read with no history).
func (h *HC) AgentReadVisible(name string) (string, error) {
	t, err := h.terminal(name)
	if err != nil {
		return "", err
	}
	out, err := h.exec("read", t.Address)
	if err != nil {
		return string(out), fmt.Errorf("reading soldier's visible pane: %w", err)
	}
	return string(out), nil
}

// AgentWait waits for the agent to reach one of until (empty: idle, done, blocked
// or error, which is what "hc wait" waits for). hc cannot wait for a given
// state, so an explicit until is polled.
func (h *HC) AgentWait(name string, until []string, timeoutMS int) (string, error) {
	t, err := h.terminal(name)
	if err != nil {
		return "", err
	}
	if len(until) == 0 {
		return h.nativeWait(t.Address, timeoutMS)
	}
	want := untilSet(until)
	start := time.Now()
	deadline := start.Add(time.Duration(timeoutMS) * time.Millisecond)
	for {
		cur, err := h.getAddr(t.Address)
		if err != nil {
			return "", err
		}
		if cur.State == "finished" {
			return "", &APIError{Code: "agent_not_running", Message: fmt.Sprintf("terminal %s finished while it was waited on", t.Address)}
		}
		st := cur.statusWord()
		graced := st == "blocked" && time.Since(start) < hcBlockedGrace
		if want[st] && !graced {
			return st, nil
		}
		if time.Now().After(deadline) {
			return "", &APIError{Code: "timeout", Message: fmt.Sprintf("terminal %s did not reach %v within %dms", t.Address, until, timeoutMS)}
		}
		time.Sleep(hcPollInterval)
	}
}

// nativeWait is "hc wait": it ends when the terminal is not working. A timeout
// is an exit-0 "check-in" for hc and an error for herdr callers, so it becomes
// the timeout APIError; a terminal that exited becomes agent_not_running.
func (h *HC) nativeWait(addr string, timeoutMS int) (string, error) {
	var reply struct {
		Ended  bool    `json:"ended"`
		Reason string  `json:"reason"`
		Status *string `json:"status"`
	}
	if err := h.execJSON(&reply, "wait", addr, "--timeout", strconv.Itoa(timeoutSeconds(timeoutMS))); err != nil {
		return "", err
	}
	switch reply.Reason {
	case "status":
		if reply.Status != nil {
			return strings.ToLower(*reply.Status), nil
		}
		return "unknown", nil
	case "exit":
		return "", &APIError{Code: "agent_not_running", Message: fmt.Sprintf("terminal %s exited while it was waited on", addr)}
	case "timeout":
		return "", &APIError{Code: "timeout", Message: fmt.Sprintf("terminal %s did not settle within %dms", addr, timeoutMS)}
	case "not_started":
		return "", &APIError{Code: "agent_prompt_stalled", Message: fmt.Sprintf("terminal %s did not start a turn", addr)}
	}
	return "", fmt.Errorf("unexpected hc wait reason %q", reply.Reason)
}

// TabClose kills the terminal, removes it and checks it is gone (hc get must
// answer not_found). Only a terminal vexillum started is touched. A terminal
// that is already gone is an agent_not_found error, which IsTabGone accepts.
func (h *HC) TabClose(tabID string) error {
	t, err := h.getAddr(tabID)
	if err != nil {
		return err
	}
	if !t.owned() {
		return fmt.Errorf("refusing to close terminal %s: vexillum did not start it", tabID)
	}
	if name := t.Labels[LabelAgent]; name != "" {
		h.forget(name)
	}
	for _, step := range []string{"kill", "rm"} {
		if _, err := h.exec(step, tabID); err != nil && !IsNotFound(err) {
			return fmt.Errorf("hc %s %s: %w", step, tabID, err)
		}
	}
	if _, err := h.getAddr(tabID); err == nil {
		return fmt.Errorf("terminal %s still exists after hc kill and hc rm", tabID)
	} else if !IsNotFound(err) {
		return err
	}
	return nil
}

// ---- Locator ----

func (h *HC) AgentGet(target string) (AgentInfo, error) {
	t, err := h.terminal(target)
	if err != nil {
		return AgentInfo{}, err
	}
	if t.agentGone() {
		return AgentInfo{}, &APIError{Code: "agent_not_found", Message: fmt.Sprintf("the agent in terminal %s exited: it is back at its shell", t.Address)}
	}
	return t.info(), nil
}

// AgentList returns the agents vexillum can act on: every terminal it started,
// plus the unlabelled terminals that report a status and run a recognized agent
// (the commanders, which the commander registry looks for by working
// directory). Shells and other programs, the user's own terminals, are not
// listed.
func (h *HC) AgentList() ([]AgentInfo, error) {
	soldiers, err := h.listLabelled(LabelManaged + "=1")
	if err != nil {
		return nil, err
	}
	all, err := h.listLabelled()
	if err != nil {
		return nil, err
	}
	managed := map[string]bool{}
	for _, s := range soldiers {
		managed[s.Address] = true
	}
	var agents []AgentInfo
	for _, l := range all {
		// A terminal that reports a status is running an agent, whatever its
		// process is called; the rest of the user's terminals are not asked about.
		if !managed[l.Address] && l.Status == "" {
			continue
		}
		t, err := h.getAddr(l.Address)
		if err != nil {
			if IsNotFound(err) {
				continue
			}
			return nil, err
		}
		if t.agentGone() {
			continue
		}
		info := t.info()
		if info.Kind == "" && !t.owned() {
			continue
		}
		agents = append(agents, info)
	}
	return agents, nil
}

// IsBlocked reports whether err is the error hc (and herdr) return when text
// is sent to an agent that is blocked on a question: typing would answer it.
func IsBlocked(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == "agent_blocked"
}

// hcGeneration is the protocol generation vexillum was written against.
const hcGeneration = 1

// Hello speaks the protocol's first message over "hc connect" (JSON-RPC over
// stdin and stdout) and returns the generation the core answers with. It is how
// doctor tells a core vexillum can talk to from one it cannot: one process, one
// line each way. It starts the core if none is running, as any hc call does.
func (h *HC) Hello() (generation int, err error) {
	bin, err := h.binary()
	if err != nil {
		return 0, err
	}
	cmd := exec.Command(bin, "connect", "--session", h.session())
	cmd.Env = hcEnv(os.Environ(), h.session())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return 0, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("running hc connect: %w", err)
	}
	defer func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	if _, err := fmt.Fprintf(stdin, "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"hello\",\"params\":{\"generation\":%d}}\n", hcGeneration); err != nil {
		return 0, fmt.Errorf("writing hello: %w", err)
	}
	type reply struct {
		line string
		err  error
	}
	got := make(chan reply, 1)
	go func() {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		got <- reply{line, err}
	}()
	select {
	case r := <-got:
		if r.err != nil && r.line == "" {
			return 0, fmt.Errorf("reading the hello answer: %w", r.err)
		}
		var resp struct {
			Result struct {
				Generation int `json:"generation"`
			} `json:"result"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(r.line), &resp); err != nil {
			return 0, fmt.Errorf("parsing the hello answer %q: %w", strings.TrimSpace(r.line), err)
		}
		if resp.Error != nil {
			return 0, fmt.Errorf("hello refused: %s", resp.Error.Message)
		}
		return resp.Result.Generation, nil
	case <-time.After(5 * time.Second):
		return 0, errors.New("no hello answer within 5s")
	}
}

// HelloOK is Hello that also requires the generation vexillum speaks.
func (h *HC) HelloOK() error {
	gen, err := h.Hello()
	if err != nil {
		return err
	}
	if gen != hcGeneration {
		return fmt.Errorf("the core speaks protocol generation %d, vexillum was written against %d", gen, hcGeneration)
	}
	return nil
}
