package forum_test

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/isaias-alt/vexillum/internal/forum"
)

// startServer runs forum.Run in-process and returns once it accepts
// connections. The returned stop func cancels it and waits for Run to
// return.
func startServer(t *testing.T, home string, opts forum.RunOptions) (forum.ServerState, func() error) {
	t.Helper()
	opts.Home = home
	ready := make(chan forum.ServerState, 1)
	opts.Ready = func(st forum.ServerState) { ready <- st }
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- forum.Run(ctx, opts) }()
	select {
	case st := <-ready:
		var once sync.Once
		var result error
		stop := func() error {
			once.Do(func() {
				cancel()
				select {
				case result = <-errc:
				case <-time.After(5 * time.Second):
					result = errors.New("Run did not return after cancel")
				}
			})
			return result
		}
		t.Cleanup(func() { _ = stop() })
		return st, stop
	case err := <-errc:
		cancel()
		t.Fatalf("Run returned early: %v", err)
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("server did not become ready")
	}
	return forum.ServerState{}, nil
}

func TestRun_BindsLoopbackOnlyAndPublishesState(t *testing.T) {
	home := t.TempDir()
	st, stop := startServer(t, home, forum.RunOptions{})
	host, _, err := net.SplitHostPort(st.Addr)
	if err != nil || host != "127.0.0.1" {
		t.Fatalf("addr = %q, want 127.0.0.1:<port>", st.Addr)
	}
	if st.PID != os.Getpid() || st.AgentToken == "" {
		t.Errorf("state = %+v", st)
	}
	info, err := os.Stat(filepath.Join(home, "forum", "server.json"))
	if err != nil {
		t.Fatalf("state file missing: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("state file mode = %v, want 0600 (it holds the agent token)", info.Mode().Perm())
	}
	client, err := forum.Discover(home)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if client == nil {
		t.Fatal("nil client")
	}
	if err := stop(); err != nil {
		t.Fatalf("Run returned %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "forum", "server.json")); !os.IsNotExist(err) {
		t.Errorf("state file should be removed on shutdown, stat err = %v", err)
	}
	if _, err := forum.Discover(home); !errors.Is(err, forum.ErrNoServer) {
		t.Errorf("Discover after stop = %v, want ErrNoServer", err)
	}
}

func TestRun_SecondServerForSameHomeRefuses(t *testing.T) {
	home := t.TempDir()
	startServer(t, home, forum.RunOptions{})
	err := forum.Run(context.Background(), forum.RunOptions{Home: home})
	var running *forum.ErrServerRunning
	if !errors.As(err, &running) {
		t.Fatalf("second Run = %v, want *ErrServerRunning", err)
	}
}

// Many starters racing at the same instant: exactly one wins the lock.
func TestAcquireLock_ConcurrentStartersOnlyOneWins(t *testing.T) {
	home := t.TempDir()
	const n = 16
	var wg sync.WaitGroup
	var mu sync.Mutex
	wins := 0
	releases := []func(){}
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			release, err := forum.AcquireLock(home)
			if err != nil {
				var running *forum.ErrServerRunning
				if !errors.As(err, &running) {
					t.Errorf("AcquireLock: unexpected error %v", err)
				}
				return
			}
			mu.Lock()
			wins++
			releases = append(releases, release)
			mu.Unlock()
		}()
	}
	close(start)
	wg.Wait()
	if wins != 1 {
		t.Errorf("%d starters won the lock, want exactly 1", wins)
	}
	for _, r := range releases {
		r()
	}
	if release, err := forum.AcquireLock(home); err != nil {
		t.Errorf("lock not reacquirable after release: %v", err)
	} else {
		release()
	}
}

func TestAcquireLock_ReclaimsStaleLock(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "forum")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A pid that is not alive: a server that crashed without releasing.
	if err := os.WriteFile(filepath.Join(dir, "server.pid"), []byte("2147483646"), 0o644); err != nil {
		t.Fatal(err)
	}
	release, err := forum.AcquireLock(home)
	if err != nil {
		t.Fatalf("AcquireLock over a stale lock: %v", err)
	}
	release()
}

func TestEnsureServer_ConcurrentCallersShareOneServer(t *testing.T) {
	home := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var runs sync.WaitGroup
	var mu sync.Mutex
	spawned := 0
	spawn := func() error {
		mu.Lock()
		spawned++
		mu.Unlock()
		runs.Add(1)
		go func() {
			defer runs.Done()
			// Losers get ErrServerRunning, exactly like a second
			// `vexillum forum serve` would.
			_ = forum.Run(ctx, forum.RunOptions{Home: home})
		}()
		return nil
	}

	const n = 8
	var wg sync.WaitGroup
	clients := make(chan *forum.Client, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := forum.EnsureServer(ctx, home, spawn, 5*time.Second)
			if err != nil {
				t.Errorf("EnsureServer: %v", err)
				return
			}
			clients <- c
		}()
	}
	wg.Wait()
	close(clients)
	count := 0
	for range clients {
		count++
	}
	if count != n {
		t.Errorf("%d callers got a client, want %d", count, n)
	}
	st, err := os.ReadFile(filepath.Join(home, "forum", "server.json"))
	if err != nil || len(st) == 0 {
		t.Errorf("no single server state published: %v", err)
	}
	cancel()
	runs.Wait()
}

func TestRun_StopsItselfWhenIdle(t *testing.T) {
	home := t.TempDir()
	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		done <- forum.Run(ctx, forum.RunOptions{Home: home, IdleTimeout: 150 * time.Millisecond, CheckInterval: 25 * time.Millisecond})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("an idle server did not stop itself")
	}
}

func TestRun_StaysAliveWhileABrowserIsConnected(t *testing.T) {
	home := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan forum.ServerState, 1)
	done := make(chan error, 1)
	go func() {
		done <- forum.Run(ctx, forum.RunOptions{Home: home, IdleTimeout: 100 * time.Millisecond, CheckInterval: 20 * time.Millisecond, Ready: func(s forum.ServerState) { ready <- s }})
	}()
	<-ready
	client, err := forum.Discover(home)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "a.html")
	writeFile(t, file, "<p>x</p>")
	open, err := client.Open(ctx, file, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Hold a "browser" through the server's own state route: the session
	// token is in session.json.
	if open.Key == "" {
		t.Fatal("no key")
	}
	pollCtx, pollCancel := context.WithCancel(ctx)
	go func() { _, _ = client.Poll(pollCtx, file, 0) }()
	select {
	case err := <-done:
		t.Fatalf("server stopped while a poll was active: %v", err)
	case <-time.After(400 * time.Millisecond):
	}
	pollCancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop after the poll went away")
	}
}

func TestRun_StopRouteShutsDownAndPendingSurvivesRestart(t *testing.T) {
	home := t.TempDir()
	st, _ := startServer(t, home, forum.RunOptions{})
	_ = st
	client, err := forum.Discover(home)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "a.html")
	writeFile(t, file, "<p>x</p>")
	ctx := context.Background()
	open, err := client.Open(ctx, file, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := forum.Discover(home); errors.Is(err, forum.ErrNoServer) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server still discoverable after stop")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// A new server resumes the same session from disk, reusing the port.
	st2, _ := startServer(t, home, forum.RunOptions{})
	if st2.Addr != st.Addr {
		t.Logf("note: restart bound %s instead of the previous %s (port taken)", st2.Addr, st.Addr)
	}
	client2, err := forum.Discover(home)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := client2.Open(ctx, file, false)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Key != open.Key || resumed.Created {
		t.Errorf("restart open = %+v, want the same session resumed", resumed)
	}
}
