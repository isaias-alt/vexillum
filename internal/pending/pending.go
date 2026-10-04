// Package pending records the commander's own pending decisions: things
// that wait on the general's approval (dispatch a mission already
// designed, land a finished one, ship a PR) and would otherwise live only
// in the commander's head, lost if the session is cut. It is a different
// category from a blocked task's state.Decision (a soldier needing the
// general): here the commander needs the general.
//
// One file per item under <project root>/pending/<id>.json, written
// atomically, so adding and clearing never rewrite a shared file and two
// writers can't lose each other's items. The caller resolves the project
// root (see internal/project); this package only sees the root it's
// handed.
package pending

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
	"github.com/isaias-alt/vexillum/internal/schemaver"
)

// SchemaVersion is the current version of the Item JSON schema. Numbering
// restarted at 0; items written before that carry 1 (see
// legacySchemaVersions).
const SchemaVersion = schemaver.Current

var legacySchemaVersions = []int{1}

const dirName = "pending"

// Item is one pending decision. Options and Recommended are optional: an
// item recorded as free text carries neither. Recommended is the 1-based
// index into Options of the commander's recommendation, 0 when there is
// none.
type Item struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"id"`
	Text          string    `json:"text"`
	Options       []string  `json:"options,omitempty"`
	Recommended   int       `json:"recommended,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// ErrNotFound is returned by Clear when no item has the given id.
var ErrNotFound = errors.New("no such pending decision")

// idPattern is the fixed shape newID produces: 8 lowercase hex characters.
var idPattern = regexp.MustCompile(`^[0-9a-f]{8}$`)

// ValidateID reports an error if id isn't the shape Add generates. An id
// reaches this package from a CLI argument and is used to build a
// filesystem path, so it must be rejected before touching the disk if it
// could escape the pending directory.
func ValidateID(id string) error {
	if !idPattern.MatchString(id) {
		return fmt.Errorf("invalid pending decision id %q: expected 8 lowercase hex characters", id)
	}
	return nil
}

func newID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func dir(projectRoot string) string {
	return filepath.Join(projectRoot, dirName)
}

func path(projectRoot, id string) string {
	return filepath.Join(dir(projectRoot), id+".json")
}

// normalizeOptions trims every option label and checks them together with
// the 1-based recommended index. It returns the cleaned labels.
func normalizeOptions(options []string, recommended int) ([]string, error) {
	var cleaned []string
	seen := make(map[string]bool, len(options))
	for _, o := range options {
		o = strings.TrimSpace(o)
		if o == "" {
			return nil, errors.New("pending decision option is empty")
		}
		if seen[o] {
			return nil, fmt.Errorf("pending decision option %q is repeated", o)
		}
		seen[o] = true
		cleaned = append(cleaned, o)
	}
	if err := validateRecommended(len(cleaned), recommended); err != nil {
		return nil, err
	}
	return cleaned, nil
}

// validateRecommended checks a 1-based recommended index against the number
// of options. 0 means no recommendation and is always valid.
func validateRecommended(options, recommended int) error {
	switch {
	case recommended == 0:
		return nil
	case options == 0:
		return errors.New("a recommended option needs at least one option")
	case recommended < 0 || recommended > options:
		return fmt.Errorf("recommended option %d is out of range (1-%d)", recommended, options)
	}
	return nil
}

// Add records text as a new pending decision and returns it. text is
// trimmed and must not be empty. options are the answers the general can
// pick from (optional) and recommended is the 1-based index of the
// recommended one, 0 for none.
func Add(projectRoot, text string, options []string, recommended int) (Item, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Item{}, errors.New("pending decision text is empty")
	}
	options, err := normalizeOptions(options, recommended)
	if err != nil {
		return Item{}, err
	}
	if err := os.MkdirAll(dir(projectRoot), 0o755); err != nil {
		return Item{}, fmt.Errorf("creating pending directory: %w", err)
	}

	// An 8-hex id colliding with an existing one is astronomically
	// unlikely, but overwriting someone's item would silently lose it, so
	// check and regenerate instead of trusting the odds.
	var id string
	for range 8 {
		candidate, err := newID()
		if err != nil {
			return Item{}, fmt.Errorf("generating pending decision id: %w", err)
		}
		if _, err := os.Stat(path(projectRoot, candidate)); os.IsNotExist(err) {
			id = candidate
			break
		}
	}
	if id == "" {
		return Item{}, errors.New("could not generate a free pending decision id")
	}

	item := Item{SchemaVersion: SchemaVersion, ID: id, Text: text, Options: options, Recommended: recommended, CreatedAt: time.Now().UTC()}
	if err := atomicfile.WriteJSON(path(projectRoot, id), item); err != nil {
		return Item{}, fmt.Errorf("saving pending decision %s: %w", id, err)
	}
	return item, nil
}

// List returns every pending decision, oldest first (ties by id). A
// missing directory yields an empty list. A corrupt file fails the whole
// listing with an error naming it, same as state.List, rather than being
// silently skipped.
func List(projectRoot string) ([]Item, error) {
	entries, err := os.ReadDir(dir(projectRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("listing pending decisions: %w", err)
	}

	var items []Item
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		p := filepath.Join(dir(projectRoot), entry.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", p, err)
		}
		var item Item
		if err := json.Unmarshal(data, &item); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", p, err)
		}
		if item.ID == "" || item.Text == "" {
			return nil, fmt.Errorf("%s: incomplete pending decision (missing id or text)", p)
		}
		if !schemaver.Supported(item.SchemaVersion, legacySchemaVersions...) {
			return nil, fmt.Errorf("%s: unsupported schema version %d (expected %d)", p, item.SchemaVersion, SchemaVersion)
		}
		item.SchemaVersion = SchemaVersion
		if err := validateRecommended(len(item.Options), item.Recommended); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		items = append(items, item)
	}

	sort.Slice(items, func(i, j int) bool {
		if !items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}

// Clear removes the pending decision with the given id. It wraps
// ErrNotFound when there is none, so clearing twice is reported rather
// than silently succeeding.
func Clear(projectRoot, id string) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	if err := os.Remove(path(projectRoot, id)); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return fmt.Errorf("clearing pending decision %s: %w", id, err)
	}
	return nil
}
