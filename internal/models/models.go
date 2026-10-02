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
//	    "<name>": {"model": "sonnet", "effort": "high", "when": "..."}
//	  },
//	  "default": {"model": "sonnet", "effort": "medium"}
//	}
//
// "model" is one of haiku, sonnet, opus, fable. "effort" is one of low,
// medium, high, xhigh, max (the values the claude CLI accepts, enforced by
// soldier.ValidateModelEffort). "when" is free text telling the commander
// when the profile applies. Unknown fields are rejected so a typo does not
// silently do nothing. See testdata/example.json.
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
// A profile that exists in no earlier layer must set model, effort and when.
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
}

// Choice is a model and effort to pass to the harness.
type Choice struct {
	Model  string
	Effort string
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
			return Choice{Model: p.Model, Effort: p.Effort}, nil
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
	Model  *string `json:"model"`
	Effort *string `json:"effort"`
	When   *string `json:"when"`
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
		if err := checkValues(where, field, p.Model, p.Effort); err != nil {
			return layer{}, err
		}
		if p.When != nil && strings.TrimSpace(*p.When) == "" {
			return layer{}, fmt.Errorf("%s: %s.when: must not be empty", where, field)
		}
	}
	if l.Default != nil {
		if err := checkValues(where, "default", l.Default.Model, l.Default.Effort); err != nil {
			return layer{}, err
		}
	}
	return l, nil
}

// checkValues validates whichever of model/effort is present, naming the
// file and field in the error. It reuses the harness's own allowed sets so
// the two can never disagree.
func checkValues(where, field string, model, effort *string) error {
	if model != nil {
		if *model == "" {
			return fmt.Errorf("%s: %s.model: must not be empty", where, field)
		}
		if err := soldier.ValidateModelEffort(*model, ""); err != nil {
			return fmt.Errorf("%s: %s.model: %w", where, field, err)
		}
	}
	if effort != nil {
		if *effort == "" {
			return fmt.Errorf("%s: %s.effort: must not be empty", where, field)
		}
		if err := soldier.ValidateModelEffort("", *effort); err != nil {
			return fmt.Errorf("%s: %s.effort: %w", where, field, err)
		}
	}
	return nil
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
