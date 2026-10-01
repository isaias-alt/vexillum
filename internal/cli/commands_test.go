package cli

import (
	"strings"
	"testing"
)

func TestCommandsRegistryEntriesAreComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Commands() {
		if c.Name == "" {
			t.Fatalf("registry entry with empty name: %+v", c)
		}
		if seen[c.Name] {
			t.Errorf("duplicate registry entry for %q", c.Name)
		}
		seen[c.Name] = true
		if strings.TrimSpace(c.Summary) == "" {
			t.Errorf("%s: empty summary", c.Name)
		}
		if strings.Contains(c.Summary, "\n") {
			t.Errorf("%s: summary must be a single line", c.Name)
		}
		if strings.TrimSpace(c.Usage) == "" {
			t.Errorf("%s: empty usage", c.Name)
		}
		if !strings.Contains(c.Usage, "vexillum "+c.Name) {
			t.Errorf("%s: usage never mentions \"vexillum %s\"", c.Name, c.Name)
		}
	}
}

func TestGeneralUsageListsEveryCommand(t *testing.T) {
	got := GeneralUsage()
	for _, c := range Commands() {
		if !strings.Contains(got, c.Name+" ") || !strings.Contains(got, c.Summary) {
			t.Errorf("general usage is missing %q or its summary", c.Name)
		}
	}
}

func TestCommandsReturnsACopy(t *testing.T) {
	cs := Commands()
	cs[0].Name = "mutated"
	if Commands()[0].Name == "mutated" {
		t.Error("Commands() exposes the registry's backing array")
	}
}
