package slot

import (
	"fmt"
	"strings"
)

const diffContext = 3

// UnifiedDiff renders a unified diff from a to b (labelled aName and bName)
// with three lines of context, for showing the user how their edited body
// differs from the template: UnifiedDiff(edited, template, "your edit",
// "vexillum template"). Line endings are normalised first. It returns ""
// when the two are equal.
func UnifiedDiff(a, b, aName, bName string) string {
	al, bl := diffLines(a), diffLines(b)
	ops := diffOps(al, bl)

	var changes []int
	for i, o := range ops {
		if o.kind != ' ' {
			changes = append(changes, i)
		}
	}
	if len(changes) == 0 {
		return ""
	}

	// Number of a/b lines consumed before each op.
	aLine := make([]int, len(ops)+1)
	bLine := make([]int, len(ops)+1)
	for i, o := range ops {
		aLine[i+1], bLine[i+1] = aLine[i], bLine[i]
		if o.kind != '+' {
			aLine[i+1]++
		}
		if o.kind != '-' {
			bLine[i+1]++
		}
	}

	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n", aName, bName)
	for i := 0; i < len(changes); {
		j := i
		for j+1 < len(changes) && changes[j+1]-changes[j]-1 <= 2*diffContext {
			j++
		}
		lo := max(changes[i]-diffContext, 0)
		hi := min(changes[j]+diffContext+1, len(ops))
		aCount, bCount := aLine[hi]-aLine[lo], bLine[hi]-bLine[lo]
		aStart, bStart := aLine[lo]+1, bLine[lo]+1
		if aCount == 0 {
			aStart = aLine[lo]
		}
		if bCount == 0 {
			bStart = bLine[lo]
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", aStart, aCount, bStart, bCount)
		for _, o := range ops[lo:hi] {
			out.WriteByte(o.kind)
			out.WriteString(o.text)
			out.WriteByte('\n')
		}
		i = j + 1
	}
	return out.String()
}

type diffOp struct {
	kind byte // ' ', '-', '+'
	text string
}

func diffLines(s string) []string {
	s = canonical(s)
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// diffOps is a plain LCS line diff; slot bodies are a few dozen lines.
func diffOps(a, b []string) []diffOp {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var ops []diffOp
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{' ', a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffOp{'-', a[i]})
			i++
		default:
			ops = append(ops, diffOp{'+', b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, diffOp{'-', a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, diffOp{'+', b[j]})
	}
	return ops
}
