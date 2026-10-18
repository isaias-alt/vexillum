package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/models"
)

const modelsUsage = `List the model and effort profiles a dispatch can use. Read-only.

Usage:
  ` + cmdname.Name + ` models

Prints the merged profile table: the built-in defaults, overridden by the
optional global ~/.vexillum/models.json, overridden by this project's
.vexillum/models.json. Each profile shows its model and effort (and its
harness, when it sets one) and a "when" line saying when it applies; the table's default entry and the files
that were read are listed at the end. Pass a profile to
'` + cmdname.Name + ` dispatch --profile <name>'.

This is a lookup table, not a router: which profile fits a task is the
commander's judgment, made from the "when" lines. A models file that is
not valid JSON, names a model or effort its harness does not accept (claude:
haiku, sonnet, opus, fable and low to max; pi: a provider/id model such as
anthropic/claude-sonnet-5 and a thinking level off to max), or names an
unknown harness is an error naming the file and field.
`

// profileSummary is the parenthesized part of a profile's line: model and
// effort, plus the harness when the profile sets one explicitly.
func profileSummary(p models.Profile) string {
	summary := p.Model + ", " + p.Effort
	if p.Harness != "" {
		summary += ", harness " + p.Harness
	}
	return summary
}

// Models runs the "vx models" command.
func Models(args []string) int {
	if len(args) > 0 {
		if args[0] == "-h" || args[0] == "--help" {
			fmt.Print(modelsUsage)
			return 0
		}
		fmt.Fprintf(os.Stderr, cmdname.Name+": models takes no arguments, got %q\n", args[0])
		return 1
	}

	projectDir, vexillumHome, err := resolveDirs()
	if err != nil {
		fmt.Fprintln(os.Stderr, cmdname.Name+":", err)
		return 1
	}
	return runModels(projectDir, vexillumHome, os.Stdout, os.Stderr)
}

func runModels(projectDir, vexillumHome string, stdout, stderr io.Writer) int {
	table, err := models.Load(projectDir, vexillumHome)
	if err != nil {
		fmt.Fprintln(stderr, cmdname.Name+":", err)
		return 1
	}

	for _, p := range table.Profiles {
		fmt.Fprintf(stdout, "%s  (%s)\n  %s\n", p.Name, profileSummary(p), p.When)
	}
	fmt.Fprintf(stdout, "%s  (%s, %s)\n  Nothing above fits. Fall back to it, do not reach for it on purpose.\n",
		models.DefaultName, table.Default.Model, table.Default.Effort)

	if len(table.Sources) == 0 {
		fmt.Fprintln(stdout, "\nSource: built-in defaults only")
		return 0
	}
	fmt.Fprintln(stdout, "\nSources, lowest priority first (over the built-in defaults):")
	for _, s := range table.Sources {
		fmt.Fprintln(stdout, "  "+s)
	}
	return 0
}
