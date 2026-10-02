package forum

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
)

// ServerState is the discovery file (<home>/forum/server.json, 0600) the
// running server publishes so the CLI can find it: where it listens and the
// agent token. It exists only while the server runs.
type ServerState struct {
	PID        int       `json:"pid"`
	Addr       string    `json:"addr"`
	AgentToken string    `json:"agent_token"`
	StartedAt  time.Time `json:"started_at"`
	// Build identifies the assets and revision the server was built with. A
	// server left running by an older binary keeps serving that binary's
	// chrome, so callers replace a server whose Build differs from theirs,
	// but only when it is idle (see EnsureServer).
	Build string `json:"build,omitempty"`
	// Protocol is the agent API version the server speaks (ProtocolVersion).
	// Absent in the file of a server that predates it.
	Protocol int `json:"protocol,omitempty"`
}

func statePath(home string) string    { return filepath.Join(serverDir(home), "server.json") }
func lastPortPath(home string) string { return filepath.Join(serverDir(home), "last-port") }

// LogPath is where a background-started server writes its log.
func LogPath(home string) string { return filepath.Join(serverDir(home), "server.log") }

// RunOptions configure Run. Zero values mean production defaults.
type RunOptions struct {
	Home string
	// Port binds a specific loopback port; 0 reuses the last port the
	// server used (so a browser tab survives a restart) and falls back to
	// any free one.
	Port int
	// IdleTimeout is how long the server lingers with no connected browser
	// and no active poll before stopping itself.
	IdleTimeout time.Duration
	// BrowserGrace is forwarded to the Hub.
	BrowserGrace time.Duration
	// CheckInterval is how often the idle check runs.
	CheckInterval time.Duration
	// PortRetry is how long to keep retrying a busy preferred port (the last
	// one used, or Port) before falling back to a free one (or failing, for
	// an explicit Port): the usual cause is the previous server still
	// releasing it. Zero means defaultPortRetry; negative means no retry.
	PortRetry time.Duration
	// Build and Protocol override the identity the server publishes (tests
	// only).
	Build    string
	Protocol int
	Log      io.Writer
	// Ready, if set, is called once the server accepts connections.
	Ready func(ServerState)
}

const (
	defaultIdleTimeout   = 5 * time.Minute
	defaultCheckInterval = time.Second
	respawnAfter         = 1500 * time.Millisecond
	defaultPortRetry     = 3 * time.Second
	portRetryStep        = 50 * time.Millisecond
)

// Run starts the one forum server for opts.Home and blocks until ctx is
// canceled, `vx forum stop` is called, or it has been idle past
// IdleTimeout. It refuses (with *ErrServerRunning) if another server holds
// the lock. It binds 127.0.0.1 only, never another interface.
func Run(ctx context.Context, opts RunOptions) error {
	if opts.IdleTimeout <= 0 {
		opts.IdleTimeout = defaultIdleTimeout
	}
	if opts.CheckInterval <= 0 {
		opts.CheckInterval = defaultCheckInterval
	}
	logw := opts.Log
	if logw == nil {
		logw = io.Discard
	}
	logf := func(format string, args ...any) {
		fmt.Fprintf(logw, time.Now().Format(time.RFC3339)+" "+format+"\n", args...)
	}

	release, err := AcquireLock(opts.Home)
	if err != nil {
		return err
	}
	defer release()

	build := opts.Build
	if build == "" {
		build = Build()
	}
	protocol := opts.Protocol
	if protocol == 0 {
		protocol = ProtocolVersion
	}

	listener, err := listenLoopback(ctx, opts.Home, opts.Port, opts.PortRetry, logf)
	if err != nil {
		return err
	}

	token, err := newToken()
	if err != nil {
		_ = listener.Close()
		return err
	}
	addr := listener.Addr().String()

	runCtx, stop := context.WithCancel(ctx)
	defer stop()

	hub := NewHub(opts.Home, HubOptions{BrowserGrace: opts.BrowserGrace, Logf: logf})
	handler := NewServer(ServerOptions{
		Hub:        hub,
		Store:      NewStore(opts.Home),
		AgentToken: token,
		Addr:       addr,
		Build:      build,
		Protocol:   protocol,
		Shutdown:   stop,
	})
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// Long-polls derive from runCtx, so stopping the server aborts them
		// at once instead of waiting out Shutdown's grace.
		BaseContext: func(net.Listener) context.Context { return runCtx },
	}

	state := ServerState{PID: os.Getpid(), Addr: addr, AgentToken: token, StartedAt: time.Now().UTC(), Build: build, Protocol: protocol}
	if err := atomicfile.WriteJSON(statePath(opts.Home), state); err != nil {
		_ = listener.Close()
		return fmt.Errorf("publishing forum server state: %w", err)
	}
	defer func() { _ = os.Remove(statePath(opts.Home)) }()
	// Record the port actually bound, whether it was the preferred one or a
	// fallback, so the next start (and every browser tab) finds it again.
	// Losing the hint only costs a new port, so a failure is logged, not fatal.
	if port := listener.Addr().(*net.TCPAddr).Port; port > 0 {
		if err := atomicfile.Write(lastPortPath(opts.Home), []byte(strconv.Itoa(port))); err != nil {
			logf("recording the forum port: %v", err)
		}
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(listener) }()
	logf("forum server listening on %s (pid %d)", addr, state.PID)
	if opts.Ready != nil {
		opts.Ready(state)
	}

	ticker := time.NewTicker(opts.CheckInterval)
	defer ticker.Stop()
	reason := "stopped"
loop:
	for {
		select {
		case <-runCtx.Done():
			break loop
		case err := <-serveErr:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("forum server: %w", err)
			}
			break loop
		case <-ticker.C:
			if hub.Idle(opts.IdleTimeout) {
				reason = "idle"
				break loop
			}
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stop()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
	}
	logf("forum server %s", reason)
	return nil
}

// readLastPort returns the port the previous server recorded, or 0.
func readLastPort(home string) int {
	data, err := os.ReadFile(lastPortPath(home))
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || port <= 0 || port > 65535 {
		return 0
	}
	return port
}

// listenLoopback binds 127.0.0.1 only. An explicit port is used as given (a
// busy one is an error after the retry window). With port 0 it prefers the
// port recorded by the previous server, so an open browser tab (whose origin
// includes the port) reconnects to the same address after a restart, and only
// falls back to any free port when that one stays taken.
func listenLoopback(ctx context.Context, home string, port int, retry time.Duration, logf func(string, ...any)) (net.Listener, error) {
	if retry == 0 {
		retry = defaultPortRetry
	}
	preferred := port
	if preferred == 0 {
		preferred = readLastPort(home)
	}
	if preferred > 0 {
		l, err := listenWithRetry(ctx, preferred, retry)
		if err == nil {
			return l, nil
		}
		if port > 0 {
			return nil, fmt.Errorf("starting forum server: port %d is not available: %w", port, err)
		}
		logf("last forum port %d is not available (%v); using a free one", preferred, err)
	}
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", "0"))
	if err != nil {
		return nil, fmt.Errorf("starting forum server: %w", err)
	}
	return l, nil
}

// listenWithRetry binds port, retrying for up to retry while it is busy.
func listenWithRetry(ctx context.Context, port int, retry time.Duration) (net.Listener, error) {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	deadline := time.Now().Add(retry)
	for {
		l, err := net.Listen("tcp", addr)
		if err == nil {
			return l, nil
		}
		if retry <= 0 || !time.Now().Before(deadline) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(portRetryStep):
		}
	}
}

// ErrNoServer means no live forum server is registered for the home.
var ErrNoServer = errors.New("no forum server is running")

// readState loads the discovery file and checks the process it names is
// alive.
func readState(home string) (ServerState, error) {
	data, err := os.ReadFile(statePath(home))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ServerState{}, ErrNoServer
		}
		return ServerState{}, fmt.Errorf("reading forum server state: %w", err)
	}
	var st ServerState
	if err := json.Unmarshal(data, &st); err != nil {
		return ServerState{}, ErrNoServer
	}
	if st.Addr == "" || !processAlive(st.PID) {
		return ServerState{}, ErrNoServer
	}
	return st, nil
}

// Discover returns a Client for the running server, or ErrNoServer. It
// verifies the server answers its health check, so a dead pid reused by an
// unrelated process is not mistaken for a server.
func Discover(home string) (*Client, error) {
	c, _, err := discover(home)
	return c, err
}

func discover(home string) (*Client, ServerState, error) {
	st, err := readState(home)
	if err != nil {
		return nil, ServerState{}, err
	}
	c := newClient(st)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.health(ctx); err != nil {
		return nil, ServerState{}, ErrNoServer
	}
	return c, st, nil
}

// errBusy marks a conditional stop refused because the server gained activity.
var errBusy = errors.New("the forum server is in use")

// replaceStale stops an idle server built from a different binary than the
// caller's and waits for it to go, so the caller starts one of its own. The
// old server's sessions are on disk and survive the restart, and a browser
// tab reconnects to the same port. The stop is conditional on the server
// still being idle (errBusy otherwise), so a tab that connected after the
// caller looked is never cut off.
func replaceStale(ctx context.Context, home string, c *Client, st ServerState) error {
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.StopIfIdle(stopCtx); err != nil {
		var ae *APIError
		if errors.As(err, &ae) && ae.Code == "busy" {
			return errBusy
		}
		return fmt.Errorf("stopping the forum server left by an older vexillum (pid %d): %w", st.PID, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := readState(home); errors.Is(err, ErrNoServer) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the forum server left by an older vexillum (pid %d) did not stop", st.PID)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// serverStatus asks the running server for its status. ok is false for a
// server that predates the status route: its activity is unknown, so callers
// must treat it as possibly busy.
func serverStatus(ctx context.Context, c *Client, st ServerState) (status ServerStatus, ok bool) {
	statusCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	s, err := c.Status(statusCtx)
	if err != nil {
		return ServerStatus{PID: st.PID, Build: st.Build, Protocol: normalizeProtocol(st.Protocol)}, false
	}
	s.Protocol = normalizeProtocol(s.Protocol)
	return s, true
}

// attach decides what to do about the server that is running. It returns a
// usable client; ErrNoServer if there is none (or an idle one of another
// build was just stopped, when replace is set); or an
// *ErrIncompatibleServer when the server cannot be used and must not be
// stopped.
//
// A server of the caller's own build is used as is. One from another build
// is used too when its protocol is compatible and it is in use, or when its
// activity cannot be known (a server too old to say): stopping it would cut
// off tabs and polls. Only an idle server is replaced, so the caller's own
// build serves the chrome. An incompatible one that is in use is an error
// telling the user how to proceed.
func attach(ctx context.Context, home string, replace bool) (*Client, error) {
	c, st, err := discover(home)
	if err != nil {
		return nil, err
	}
	if st.Build == Build() {
		return c, nil
	}
	status, known := serverStatus(ctx, c, st)
	compatible := ProtocolCompatible(status.Protocol)
	busy := !known || status.Busy()
	if !busy && replace {
		switch err := replaceStale(ctx, home, c, st); {
		case err == nil:
			return nil, ErrNoServer
		case errors.Is(err, errBusy):
			// It gained a tab between the status and the stop; re-read it.
			status, _ = serverStatus(ctx, c, st)
			busy = true
		default:
			return nil, err
		}
	}
	if compatible {
		return c, nil
	}
	return nil, &ErrIncompatibleServer{PID: status.PID, Build: status.Build, Protocol: status.Protocol, Activity: status.Activity}
}

// EnsureServer returns a Client for the running server, starting one via
// spawn first if none is. spawn only has to launch a detached server
// process; EnsureServer waits (up to wait) for it to publish itself.
// Several callers racing here are safe: only one server wins the lock, and
// every caller then discovers that one.
//
// A server from a different build is replaced only when it is idle (no open
// session, connected tab or waiting poll). A busy one is reused if its
// protocol is compatible with this binary's, and otherwise EnsureServer fails
// with *ErrIncompatibleServer rather than cutting off its users.
func EnsureServer(ctx context.Context, home string, spawn func() error, wait time.Duration) (*Client, error) {
	c, err := attach(ctx, home, true)
	switch {
	case err == nil:
		return c, nil
	case !errors.Is(err, ErrNoServer):
		return nil, err
	}
	if err := spawn(); err != nil {
		return nil, fmt.Errorf("starting forum server: %w", err)
	}
	deadline := time.Now().Add(wait)
	nextSpawn := time.Now().Add(respawnAfter)
	for {
		// Whatever compatible server is up now is fine, whoever built it:
		// replacing again here could ping-pong with a concurrent caller
		// from another build.
		c, err := attach(ctx, home, false)
		switch {
		case err == nil:
			return c, nil
		case !errors.Is(err, ErrNoServer):
			return nil, err
		}
		now := time.Now()
		if now.After(deadline) {
			return nil, fmt.Errorf("forum server did not come up within %s (see %s)", wait, LogPath(home))
		}
		// A server that was shutting down at the instant ours started still
		// held the lock, so ours exited as "already running" and then the
		// old one went away. Spawning again is harmless (the lock admits
		// only one) and closes that window.
		if now.After(nextSpawn) {
			if err := spawn(); err != nil {
				return nil, fmt.Errorf("starting forum server: %w", err)
			}
			nextSpawn = now.Add(respawnAfter)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
