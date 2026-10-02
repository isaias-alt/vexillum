package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/install"
	"github.com/isaias-alt/vexillum/internal/scaffold"
	"github.com/isaias-alt/vexillum/internal/slot"
)

// The global scaffold (--global) applies to every Claude Code session on the
// machine. There is no project AGENTS.md to hold a vexillum block, and
// editing the user's global ~/.claude/CLAUDE.md or an arbitrary AGENTS.md
// would be far more surprising than what the global scaffold always did:
// own a single file, ~/.claude/rules/vexillum.md, which Claude Code loads
// unconditionally. So that path stays for --global, but its content is now
// the same short always-on core that goes into a project's AGENTS.md block
// (without markers, since the file is entirely vexillum's), and the skills
// that carry the operational detail are installed to ~/.claude/skills/. The
// hash of what was written is kept in ~/.vexillum/config.json so upgrade can
// tell an untouched file from an edited one.

const globalRulesRel = ".claude/rules/vexillum.md"

type globalPlan struct {
	*projectPlan
	home        string
	rulesPath   string
	rules       string // current content, "" when missing
	rulesExists bool
	core        string
	rulesWrite  bool
	rulesBackup bool // overwrite an edited file (--force): save it first
	rulesEdited bool // differs and is not safe to refresh
	rulesStale  bool // init only: differs but was not touched by the user
}

func inspectGlobal(kind setupKind, opts setupOptions, vexillumHome, home string) (*globalPlan, error) {
	g := &globalPlan{
		projectPlan: &projectPlan{
			kind:      kind,
			opts:      opts,
			dir:       home,
			configDir: vexillumHome,
			skillsDir: filepath.Join(home, ".claude", "skills"),
		},
		home:      home,
		rulesPath: filepath.Join(home, filepath.FromSlash(globalRulesRel)),
	}
	p := g.projectPlan
	if cfg, err := scaffold.ReadConfig(vexillumHome); err == nil {
		p.cfg, p.cfgExists = cfg, true
	} else if scaffold.GlobalInitialized(vexillumHome) {
		return nil, fmt.Errorf("cannot read %s: %w", filepath.Join(vexillumHome, "config.json"), err)
	}

	data, err := os.ReadFile(g.rulesPath)
	switch {
	case err == nil:
		g.rules, g.rulesExists = string(data), true
	case !os.IsNotExist(err):
		return nil, fmt.Errorf("reading %s: %w", g.rulesPath, err)
	}

	p.lang, p.langSource = install.ResolveLang(g.rules, opts.Lang)
	if g.core, err = install.SlotTemplate(p.lang); err != nil {
		return nil, err
	}
	if err := p.inspectSkills(); err != nil {
		return nil, err
	}

	switch {
	case !g.rulesExists:
		g.rulesWrite = true
	case g.rules == g.core:
	default:
		untouched := p.cfg.VexillumRuleHash != "" && p.cfg.VexillumRuleHash == scaffold.HashContent(g.rules)
		switch {
		case untouched && kind == setupUpgrade:
			g.rulesWrite = true
		case untouched:
			g.rulesStale = true
		case kind == setupUpgrade && opts.Force:
			g.rulesWrite, g.rulesBackup = true, true
		default:
			g.rulesEdited = true
		}
	}
	return g, nil
}

func (g *globalPlan) hasWork() bool {
	if g.rulesWrite || !g.cfgExists {
		return true
	}
	for _, s := range g.skills {
		switch s.act {
		case skillInstall, skillRefresh, skillOverwrite:
			return true
		}
	}
	return false
}

func (g *globalPlan) report(e *setupEnv) {
	switch {
	case g.rulesEdited:
		e.say("~/%s: you edited it, so it is left as it is. Rerun '%s upgrade --global --force' to replace it with the current core (a backup is saved first).", globalRulesRel, cmdname.Name)
	case g.rulesStale:
		e.say("~/%s is older than this binary's core. Run '%s upgrade --global' to refresh it.", globalRulesRel, cmdname.Name)
	}
	g.projectPlan.reportSkills(e)
}

func runInitGlobal(e *setupEnv, vexillumHome, home string) int {
	return runGlobalSetup(e, setupInit, vexillumHome, home)
}

func runUpgradeGlobal(e *setupEnv, vexillumHome, home string) int {
	if !scaffold.GlobalInitialized(vexillumHome) {
		fmt.Fprintf(e.stderr, cmdname.Name+": global scaffold not initialized (no %s)\n", filepath.Join(vexillumHome, "config.json"))
		fmt.Fprintln(e.stderr, "run '"+cmdname.Name+" init --global' first.")
		return 1
	}
	return runGlobalSetup(e, setupUpgrade, vexillumHome, home)
}

func runGlobalSetup(e *setupEnv, kind setupKind, vexillumHome, home string) int {
	g, err := inspectGlobal(kind, e.opts, vexillumHome, home)
	if err != nil {
		return e.fail("%v", err)
	}
	g.report(e)

	if !g.hasWork() {
		if kind == setupInit {
			e.say("Already initialized globally and up to date. Nothing to change.")
		} else {
			e.say("Everything is already up to date (global). Nothing to change.")
		}
		return 0
	}

	var userFiles []string
	if g.rulesWrite {
		what := "write the commander core"
		if g.rulesExists {
			what = "replace it with the current commander core"
			if g.rulesBackup {
				what += " (your version is saved to ~/.vexillum/backups first)"
			}
		}
		userFiles = append(userFiles, fmt.Sprintf("%-26s %s", "~/"+globalRulesRel, what))
	}
	var own []string
	if !g.cfgExists {
		own = append(own, "~/.vexillum/config.json")
	}
	if names := g.skillsOf(skillInstall); len(names) > 0 {
		own = append(own, "~/.claude/skills/ ("+install.SkillList(names)+g.askedFirst()+")")
	}
	if names := append(g.skillsOf(skillRefresh), g.skillsOf(skillOverwrite)...); len(names) > 0 {
		own = append(own, "the installed skills "+install.SkillList(names)+" in ~/.claude/skills/")
	}
	proceed, code := e.consent(kind, " (this applies to every Claude Code session on this machine)", userFiles, own)
	if !proceed {
		return code
	}

	created, err := scaffold.EnsureDir(vexillumHome)
	if err != nil {
		return e.fail("cannot create %s: %v", vexillumHome, err)
	}
	if created {
		e.say("Created %s", vexillumHome)
	}
	if !g.cfgExists {
		if err := scaffold.WriteConfig(vexillumHome); err != nil {
			return e.fail("cannot write %s: %v", filepath.Join(vexillumHome, "config.json"), err)
		}
		if g.cfg, err = scaffold.ReadConfig(vexillumHome); err != nil {
			return e.fail("cannot read %s: %v", filepath.Join(vexillumHome, "config.json"), err)
		}
		e.say("Created %s", filepath.Join(vexillumHome, "config.json"))
	}
	cfgDirty := false

	if g.rulesWrite {
		lang, err := e.chooseLang(g.lang, g.langSource, "~/"+globalRulesRel)
		if err != nil {
			return e.askFailed(err)
		}
		if lang != g.lang {
			g.lang = lang
			if g.core, err = install.SlotTemplate(lang); err != nil {
				return e.fail("%v", err)
			}
		}
		if g.rulesBackup {
			backup := filepath.Join(vexillumHome, "backups", "rules-vexillum.md")
			if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
				return e.fail("%v", err)
			}
			if err := atomicfile.Write(backup, []byte(g.rules)); err != nil {
				return e.fail("saving a backup of ~/%s: %v", globalRulesRel, err)
			}
			e.say("Saved your version to %s", backup)
		}
		if err := os.MkdirAll(filepath.Dir(g.rulesPath), 0o755); err != nil {
			return e.fail("cannot create %s: %v", filepath.Dir(g.rulesPath), err)
		}
		if err := slot.WriteFile(g.rulesPath, g.core); err != nil {
			return e.fail("cannot write ~/%s: %v", globalRulesRel, err)
		}
		g.cfg.VexillumRuleHash = scaffold.HashContent(g.core)
		cfgDirty = true
		if g.rulesExists {
			e.say("Updated ~/%s", globalRulesRel)
		} else {
			e.say("Created ~/%s", globalRulesRel)
		}
	} else if g.rulesExists && g.rules == g.core && g.cfg.VexillumRuleHash != scaffold.HashContent(g.core) {
		g.cfg.VexillumRuleHash = scaffold.HashContent(g.core)
		cfgDirty = true
	}

	if code := g.projectPlan.applySkills(e, &cfgDirty); code != 0 {
		return code
	}
	if cfgDirty {
		if err := scaffold.SaveConfig(vexillumHome, g.cfg); err != nil {
			return e.fail("cannot update %s: %v", filepath.Join(vexillumHome, "config.json"), err)
		}
	}

	if kind == setupInit {
		e.say("%s initialized globally - this applies to every Claude Code session on this machine.", cmdname.Name)
	} else {
		e.say("%s upgrade complete (global).", cmdname.Name)
	}
	return 0
}
