package main

import (
	"fmt"
	"os"

	"github.com/isaias-alt/vexillum/internal/buildinfo"
	"github.com/isaias-alt/vexillum/internal/cli"
	"github.com/isaias-alt/vexillum/internal/cmdname"
)

// version is set at build time via -ldflags "-X main.version=...".
// It stays "dev" for a plain `go build` outside the release pipeline.
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	buildinfo.Version = version
	if len(args) == 0 {
		fmt.Print(cli.GeneralUsage())
		return 0
	}

	switch args[0] {
	case "-h", "--help":
		fmt.Print(cli.GeneralUsage())
		return 0
	case "-v", "--version":
		fmt.Println(buildinfo.FormatVersion(cmdname.Name, version, buildinfo.ReadVCS()))
		return 0
	case "init":
		return cli.Init(args[1:])
	case "upgrade":
		return cli.Upgrade(args[1:])
	case "doctor":
		return cli.Doctor(args[1:])
	case "dispatch":
		return cli.Dispatch(args[1:])
	case "models":
		return cli.Models(args[1:])
	case "yolo":
		return cli.Yolo(args[1:])
	case "redispatch":
		return cli.Redispatch(args[1:])
	case "decide":
		return cli.Decide(args[1:])
	case "prompt":
		return cli.Reprompt(args[1:])
	case "pending":
		return cli.Pending(args[1:])
	case "status":
		return cli.Status(args[1:])
	case "land":
		return cli.Land(args[1:])
	case "ship":
		return cli.Ship(args[1:])
	case "strike":
		return cli.Strike(args[1:])
	case "sentinel":
		return cli.Sentinel(args[1:])
	case "forum":
		return cli.Forum(args[1:])
	default:
		fmt.Fprintf(os.Stderr, cmdname.Name+": unknown command %q\n", args[0])
		fmt.Fprintln(os.Stderr, "Run '"+cmdname.Name+" --help' for a list of commands.")
		return 1
	}
}
