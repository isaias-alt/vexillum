package cli

import (
	"fmt"
	"slices"
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
	cmds := Commands()
	width := 0
	for _, c := range cmds {
		width = max(width, len(c.Name))
	}

	lines := strings.Split(GeneralUsage(), "\n")
	for _, c := range cmds {
		want := fmt.Sprintf("  %-*s  %s", width, c.Name, c.Summary)
		if !slices.Contains(lines, want) {
			t.Errorf("general usage has no exact line %q", want)
		}
	}
}

// firstParagraph returns the first paragraph of usage on one line, without
// its trailing period.
func firstParagraph(usage string) string {
	para, _, _ := strings.Cut(usage, "\n\n")
	return strings.TrimSuffix(strings.Join(strings.Fields(para), " "), ".")
}

func TestSummaryOpensEachUsage(t *testing.T) {
	for _, c := range Commands() {
		para := firstParagraph(c.Usage)
		summary := strings.TrimSuffix(c.Summary, ".")
		onBoundary := para == summary
		for _, sep := range []string{" ", ",", "."} {
			onBoundary = onBoundary || strings.HasPrefix(para, summary+sep)
		}
		if !onBoundary {
			t.Errorf("%s: summary %q is not the opening of usage's first paragraph %q", c.Name, c.Summary, para)
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
