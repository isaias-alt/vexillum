package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/pending"
	"github.com/isaias-alt/vexillum/internal/project"
)

const pendingUsage = `Record and clear the commander's own pending decisions.

These are things that wait on the general's approval (dispatch a mission
already designed, land a finished one, ship a PR) and would otherwise live
only in the commander's head, lost if the session is cut.

Usage:
  ` + cmdname.Name + ` pending add <text>
  ` + cmdname.Name + ` pending list [--json]
  ` + cmdname.Name + ` pending clear <id>

add records <text> as a new pending decision and prints its id. clear
removes one once the general has decided. list prints them oldest first.

This is a different category from a blocked task (a soldier needing the
general, see '` + cmdname.Name + ` decide'): here the commander needs the general. A
done mission awaiting '` + cmdname.Name + ` land' needs no entry, the /muster skill derives
that from git; use this for decisions that exist nowhere else.

Items persist as <project root>/pending/<id>.json, written atomically, so
they survive a cut session. --json prints a single JSON object:

  {
    "schema_version": 1,
    "generated_at": "2026-09-24T12:00:00Z",
    "pending": [
      {"schema_version": 1, "id": "1a2b3c4d", "text": "...", "created_at": "..."}
    ]
  }

The /muster skill reads this to show a "Pending decisions" section.
`

// pendingSnapshot is the 'pending list --json' envelope.
type pendingSnapshot struct {
	SchemaVersion int            `json:"schema_version"`
	GeneratedAt   time.Time      `json:"generated_at"`
	Pending       []pending.Item `json:"pending"`
}

// Pending runs the "vx pending" command.
func Pending(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, pendingUsage)
		return 1
	}
	if args[0] == "-h" || args[0] == "--help" {
		fmt.Print(pendingUsage)
		return 0
	}

	projectDir, vexillumHome, err := resolveDirs()
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}
	return runPending(projectDir, vexillumHome, args, os.Stdout, os.Stderr)
}

func runPending(projectDir, vexillumHome string, args []string, stdout, stderr io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}
	usageErr := func(msg string) int {
		fmt.Fprintln(stderr, cmdname.Name+":", msg)
		fmt.Fprint(stderr, pendingUsage)
		return 1
	}

	sub, rest := args[0], args[1:]
	switch sub {
	case "add", "list", "clear":
	default:
		return usageErr(fmt.Sprintf("unknown pending subcommand %q", sub))
	}

	// Validate arguments before touching the project root.
	var (
		text, id   string
		jsonOutput bool
	)
	switch sub {
	case "add":
		text = strings.TrimSpace(strings.Join(rest, " "))
		if text == "" {
			return usageErr("pending add needs the decision's text")
		}
	case "clear":
		if len(rest) != 1 {
			return usageErr("pending clear takes exactly one id")
		}
		id = rest[0]
		if err := pending.ValidateID(id); err != nil {
			return fail(err)
		}
	case "list":
		for _, a := range rest {
			if a != "--json" {
				return usageErr(fmt.Sprintf("unknown pending list flag %q", a))
			}
			jsonOutput = true
		}
	}

	projectRoot, err := project.Root(vexillumHome, projectDir)
	if err != nil {
		return fail(err)
	}

	switch sub {
	case "add":
		item, err := pending.Add(projectRoot, text)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "pending_id=%s\n", item.ID)
	case "clear":
		if err := pending.Clear(projectRoot, id); err != nil {
			if errors.Is(err, pending.ErrNotFound) {
				return fail(fmt.Errorf("no pending decision %s (already cleared?)", id))
			}
			return fail(err)
		}
		fmt.Fprintf(stdout, "cleared pending decision %s\n", id)
	case "list":
		items, err := pending.List(projectRoot)
		if err != nil {
			return fail(err)
		}
		if items == nil {
			// Same reasoning as status --json: no "null" for an empty list.
			items = []pending.Item{}
		}
		if jsonOutput {
			enc := json.NewEncoder(stdout)
			enc.SetIndent("", "  ")
			snapshot := pendingSnapshot{SchemaVersion: pending.SchemaVersion, GeneratedAt: time.Now().UTC(), Pending: items}
			if err := enc.Encode(snapshot); err != nil {
				return fail(fmt.Errorf("encoding pending list: %w", err))
			}
			return 0
		}
		if len(items) == 0 {
			fmt.Fprintln(stdout, "no pending decisions.")
			return 0
		}
		for _, it := range items {
			fmt.Fprintf(stdout, "%s  %s  %s\n", it.ID, it.CreatedAt.Format("2006-01-02 15:04"), it.Text)
		}
	}
	return 0
}
