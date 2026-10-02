package slot

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// Lang is a language the slot ships in.
type Lang string

const (
	LangEN Lang = "en"
	LangES Lang = "es"
)

// ParseLang validates a --lang flag value.
func ParseLang(s string) (Lang, error) {
	switch Lang(strings.ToLower(strings.TrimSpace(s))) {
	case LangEN:
		return LangEN, nil
	case LangES:
		return LangES, nil
	}
	return "", fmt.Errorf("unsupported language %q (want en or es)", s)
}

const (
	// minStopwords is how many stopword hits a text needs before a decision
	// is made; below it the result is the default.
	minStopwords = 8
	// minShare is the share of the hits the winner needs.
	minShare = 0.7
)

// Words that occur in both languages ("a", "no", "me", "he", "son", "pan",
// "red", "most") are deliberately left out of both lists.
var stopwords = map[Lang]map[string]bool{
	LangEN: wordSet(`the and of to is are in that for with this you it not be as on we
		use when before from if or should will can your have has was were
		their they them which what how into than then also only each any all
		but by at an its our do does`),
	LangES: wordSet(`el la los las de del que y en un una unos unas es se por para con lo
		como más pero su sus al este esta estos estas cuando antes si sin
		sobre ser también hay debe tu nos le les ya muy entre desde cada
		todo todos todas del tiene tienen puede pueden está están qué cómo`),
}

func wordSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

var (
	inlineCodeRE = regexp.MustCompile("`[^`\n]*`")
	urlRE        = regexp.MustCompile(`https?://\S+`)
)

// DetectLanguage guesses whether an existing AGENTS.md is written in English
// or Spanish by counting frequent stopwords of each language. It is
// deterministic and offline. Fenced code, inline code, URLs and the vexillum
// block itself are ignored. The confidence is the winner's share of the
// stopword hits, in [0.7, 1]; when the text is empty, too short or mixed
// (fewer than 8 hits, or a winner under 70%), it returns (LangEN, 0) - the
// default - and callers should treat 0 as "no opinion".
func DetectLanguage(content string) (Lang, float64) {
	counts := map[Lang]int{}
	for _, w := range tokens(stripForLang(content)) {
		for lang, set := range stopwords {
			if set[w] {
				counts[lang]++
			}
		}
	}
	total := counts[LangEN] + counts[LangES]
	if total < minStopwords {
		return LangEN, 0
	}
	winner := LangEN
	if counts[LangES] > counts[LangEN] {
		winner = LangES
	}
	share := float64(counts[winner]) / float64(total)
	if share < minShare {
		return LangEN, 0
	}
	return winner, share
}

// stripForLang drops everything that is not the user's own prose.
func stripForLang(content string) string {
	d := parse(content)
	skip := map[int]bool{}
	pairs, orphans := d.pairs()
	for _, p := range pairs {
		for i := p.begin.line; i <= p.end.line; i++ {
			skip[i] = true
		}
	}
	for _, o := range orphans {
		skip[o.line] = true
	}

	var b strings.Builder
	var fenceCh byte
	fenceN := 0
	for i := range d.lines {
		t := d.text(i)
		if fenceN > 0 {
			if closesFence(t, fenceCh, fenceN) {
				fenceN = 0
			}
			continue
		}
		if ch, n, ok := opensFence(t); ok {
			fenceCh, fenceN = ch, n
			continue
		}
		if skip[i] {
			continue
		}
		t = inlineCodeRE.ReplaceAllString(t, " ")
		t = urlRE.ReplaceAllString(t, " ")
		b.WriteString(t)
		b.WriteByte('\n')
	}
	return b.String()
}

func tokens(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) })
}
