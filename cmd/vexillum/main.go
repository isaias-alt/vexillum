package main

import (
	"fmt"
	"os"

	"github.com/isaias-alt/vexillum/internal/cli"
)

const usage = `vexillum is a CLI orchestrator for code agents.

Usage:
  vexillum <command> [flags]

Commands:
  init      Prepare the current project to be orchestrated by vexillum
  doctor    Report on the health of the vexillum environment
  dispatch  Dispatch a soldier (mission or scout) into an isolated camp
  land      Land a finished mission's work into the base branch
  release   Release a soldier's camp back to the pool

Flags:
  -h, --help   Show this help message
`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Print(usage)
		return 0
	}

	switch args[0] {
	case "-h", "--help":
		fmt.Print(usage)
		return 0
	case "init":
		return cli.Init(args[1:])
	case "doctor":
		return cli.Doctor(args[1:])
	case "dispatch":
		return cli.Dispatch(args[1:])
	case "land":
		return cli.Land(args[1:])
	case "release":
		return cli.Release(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "vexillum: unknown command %q\n", args[0])
		fmt.Fprintln(os.Stderr, "Run 'vexillum --help' for a list of commands.")
		return 1
	}
}
