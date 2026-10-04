package main

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/isaias-alt/vexillum/internal/cli"
)

// These tests are the gate that keeps the Spanish command reference
// complete: a new command, or a new flag in a command's help, fails here
// until its Spanish text exists, and a translation that drops or invents a
// literal (flag, synopsis, quoted output, placeholder, env var) fails too.

func TestEveryCommandHasSpanishText(t *testing.T) {
	known := map[string]bool{}
	for _, c := range cli.Commands() {
		known[c.Name] = true
		es, ok := commandsES[c.Name]
		if !ok {
			t.Errorf("command %q has no Spanish text: add it to commandsES in cli_es.go", c.Name)
			continue
		}
		if strings.TrimSpace(es.Summary) == "" || strings.TrimSpace(es.Usage) == "" {
			t.Errorf("command %q has an empty Spanish summary or usage", c.Name)
		}
	}
	for name := range commandsES {
		if !known[name] {
			t.Errorf("commandsES has an entry for %q, which is not a command", name)
		}
	}
}

func TestSpanishSummaryOpensItsUsage(t *testing.T) {
	for _, c := range cli.Commands() {
		es := commandsES[c.Name]
		if strings.Contains(es.Summary, "\n") {
			t.Errorf("%s: Spanish summary spans several lines", c.Name)
		}
		if es.Summary == c.Summary {
			t.Errorf("%s: Spanish summary is the English one", c.Name)
		}
		para := strings.Join(strings.Fields(paragraphs(es.Usage)[0]), " ")
		summary := strings.TrimSuffix(es.Summary, ".")
		onBoundary := para == summary || para == summary+"."
		for _, sep := range []string{" ", ",", "."} {
			onBoundary = onBoundary || strings.HasPrefix(para, summary+sep)
		}
		if !onBoundary {
			t.Errorf("%s: Spanish summary %q does not open the first paragraph %q", c.Name, es.Summary, para)
		}
	}
}

// The Spanish help keeps the structure of the English one, so a truncated or
// restructured translation is caught.
func TestSpanishUsageKeepsTheStructure(t *testing.T) {
	for _, c := range cli.Commands() {
		es := commandsES[c.Name]
		if es.Usage == c.Usage {
			t.Errorf("%s: Spanish usage is the English one", c.Name)
		}
		if got, want := len(paragraphs(es.Usage)), len(paragraphs(c.Usage)); got != want {
			t.Errorf("%s: Spanish usage has %d paragraphs, English has %d", c.Name, got, want)
		}
		if got, want := synopsis(es.Usage, "Uso:"), synopsis(c.Usage, "Usage:"); got != want {
			t.Errorf("%s: Spanish synopsis differs from the English one:\nes: %q\nen: %q", c.Name, got, want)
		}
		if strings.Contains(es.Usage, "Usage:") {
			t.Errorf("%s: Spanish usage keeps the English \"Usage:\" heading", c.Name)
		}
	}
}

// Literals are what a reader copies: flags, quoted output and statuses,
// quoted commands, placeholders, environment variables and paths. The
// Spanish text keeps every literal of the English help, and invents no flag.
func TestSpanishUsageKeepsTheLiterals(t *testing.T) {
	for _, c := range cli.Commands() {
		es := commandsES[c.Name]
		for _, lit := range missing(literals(c.Usage), literals(es.Usage)) {
			t.Errorf("%s: the Spanish usage lost the literal %q", c.Name, lit)
		}
		for _, f := range missing(flags(es.Usage), flags(c.Usage)) {
			t.Errorf("%s: the Spanish usage mentions the flag %q, which the help does not", c.Name, f)
		}
	}
}

// Every flag the help documents in a definition line ("  --flag   text") has
// its Spanish definition line, not just a mention in the synopsis.
func TestSpanishUsageDocumentsEveryFlag(t *testing.T) {
	for _, c := range cli.Commands() {
		es := commandsES[c.Name]
		for _, f := range missing(flagDefinitions(c.Usage), flagDefinitions(es.Usage)) {
			t.Errorf("%s: the Spanish usage has no definition line for the flag %q", c.Name, f)
		}
	}
}

// Leftover English prose is the usual way a translation is half done. The
// check strips what is meant to stay English and flags common English words.
func TestSpanishUsageHasNoEnglishProse(t *testing.T) {
	// \b is ASCII-only in RE2, so "así" would match "as": use letter classes.
	english := regexp.MustCompile(`(?i)(?:^|[^\p{L}-])(the|and|with|which|when|that|this|does|are|is|not|from|into|then|only|once|until|instead|has|have|its|of|for|if|an|be|by|it|to|as)(?:$|[^\p{L}-])`)
	for _, c := range cli.Commands() {
		es := commandsES[c.Name]
		for _, text := range []string{es.Summary, es.Usage} {
			for _, line := range strings.Split(proseOnly(text), "\n") {
				if m := english.FindString(line); m != "" {
					t.Errorf("%s: English word %q in %q", c.Name, m, strings.TrimSpace(line))
				}
			}
		}
	}
}

func TestSpanishTextHasNoEmDash(t *testing.T) {
	for name, es := range commandsES {
		if strings.ContainsRune(es.Summary+es.Usage, '\u2014') {
			t.Errorf("%s: Spanish text contains an em dash; use a plain dash", name)
		}
	}
}

func TestLocalizeCommandsFailsOnMissingText(t *testing.T) {
	_, err := localizeCommands([]cli.Command{{Name: "no-such-command", Summary: "x", Usage: "x"}})
	if err == nil || !strings.Contains(err.Error(), "no-such-command") {
		t.Errorf("err = %v, want one naming the command without Spanish text", err)
	}
}

// paragraphs splits help text on blank lines.
func paragraphs(s string) []string {
	var out []string
	for _, p := range regexp.MustCompile(`\n[ \t]*\n`).Split(strings.TrimSpace(s), -1) {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

// synopsis returns the command lines under the usage heading, cut before the
// description column some synopses carry, so only the literal part compares.
func synopsis(usage, heading string) string {
	var out []string
	in := false
	for _, ln := range strings.Split(usage, "\n") {
		switch {
		case strings.TrimSpace(ln) == heading:
			in = true
		case in && strings.TrimSpace(ln) == "":
			return strings.Join(out, "\n")
		case in:
			trimmed := strings.TrimSpace(ln)
			if !strings.HasPrefix(trimmed, "vx ") && !strings.HasPrefix(trimmed, "[") {
				continue // the description of a synopsis line
			}
			if i := strings.Index(trimmed, "  "); i >= 0 {
				trimmed = trimmed[:i]
			}
			out = append(out, trimmed)
		}
	}
	return strings.Join(out, "\n")
}

var (
	flagRE        = regexp.MustCompile(`(?:^|[\s(\[|])(--?[A-Za-z][A-Za-z0-9-]*)`)
	doubleQuoteRE = regexp.MustCompile(`"[^"]*"`)
	singleQuoteRE = regexp.MustCompile(`'(?:vx|git|brew|herdr)[^']*'`)
	placeholderRE = regexp.MustCompile(`<[a-z][a-z0-9-]*>`)
	envVarRE      = regexp.MustCompile(`\b[A-Z][A-Z0-9]*_[A-Z0-9_]+\b`)
	flagDefRE     = regexp.MustCompile(`(?m)^  (--?[A-Za-z][A-Za-z0-9-]*)`)
	jsonKeyRE     = regexp.MustCompile(`^"[a-z_]+": `)
	// Paths that start at ~, a dot directory or a placeholder, or name a
	// file: not prose such as "clean/landed".
	pathRE = regexp.MustCompile(`(?:~|\.[a-z]+|<[a-z -]+>)/[A-Za-z0-9<>_./*-]*[A-Za-z0-9>*]|[A-Za-z0-9_-]+(?:/[A-Za-z0-9<>_-]+)*/[A-Za-z0-9<>_-]+\.(?:md|json|txt|go|html)`)
)

// flagDefinitions returns the flags that open an indented line, the way the
// help lists them.
func flagDefinitions(s string) map[string]bool {
	out := map[string]bool{}
	for _, m := range flagDefRE.FindAllStringSubmatch(s, -1) {
		out[m[1]] = true
	}
	return out
}

func flags(s string) map[string]bool {
	out := map[string]bool{}
	for _, m := range flagRE.FindAllStringSubmatch(s, -1) {
		out[m[1]] = true
	}
	return out
}

// literals collects everything in s that must survive translation verbatim.
// Whitespace is collapsed first so a quote or a command wrapped across two
// lines is still one literal.
func literals(s string) map[string]bool {
	flat := strings.Join(strings.Fields(s), " ")
	out := flags(flat)
	for _, re := range []*regexp.Regexp{doubleQuoteRE, singleQuoteRE, placeholderRE, envVarRE, pathRE} {
		for _, m := range re.FindAllString(flat, -1) {
			out[strings.TrimRight(m, ".,;:)")] = true
		}
	}
	return out
}

// missing lists, sorted, the keys of want that have is lacking.
func missing(want, have map[string]bool) []string {
	var out []string
	for k := range want {
		if !have[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// proseOnly blanks out the parts of Spanish help that are meant to stay in
// English: quoted output, quoted commands, JSON examples, synopsis lines and
// indented literals.
func proseOnly(s string) string {
	var kept []string
	for _, ln := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "vx ") || strings.HasPrefix(trimmed, "[--") ||
			strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "}") ||
			jsonKeyRE.MatchString(trimmed) || strings.HasPrefix(trimmed, "]") ||
			strings.HasPrefix(trimmed, "VEXILLUM_") {
			continue
		}
		kept = append(kept, ln)
	}
	text := strings.Join(kept, " \n")
	text = doubleQuoteRE.ReplaceAllString(text, " ")
	text = singleQuoteRE.ReplaceAllString(text, " ")
	text = flagRE.ReplaceAllString(text, " ")
	return strings.ReplaceAll(text, " \n", "\n")
}
