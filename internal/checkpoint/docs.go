package checkpoint

import (
	"regexp"
	"strings"
)

// exportedDeclPattern matches the start of an added line that introduces
// a new top-level exported Go declaration - "func Name(...)", "func (r
// *T) Name(...)", "type Name ...", "const Name ..." or "var Name ...".
// It does not see into a grouped "const ( ... )" / "var ( ... )" block -
// out of scope for a v1 docs check that's meant to stay light (PRD
// v2/docs/review-tool.md's own "Alcance" explicitly allows a lighter docs
// step rather than blocking the rest of the pipeline on it).
var exportedDeclPattern = regexp.MustCompile(`^(?:func|type|const|var)\s+(?:\([^)]*\)\s*)?([A-Z][A-Za-z0-9_]*)`)

// runDocs checks that any exported Go declaration this mission's diff
// introduces carries a doc comment - Go's own convention, and the
// closest deterministic equivalent to review-tool' own docs step that
// doesn't require reimplementing an entire documentation generator.
// Non-Go projects, and _test.go files (whose exported Test/Benchmark/...
// functions are never doc-commented by convention in this codebase or
// idiomatically elsewhere), are skipped rather than failed.
func runDocs(campPath, base string) (StepResult, error) {
	if !hasFile(campPath, "go.mod") {
		return StepResult{Step: StepDocs, Passed: true, Detail: "no go.mod detected, docs check skipped"}, nil
	}

	diff, err := gitDiff(campPath, base, "*.go")
	if err != nil {
		return StepResult{}, err
	}
	if strings.TrimSpace(diff) == "" {
		return StepResult{Step: StepDocs, Passed: true, Detail: "no Go changes to check"}, nil
	}

	undocumented := findUndocumentedExports(diff)
	if len(undocumented) > 0 {
		return StepResult{Step: StepDocs, Passed: false, Detail: "new exported declarations without a doc comment: " + strings.Join(undocumented, ", ")}, nil
	}
	return StepResult{Step: StepDocs, Passed: true}, nil
}

// findUndocumentedExports walks a unified diff's hunks (as produced by
// gitDiff) file by file, and reports "<file>:<name>" for every newly
// added exported declaration whose immediately preceding line (in the
// resulting file, so a removed "-" line never counts) isn't a "//"
// comment.
func findUndocumentedExports(diff string) []string {
	var undocumented []string
	var hunkLines []string
	var currentFile string

	checkable := func(file string) bool {
		return strings.HasSuffix(file, ".go") && !strings.HasSuffix(file, "_test.go")
	}

	flush := func() {
		if checkable(currentFile) {
			for i, raw := range hunkLines {
				if len(raw) == 0 || raw[0] != '+' || strings.HasPrefix(raw, "+++") {
					continue
				}
				content := strings.TrimLeft(raw[1:], " \t")
				m := exportedDeclPattern.FindStringSubmatch(content)
				if m == nil {
					continue
				}
				name := m[1]
				if isTestEntryPoint(name) || hasDocCommentAbove(hunkLines, i) {
					continue
				}
				undocumented = append(undocumented, currentFile+":"+name)
			}
		}
		hunkLines = nil
	}

	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ "):
			flush()
			currentFile = strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/")
		case strings.HasPrefix(line, "diff --git"):
			flush()
		case strings.HasPrefix(line, "@@"):
			flush()
		default:
			hunkLines = append(hunkLines, line)
		}
	}
	flush()
	return undocumented
}

// hasDocCommentAbove reports whether the resulting file's line right
// above hunkLines[idx] (skipping over any removed "-" line, which never
// makes it into the resulting file) is a "//" comment. A blank line
// breaks the immediately-above rule, same as gofmt/govet's own doc
// comment convention.
func hasDocCommentAbove(hunkLines []string, idx int) bool {
	for j := idx - 1; j >= 0; j-- {
		raw := hunkLines[j]
		if len(raw) == 0 || raw[0] == '\\' {
			continue
		}
		if raw[0] == '-' {
			continue
		}
		content := strings.TrimSpace(raw[1:])
		if content == "" {
			return false
		}
		return strings.HasPrefix(content, "//")
	}
	return false
}

// isTestEntryPoint reports whether name is one of Go's own exported test
// entry-point prefixes (Test, Benchmark, Example, Fuzz) - conventionally
// undocumented, in this codebase and idiomatically elsewhere, so they're
// never flagged even outside a _test.go file.
func isTestEntryPoint(name string) bool {
	for _, prefix := range []string{"Test", "Benchmark", "Example", "Fuzz"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
