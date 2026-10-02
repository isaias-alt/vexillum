package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/forum"
)

const forumUsage = `Open a local HTML artifact for visual review and collect the user's feedback.

Usage:
  ` + cmdname.Name + ` forum <html-file> [--no-open] [--reopen] [--port <n>]
  ` + cmdname.Name + ` forum poll <html-file> [--reply <text> | --reply-file <path|->] [--timeout <duration>]
  ` + cmdname.Name + ` forum poll --all [--reply-to <html-file> (--reply <text> | --reply-file <path|->)] [--timeout <duration>]
  ` + cmdname.Name + ` forum end <html-file>
  ` + cmdname.Name + ` forum stop

forum <html-file> opens (or resumes) the review session for that file and
returns right away, printing the session URL and the next step; one local
server per user keeps running in the background on 127.0.0.1 and stops
itself when nothing is connected. The artifact gets window.forum.queuePrompt
and window.forum.sendQueuedPrompts, and the user can chat, queue messages and
send them to the agent from the browser. A Mermaid diagram authored as
<div class="mermaid">...</div> becomes an editable whiteboard. Sessions are
identified by the file's absolute path.

  --no-open   do not open the browser
  --reopen    reopen a session the user ended from the browser (only when the
              user asked for further review)
  --port      port to bind if the server is not already running

forum poll blocks until the user sends feedback, ends the session, or leaves
the browser disconnected past a grace period (status browser_disconnected;
the session stays resumable). Delivered feedback is consumed. --reply shows
the agent's markdown answer in the browser's conversation panel before it
waits again; --reply-file reads it from a file (- is stdin). --timeout
returns status timeout if nothing arrives in time. Run it again after each
response; see skills/forum/SKILL.md for the exact output format.

forum poll --all listens to every open session at once, so one poll covers
several review windows. Each call delivers the feedback of one session (the
one waiting longest) and names it in the output (session and file);
other_sessions_pending says how many more are waiting, and the next call
delivers them. A session that ends while it waits is reported once as ended,
and no_sessions means nothing is open. Sessions opened while it waits join it.
--reply-to names the session a --reply answers, since the poll itself has no
file.

Delivery is confirmed, not assumed: a poll leases its prompts and the command
acknowledges them once its output was written. If a poll dies before that,
running poll again delivers the same prompts again, marked redelivered, so
nothing is lost; skip any uid you already applied.

forum end ends the session as the agent (a plain forum <html-file> reopens
it later). forum stop shuts the background server down.
`

// forumHome is ~/.vexillum. Unlike the project commands it is not tied to
// the current project (or refused inside a camp): the forum server is one
// per user, and a soldier running in a camp may legitimately use it.
func forumHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".vexillum"), nil
}

// forumSpawn launches the detached background server. A variable so tests
// can run the server in-process instead of re-executing the test binary.
var forumSpawn = spawnForumServer

// Forum runs the "vx forum" command.
func Forum(args []string) int {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Print(forumUsage)
		return 0
	}
	if len(args) == 0 {
		fmt.Print(forumUsage)
		return 1
	}
	home, err := forumHome()
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch args[0] {
	case "serve":
		return runForumServe(ctx, home, args[1:], os.Stderr)
	case "poll":
		return runForumPoll(ctx, home, args[1:], os.Stdin, os.Stdout, os.Stderr)
	case "end":
		return runForumEnd(ctx, home, args[1:], os.Stdout, os.Stderr)
	case "stop":
		return runForumStop(ctx, home, os.Stdout, os.Stderr)
	}
	return runForumOpen(ctx, home, args, os.Stdout, os.Stderr)
}

type forumOpenArgs struct {
	file   string
	port   int
	noOpen bool
	reopen bool
}

func parseForumOpenArgs(args []string) (forumOpenArgs, error) {
	var a forumOpenArgs
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; arg {
		case "--port":
			i++
			if i >= len(args) {
				return a, fmt.Errorf("--port requires a value")
			}
			port, err := parsePort(args[i])
			if err != nil {
				return a, err
			}
			a.port = port
		case "--no-open":
			a.noOpen = true
		case "--reopen":
			a.reopen = true
		default:
			if strings.HasPrefix(arg, "--") {
				return a, fmt.Errorf("unknown flag %q", arg)
			}
			if a.file != "" {
				return a, fmt.Errorf("unexpected extra argument %q", arg)
			}
			a.file = arg
		}
	}
	if a.file == "" {
		return a, fmt.Errorf("missing html file")
	}
	return a, nil
}

func parsePort(raw string) (int, error) {
	port, err := strconv.Atoi(raw)
	if err != nil || port <= 0 || port > 65535 {
		return 0, fmt.Errorf("invalid --port value %q", raw)
	}
	return port, nil
}

// absArtifact resolves file to the absolute path that identifies its
// session, checking it exists and is a file.
func absArtifact(file string) (string, error) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", file, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory, not an HTML file", abs)
	}
	return abs, nil
}

// ensureForumServer returns a client for the running server, starting the
// background one first if needed.
func ensureForumServer(ctx context.Context, home string, port int) (*forum.Client, error) {
	return forum.EnsureServer(ctx, home, func() error { return forumSpawn(home, port) }, 15*time.Second)
}

func runForumOpen(ctx context.Context, home string, args []string, stdout, stderr io.Writer) int {
	a, err := parseForumOpenArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}
	abs, err := absArtifact(a.file)
	if err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}
	client, err := ensureForumServer(ctx, home, a.port)
	if err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}
	res, err := client.Open(ctx, abs, a.reopen)
	if err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}

	fmt.Fprintf(stdout, "session: %s\n", res.Key)
	fmt.Fprintf(stdout, "file: %s\n", res.File)
	fmt.Fprintf(stdout, "status: %s\n", res.Status)
	if res.Status == forum.OpenUserEnded {
		fmt.Fprintf(stdout, "next_step: %s\n", forum.OpenNextStep(abs, res))
		return 1
	}
	fmt.Fprintf(stdout, "url: %s\n", res.URL)
	fmt.Fprintf(stdout, "pending_prompts: %d\n", res.Pending)
	fmt.Fprintf(stdout, "next_step: %s\n", forum.OpenNextStep(abs, res))

	// A browser already showing this session does not need a second tab.
	if !a.noOpen && !res.BrowserConnected {
		if openErr := openBrowser(res.URL); openErr != nil {
			fmt.Fprintf(stderr, cmdname.Name+": warning: could not open a browser automatically: %v\n", openErr)
		}
	}
	return 0
}

type forumPollArgs struct {
	file      string
	all       bool
	replyTo   string
	reply     string
	replyFile string
	hasReply  bool
	timeout   time.Duration
}

func parseForumPollArgs(args []string) (forumPollArgs, error) {
	var a forumPollArgs
	for i := 0; i < len(args); i++ {
		switch arg := args[i]; arg {
		case "--all":
			a.all = true
		case "--reply", "--reply-file", "--reply-to", "--timeout":
			i++
			if i >= len(args) {
				return a, fmt.Errorf("%s requires a value", arg)
			}
			switch arg {
			case "--reply-to":
				a.replyTo = args[i]
			case "--reply":
				a.reply, a.hasReply = args[i], true
			case "--reply-file":
				a.replyFile, a.hasReply = args[i], true
			case "--timeout":
				d, err := time.ParseDuration(args[i])
				if err != nil || d <= 0 {
					return a, fmt.Errorf("invalid --timeout value %q (want a duration like 30s or 10m)", args[i])
				}
				a.timeout = d
			}
		default:
			if strings.HasPrefix(arg, "--") {
				return a, fmt.Errorf("unknown flag %q", arg)
			}
			if a.file != "" {
				return a, fmt.Errorf("unexpected extra argument %q", arg)
			}
			a.file = arg
		}
	}
	if a.reply != "" && a.replyFile != "" {
		return a, fmt.Errorf("pass --reply or --reply-file, not both")
	}
	if a.all {
		switch {
		case a.file != "":
			return a, fmt.Errorf("pass --all or an html file, not both")
		case a.hasReply && a.replyTo == "":
			return a, fmt.Errorf("--all needs --reply-to <html-file> to say which session a reply answers")
		case !a.hasReply && a.replyTo != "":
			return a, fmt.Errorf("--reply-to needs --reply or --reply-file")
		}
		return a, nil
	}
	if a.replyTo != "" {
		return a, fmt.Errorf("--reply-to only goes with --all; name the file instead")
	}
	if a.file == "" {
		return a, fmt.Errorf("missing html file")
	}
	return a, nil
}

func runForumPoll(ctx context.Context, home string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	a, err := parseForumPollArgs(args)
	if err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}
	// The session a reply goes to; a multiplexed poll has no file of its own.
	target := a.file
	if a.all {
		target = a.replyTo
	}
	abs := ""
	if target != "" {
		if abs, err = filepath.Abs(target); err != nil {
			fmt.Fprintf(stderr, cmdname.Name+": resolving %s: %v\n", target, err)
			return 1
		}
	}

	reply := a.reply
	if a.replyFile != "" {
		var data []byte
		if a.replyFile == "-" {
			data, err = io.ReadAll(stdin)
		} else {
			data, err = os.ReadFile(a.replyFile)
		}
		if err != nil {
			fmt.Fprintf(stderr, cmdname.Name+": reading reply: %v\n", err)
			return 1
		}
		reply = string(data)
	}

	client, err := ensureForumServer(ctx, home, 0)
	if err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}

	if a.hasReply {
		if err := client.Reply(ctx, abs, reply); err != nil {
			var ae *forum.APIError
			switch {
			case errors.As(err, &ae) && ae.Code == "no_session":
				fmt.Fprintln(stderr, noSessionMessage(abs))
				return 1
			case errors.As(err, &ae) && ae.Code == "ended":
				// The session ended: nothing to show a reply in, but the poll
				// below still delivers any final feedback exactly once.
				fmt.Fprintln(stderr, cmdname.Name+": warning: reply not shown, the session already ended")
			default:
				fmt.Fprintln(stderr, cmdname.Name+": reply failed:", err)
				return 1
			}
		}
	}

	// The server can disappear under a long poll (stopped by hand, crashed,
	// idle-stopped); everything pending is on disk, so start it again and
	// keep waiting instead of surfacing a connection error the agent can do
	// nothing about.
	const maxReconnects = 5
	failures := 0
	for {
		var res forum.PollResponse
		if a.all {
			res, err = client.PollAll(ctx, a.timeout)
		} else {
			res, err = client.Poll(ctx, abs, a.timeout)
		}
		if err == nil {
			out := forum.FormatPoll(abs, res)
			if a.all {
				out = forum.FormatPoll("", res)
			}
			if _, werr := fmt.Fprint(stdout, out); werr != nil {
				// Nobody read it: leave the prompts unconfirmed so the next
				// poll delivers them again.
				fmt.Fprintf(stderr, cmdname.Name+": writing the poll result: %v\n", werr)
				return 1
			}
			ackDelivery(client, res, stderr)
			return 0
		}
		if ctx.Err() != nil {
			return 1
		}
		var ae *forum.APIError
		if errors.As(err, &ae) {
			if ae.Code == "no_session" {
				fmt.Fprintln(stderr, noSessionMessage(abs))
			} else {
				fmt.Fprintln(stderr, cmdname.Name+":", err)
			}
			return 1
		}
		failures++
		if failures > maxReconnects {
			fmt.Fprintf(stderr, cmdname.Name+": lost the forum server and could not restart it: %v\n", err)
			return 1
		}
		select {
		case <-ctx.Done():
			return 1
		case <-time.After(500 * time.Millisecond):
		}
		if client, err = ensureForumServer(ctx, home, 0); err != nil {
			fmt.Fprintln(stderr, cmdname.Name+":", err)
			return 1
		}
	}
}

// ackDelivery tells the server the prompts in res were read. Best effort: an
// unconfirmed delivery is not lost, it comes back with the next poll marked
// redelivered. It uses its own short deadline because the poll's context may
// already be canceled by the signal that is ending this process.
func ackDelivery(client *forum.Client, res forum.PollResponse, stderr io.Writer) {
	if res.Delivery == "" || res.File == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ack(ctx, res.File, res.Delivery); err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": warning: could not confirm delivery (the prompts will be redelivered by the next poll): %v\n", err)
	}
}

func noSessionMessage(abs string) string {
	return fmt.Sprintf(cmdname.Name+": no forum session for %s - run `"+cmdname.Name+" forum %s` first", abs, abs)
}

func runForumEnd(ctx context.Context, home string, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(stderr, cmdname.Name+": usage: "+cmdname.Name+" forum end <html-file>")
		return 1
	}
	abs, err := filepath.Abs(args[0])
	if err != nil {
		fmt.Fprintf(stderr, cmdname.Name+": resolving %s: %v\n", args[0], err)
		return 1
	}
	client, err := ensureForumServer(ctx, home, 0)
	if err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}
	if err := client.End(ctx, abs); err != nil {
		var ae *forum.APIError
		if errors.As(err, &ae) && ae.Code == "no_session" {
			fmt.Fprintln(stderr, noSessionMessage(abs))
			return 1
		}
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}
	fmt.Fprintf(stdout, "session: %s\nstatus: ended\n", forum.SessionKey(abs))
	fmt.Fprintf(stdout, "next_step: Session ended. Run `"+cmdname.Name+" forum %s` to reopen it if the user wants further review.\n", abs)
	return 0
}

func runForumStop(ctx context.Context, home string, stdout, stderr io.Writer) int {
	client, err := forum.Discover(home)
	if err != nil {
		if errors.Is(err, forum.ErrNoServer) {
			fmt.Fprintln(stdout, "no forum server is running")
			return 0
		}
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}
	if err := client.Stop(ctx); err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}
	fmt.Fprintln(stdout, "forum server stopping")
	return 0
}

// runForumServe is the background server process ("vx forum serve",
// started by spawnForumServer; not meant to be run by hand). It exits 0 when
// another server already holds the lock - several clients racing to start
// one is expected, and exactly one wins.
func runForumServe(ctx context.Context, home string, args []string, stderr io.Writer) int {
	port := 0
	for i := 0; i < len(args); i++ {
		if args[i] != "--port" || i+1 >= len(args) {
			fmt.Fprintf(stderr, cmdname.Name+": unexpected argument %q\n", args[i])
			return 1
		}
		i++
		p, err := parsePort(args[i])
		if err != nil {
			fmt.Fprintln(stderr, cmdname.Name+":", err)
			return 1
		}
		port = p
	}
	err := forum.Run(ctx, forum.RunOptions{Home: home, Port: port, Log: os.Stdout})
	var running *forum.ErrServerRunning
	if errors.As(err, &running) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}
	return 0
}

// spawnForumServer starts "vx forum serve" detached, logging to the
// forum log, so it outlives this command and whatever shell launched it.
func spawnForumServer(home string, port int) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating the "+cmdname.Name+" binary: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(forum.LogPath(home)), 0o755); err != nil {
		return err
	}
	logFile, err := os.OpenFile(forum.LogPath(home), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()

	args := []string{"forum", "serve"}
	if port > 0 {
		args = append(args, "--port", strconv.Itoa(port))
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	// The server is its own process group leader now; do not wait on it.
	return cmd.Process.Release()
}

// openBrowser best-effort opens url in the system's default browser. A
// failure here is reported but never fails the command - the server is
// already up and the URL already printed.
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
