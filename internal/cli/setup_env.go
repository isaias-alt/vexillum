package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/install"
	"github.com/isaias-alt/vexillum/internal/slot"
)

// setupKind says which command is running the shared setup engine.
type setupKind int

const (
	setupInit setupKind = iota
	setupUpgrade
)

func (k setupKind) String() string {
	if k == setupUpgrade {
		return "upgrade"
	}
	return "init"
}

// setupOptions are the flags "vx init" and "vx upgrade" share.
type setupOptions struct {
	// Yes accepts every question; it is also what lets a run without a
	// terminal act at all.
	Yes bool
	// Force (upgrade only) overwrites what the user edited, after saving a
	// backup.
	Force bool
	// Lang is "" to detect, or the --lang value.
	Lang slot.Lang
	// Skills is nil to ask, or the --skills / --no-skills decision.
	Skills *bool
}

// parseSetupArgs parses the flags of init (allowForce false) or upgrade. help
// is true when -h/--help was given. An error is a usage problem to report.
func parseSetupArgs(kind setupKind, args []string) (opts setupOptions, help bool, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			return opts, true, nil
		case a == "--yes" || a == "-y":
			opts.Yes = true
		case a == "--force" && kind == setupUpgrade:
			opts.Force = true
		case a == "--skills" || a == "--no-skills":
			want := a == "--skills"
			if opts.Skills != nil && *opts.Skills != want {
				return opts, false, fmt.Errorf("--skills and --no-skills contradict each other")
			}
			opts.Skills = &want
		case a == "--lang" || strings.HasPrefix(a, "--lang="):
			val := strings.TrimPrefix(a, "--lang=")
			if a == "--lang" {
				if i+1 >= len(args) {
					return opts, false, fmt.Errorf("--lang needs a value (en or es)")
				}
				i++
				val = args[i]
			}
			lang, err := slot.ParseLang(val)
			if err != nil {
				return opts, false, err
			}
			opts.Lang = lang
		default:
			return opts, false, fmt.Errorf("unknown %s flag %q", kind, a)
		}
	}
	return opts, false, nil
}

// setupEnv is everything the setup engine needs from the outside: flags,
// whether there is a person to ask, and the streams.
type setupEnv struct {
	opts        setupOptions
	interactive bool
	prompt      *prompter
	stdout      io.Writer
	stderr      io.Writer
}

func newSetupEnv(opts setupOptions, stdin io.Reader, interactive bool, stdout, stderr io.Writer) *setupEnv {
	return &setupEnv{
		opts:        opts,
		interactive: interactive,
		prompt:      newPrompter(stdin, stdout),
		stdout:      stdout,
		stderr:      stderr,
	}
}

// osSetupEnv wires the environment to the real process streams.
func osSetupEnv(opts setupOptions) *setupEnv {
	return newSetupEnv(opts, os.Stdin, isTerminal(os.Stdin), os.Stdout, os.Stderr)
}

func (e *setupEnv) say(format string, args ...any) {
	fmt.Fprintf(e.stdout, format+"\n", args...)
}

func (e *setupEnv) warn(format string, args ...any) {
	fmt.Fprintf(e.stderr, cmdname.Name+": warning: "+format+"\n", args...)
}

func (e *setupEnv) fail(format string, args ...any) int {
	fmt.Fprintf(e.stderr, cmdname.Name+": "+format+"\n", args...)
	return 1
}

// ask returns the answer to a yes/no question: automatically yes with --yes,
// the prompt's answer when there is a terminal, and the default otherwise.
// A prompt that cannot be answered ends the run, see askFailed.
func (e *setupEnv) ask(question string, def bool) (bool, error) {
	if e.opts.Yes {
		return true, nil
	}
	if !e.interactive {
		return def, nil
	}
	return e.prompt.confirm(question, def)
}

// askFailed reports a question that got no answer and returns the exit code.
func (e *setupEnv) askFailed(err error) int {
	fmt.Fprintf(e.stderr, cmdname.Name+": %v - nothing further was changed; re-run with --yes to accept without questions\n", err)
	return 1
}

// consent prints the notice (which of the user's files the command touches)
// and decides whether to go on. proceed is true when the run continues; when
// it is false, code is the exit code.
func (e *setupEnv) consent(kind setupKind, scope string, userFiles, ownFiles []string) (proceed bool, code int) {
	if len(userFiles) == 0 {
		e.say("%s %s changes none of your own files%s.", cmdname.Name, kind, scope)
	} else {
		e.say("%s %s will change these files of yours%s:", cmdname.Name, kind, scope)
	}
	for _, l := range userFiles {
		e.say("  %s", l)
	}
	if len(ownFiles) > 0 {
		e.say("It also writes vexillum's own files: %s.", strings.Join(ownFiles, "; "))
	}
	if e.opts.Yes {
		return true, 0
	}
	if !e.interactive {
		e.say("")
		fmt.Fprintf(e.stderr, cmdname.Name+": not running in a terminal, so nothing was changed.\n"+
			"Re-run with --yes to accept all of the above (add --skills or --no-skills to decide about the skills).\n")
		return false, 1
	}
	ok, err := e.prompt.confirm("Continue?", true)
	if err != nil {
		return false, e.askFailed(err)
	}
	if !ok {
		e.say("Nothing was changed.")
		return false, 0
	}
	return true, 0
}

// chooseLang settles the block language once it is known that the block is
// being written. A language that came from the flag or from an existing
// block is final. Otherwise the detected (or default) language is shown and
// the person confirms or overrides it; without a terminal or with --yes it is
// just announced.
func (e *setupEnv) chooseLang(lang slot.Lang, src install.LangSource, target string) (slot.Lang, error) {
	var found string
	switch src {
	case install.LangFlag, install.LangBlock:
		return lang, nil
	case install.LangDetected:
		found = fmt.Sprintf("Detected language: %s (from %s).", lang, target)
	default:
		found = fmt.Sprintf("No language detected in %s, defaulting to %s.", target, lang)
	}
	if e.opts.Yes || !e.interactive {
		e.say("%s", found)
		return lang, nil
	}
	e.say("%s", found)
	ok, err := e.prompt.confirm(fmt.Sprintf("Use %s for the vexillum block?", lang), true)
	if err != nil {
		return lang, err
	}
	if ok {
		return lang, nil
	}
	for i := 0; i < maxAnswerTries; i++ {
		ans, err := e.prompt.readLine("Language (en/es): ")
		if err != nil {
			return lang, err
		}
		if chosen, perr := slot.ParseLang(ans); perr == nil {
			return chosen, nil
		}
		e.say("Please type en or es.")
	}
	return lang, errNoAnswer
}

// skillQuestion is the question that gates installing the skills.
func skillQuestion(names []string) string {
	return fmt.Sprintf("Install the %s skills?", install.SkillList(names))
}
