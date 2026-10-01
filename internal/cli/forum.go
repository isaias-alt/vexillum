package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/isaias-alt/vexillum/internal/forum"
	"github.com/isaias-alt/vexillum/internal/project"
)

const forumUsage = `Serve a local HTML artifact and edit its Mermaid diagrams as whiteboards.

Usage:
  vexillum forum <html-file> [--port <n>] [--no-open]

Starts a local HTTP server for the given file. Any Mermaid diagram authored
as <div class="mermaid">...</div> renders as an editable Excalidraw
whiteboard: click it to unlock editing, edits autosave locally, and a
"Queue feedback" button writes the edited scene plus a PNG preview to
~/.vexillum/<project>/forums/<key>/whiteboards/. An artifact with no
.mermaid container is served completely unmodified.

--port binds a specific port instead of letting the OS choose a free one.
--no-open skips opening the file in the system browser.

Runs in the foreground until interrupted (Ctrl-C).
`

// Forum runs the "vexillum forum" command.
func Forum(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(forumUsage)
		return 0
	}
	if len(args) == 0 {
		fmt.Print(forumUsage)
		return 1
	}

	file, port, noOpen, err := parseForumArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum:", err)
		return 1
	}

	projectDir, vexillumHome, err := resolveDirs()
	if err != nil {
		fmt.Fprintln(os.Stderr, "vexillum:", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runForum(ctx, projectDir, vexillumHome, file, port, noOpen, os.Stdout, os.Stderr)
}

func parseForumArgs(args []string) (file string, port int, noOpen bool, err error) {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--port":
			i++
			if i >= len(args) {
				return "", 0, false, fmt.Errorf("--port requires a value")
			}
			var perr error
			port, perr = parsePort(args[i])
			if perr != nil {
				return "", 0, false, perr
			}
		case "--no-open":
			noOpen = true
		default:
			if file != "" {
				return "", 0, false, fmt.Errorf("unexpected extra argument %q", a)
			}
			file = a
		}
	}
	if file == "" {
		return "", 0, false, fmt.Errorf("missing html file")
	}
	return file, port, noOpen, nil
}

func parsePort(raw string) (int, error) {
	var port int
	if _, err := fmt.Sscanf(raw, "%d", &port); err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("invalid --port value %q", raw)
	}
	return port, nil
}

// runForum serves file until ctx is canceled (Forum cancels it on
// Ctrl-C/SIGTERM; tests pass their own cancellable context so the success
// path doesn't need a real OS signal to end).
func runForum(ctx context.Context, projectDir, vexillumHome, file string, port int, noOpen bool, stdout, stderr io.Writer) int {
	absFile, err := filepath.Abs(file)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: resolving %s: %v\n", file, err)
		return 1
	}
	if info, statErr := os.Stat(absFile); statErr != nil {
		fmt.Fprintf(stderr, "vexillum: %v\n", statErr)
		return 1
	} else if info.IsDir() {
		fmt.Fprintf(stderr, "vexillum: %s is a directory, not an HTML file\n", absFile)
		return 1
	}

	projectRoot, err := project.Root(vexillumHome, projectDir)
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: resolving project root: %v\n", err)
		return 1
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		fmt.Fprintf(stderr, "vexillum: starting server: %v\n", err)
		return 1
	}

	store := forum.NewStore(projectRoot)
	srv := &http.Server{Handler: forum.NewServer(absFile, store)}

	url := fmt.Sprintf("http://%s/", listener.Addr().String())
	fmt.Fprintf(stdout, "vexillum forum: serving %s at %s\n", absFile, url)
	fmt.Fprintln(stdout, "vexillum forum: press Ctrl-C to stop")

	if !noOpen {
		if openErr := openBrowser(url); openErr != nil {
			fmt.Fprintf(stderr, "vexillum: warning: could not open a browser automatically: %v\n", openErr)
		}
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- srv.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(stderr, "vexillum: shutting down: %v\n", err)
			return 1
		}
		return 0
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(stderr, "vexillum: server error: %v\n", err)
			return 1
		}
		return 0
	}
}

// openBrowser best-effort opens url in the system's default browser. A
// failure here is reported but never fails the command - the server is
// already up and the URL already printed above.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
