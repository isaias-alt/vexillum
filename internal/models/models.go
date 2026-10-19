// Package models loads the table of named model/effort profiles that
// `vx dispatch --profile` and `vx models` read.
//
// It is a lookup table, not routing logic: which profile fits a task is the
// commander's judgment, made from each profile's "when" text (see the
// vexillum skill). This package only stores the values and validates them.
//
// # File format
//
// A models file is JSON, in English:
//
//	{
//	  "profiles": {
//	    "<name>": {"model": "sonnet", "effort": "high", "when": "...", "harness": "claude"}
//	  },
//	  "default": {"model": "sonnet", "effort": "medium"}
//	}
//
// "model" and "effort" are checked by the profile's harness
// (soldier.ValidateModelEffort): on claude "model" is one of haiku, sonnet,
// opus, fable and "effort" one of low, medium, high, xhigh, max; on pi "model"
// is a provider/id such as "anthropic/claude-sonnet-5" and "effort" is a
// thinking level (off, minimal, low, medium, high, xhigh, max); on opencode
// "model" is provider/model with an optional #variant, such as
// "opencode/glm-5.3-flash-free" or "anthropic/claude-sonnet-5#high", and
// "effort" is only a label that is recorded on the task and never passed to
// opencode (the variant, when there is one, is the one after the #). "when" is
// free text telling the commander when the profile applies. "harness" is
// optional and names the agent CLI the profile's soldiers run on: "claude"
// (the default when absent), "pi" or "opencode" (soldier.HarnessFor). The
// default entry has no harness: it is always claude. Unknown fields are
// rejected so a typo does not silently do nothing. See testdata/example.json.
//
// models.json carries no schema_version: strict decoding already makes an
// older vexillum refuse a file that uses a field it does not know, such as
// "harness", which is the guard a version number would add.
//
// # Layers
//
// Three layers are merged, later ones winning:
//
//  1. the defaults embedded in the binary (DefaultJSON),
//  2. the optional global base, <vexillumHome>/models.json,
//  3. the optional project file, <projectDir>/.vexillum/models.json.
//
// The merge is per field: a layer may override just the "effort" of an
// existing profile, add a whole new profile, or change only "default.model".
// A profile that exists in no earlier layer must set model, effort and when;
// harness is always optional.
// A profile cannot be removed, only redefined. The reserved name "default"
// cannot be used as a profile name; it selects the default entry in
// `vx dispatch --profile default`.
package models

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/isaias-alt/vexillum/internal/soldier"
)

// FileName is the name of a models file, in both the project's .vexillum/
// directory and the global vexillum home.
const FileName = "models.json"

// DefaultName is the reserved profile name that resolves to the default
// entry rather than a named profile.
const DefaultName = "default"

//go:embed default-models.json
var defaultJSON []byte

// DefaultJSON returns the embedded defaults: the content `vx init` writes to
// a new project's .vexillum/models.json. The returned slice is a copy.
func DefaultJSON() []byte {
	return append([]byte(nil), defaultJSON...)
}

// Profile is one named model/effort pair with its selection guidance.
type Profile struct {
	Name   string
	Model  string
	Effort string
	When   string
	// Harness is the profile's explicit harness name, or "" when no layer
	// set one, which means soldier.HarnessClaude.
	Harness string
}

// Choice is a model and effort to pass to the harness, and the harness name
// to run them on ("" means soldier.HarnessClaude).
type Choice struct {
	Model   string
	Effort  string
	Harness string
}

// Table is the merged result of all layers.
type Table struct {
	// Profiles are in display order: the embedded profiles first, in their
	// canonical order, then any user-added ones sorted by name.
	Profiles []Profile
	Default  Choice
	// Sources lists the files that were read and merged, lowest priority
	// first. The embedded defaults are not listed.
	Sources []string
}

// Lookup resolves a profile name (or DefaultName) to its choice. An unknown
// name is an error listing the valid ones.
func (t Table) Lookup(name string) (Choice, error) {
	if name == DefaultName {
		return t.Default, nil
	}
	for _, p := range t.Profiles {
		if p.Name == name {
			return Choice{Model: p.Model, Effort: p.Effort, Harness: p.Harness}, nil
		}
	}
	return Choice{}, fmt.Errorf("unknown profile %q (valid profiles: %s)", name, strings.Join(t.Names(), ", "))
}

// Names lists every valid --profile value, DefaultName last.
func (t Table) Names() []string {
	names := make([]string, 0, len(t.Profiles)+1)
	for _, p := range t.Profiles {
		names = append(names, p.Name)
	}
	return append(names, DefaultName)
}

// layer is one file's contents. Fields are pointers so an absent field is
// distinguishable from an empty one, which is what makes the merge per field.
type layer struct {
	Profiles map[string]*profileLayer `json:"profiles"`
	Default  *choiceLayer             `json:"default"`
}

type profileLayer struct {
	Model   *string `json:"model"`
	Effort  *string `json:"effort"`
	When    *string `json:"when"`
	Harness *string `json:"harness"`
}

type choiceLayer struct {
	Model  *string `json:"model"`
	Effort *string `json:"effort"`
}

// Load merges the embedded defaults, the optional global base at
// vexillumHome/models.json and the optional project file at
// projectDir/.vexillum/models.json (later wins) and validates the result.
// A missing file is skipped; an unreadable or invalid one is an error naming
// the file and the offending field.
func Load(projectDir, vexillumHome string) (Table, error) {
	base, err := parse("built-in defaults", defaultJSON)
	if err != nil {
		return Table{}, err
	}
	acc := newAccumulator()
	if err := acc.apply("built-in defaults", base); err != nil {
		return Table{}, err
	}

	var sources []string
	for _, path := range []string{
		filepath.Join(vexillumHome, FileName),
		filepath.Join(projectDir, ".vexillum", FileName),
	} {
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return Table{}, fmt.Errorf("reading %s: %w", path, err)
		}
		l, err := parse(path, data)
		if err != nil {
			return Table{}, err
		}
		if err := acc.apply(path, l); err != nil {
			return Table{}, err
		}
		sources = append(sources, path)
	}
	return acc.table(sources), nil
}

// parse decodes one file strictly: unknown fields and trailing data are
// errors, and every value that is present must be valid on its own.
func parse(where string, data []byte) (layer, error) {
	var l layer
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&l); err != nil {
		return layer{}, fmt.Errorf("%s: invalid JSON: %w", where, err)
	}
	if dec.More() {
		return layer{}, fmt.Errorf("%s: invalid JSON: unexpected data after the top-level object", where)
	}
	for name, p := range l.Profiles {
		field := `profiles["` + name + `"]`
		if name == DefaultName {
			return layer{}, fmt.Errorf("%s: %s: %q is reserved for the default entry, pick another profile name", where, field, DefaultName)
		}
		if strings.TrimSpace(name) == "" || strings.ContainsAny(name, " \t\n") {
			return layer{}, fmt.Errorf("%s: %s: profile names must be non-empty and contain no whitespace", where, field)
		}
		if p == nil {
			return layer{}, fmt.Errorf("%s: %s: must be an object with model, effort and when", where, field)
		}
		if err := checkHarness(where, field, p.Harness); err != nil {
			return layer{}, err
		}
		if err := checkNotEmpty(where, field, p.Model, p.Effort); err != nil {
			return layer{}, err
		}
		if p.When != nil && strings.TrimSpace(*p.When) == "" {
			return layer{}, fmt.Errorf("%s: %s.when: must not be empty", where, field)
		}
	}
	if l.Default != nil {
		if err := checkValues(where, "default", nil, l.Default.Model, l.Default.Effort); err != nil {
			return layer{}, err
		}
	}
	return l, nil
}

// checkHarness validates a profile's harness name when present: it must be
// one vexillum knows about, implemented or reserved, so a typo is an error at
// load time while a reserved name is not (it fails at dispatch instead, see
// soldier.HarnessFor).
func checkHarness(where, field string, harness *string) error {
	if harness == nil {
		return nil
	}
	if *harness == "" {
		return fmt.Errorf("%s: %s.harness: must not be empty (omit it for the default, claude)", where, field)
	}
	if !soldier.KnownHarness(*harness) {
		_, err := soldier.HarnessFor(*harness)
		return fmt.Errorf("%s: %s.harness: %w", where, field, err)
	}
	return nil
}

// checkNotEmpty rejects a model or effort that is present but empty. What
// they may hold depends on the profile's harness, which a later layer can
// still change, so the values themselves are checked once the layers are
// merged (see checkProfileValues).
func checkNotEmpty(where, field string, model, effort *string) error {
	if model != nil && *model == "" {
		return fmt.Errorf("%s: %s.model: must not be empty", where, field)
	}
	if effort != nil && *effort == "" {
		return fmt.Errorf("%s: %s.effort: must not be empty", where, field)
	}
	return nil
}

// checkValues validates whichever of model/effort is present against the
// harness named by harness (nil means the claude default), naming the file
// and field in the error. It reuses the harness's own validation so the two
// can never disagree. A reserved harness with no implementation has none to
// check against yet, so its model and effort are accepted as written; they
// are validated by the harness once it exists.
func checkValues(where, field string, harness, model, effort *string) error {
	name := ""
	if harness != nil {
		name = *harness
	}
	if err := checkNotEmpty(where, field, model, effort); err != nil {
		return err
	}
	if _, err := soldier.HarnessFor(name); err != nil {
		return nil
	}
	if model != nil {
		if err := soldier.ValidateModelEffort(name, *model, ""); err != nil {
			return fmt.Errorf("%s: %s.model: %w", where, field, err)
		}
	}
	if effort != nil {
		if err := soldier.ValidateModelEffort(name, "", *effort); err != nil {
			return fmt.Errorf("%s: %s.effort: %w", where, field, err)
		}
	}
	return nil
}

// checkProfileValues validates a profile after a layer was merged into it,
// against the profile's merged harness. A layer that sets only "model" on a
// profile an earlier layer gave harness "pi" is checked as Pi's, not as the
// claude default, and a layer that changes the harness re-checks the model
// and effort it inherits: a profile cannot end up with a claude alias on Pi.
func checkProfileValues(where string, cur *Profile, p *profileLayer) error {
	field := `profiles["` + cur.Name + `"]`
	var model, effort *string
	if p.Model != nil || p.Harness != nil {
		model = &cur.Model
	}
	if p.Effort != nil || p.Harness != nil {
		effort = &cur.Effort
	}
	harness := cur.Harness
	return checkValues(where, field, &harness, model, effort)
}

func profileNames(l layer) []string {
	names := make([]string, 0, len(l.Profiles))
	for n := range l.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

type accumulator struct {
	profiles map[string]*Profile
	def      Choice
}

func newAccumulator() *accumulator {
	return &accumulator{profiles: map[string]*Profile{}}
}

// apply merges one validated layer into the accumulator, per field. A brand
// new profile must be complete.
func (a *accumulator) apply(where string, l layer) error {
	for _, name := range profileNames(l) {
		p := l.Profiles[name]
		cur, exists := a.profiles[name]
		if !exists {
			cur = &Profile{Name: name}
			var missing []string
			if p.Model == nil {
				missing = append(missing, "model")
			}
			if p.Effort == nil {
				missing = append(missing, "effort")
			}
			if p.When == nil {
				missing = append(missing, "when")
			}
			if len(missing) > 0 {
				return fmt.Errorf(`%s: profiles["%s"]: new profile is missing %s`, where, name, strings.Join(missing, ", "))
			}
			a.profiles[name] = cur
		}
		if p.Model != nil {
			cur.Model = *p.Model
		}
		if p.Effort != nil {
			cur.Effort = *p.Effort
		}
		if p.When != nil {
			cur.When = *p.When
		}
		if p.Harness != nil {
			cur.Harness = *p.Harness
		}
		if err := checkProfileValues(where, cur, p); err != nil {
			return err
		}
	}
	if l.Default != nil {
		if l.Default.Model != nil {
			a.def.Model = *l.Default.Model
		}
		if l.Default.Effort != nil {
			a.def.Effort = *l.Default.Effort
		}
	}
	return nil
}

func (a *accumulator) table(sources []string) Table {
	t := Table{Default: a.def, Sources: sources}
	seen := map[string]bool{}
	// JSON objects have no order, so the built-in profiles are listed in
	// their canonical priority order, then user-added ones by name.
	for _, name := range canonicalOrder {
		if p, ok := a.profiles[name]; ok {
			t.Profiles = append(t.Profiles, *p)
			seen[name] = true
		}
	}
	var rest []string
	for name := range a.profiles {
		if !seen[name] {
			rest = append(rest, name)
		}
	}
	sort.Strings(rest)
	for _, name := range rest {
		t.Profiles = append(t.Profiles, *a.profiles[name])
	}
	return t
}

// canonicalOrder is the display and priority order of the built-in profiles:
// the order the commander rules always listed them in (first match wins).
var canonicalOrder = []string{"scout-facts", "scout-judgment", "mission-mechanical", "mission-big", "opus-on-request"}
