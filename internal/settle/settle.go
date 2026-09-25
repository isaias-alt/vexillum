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
