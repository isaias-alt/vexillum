// Package commander holds the always-on core of the commander rules: the
// short block vexillum keeps in the project's AGENTS.md (the "slot"). It
// carries what must be in context on every turn (role check, vocabulary,
// authority, tone) plus one hard instruction to load the "vexillum" skill
// before any operational step. The operational detail (dispatching, models,
// landing, shipping, striking, the sentinel) lives in that skill, see
// skills/vexillum.
//
// The core ships in English and Spanish. The domain vocabulary (commander,
// soldier, mission, scout, camp, sentinel) stays in English in both.
package commander

import (
	"bytes"
	_ "embed"
	"fmt"
	"text/template"

	"github.com/isaias-alt/vexillum/internal/cmdname"
)

// Language codes accepted by Core.
const (
	LangEN = "en"
	LangES = "es"
)

// Languages lists the supported language codes, default first.
func Languages() []string { return []string{LangEN, LangES} }

//go:embed core.en.md
var coreEN string

//go:embed core.es.md
var coreES string

// Core renders the always-on core body for lang ("en" or "es"). It is the
// text that goes between the AGENTS.md slot markers, with the executable
// name filled in from cmdname.Name. The result ends with a single newline.
func Core(lang string) (string, error) {
	var src string
	switch lang {
	case LangEN:
		src = coreEN
	case LangES:
		src = coreES
	default:
		return "", fmt.Errorf("unsupported language %q (expected one of en, es)", lang)
	}
	tmpl, err := template.New("core-" + lang).Option("missingkey=error").Parse(src)
	if err != nil {
		return "", fmt.Errorf("parsing core %s template: %w", lang, err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct{ Cmd string }{Cmd: cmdname.Name}); err != nil {
		return "", fmt.Errorf("rendering core %s template: %w", lang, err)
	}
	return buf.String(), nil
}
