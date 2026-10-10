package forum

import (
	"fmt"
	"strings"

	"github.com/isaias-alt/vexillum/internal/cmdname"
)

// ProtocolVersion is the version of the agent-facing API (/api/agent/*) this
// binary speaks and serves. Bump it only for a change an older client or
// server cannot cope with; a newer build that keeps the API compatible keeps
// the same number, which is what lets a client reuse a running server from a
// different build instead of replacing it.
const ProtocolVersion = 1

// ProtocolCompatible reports whether this binary can drive a server speaking
// protocol.
func ProtocolCompatible(protocol int) bool {
	return protocol == ProtocolVersion
}

// Activity is what a server is doing for people right now. A server with any
// of it must not be stopped to make way for another build: the tabs would
// lose their live feed and an agent its poll.
type Activity struct {
	// Sessions counts sessions that are open (not ended) in this server.
	Sessions int `json:"sessions"`
	// Browsers counts open browser tabs (live feeds and state long-polls),
	// including tabs of a session that has since ended.
	Browsers int `json:"browsers"`
	// Pollers counts agent polls currently waiting for feedback.
	Pollers int `json:"pollers"`
}

// Busy reports whether anything is open or connected.
func (a Activity) Busy() bool { return a.Sessions > 0 || a.Browsers > 0 || a.Pollers > 0 }

func (a Activity) String() string {
	var parts []string
	add := func(n int, one, many string) {
		switch {
		case n == 1:
			parts = append(parts, "1 "+one)
		case n > 1:
			parts = append(parts, fmt.Sprintf("%d %s", n, many))
		}
	}
	add(a.Sessions, "active session", "active sessions")
	add(a.Browsers, "connected tab", "connected tabs")
	add(a.Pollers, "waiting poll", "waiting polls")
	if len(parts) == 0 {
		return "no activity"
	}
	return strings.Join(parts, ", ")
}

// Activity reports the hub's current sessions, tabs and polls.
func (h *Hub) Activity() Activity {
	h.mu.Lock()
	defer h.mu.Unlock()
	var a Activity
	for _, l := range h.sessions {
		if l.rec.Status != StatusEnded {
			a.Sessions++
		}
		a.Browsers += l.browsers
		a.Pollers += l.pollers
	}
	return a
}

// ServerStatus is the body of GET /api/agent/status: who the running server
// is and whether it is in use.
type ServerStatus struct {
	PID      int    `json:"pid"`
	Build    string `json:"build"`
	Protocol int    `json:"protocol"`
	Activity
}

// ErrIncompatibleServer is returned by EnsureServer when the running server
// speaks a protocol this binary cannot use and is too busy to be replaced.
type ErrIncompatibleServer struct {
	PID      int
	Build    string
	Protocol int
	Activity Activity
}

func (e *ErrIncompatibleServer) Error() string {
	head := fmt.Sprintf("the forum server running (pid %d, build %s, protocol %d) speaks a protocol this %s (protocol %d) cannot use",
		e.PID, e.Build, e.Protocol, cmdname.Name, ProtocolVersion)
	if !e.Activity.Busy() {
		return head + fmt.Sprintf(", so it was left running. Run `%s forum stop` and retry", cmdname.Name)
	}
	return head + fmt.Sprintf(" and it is in use (%s), so it was left running. Finish or end those sessions and close their tabs, then retry; "+
		"or run `%s forum stop` to stop it now (sessions are saved and resume on the next start)", e.Activity, cmdname.Name)
}
