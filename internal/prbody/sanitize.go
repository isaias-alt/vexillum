// Package prbody builds the title and body of the pull request "vx ship"
// opens. Both are public, so everything is generated from facts about the
// branch (commit subjects, diff stat, tribunal outcome) and every piece of
// text that comes from the camp or the reviewer goes through a Sanitizer
// first. The dispatch prompt never appears in a pull request.
package prbody

import (
	"regexp"
	"strings"
)

// minForbiddenLen is the shortest trimmed line that counts as quoting the
// prompt. Shorter lines ("fix: ship title") overlap prompt text by chance.
const minForbiddenLen = 40

var sensitivePatterns = []*regexp.Regexp{
	// Absolute home directories, Unix and Windows.
	regexp.MustCompile(`(^|[^A-Za-z0-9_.-])/(Users|home)/\S`),
	regexp.MustCompile(`(?i)\b[a-z]:\\Users\\`),
	// A local server: localhost or a loopback address with a port.
	regexp.MustCompile(`(?i)(\blocalhost|\b127\.\d+\.\d+\.\d+|\b0\.0\.0\.0|\[::1\]):\d+`),
	// Well-known token and key formats.
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bxox[abeprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`),
	regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]{20,}`),
	// Credentials embedded in a URL.
	regexp.MustCompile(`://[^/\s:@]+:[^/\s@]+@`),
	// name=value or name: value where the name says it is a secret.
	regexp.MustCompile(`(?i)(secret|token|passwd|password|api[_-]?key|access[_-]?key|private[_-]?key|credential)[a-z0-9_.-]*["']?\s*[:=]\s*["']?[A-Za-z0-9_\-./+=]{12,}`),
}

// Sanitizer filters text bound for a public pull request, line by line.
type Sanitizer struct {
	forbidden []string
}

// NewSanitizer returns a Sanitizer that also drops any line quoting one of
// the given texts (the dispatch prompt and the amendments), so a body the
// commander pastes from the prompt cannot leak it.
func NewSanitizer(forbidden ...string) Sanitizer {
	var lines []string
	for _, text := range forbidden {
		for _, line := range strings.Split(text, "\n") {
			if line = strings.TrimSpace(line); len(line) >= minForbiddenLen {
				lines = append(lines, line)
			}
		}
	}
	return Sanitizer{forbidden: lines}
}

// Sensitive reports whether a single line must not be published.
func (s Sanitizer) Sensitive(line string) bool {
	for _, re := range sensitivePatterns {
		if re.MatchString(line) {
			return true
		}
	}
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < minForbiddenLen {
		// A short line can still contain a forbidden one, handled below.
		trimmed = ""
	}
	for _, f := range s.forbidden {
		if strings.Contains(line, f) || (trimmed != "" && strings.Contains(f, trimmed)) {
			return true
		}
	}
	return false
}

// Text drops every sensitive line of text and returns the rest, along with
// how many lines it dropped. Runs of blank lines left behind collapse to one.
func (s Sanitizer) Text(text string) (string, int) {
	var out []string
	dropped := 0
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if s.Sensitive(line) {
			dropped++
			continue
		}
		if strings.TrimSpace(line) == "" && len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
			continue
		}
		out = append(out, strings.TrimRight(line, " \t"))
	}
	return strings.TrimSpace(strings.Join(out, "\n")), dropped
}
