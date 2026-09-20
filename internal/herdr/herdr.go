// Package herdr wraps the herdr CLI (the backend primary session tool,
// see AGENTS.md) enough for vexillum to run a soldier inside a real,
// supervisable pane instead of a headless background process. It shells
// out to the installed `herdr` binary - the skill file it ships
// (`herdr --skill`) names the binary itself as the authority for command
// syntax, and every control command returns newline-delimited JSON on
// stdout, so a thin CLI wrapper is simpler and more robust than
// reimplementing its socket protocol by hand.
package herdr

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Client is what internal/soldier needs from herdr. It's an interface so
// tests can exercise the orchestration logic (status mapping, the
// workspace-trust-dialog retry, task persistence) without a real herdr
// server or a real Claude Code session.
type Client interface {
	// CreateTab creates a tab (and its root pane) in workspaceID, with
	// the pane's shell starting in cwd. Returns the new tab and pane
	// ids.
	CreateTab(workspaceID, cwd, label string) (tabID, paneID string, err error)

	// AgentStart starts a supported interactive agent (kind, e.g.
	// "claude") in an existing, at-prompt pane. name must match
	// herdr's agent name pattern. agentArgs are passed through to the
	// underlying agent binary unchanged (herdr's "-- <agent-args...>").
	// Returns an *APIError with code "agent_not_ready" if the agent is
	// blocked during its own startup (e.g. a first-run dialog) rather
	// than ready for prompts.
	AgentStart(name, kind, paneID string, agentArgs ...string) error

	// AgentSendKeys sends logical keys (e.g. "down", "enter") to the
	// named agent's pane.
	AgentSendKeys(name string, keys ...string) error

	// AgentReady reports whether the named agent is ready for
	// interactive input.
	AgentReady(name string) (bool, error)

	// AgentStatus reports the named agent's current agent_status (idle,
	// working, blocked, done, unknown) without waiting for it to change -
	// used by internal/sentinel to poll for transitions.
	AgentStatus(name string) (string, error)

	// AgentPrompt submits text to the named agent and waits (up to
	// timeoutMS) for it to settle into idle, done, or blocked.
	// Returns the settled agent_status.
	AgentPrompt(name, text string, timeoutMS int) (status string, err error)

	// AgentRead returns recent unwrapped transcript text from the
	// named agent's pane.
	AgentRead(name string, lines int) (string, error)

	// TabClose closes the tab (and its pane). Only call this once the
	// work it held is safely landed elsewhere - see
	// internal/soldier.ReleaseInHerdr.
	TabClose(tabID string) error
}

// APIError is a structured error herdr returned on stderr (CLI exit
// status 1). Code is one of herdr's stable error codes (e.g.
// "agent_not_ready", "agent_blocked").
type APIError struct {
	Code    string
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("herdr: %s (%s)", e.Message, e.Code)
}

// IsNotReady reports whether err is the "agent_not_ready" APIError herdr
// returns when an agent is blocked during its own startup.
func IsNotReady(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == "agent_not_ready"
}

// IsPaneBusy reports whether err is the "agent_pane_busy" APIError herdr
// returns when a just-created pane's shell hasn't settled at its
// interactive prompt yet - observed transiently right after tab create,
// not documented in herdr's own docs as of protocol 22.
func IsPaneBusy(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == "agent_pane_busy"
}

// IsNameTaken reports whether err is the "agent_name_taken" APIError
// herdr returns when another live agent already holds the requested
// name - observed with two soldiers dispatched close together whose
// prompts produce the same slug.
func IsNameTaken(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == "agent_name_taken"
}

// IsTimeout reports whether err is the "timeout" APIError herdr returns
// when a caller-supplied --timeout on "agent prompt --wait" expires
// before the agent settles ("herdr agent prompt --help": "A caller
// timeout that expires first returns timeout"). internal/soldier uses a
// short timeout as a quick-settle probe rather than a failure signal -
// this lets it tell "still working, the sentinel will pick it up" apart
// from a real error.
func IsTimeout(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == "timeout"
}

// CLI is the real Client, implemented by shelling out to the `herdr`
// binary.
type CLI struct{}

func (CLI) CreateTab(workspaceID, cwd, label string) (string, string, error) {
	result, err := run("tab", "create", "--workspace", workspaceID, "--cwd", cwd, "--label", label, "--no-focus")
	if err != nil {
		return "", "", err
	}
	rootPane, _ := result["root_pane"].(map[string]any)
	paneID, _ := rootPane["pane_id"].(string)
	tab, _ := result["tab"].(map[string]any)
	tabID, _ := tab["tab_id"].(string)
	if paneID == "" || tabID == "" {
		return "", "", fmt.Errorf("unexpected tab create response: %v", result)
	}
	return tabID, paneID, nil
}

func (CLI) AgentStart(name, kind, paneID string, agentArgs ...string) error {
	args := []string{"agent", "start", name, "--kind", kind, "--pane", paneID}
	if len(agentArgs) > 0 {
		args = append(args, "--")
		args = append(args, agentArgs...)
	}
	_, err := run(args...)
	return err
}

func (CLI) AgentSendKeys(name string, keys ...string) error {
	args := append([]string{"agent", "send-keys", name}, keys...)
	_, err := run(args...)
	return err
}

func (CLI) AgentReady(name string) (bool, error) {
	result, err := run("agent", "get", name)
	if err != nil {
		return false, err
	}
	agent, _ := result["agent"].(map[string]any)
	ready, _ := agent["interactive_ready"].(bool)
	return ready, nil
}

func (CLI) AgentStatus(name string) (string, error) {
	result, err := run("agent", "get", name)
	if err != nil {
		return "", err
	}
	agent, _ := result["agent"].(map[string]any)
	status, _ := agent["agent_status"].(string)
	return status, nil
}

func (CLI) AgentPrompt(name, text string, timeoutMS int) (string, error) {
	result, err := run("agent", "prompt", name, text, "--wait", "--timeout", strconv.Itoa(timeoutMS))
	if err != nil {
		return "", err
	}
	agent, _ := result["agent"].(map[string]any)
	status, _ := agent["agent_status"].(string)
	return status, nil
}

// AgentRead is the one herdr command whose stdout is plain transcript
// text, not a JSON envelope - it's read separately from run().
func (CLI) AgentRead(name string, lines int) (string, error) {
	cmd := exec.Command("herdr", "agent", "read", name, "--source", "recent-unwrapped", "--lines", strconv.Itoa(lines))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("reading soldier transcript: %w", err)
	}
	return string(out), nil
}

func (CLI) TabClose(tabID string) error {
	_, err := run("tab", "close", tabID)
	return err
}

// run executes a herdr command that returns a JSON envelope
// (`{"id":...,"result":{...}}` on success, `{"id":...,"error":{...}}` on
// CLI exit status 1) and returns the decoded result object.
func run(args ...string) (map[string]any, error) {
	cmd := exec.Command("herdr", args...)
	stdout, err := cmd.Output()
	if err == nil {
		var resp struct {
			Result map[string]any `json:"result"`
		}
		if jsonErr := json.Unmarshal(stdout, &resp); jsonErr != nil {
			return nil, fmt.Errorf("parsing herdr response to %v: %w (raw: %s)", args, jsonErr, stdout)
		}
		return resp.Result, nil
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return nil, fmt.Errorf("running herdr %v: %w", args, err)
	}

	if exitErr.ExitCode() == 1 {
		var resp struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if jsonErr := json.Unmarshal(exitErr.Stderr, &resp); jsonErr == nil && resp.Error.Code != "" {
			return nil, &APIError{Code: resp.Error.Code, Message: resp.Error.Message}
		}
	}
	return nil, fmt.Errorf("herdr %v failed: %s", args, strings.TrimSpace(string(exitErr.Stderr)))
}
