package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadSpawnsKeepsOnlyDirectChildren(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spawn.log")
	log := "" +
		"1000\t7000000\t42\tgit\t0\trev-parse --show-toplevel\n" +
		"1001\t5000000\t99\therdr\t0\tagent get x\n" + // another parent, e.g. a detached sentinel
		"1002\t6000000\t42\therdr\t1\tagent get y\n" +
		"not a log line\n"
	if err := os.WriteFile(path, []byte(log), 0o644); err != nil {
		t.Fatal(err)
	}
	spawns, err := readSpawns(path, 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(spawns) != 2 {
		t.Fatalf("want the 2 spawns of pid 42, got %+v", spawns)
	}
	if spawns[0].Tool != "git" || spawns[0].Duration != 7*time.Millisecond || spawns[0].Args != "rev-parse --show-toplevel" {
		t.Errorf("unexpected first spawn: %+v", spawns[0])
	}
	if spawns[1].Exit != 1 {
		t.Errorf("the exit code must be kept, got %+v", spawns[1])
	}
}

func TestReadSpawnsMissingLogMeansNoSpawns(t *testing.T) {
	spawns, err := readSpawns(filepath.Join(t.TempDir(), "absent.log"), 1)
	if err != nil || len(spawns) != 0 {
		t.Errorf("a command that spawned nothing writes no log: got %v, %v", spawns, err)
	}
}

func TestSummarizeSpawnsGroupsByToolAndFirstTwoArguments(t *testing.T) {
	sum := summarizeSpawns([]spawn{
		{Tool: "git", Args: "rev-parse --abbrev-ref HEAD", Duration: 7 * time.Millisecond},
		{Tool: "git", Args: "rev-parse --abbrev-ref HEAD", Duration: 9 * time.Millisecond},
		{Tool: "git", Args: "worktree add -b vexillum/x /p main", Duration: 40 * time.Millisecond},
	})
	if sum.Count != 3 || sum.TotalMS != 56 {
		t.Fatalf("unexpected totals: %+v", sum)
	}
	if len(sum.Calls) != 2 || sum.Calls[0].Call != "git worktree add" || sum.Calls[1].Count != 2 {
		t.Errorf("calls must be grouped and the most expensive first: %+v", sum.Calls)
	}
}
