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
	Log           io.Writer
	// Ready, if set, is called once the server accepts connections.
	Ready func(ServerState)
}

const (
	defaultIdleTimeout   = 5 * time.Minute
	defaultCheckInterval = time.Second
)

// Run starts the one forum server for opts.Home and blocks until ctx is
// canceled, `vexillum forum stop` is called, or it has been idle past
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

	listener, err := listenLoopback(opts.Home, opts.Port)
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
		Shutdown:   stop,
	})
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// Long-polls derive from runCtx, so stopping the server aborts them
		// at once instead of waiting out Shutdown's grace.
		BaseContext: func(net.Listener) context.Context { return runCtx },
	}

	state := ServerState{PID: os.Getpid(), Addr: addr, AgentToken: token, StartedAt: time.Now().UTC()}
	if err := atomicfile.WriteJSON(statePath(opts.Home), state); err != nil {
		_ = listener.Close()
		return fmt.Errorf("publishing forum server state: %w", err)
	}
	defer func() { _ = os.Remove(statePath(opts.Home)) }()
	if port := listener.Addr().(*net.TCPAddr).Port; port > 0 {
		// Best-effort hint for the next start; losing it only costs a new port.
		_ = atomicfile.Write(lastPortPath(opts.Home), []byte(strconv.Itoa(port)))
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

// listenLoopback binds 127.0.0.1 only. With port 0 it first tries the port
// recorded by the previous server so an open browser tab reconnects to the
// same address after a restart.
func listenLoopback(home string, port int) (net.Listener, error) {
	if port == 0 {
		if data, err := os.ReadFile(lastPortPath(home)); err == nil {
			if hint, convErr := strconv.Atoi(strings.TrimSpace(string(data))); convErr == nil && hint > 0 && hint < 65536 {
				if l, lerr := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(hint))); lerr == nil {
					return l, nil
				}
			}
		}
	}
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("starting forum server: %w", err)
	}
	return l, nil
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
	st, err := readState(home)
	if err != nil {
		return nil, err
	}
	c := newClient(st)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.health(ctx); err != nil {
		return nil, ErrNoServer
	}
	return c, nil
}

// EnsureServer returns a Client for the running server, starting one via
// spawn first if none is. spawn only has to launch a detached server
// process; EnsureServer waits (up to wait) for it to publish itself.
// Several callers racing here are safe: only one server wins the lock, and
// every caller then discovers that one.
func EnsureServer(ctx context.Context, home string, spawn func() error, wait time.Duration) (*Client, error) {
	if c, err := Discover(home); err == nil {
		return c, nil
	} else if !errors.Is(err, ErrNoServer) {
		return nil, err
	}
	if err := spawn(); err != nil {
		return nil, fmt.Errorf("starting forum server: %w", err)
	}
	deadline := time.Now().Add(wait)
	for {
		if c, err := Discover(home); err == nil {
			return c, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("forum server did not come up within %s (see %s)", wait, LogPath(home))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}
