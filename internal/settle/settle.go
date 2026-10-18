// Package settle corroborates whether a task that just went idle/done
// according to herdr actually finished, or only looks like it did - idle
// alone never proves why (see internal/pause's package doc). It exists so
// internal/sentinel's polling loop and internal/soldier's own quick-settle
// probe (right after dispatch) reach the same verdict from the same
// evidence, instead of two independent paths applying different criteria
// to the same decision - the bug that let a soldier with a valid,
// unexpired pause file and no new commits get recorded as Done straight
// out of dispatch, before the sentinel ever got a chance to check.
package settle

import (
	"regexp"
	"strings"

	"github.com/isaias-alt/vexillum/internal/camp"
	"github.com/isaias-alt/vexillum/internal/report"
	"github.com/isaias-alt/vexillum/internal/state"
)

// HasCompletionSignal reports whether task already has hard proof of
// completion: a scout's internal/report file, or a mission's own commit
// ahead of its camp's base (internal/camp.HasNewCommits). A mission
// missing CampPath/CampBase (a task dispatched before this field existed,
// or one whose camp acquisition never completed) or a camp.HasNewCommits
// error (base ref moved, camp worktree gone) reports no signal rather
// than guessing - a caller must never let a single unreadable camp stand
// in for proof, so "can't prove it" is treated the same as "not proven".
func HasCompletionSignal(projectRoot string, task state.Task) (bool, string, error) {
	switch task.Kind {
	case state.KindScout:
		if report.Exists(projectRoot, task.HerdrAgentName, task.ID) {
			return true, report.Path(projectRoot, task.HerdrAgentName, task.ID), nil
		}
		return false, "", nil
	case state.KindMission:
		if task.CampPath == "" || task.CampBase == "" {
			return false, "", nil
		}
		has, err := camp.HasNewCommits(task.CampPath, task.CampBase)
		if err != nil {
			return false, "", nil
		}
		return has, "", nil
	default:
		return false, "", nil
	}
}

// backgroundWorkTailLines is how many trailing non-empty lines of a pane
// BackgroundWorkPending looks at: the turn summary sits just above the input
// box and the status line, a handful of lines from the bottom.
const backgroundWorkTailLines = 12

var (
	// turnSummaryRE matches the line Claude Code prints when a turn ends, for
	// example "✻ Sautéed for 3s · done 3:38 PM".
	turnSummaryRE = regexp.MustCompile(`\S\s+\p{L}+ for \d+\s*[hms]\b`)

	// backgroundWorkRE matches the segment that line gets while a background
	// job the turn started is still going: "· 1 shell still running".
	backgroundWorkRE = regexp.MustCompile(`·\s*(\d+\s+[^·\n]*?\bstill running)\b`)
)

// BackgroundWorkPending reports whether output, a pane read, ends on a turn
// that Claude Code itself says has background work still running, and returns
// that segment of its turn summary ("1 shell still running"). Only the last
// turn summary counts: an earlier turn's line further up says nothing about
// the turn that just ended.
//
// herdr reports such a pane as idle, but the soldier is not finished: when the
// job completes it wakes up and writes its real final report. Settling the
// task done on that idle would save a transcript without the report, so both
// internal/sentinel and the quick-settle probe in internal/soldier hold the
// task running instead.
func BackgroundWorkPending(output string) (string, bool) {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	seen := 0
	for i := len(lines) - 1; i >= 0 && seen < backgroundWorkTailLines; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		seen++
		if !turnSummaryRE.MatchString(lines[i]) {
			continue
		}
		if m := backgroundWorkRE.FindStringSubmatch(lines[i]); m != nil {
			return strings.Join(strings.Fields(m[1]), " "), true
		}
		return "", false
	}
	return "", false
}
