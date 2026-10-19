package doctorcheck

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/isaias-alt/vexillum/internal/cmdname"
	"github.com/isaias-alt/vexillum/internal/scaffold"
	"github.com/isaias-alt/vexillum/internal/soldier"
)

// Commanders returns the harnesses the project's commander runs in, read
// from .vexillum/config.json (scaffold.DefaultCommander for a project that is
// not initialized, whose config does not name any, or whose config cannot be
// read). A name no harness has is dropped and reported by a warning, so a
// typo in a hand-edited config is visible without making the environment
// unusable; when nothing valid is left the default applies.
func Commanders(projectDir string) ([]string, []Result) {
	cfg, err := scaffold.ReadConfig(filepath.Join(projectDir, ".vexillum"))
	if err != nil {
		return []string{scaffold.DefaultCommander}, nil
	}
	var valid, unknown []string
	for _, name := range cfg.CommanderHarnesses() {
		if _, err := soldier.HarnessFor(name); err != nil {
			unknown = append(unknown, fmt.Sprintf("%q", name))
			continue
		}
		if !slices.Contains(valid, name) {
			valid = append(valid, name)
		}
	}
	if len(unknown) == 0 {
		return valid, nil
	}
	warn := Result{
		Name: "commander harnesses",
		Warn: true,
		Detail: fmt.Sprintf("config.json names %s, which vexillum does not know (supported: %s); fix \"commander\" in .vexillum/config.json or run '%s init --commander'",
			strings.Join(unknown, ", "), strings.Join(soldier.HarnessNames(), ", "), cmdname.Name),
	}
	if len(valid) == 0 {
		valid = []string{scaffold.DefaultCommander}
	}
	return valid, []Result{warn}
}
