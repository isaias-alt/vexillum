package main

import "testing"

func TestCountRunnableLeavesOutTheScenariosThatWillBeSkipped(t *testing.T) {
	scs := []scenario{
		{name: "cheap"},
		{name: "slow one", slow: true},
		{name: "real herdr", realHerdr: true},
		{name: "real tools", realTools: true},
		{name: "needs a missing tool", needs: "vxbench-no-such-tool"},
	}
	if got := (&bench{}).countRunnable(scs); got != 1 {
		t.Errorf("countRunnable with no flag = %d, want 1", got)
	}
	all := &bench{cfg: config{slow: true, realHerdr: true, realTools: true}}
	if got := all.countRunnable(scs); got != 4 {
		t.Errorf("countRunnable with every flag = %d, want 4 (the missing tool still skips)", got)
	}
}
