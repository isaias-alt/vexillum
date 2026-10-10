package doctorcheck

import (
	"fmt"
	"path/filepath"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/install"
	"github.com/isaias-alt/vexillum/internal/models"
	"github.com/isaias-alt/vexillum/internal/scaffold"
	"github.com/isaias-alt/vexillum/internal/slot"
	"github.com/isaias-alt/vexillum/internal/yolo"
)

// ProjectChecks reports on what vexillum put in an initialized project: the
// vexillum block (in AGENTS.md or CLAUDE.md), the CLAUDE.md import when the
// block is in AGENTS.md, the first-party skills, a leftover rules file from
// an older version, models.json, yolo mode, and the sentinel Stop hook with
// the sentinel behind it. Every check is read-only, and none is Required: a
// stale or missing piece is something to fix with init or upgrade, not a
// reason to call the environment unusable.
func ProjectChecks(projectDir, vexillumHome, homeDir string) []Result {
	var out []Result
	slotRes, slotState, inClaude := Slot(projectDir)
	out = append(out, slotRes)
	if slotState != slot.StateAbsent && !inClaude {
		out = append(out, ClaudeImport(projectDir))
	}
	out = append(out, Skills(projectDir)...)
	out = append(out, Models(projectDir, vexillumHome))
	out = append(out, Yolo(projectDir))
	out = append(out, StopHooks(projectDir, vexillumHome, homeDir)...)
	return out
}

// Slot reports the state of the vexillum block in the file that holds it,
// AGENTS.md or CLAUDE.md (see install.LocateSlotFile): absent, current,
// stale, drifted or malformed. The template it compares with is the one for
// the language the block itself is written in. inClaude is true when that
// file is CLAUDE.md.
func Slot(projectDir string) (res Result, state slot.State, inClaude bool) {
	name := install.AgentsFile + " vexillum block"
	file, _, err := install.LocateSlotFile(projectDir)
	if err != nil {
		return Result{Name: name, Warn: true, Detail: err.Error()}, slot.StateAbsent, false
	}
	inClaude = file.Name == install.ClaudeFile
	slotName := file.Name + " vexillum block"
	lang, _ := install.ResolveLang(file.Content, "")
	ins, _, err := install.InspectSlot(file.Content, lang)
	if err != nil {
		return Result{Name: slotName, Warn: true, Detail: err.Error()}, slot.StateAbsent, inClaude
	}
	up := "run '" + cmdname.Name + " upgrade'"
	switch ins.State {
	case slot.StateCurrent:
		return Result{Name: slotName, OK: true, Detail: fmt.Sprintf("current (%s)", lang)}, ins.State, inClaude
	case slot.StateStale:
		return Result{Name: slotName, Warn: true, Detail: "stale, older than this binary's template; " + up}, ins.State, inClaude
	case slot.StateDrifted:
		return Result{Name: slotName, Warn: true, Detail: "drifted, edited by hand; " + up + " shows the diff (--force overwrites, after a backup)"}, ins.State, inClaude
	case slot.StateMalformed:
		return Result{Name: slotName, Warn: true, Detail: "malformed (" + ins.Reason + "); " + up + " lists the repairs"}, ins.State, inClaude
	}
	return Result{Name: slotName, Detail: "absent, run '" + cmdname.Name + " init'"}, slot.StateAbsent, inClaude
}

// ClaudeImport reports whether CLAUDE.md imports AGENTS.md, which is how
// Claude Code gets to see the vexillum block.
func ClaudeImport(projectDir string) Result {
	const name = "CLAUDE.md imports AGENTS.md"
	c, err := slot.EnsureClaudeImport(projectDir)
	if err != nil {
		return Result{Name: name, Warn: true, Detail: err.Error()}
	}
	if c.Action == slot.ClaudeOK {
		return Result{Name: name, OK: true}
	}
	why := "CLAUDE.md does not exist"
	if c.Exists {
		why = "CLAUDE.md has no " + slot.ClaudeImportLine + " line"
	}
	return Result{Name: name, Warn: true, Detail: why + ", so Claude Code will not see the vexillum block; add the line, or run '" + cmdname.Name + " init'"}
}

// Skills reports each first-party skill installed in the project and how it
// compares with the embedded copy.
func Skills(projectDir string) []Result {
	cfg, _ := scaffold.ReadConfig(filepath.Join(projectDir, ".vexillum"))

	var out []Result
	for _, name := range install.SkillNames() {
		label := "skill " + name
		st, err := install.InspectSkill(filepath.Join(projectDir, ".claude", "skills"), name, cfg.Skills[name])
		if err != nil {
			out = append(out, Result{Name: label, Warn: true, Detail: err.Error()})
			continue
		}
		if st.State == install.SkillMissing {
			out = append(out, Result{Name: label, Detail: "missing, run '" + cmdname.Name + " init' to install it"})
			continue
		}
		suffix := ""
		if st.Linked {
			suffix = " (symlink)"
		}
		switch st.State {
		case install.SkillCurrent:
			out = append(out, Result{Name: label, OK: true, Detail: "installed, current" + suffix})
		case install.SkillStale:
			out = append(out, Result{Name: label, Warn: true, Detail: "installed, stale" + suffix + "; run '" + cmdname.Name + " upgrade'"})
		default:
			out = append(out, Result{Name: label, Warn: true, Detail: "installed, edited by hand" + suffix + "; '" + cmdname.Name + " upgrade --force' replaces it (after a backup)"})
		}
	}
	return out
}

// Models reports whether the model profiles load: the embedded defaults
// merged with ~/.vexillum/models.json and the project's .vexillum/models.json.
// An invalid file is a warning naming the file and field.
func Models(projectDir, vexillumHome string) Result {
	const name = models.FileName
	t, err := models.Load(projectDir, vexillumHome)
	if err != nil {
		return Result{Name: name, Warn: true, Detail: "invalid: " + err.Error()}
	}
	detail := fmt.Sprintf("valid, %d profiles", len(t.Profiles))
	if len(t.Sources) == 0 {
		detail += " (built-in defaults only)"
	}
	return Result{Name: name, OK: true, Detail: detail}
}

// Yolo reports whether yolo mode is on for the project: the commander lands
// a finished, verified mission without asking the general first. Either state
// is healthy; an invalid .vexillum/yolo.json is a warning, and the commander
// treats anything but "on" as off.
func Yolo(projectDir string) Result {
	const name = "yolo mode"
	on, err := yolo.Enabled(projectDir)
	if err != nil {
		return Result{Name: name, Warn: true, Detail: "invalid: " + err.Error()}
	}
	if on {
		return Result{Name: name, OK: true, Detail: "on, the commander lands a finished mission without asking (" + cmdname.Name + " yolo off to stop)"}
	}
	return Result{Name: name, OK: true, Detail: "off"}
}
