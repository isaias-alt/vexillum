package prbody

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// maxTitleLen is the longest derived pull request title.
const maxTitleLen = 72

// rootArea names the files that sit at the top of the repository, which have
// no directory to call an area.
const rootArea = "(repo root)"

var conventionalPrefix = regexp.MustCompile(`^(feat|fix|docs)(\([^)]*\))?!?:`)

// reviewerTitleShape is a conventional-commit title: any common type, an
// optional scope, an optional "!", then text.
var reviewerTitleShape = regexp.MustCompile(`^(feat|fix|docs|refactor|perf|test|build|ci|chore|style|revert)(\([^)]*\))?!?: \S`)

// Limits on what is accepted from the reviewer. The title is cut to
// maxTitleLen after it passes, so the cap here only rejects a reviewer that
// ignored the length by a wide margin.
const (
	maxReviewerTitleLen       = 120
	maxReviewerDescriptionLen = 2000
	maxReviewerDescriptionLns = 20
)

// Facts are what git says about a branch, the only source of a generated
// pull request description.
type Facts struct {
	// Subjects are the commit subjects of base..HEAD, oldest first.
	Subjects []string
	Files    int
	Added    int
	Deleted  int
	// Areas are the top-level directories the diff touches, sorted, with
	// rootArea standing for files at the repository root.
	Areas []string
}

// Gather reads Facts for the branch checked out in campPath against base.
func Gather(campPath, base string) (Facts, error) {
	var f Facts
	log, err := git(campPath, "log", "--reverse", "--format=%s", base+"..HEAD")
	if err != nil {
		return f, err
	}
	for _, line := range strings.Split(log, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			f.Subjects = append(f.Subjects, line)
		}
	}
	numstat, err := git(campPath, "diff", "--numstat", "--no-renames", base+"..HEAD")
	if err != nil {
		return f, err
	}
	f.Files, f.Added, f.Deleted, f.Areas = parseNumstat(numstat)
	return f, nil
}

// parseNumstat totals "git diff --numstat" output (added, deleted, path per
// line; "-" counts for a binary file) and lists the top-level areas.
func parseNumstat(numstat string) (files, added, deleted int, areas []string) {
	seen := map[string]bool{}
	for _, line := range strings.Split(numstat, "\n") {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		files++
		if n, err := strconv.Atoi(parts[0]); err == nil {
			added += n
		}
		if n, err := strconv.Atoi(parts[1]); err == nil {
			deleted += n
		}
		area := rootArea
		if dir, _, ok := strings.Cut(parts[2], "/"); ok {
			area = dir + "/"
		}
		if !seen[area] {
			seen[area] = true
			areas = append(areas, area)
		}
	}
	sort.Strings(areas)
	return files, added, deleted, areas
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// oneLine collapses whitespace so a title is a single line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// truncate shortens s to at most max bytes, cutting at a word boundary when
// one is close enough and marking the cut with "...".
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max - 3
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	head := s[:cut]
	if i := strings.LastIndex(head, " "); i > max/2 {
		head = head[:i]
	}
	return strings.TrimRight(head, " ,;:-") + "..."
}

// Title picks the pull request title. A non-empty override wins as given
// (one line), but must be safe to publish. Otherwise it is the subject of the
// only commit, or of the newest commit with a feat, fix or docs prefix, or of
// the first commit, skipping any subject that is not safe to publish. With no
// usable commit it is fallback.
func Title(s Sanitizer, subjects []string, override, fallback string) (string, error) {
	if override = oneLine(override); override != "" {
		if s.Sensitive(override) {
			return "", errors.New("the --title contains text that must not be published (a home path, a localhost port, a secret or the mission prompt)")
		}
		return override, nil
	}
	var safe []string
	for _, subject := range subjects {
		if subject = oneLine(subject); subject != "" && !s.Sensitive(subject) {
			safe = append(safe, subject)
		}
	}
	if len(safe) == 0 {
		return fallback, nil
	}
	pick := safe[0]
	if len(safe) > 1 {
		for i := len(safe) - 1; i >= 0; i-- {
			if conventionalPrefix.MatchString(safe[i]) {
				pick = safe[i]
				break
			}
		}
	}
	return truncate(pick, maxTitleLen), nil
}

// ReviewerTitle validates the pull request title the review proposed. ok is
// false, and the caller falls back to the commits, when it is missing, is not
// one line in conventional-commit style, is far too long, or contains text
// that must not be published.
func ReviewerTitle(s Sanitizer, title string) (string, bool) {
	title = strings.TrimSpace(title)
	if title == "" || strings.ContainsAny(title, "\r\n") || len(title) > maxReviewerTitleLen {
		return "", false
	}
	if !reviewerTitleShape.MatchString(title) || s.Sensitive(title) {
		return "", false
	}
	return truncate(oneLine(title), maxTitleLen), true
}

// ReviewerDescription validates the description the review proposed. ok is
// false, and the caller falls back to the commit subjects, when it is
// missing, too long, uses markdown headings (they would clash with the
// body's own sections), or has any line that must not be published. A
// description that needed editing is not published half-cut.
func ReviewerDescription(s Sanitizer, description string) (string, bool) {
	description = strings.TrimSpace(strings.ReplaceAll(description, "\r\n", "\n"))
	if description == "" || len(description) > maxReviewerDescriptionLen {
		return "", false
	}
	lines := strings.Split(description, "\n")
	if len(lines) > maxReviewerDescriptionLns {
		return "", false
	}
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "#") || s.Sensitive(line) {
			return "", false
		}
	}
	cleaned, _ := s.Text(description)
	return cleaned, cleaned != ""
}

// Input is everything Body composes a description from.
type Input struct {
	TaskID       string
	TribunalName string
	Facts        Facts
	// Steps are the tribunal steps that passed in the final round, in order.
	Steps []string
	// FixRounds is how many fix rounds ran; review rounds are one more.
	FixRounds int
	// Notes are the review's info findings, one line each.
	Notes []string
	// Description, when not empty, replaces the generated What section.
	Description string
}

// Body composes the pull request description. Every piece of text from the
// camp or the reviewer is sanitized; the number of dropped lines is returned
// so the caller can say so.
func Body(s Sanitizer, in Input) (string, int) {
	dropped := 0
	var sections []string

	what, n := s.Text(in.Description)
	dropped += n
	if what == "" {
		what = bullets(s, in.Facts.Subjects, &dropped)
	}
	if what != "" {
		sections = append(sections, "## What\n\n"+what)
	}

	if line := diffSummary(in.Facts); line != "" {
		sections = append(sections, "## Changes\n\n"+line)
	}

	if len(in.Steps) > 0 {
		sections = append(sections, "## Verification\n\n"+verificationLine(in.TribunalName, in.Steps, in.FixRounds))
	}

	var notes []string
	for _, note := range in.Notes {
		note, n := s.Text(note)
		dropped += n
		if note != "" {
			notes = append(notes, oneLine(note))
		}
	}
	if len(notes) > 0 {
		sections = append(sections, "## Tribunal notes\n\nNon-blocking findings from the adversarial review:\n\n"+bulletList(notes))
	}

	footer := fmt.Sprintf("---\nvexillum mission %s, verified by %s: lint, tests, review, and docs all passed.", in.TaskID, in.TribunalName)
	sections = append(sections, footer)
	return strings.Join(sections, "\n\n"), dropped
}

// bullets renders the commit subjects as a deduplicated bullet list, in order.
func bullets(s Sanitizer, subjects []string, dropped *int) string {
	seen := map[string]bool{}
	var items []string
	for _, subject := range subjects {
		subject = oneLine(subject)
		if subject == "" || seen[subject] {
			continue
		}
		seen[subject] = true
		if s.Sensitive(subject) {
			*dropped++
			continue
		}
		items = append(items, subject)
	}
	return bulletList(items)
}

func bulletList(items []string) string {
	var b strings.Builder
	for i, item := range items {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("- " + item)
	}
	return b.String()
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// diffSummary is the "N files changed, +A -D across areas" line, empty when
// the diff is.
func diffSummary(f Facts) string {
	if f.Files == 0 {
		return ""
	}
	line := fmt.Sprintf("%s changed, +%d -%d", plural(f.Files, "file"), f.Added, f.Deleted)
	if len(f.Areas) > 0 {
		quoted := make([]string, len(f.Areas))
		for i, a := range f.Areas {
			quoted[i] = "`" + a + "`"
		}
		line += ". Areas touched: " + strings.Join(quoted, ", ")
	}
	return line + "."
}

func verificationLine(name string, steps []string, fixRounds int) string {
	title := strings.ToUpper(name[:1]) + name[1:]
	return fmt.Sprintf("%s steps passed: %s. %s, %s.", title, strings.Join(steps, ", "), plural(fixRounds+1, "review round"), plural(fixRounds, "fix round"))
}
