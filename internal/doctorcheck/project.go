package doctorcheck

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/install"
	"github.com/isaias-alt/vexillum/internal/models"
	"github.com/isaias-alt/vexillum/internal/scaffold"
	"github.com/isaias-alt/vexillum/internal/slot"
)

// ProjectChecks reports on what vexillum put in an initialized project: the
// AGENTS.md block, the CLAUDE.md import, the first-party skills, a leftover
// rules file from an older version, models.json, and the sentinel Stop hook
// with the sentinel behind it. Every check is
// read-only, and none is Required: a stale or missing piece is something to
// fix with init or upgrade, not a reason to call the environment unusable.
func ProjectChecks(projectDir, vexillumHome, homeDir string) []Result {
	var out []Result
	slotRes, slotState := Slot(projectDir)
	out = append(out, slotRes)
	if slotState != slot.StateAbsent {
		out = append(out, ClaudeImport(projectDir))
	}
	if r, ok := LegacyRules(projectDir); ok {
		out = append(out, r)
	}
	out = append(out, Skills(projectDir, vexillumHome, homeDir)...)
	out = append(out, Models(projectDir, vexillumHome))
	out = append(out, StopHooks(projectDir, vexillumHome, homeDir)...)
	return out
}

const slotName = "AGENTS.md vexillum block"

// Slot reports the state of the vexillum block in projectDir/AGENTS.md:
// absent, current, stale, drifted or malformed. The template it compares
// with is the one for the language the block itself is written in.
func Slot(projectDir string) (Result, slot.State) {
	agents, err := install.ReadAgents(projectDir)
	if err != nil {
		return Result{Name: slotName, Warn: true, Detail: err.Error()}, slot.StateAbsent
	}
	lang, _ := install.ResolveLang(agents.Content, "")
	ins, _, err := install.InspectSlot(agents.Content, lang)
	if err != nil {
		return Result{Name: slotName, Warn: true, Detail: err.Error()}, slot.StateAbsent
	}
	up := "run '" + cmdname.Name + " upgrade'"
	switch ins.State {
	case slot.StateCurrent:
		return Result{Name: slotName, OK: true, Detail: fmt.Sprintf("current (%s)", lang)}, ins.State
	case slot.StateStale:
		return Result{Name: slotName, Warn: true, Detail: "stale, older than this binary's template; " + up}, ins.State
	case slot.StateDrifted:
		return Result{Name: slotName, Warn: true, Detail: "drifted, edited by hand; " + up + " shows the diff (--force overwrites, after a backup)"}, ins.State
	case slot.StateMalformed:
		return Result{Name: slotName, Warn: true, Detail: "malformed (" + ins.Reason + "); " + up + " lists the repairs"}, ins.State
	}
	return Result{Name: slotName, Detail: "absent, run '" + cmdname.Name + " init'"}, slot.StateAbsent
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

// LegacyRules reports a .claude/rules/vexillum.md left by an older version.
// ok is false when there is none.
func LegacyRules(projectDir string) (Result, bool) {
	path := filepath.Join(projectDir, ".claude", "rules", "vexillum.md")
	if _, err := os.Stat(path); err != nil {
		return Result{}, false
	}
	return Result{
		Name:   ".claude/rules/vexillum.md",
		Warn:   true,
		Detail: "from an older vexillum, now redundant with the AGENTS.md block and the skills; run '" + cmdname.Name + " upgrade' to migrate",
	}, true
}

// Skills reports each first-party skill: where it is installed (the project
// first, then globally) and how it compares with the embedded copy.
func Skills(projectDir, vexillumHome, homeDir string) []Result {
	projectCfg, _ := scaffold.ReadConfig(filepath.Join(projectDir, ".vexillum"))
	globalCfg, _ := scaffold.ReadConfig(vexillumHome)

	var out []Result
	for _, name := range install.SkillNames() {
		label := "skill " + name
		var found *install.SkillStatus
		where := ""
		for _, loc := range []struct {
			dir, cfgHash, where string
		}{
			{filepath.Join(projectDir, ".claude", "skills"), projectCfg.Skills[name], ""},
			{filepath.Join(homeDir, ".claude", "skills"), globalCfg.Skills[name], " (global)"},
		} {
			st, err := install.InspectSkill(loc.dir, name, loc.cfgHash)
			if err != nil {
				out = append(out, Result{Name: label, Warn: true, Detail: err.Error()})
				found = nil
				break
			}
			if st.State != install.SkillMissing {
				found, where = &st, loc.where
				break
			}
		}
		if found == nil {
			if len(out) > 0 && out[len(out)-1].Name == label {
				continue // the error above already speaks for this skill
			}
			out = append(out, Result{Name: label, Detail: "missing, run '" + cmdname.Name + " init' to install it"})
			continue
		}
		var notes []string
		if found.Linked {
			notes = append(notes, "symlink")
		}
		if where != "" {
			notes = append(notes, "global")
		}
		suffix := ""
		if len(notes) > 0 {
			suffix = " (" + strings.Join(notes, ", ") + ")"
		}
		switch found.State {
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
