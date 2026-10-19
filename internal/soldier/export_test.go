package soldier

import "time"

// SetNotReadyPollWindow shortens how long startAgent polls an agent that
// failed to start with agent_not_ready, and returns a func that restores it.
func SetNotReadyPollWindow(d time.Duration) (restore func()) {
	old := notReadyPollWindow
	notReadyPollWindow = d
	return func() { notReadyPollWindow = old }
}

// SetReservedHarnesses replaces the reserved harness names, and returns a
// func that restores them.
func SetReservedHarnesses(names []string) (restore func()) {
	old := reservedHarnesses
	reservedHarnesses = names
	return func() { reservedHarnesses = old }
}

// OpenCodeBlockKind is the kind ParseOpenCodeBlocked reports, as a string.
func ParseOpenCodeBlocked(visible string) (kind string, question string, options []string, ok bool) {
	k, d, ok := parseOpenCodeBlocked(visible)
	if !ok {
		return "", "", nil, false
	}
	return map[openCodeBlockKind]string{openCodeBlockPermission: "permission", openCodeBlockQuestion: "question"}[k], d.Question, d.Options, true
}

// OpenCodePlanFor exposes openCodePlanFor for a screen kind given by name.
func OpenCodePlanFor(kind string, options []string, answer string) (keys []string, typed, chosen, why string) {
	k := map[string]openCodeBlockKind{"permission": openCodeBlockPermission, "question": openCodeBlockQuestion}[kind]
	plan, why := openCodePlanFor(k, options, answer)
	return plan.keys, plan.typed, plan.chosen, why
}

// SetOpenCodeLeaveBlockedWindow shortens how long an answered OpenCode pane is
// waited on to leave blocked, and returns a func that restores it.
func SetOpenCodeLeaveBlockedWindow(d time.Duration) (restore func()) {
	old := openCodeLeaveBlockedWindow
	openCodeLeaveBlockedWindow = d
	return func() { openCodeLeaveBlockedWindow = old }
}

// SetOpenCodeTextFieldWindow shortens how long a selected "Type your own
// answer" is waited on to open its text field, and returns a func that
// restores it.
func SetOpenCodeTextFieldWindow(d time.Duration) (restore func()) {
	old := openCodeTextFieldWindow
	openCodeTextFieldWindow = d
	return func() { openCodeTextFieldWindow = old }
}
