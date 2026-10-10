package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/install"
	"github.com/isaias-alt/vexillum/internal/models"
	"github.com/isaias-alt/vexillum/internal/scaffold"
	"github.com/isaias-alt/vexillum/internal/slot"
)

type slotPlan int

const (
	slotNothing slotPlan = iota
	slotWrite
	slotReportDrift
	slotRepair
	slotReportMalformed
)

type skillAct int

const (
	skillNothing skillAct = iota
	skillInstall
	skillRefresh
	skillOverwrite
	skillKeepEdited
	skillKeepStale
	skillKeepLinked
	skillKeepMissing
)

type skillPlan struct {
	status install.SkillStatus
	act    skillAct
}

// projectPlan is everything init/upgrade learned about the project before
// writing anything.
type projectPlan struct {
	kind      setupKind
	opts      setupOptions
	dir       string
	configDir string
	skillsDir string
	cfg       scaffold.Config
	cfgExists bool
	// slotFile is the file that holds, or will hold, the vexillum block:
	// AGENTS.md or CLAUDE.md.
	slotFile install.SlotFile
	// chooseSlot is true when the person may still pick the file: there is
	// no AGENTS.md and no block anywhere yet.
	chooseSlot bool
	lang       slot.Lang
	langSource install.LangSource
	slotIns    slot.Inspection
	tmpl       string
	claude     slot.ClaudeImport
	skills     []skillPlan
	hookNeeded bool
	hookErr    error
	needIgnore bool
	needModels bool
}

func (p *projectPlan) slotPlan() slotPlan {
	switch p.slotIns.State {
	case slot.StateAbsent, slot.StateStale:
		return slotWrite
	case slot.StateDrifted:
		if p.kind == setupUpgrade && p.opts.Force {
			return slotWrite
		}
		return slotReportDrift
	case slot.StateMalformed:
		if p.kind == setupUpgrade {
			return slotRepair
		}
		return slotReportMalformed
	}
	return slotNothing
}

func (p *projectPlan) skillsOf(act skillAct) []string {
	var names []string
	for _, s := range p.skills {
		if s.act == act {
			names = append(names, s.status.Name)
		}
	}
	return names
}

// inspectProject reads the project's state. It writes nothing.
func inspectProject(kind setupKind, opts setupOptions, projectDir string) (*projectPlan, error) {
	p := &projectPlan{
		kind:      kind,
		opts:      opts,
		dir:       projectDir,
		configDir: filepath.Join(projectDir, ".vexillum"),
		skillsDir: filepath.Join(projectDir, ".claude", "skills"),
	}
	cfg, err := scaffold.ReadConfig(p.configDir)
	switch {
	case err == nil:
		p.cfg, p.cfgExists = cfg, true
	case errors.Is(err, fs.ErrNotExist):
	default:
		return nil, fmt.Errorf("cannot read .vexillum/config.json: %w", err)
	}

	var file install.SlotFile
	if file, p.chooseSlot, err = install.LocateSlotFile(projectDir); err != nil {
		return nil, err
	}
	if err := p.useSlotFile(file); err != nil {
		return nil, err
	}

	if err := p.inspectSkills(); err != nil {
		return nil, err
	}

	p.hookNeeded, p.hookErr = scaffold.SentinelHookNeeded(projectDir)
	p.needIgnore = !exists(filepath.Join(p.configDir, ".gitignore"))
	p.needModels = !exists(filepath.Join(p.configDir, models.FileName))

	return p, nil
}

// useSlotFile points the plan at file as the home of the vexillum block and
// inspects the block in it, and what CLAUDE.md needs in response: nothing
// when it is the file that holds the block, the @AGENTS.md import otherwise.
func (p *projectPlan) useSlotFile(file install.SlotFile) error {
	var err error
	p.slotFile = file
	p.lang, p.langSource = install.ResolveLang(file.Content, p.opts.Lang)
	if p.slotIns, p.tmpl, err = install.InspectSlot(file.Content, p.lang); err != nil {
		return err
	}
	if file.Name == install.ClaudeFile {
		p.claude = slot.ClaudeImport{Path: file.Path, Exists: file.Exists, Imports: true}
		return nil
	}
	p.claude, err = slot.EnsureClaudeImport(p.dir)
	return err
}

// chooseSlotFile asks whether the block should go to CLAUDE.md when the
// project has no AGENTS.md. CLAUDE.md is the default answer, which --yes and
// a run without a terminal take; no creates an AGENTS.md instead.
func (p *projectPlan) chooseSlotFile(e *setupEnv) error {
	if !p.chooseSlot {
		return nil
	}
	p.chooseSlot = false
	if e.opts.Yes || !e.interactive {
		return nil
	}
	q := "This project has no AGENTS.md. Put the vexillum block in CLAUDE.md instead of creating an AGENTS.md?"
	if p.slotFile.Exists {
		q = "This project has no AGENTS.md. Add the vexillum block to your CLAUDE.md instead of creating an AGENTS.md?"
	}
	ok, err := e.prompt.confirm(q, true)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	agents, err := install.ReadSlotFile(p.dir, install.AgentsFile)
	if err != nil {
		return err
	}
	return p.useSlotFile(agents)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// inspectSkills classifies every skill and decides what init or upgrade
// would do with it.
func (p *projectPlan) inspectSkills() error {
	names := install.SkillNames()
	var sts []install.SkillStatus
	anyInstalled := false
	for _, n := range names {
		st, err := install.InspectSkill(p.skillsDir, n, p.cfg.Skills[n])
		if err != nil {
			return err
		}
		if st.State != install.SkillMissing {
			anyInstalled = true
		}
		sts = append(sts, st)
	}
	declined := p.opts.Skills != nil && !*p.opts.Skills
	for _, st := range sts {
		act := skillNothing
		switch {
		case declined:
		case st.Linked && st.State != install.SkillMissing:
			act = skillKeepLinked
		case st.State == install.SkillMissing:
			// init installs what is missing. upgrade only does so when
			// asked (--skills) or when no skill is installed yet (a project
			// being migrated), so a skill the user removed on purpose is
			// not brought back by every upgrade.
			if p.kind == setupInit || (p.opts.Skills != nil && *p.opts.Skills) || !anyInstalled {
				act = skillInstall
			} else {
				act = skillKeepMissing
			}
		case st.State == install.SkillStale:
			act = skillKeepStale
			if p.kind == setupUpgrade {
				act = skillRefresh
			}
		case st.State == install.SkillEdited:
			act = skillKeepEdited
			if p.kind == setupUpgrade && p.opts.Force {
				act = skillOverwrite
			}
		}
		p.skills = append(p.skills, skillPlan{status: st, act: act})
	}
	return nil
}

// userFileLines lists, for the consent notice, the files of the user's that
// the run will change.
func (p *projectPlan) userFileLines() []string {
	var lines []string
	pad := func(name, what string) { lines = append(lines, fmt.Sprintf("%-26s %s", name, what)) }
	name := p.slotFile.Name

	switch p.slotPlan() {
	case slotWrite:
		switch {
		case !p.slotFile.Exists:
			pad(name, "create it with the vexillum block")
		case p.slotIns.State == slot.StateAbsent:
			pad(name, "add the vexillum block")
		case p.slotIns.State == slot.StateDrifted:
			pad(name, "replace your edited vexillum block (your version is saved to "+slot.BackupRelPath+" first)")
		default:
			pad(name, "update the vexillum block")
		}
	case slotRepair:
		pad(name, "repair the malformed vexillum block (shows the repairs and asks first)")
	}
	switch p.claude.Action {
	case slot.ClaudeCreate:
		pad("CLAUDE.md", "create it with the "+slot.ClaudeImportLine+" import, so Claude Code sees the block")
	case slot.ClaudeAppend:
		pad("CLAUDE.md", "add the "+slot.ClaudeImportLine+" import, so Claude Code sees the block (asks first)")
	}
	if p.hookNeeded {
		pad(".claude/settings.json", "add the sentinel Stop hook")
	}
	return lines
}

// askedFirst is the note on the skills line of the notice: whether the
// person will still be asked about installing them.
func (p *projectPlan) askedFirst() string {
	if p.opts.Yes || p.opts.Skills != nil {
		return ""
	}
	return ", you are asked first"
}

// ownFileLines lists vexillum's own files the run writes.
func (p *projectPlan) ownFileLines() []string {
	var own []string
	if !p.cfgExists {
		own = append(own, ".vexillum/config.json")
	}
	if p.needIgnore {
		own = append(own, ".vexillum/.gitignore")
	}
	if p.needModels {
		own = append(own, ".vexillum/"+models.FileName)
	}
	if names := p.skillsOf(skillInstall); len(names) > 0 {
		own = append(own, ".claude/skills/ ("+install.SkillList(names)+p.askedFirst()+")")
	}
	if names := append(p.skillsOf(skillRefresh), p.skillsOf(skillOverwrite)...); len(names) > 0 {
		own = append(own, "the installed skills "+install.SkillList(names)+" in .claude/skills/")
	}
	return own
}

// hasWork reports whether there is anything to write.
func (p *projectPlan) hasWork() bool {
	if p.slotPlan() == slotWrite || p.slotPlan() == slotRepair {
		return true
	}
	if p.claude.Action != slot.ClaudeOK || p.hookNeeded || p.needIgnore || p.needModels || !p.cfgExists {
		return true
	}
	for _, s := range p.skills {
		switch s.act {
		case skillInstall, skillRefresh, skillOverwrite:
			return true
		}
	}
	return false
}

// report prints what was found that is not about to be written: edits that
// are left alone, a hook it could not add, and so on.
func (p *projectPlan) report(e *setupEnv) {
	switch p.slotPlan() {
	case slotReportDrift:
		e.say("%s: the vexillum block was edited by hand, so it is left as it is. What you have, against the current template:", p.slotFile.Name)
		e.say("%s", slot.UnifiedDiff(p.slotIns.Body, p.tmpl, "your edit", "vexillum template"))
		e.say("Re-run '%s upgrade --force' to replace it (your version is saved first).", cmdname.Name)
	case slotReportMalformed:
		e.say("%s: the vexillum block is malformed (%s), so it is left as it is. Run '%s upgrade' to repair it.", p.slotFile.Name, p.slotIns.Reason, cmdname.Name)
	}
	if p.hookErr != nil {
		e.warn("could not add the sentinel Stop hook: %v", p.hookErr)
	}
	p.reportSkills(e)
}

// reportSkills prints the skills that are left alone and why.
func (p *projectPlan) reportSkills(e *setupEnv) {
	for _, s := range p.skills {
		name := s.status.Name
		switch s.act {
		case skillKeepEdited:
			var hint string
			if p.kind == setupInit {
				hint = "left as it is; '" + cmdname.Name + " upgrade --force' overwrites it (a backup is saved first)"
			} else {
				hint = "left as it is; rerun with --force to overwrite it (a backup is saved first)"
			}
			e.say("Skill %s: edited by hand, %s.", name, hint)
		case skillKeepStale:
			e.say("Skill %s: older than this binary's copy. Run '%s upgrade' to refresh it.", name, cmdname.Name)
		case skillKeepLinked:
			if s.status.State != install.SkillCurrent {
				e.say("Skill %s: a symlink and %s, managed by you, so left as it is.", name, s.status.State)
			}
		case skillKeepMissing:
			e.say("Skill %s: not installed. Run '%s upgrade --skills' (or '%s init') to install it.", name, cmdname.Name, cmdname.Name)
		}
	}
}

// runInit is "vx init" for the current project.
func runInit(e *setupEnv, projectDir, vexillumHome string) int {
	if err := refuseInsideVexillumHome(projectDir, vexillumHome); err != nil {
		return e.fail("%v", err)
	}
	if !scaffold.GitInstalled() {
		fmt.Fprintln(e.stderr, cmdname.Name+": "+scaffold.GitMissingMessage)
		return 1
	}
	if !scaffold.IsGitRepo(projectDir) {
		fmt.Fprintln(e.stderr, cmdname.Name+": current directory is not a git repository")
		fmt.Fprintln(e.stderr, cmdname.Name+" requires git; run 'git init' first.")
		return 1
	}
	return runProjectSetup(e, setupInit, projectDir, vexillumHome)
}

// runUpgrade is "vx upgrade" for the current project.
func runUpgrade(e *setupEnv, projectDir, vexillumHome string) int {
	if err := refuseInsideVexillumHome(projectDir, vexillumHome); err != nil {
		return e.fail("%v", err)
	}
	if !scaffold.ProjectInitialized(projectDir) {
		fmt.Fprintln(e.stderr, cmdname.Name+": project not initialized here (no .vexillum/config.json)")
		fmt.Fprintln(e.stderr, "run '"+cmdname.Name+" init' first.")
		return 1
	}
	return runProjectSetup(e, setupUpgrade, projectDir, vexillumHome)
}

func runProjectSetup(e *setupEnv, kind setupKind, projectDir, vexillumHome string) int {
	p, err := inspectProject(kind, e.opts, projectDir)
	if err != nil {
		return e.fail("%v", err)
	}
	if err := p.chooseSlotFile(e); err != nil {
		return e.askFailed(err)
	}
	p.report(e)

	if !p.hasWork() {
		if kind == setupInit {
			e.say("Project already initialized and up to date. Nothing to change.")
		} else {
			e.say("Everything is already up to date. Nothing to change.")
		}
		return 0
	}

	proceed, code := e.consent(kind, " in "+projectDir, p.userFileLines(), p.ownFileLines())
	if !proceed {
		return code
	}
	return p.apply(e, vexillumHome)
}

// apply performs the writes after consent.
func (p *projectPlan) apply(e *setupEnv, vexillumHome string) int {
	created, err := scaffold.EnsureDir(vexillumHome)
	if err != nil {
		return e.fail("cannot create %s: %v", vexillumHome, err)
	}
	if created {
		e.say("Created %s", vexillumHome)
	}

	if !p.cfgExists {
		if err := scaffold.WriteConfig(p.configDir); err != nil {
			return e.fail("cannot write .vexillum/config.json: %v", err)
		}
		if p.cfg, err = scaffold.ReadConfig(p.configDir); err != nil {
			return e.fail("cannot read .vexillum/config.json: %v", err)
		}
		e.say("Created .vexillum/config.json")
	}
	cfgDirty := false

	// forum artifacts live in .vexillum/forum/ and are scratch; ignore them
	// without touching the user's own root .gitignore.
	if p.needIgnore {
		if _, err := scaffold.EnsureForumIgnore(p.configDir); err != nil {
			return e.fail("cannot write .vexillum/.gitignore: %v", err)
		}
		e.say("Created .vexillum/.gitignore (ignores .vexillum/forum/)")
	}

	// models.json is the user's to edit: only ever created, never touched
	// once it exists.
	if p.needModels {
		if err := writeDefaultModels(p.configDir); err != nil {
			return e.fail("cannot write .vexillum/%s: %v", models.FileName, err)
		}
		e.say("Created .vexillum/%s (edit it to change the model and effort profiles)", models.FileName)
	}

	if code := p.applySlot(e); code != 0 {
		return code
	}
	if code := p.applyClaudeImport(e); code != 0 {
		return code
	}
	if code := p.applySkills(e, &cfgDirty); code != 0 {
		return code
	}

	// .claude/settings.json isn't vexillum's file - it's the user's own
	// Claude Code config. A malformed existing file is a pre-existing
	// problem, reported earlier, not a failure of this run.
	if p.hookNeeded && p.hookErr == nil {
		if added, err := scaffold.EnsureSentinelHook(p.dir); err != nil {
			e.warn("could not add the sentinel Stop hook: %v", err)
		} else if added {
			e.say("Added %s sentinel Stop hook to .claude/settings.json", cmdname.Name)
		}
	}

	if cfgDirty {
		if err := scaffold.SaveConfig(p.configDir, p.cfg); err != nil {
			return e.fail("cannot update .vexillum/config.json: %v", err)
		}
	}

	if p.kind == setupInit {
		e.say("%s initialized.", cmdname.Name)
	} else {
		e.say("%s upgrade complete.", cmdname.Name)
	}
	return 0
}

func writeDefaultModels(configDir string) error {
	return os.WriteFile(filepath.Join(configDir, models.FileName), models.DefaultJSON(), 0o644)
}

// applySlot writes (or repairs) the vexillum block in the slot file.
func (p *projectPlan) applySlot(e *setupEnv) int {
	plan := p.slotPlan()
	if plan != slotWrite && plan != slotRepair {
		return 0
	}
	name := p.slotFile.Name
	target := name
	if !p.slotFile.Exists {
		target = name + " (it does not exist yet)"
	}
	lang, err := e.chooseLang(p.lang, p.langSource, target)
	if err != nil {
		return e.askFailed(err)
	}
	if lang != p.lang {
		p.lang = lang
		if p.slotIns, p.tmpl, err = install.InspectSlot(p.slotFile.Content, lang); err != nil {
			return e.fail("%v", err)
		}
	}
	if !p.slotFile.Exists {
		e.say("Language: %s", lang)
	}

	content := p.slotFile.Content
	if plan == slotRepair {
		repaired, actions := slot.Repair(content)
		e.say("%s: the vexillum block is malformed (%s). Repairs:", name, p.slotIns.Reason)
		for _, a := range actions {
			e.say("  - %s", a)
		}
		ok, err := e.ask("Apply these repairs?", true)
		if err != nil {
			return e.askFailed(err)
		}
		if !ok {
			e.say("%s was left as it is.", name)
			return 0
		}
		backup, err := install.SaveSlotFileBackup(p.dir, name, content)
		if err != nil {
			return e.fail("%v", err)
		}
		e.say("Saved the file as it was to %s", relTo(p.dir, backup))
		content = repaired
		if p.slotIns, _, err = install.InspectSlot(content, p.lang); err != nil {
			return e.fail("%v", err)
		}
	}

	force := false
	if p.slotIns.State == slot.StateDrifted {
		if !(p.kind == setupUpgrade && p.opts.Force) {
			// Only reachable after a repair: write the repaired markers
			// and leave the edited body alone.
			if err := slot.WriteFile(p.slotFile.Path, content); err != nil {
				return e.fail("cannot write %s: %v", name, err)
			}
			e.say("%s: markers repaired. The block was edited by hand, so its text is left as it is; run '%s upgrade --force' to replace it.", name, cmdname.Name)
			return 0
		}
		force = true
		backup, err := slot.SaveBackup(p.dir, p.slotIns.Body)
		if err != nil {
			return e.fail("%v", err)
		}
		e.say("Saved your edited block to %s", relTo(p.dir, backup))
	}

	out, err := slot.Upsert(content, p.tmpl, force)
	if err != nil {
		return e.fail("cannot update the vexillum block: %v", err)
	}
	if out != p.slotFile.Content {
		if err := slot.WriteFile(p.slotFile.Path, out); err != nil {
			return e.fail("cannot write %s: %v", name, err)
		}
		switch {
		case !p.slotFile.Exists:
			e.say("Created %s with the vexillum block", name)
		case p.slotIns.State == slot.StateAbsent:
			e.say("Added the vexillum block to %s", name)
		default:
			e.say("Updated the vexillum block in %s", name)
		}
	}
	p.slotFile.Content, p.slotFile.Exists = out, true
	return 0
}

// applyClaudeImport makes sure CLAUDE.md imports AGENTS.md, so Claude Code,
// which does not read AGENTS.md on its own when a CLAUDE.md exists, sees the
// block. Editing an existing CLAUDE.md is asked about separately.
func (p *projectPlan) applyClaudeImport(e *setupEnv) int {
	c := p.claude
	switch c.Action {
	case slot.ClaudeCreate:
		if err := slot.WriteFile(c.Path, c.NewContent); err != nil {
			return e.fail("cannot write CLAUDE.md: %v", err)
		}
		e.say("Created CLAUDE.md with the %s import", slot.ClaudeImportLine)
	case slot.ClaudeAppend:
		ok, err := e.ask(fmt.Sprintf("CLAUDE.md does not import AGENTS.md, so Claude Code would not see the vexillum block. Add the line %s to it?", slot.ClaudeImportLine), true)
		if err != nil {
			return e.askFailed(err)
		}
		if !ok {
			e.warn("CLAUDE.md does not import AGENTS.md, so Claude Code will not see the vexillum block. Add the line %s to CLAUDE.md yourself.", slot.ClaudeImportLine)
			return 0
		}
		if err := slot.WriteFile(c.Path, c.NewContent); err != nil {
			return e.fail("cannot write CLAUDE.md: %v", err)
		}
		e.say("Added the %s import to CLAUDE.md", slot.ClaudeImportLine)
	}
	return 0
}

// applySkills installs, refreshes or overwrites the skills as planned and
// records what it wrote in the config.
func (p *projectPlan) applySkills(e *setupEnv, cfgDirty *bool) int {
	install1 := p.skillsOf(skillInstall)
	doInstall := false
	if len(install1) > 0 {
		switch {
		case p.opts.Skills != nil:
			doInstall = *p.opts.Skills
		default:
			e.say("These skills teach the commander how to use vexillum well.")
			ok, err := e.ask(skillQuestion(install1), true)
			if err != nil {
				return e.askFailed(err)
			}
			doInstall = ok
		}
	}

	if p.cfg.Skills == nil {
		p.cfg.Skills = map[string]string{}
	}
	record := func(name, hash string) {
		if p.cfg.Skills[name] != hash {
			p.cfg.Skills[name] = hash
			*cfgDirty = true
		}
	}
	backupRoot := filepath.Join(p.configDir, "backups", "skills")
	for _, s := range p.skills {
		name := s.status.Name
		switch s.act {
		case skillInstall:
			if !doInstall {
				continue
			}
			if err := install.InstallSkill(p.skillsDir, name); err != nil {
				return e.fail("%v", err)
			}
			e.say("Installed skill %s (.claude/skills/%s)", name, name)
		case skillRefresh:
			if err := install.InstallSkill(p.skillsDir, name); err != nil {
				return e.fail("%v", err)
			}
			e.say("Refreshed skill %s", name)
		case skillOverwrite:
			dest, err := install.BackupSkill(p.skillsDir, name, backupRoot)
			if err != nil {
				return e.fail("%v", err)
			}
			if err := install.InstallSkill(p.skillsDir, name); err != nil {
				return e.fail("%v", err)
			}
			e.say("Overwrote skill %s (your version is saved in %s)", name, relTo(p.dir, dest))
		case skillNothing:
			if s.status.State != install.SkillCurrent {
				continue
			}
			// Already what this binary ships: just make sure the config
			// knows, so a later upgrade can tell a stale copy from an
			// edited one.
		default:
			continue
		}
		h, err := installedHash(p.skillsDir, name)
		if err != nil {
			return e.fail("%v", err)
		}
		record(name, h)
	}
	return 0
}

// installedHash is the content hash of what is now installed for name.
func installedHash(skillsDir, name string) (string, error) {
	st, err := install.InspectSkill(skillsDir, name, "")
	if err != nil {
		return "", err
	}
	return st.Hash, nil
}

// relTo shows path relative to base when it lies under it.
func relTo(base, path string) string {
	if rel, err := filepath.Rel(base, path); err == nil && !filepath.IsAbs(rel) && rel != ".." && !hasDotDot(rel) {
		return rel
	}
	return path
}

func hasDotDot(rel string) bool {
	return len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator)
}
