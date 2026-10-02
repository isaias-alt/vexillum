package commander

import (
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/cmdname"
)

// maxCoreBytes is the budget for the always-on core: it is paid in context
// on every turn, so it must stay short.
const maxCoreBytes = 3 * 1024

func TestCoreContract(t *testing.T) {
	// The domain vocabulary stays in English in every language.
	terms := []string{"commander", "soldier", "mission", "scout", "camp", "sentinel", "general"}
	// The hard instruction that loads the operational skill.
	skillName := "`vexillum`"

	for _, lang := range Languages() {
		t.Run(lang, func(t *testing.T) {
			body, err := Core(lang)
			if err != nil {
				t.Fatal(err)
			}
			for _, term := range terms {
				if !strings.Contains(body, "**"+term+"**") {
					t.Errorf("vocabulary entry %q missing", term)
				}
			}
			if !strings.Contains(body, "skill") || !strings.Contains(body, skillName) {
				t.Errorf("no instruction to load the %s skill", skillName)
			}
			for _, moment := range []string{"dispatch", "land", "ship", "release", "sentinel"} {
				if !strings.Contains(body, moment) {
					t.Errorf("core never mentions %q as a trigger for the skill", moment)
				}
			}
			if !strings.Contains(body, "`"+cmdname.Name+" dispatch`") {
				t.Errorf("role check does not name `%s dispatch`", cmdname.Name)
			}
			if strings.Contains(body, "{{") || strings.Contains(body, "<no value>") {
				t.Error("unrendered template action left in the body")
			}
			if strings.Contains(body, "—") {
				t.Error("body contains an em dash")
			}
			if !strings.HasSuffix(body, "\n") || strings.HasSuffix(body, "\n\n") {
				t.Error("body must end with exactly one newline")
			}
			if len(body) > maxCoreBytes {
				t.Errorf("core is %d bytes, budget is %d", len(body), maxCoreBytes)
			}
		})
	}
}

func TestCoreLanguages(t *testing.T) {
	en, _ := Core(LangEN)
	es, _ := Core(LangES)
	if en == es {
		t.Error("en and es cores are identical")
	}
	if !strings.Contains(es, "Leé esto primero") {
		t.Error("es core is not in Spanish with voseo")
	}
	if _, err := Core("fr"); err == nil {
		t.Error("expected an error for an unsupported language")
	}
}
