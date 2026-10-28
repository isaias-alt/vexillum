package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// fmtNum formats a time in milliseconds with three significant figures at
// most: 0.75, 8.24, 12.3, 334, 15094. The unit is not written; tables say it
// once in their heading.
func fmtNum(v float64) string {
	switch {
	case v >= 100:
		return fmt.Sprintf("%.0f", v)
	case v >= 10:
		return fmt.Sprintf("%.1f", v)
	default:
		return fmt.Sprintf("%.2f", v)
	}
}

// fmtMS is fmtNum with the unit, for prose.
func fmtMS(v float64) string { return fmtNum(v) + " ms" }

// fmtPct formats a signed change in percent: +3.2%, -12.5%, +215%, 0.0%.
func fmtPct(p float64) string {
	switch {
	case p > -0.05 && p < 0.05:
		return "0.0%"
	case p >= 100 || p <= -100:
		return fmt.Sprintf("%+.0f%%", p)
	}
	return fmt.Sprintf("%+.1f%%", p)
}

// fmtAbsPct formats a size in percent, without a sign: 20%, 32.5%.
func fmtAbsPct(p float64) string {
	if p < 0 {
		p = -p
	}
	if p >= 100 || p == float64(int(p)) {
		return fmt.Sprintf("%.0f%%", p)
	}
	return fmt.Sprintf("%.1f%%", p)
}

// style is how a piece of terminal text is painted.
type style int

const (
	stylePlain style = iota
	styleBold
	styleDim
	styleRed
	styleGreen
	styleYellow
	styleCyan
)

var styleCodes = map[style]string{
	styleBold:   "1",
	styleDim:    "2",
	styleRed:    "31",
	styleGreen:  "32",
	styleYellow: "33",
	styleCyan:   "36",
}

// palette paints text with ANSI escapes when it is on, and returns it as it
// is when it is off.
type palette struct{ on bool }

func (p palette) paint(s style, text string) string {
	code, ok := styleCodes[s]
	if !p.on || !ok || text == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

// colorEnabled decides whether to paint. mode is the -color flag: "always",
// "never", or "auto", which paints only when out is a terminal, NO_COLOR is
// unset or empty (https://no-color.org) and the terminal is not "dumb".
func colorEnabled(mode string, out *os.File, getenv func(string) string) bool {
	switch mode {
	case "always":
		return true
	case "never":
		return false
	}
	if getenv("NO_COLOR") != "" || getenv("TERM") == "dumb" {
		return false
	}
	info, err := out.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// column is one column of a terminal table.
type column struct {
	title string
	// right aligns the column's cells, which suits numbers.
	right bool
}

// cell is one piece of text in a table, with the style it is painted in.
type cell struct {
	text  string
	style style
}

func plain(text string) cell { return cell{text: text} }

// writeTable prints an aligned table, indented, with a bold heading row and a
// rule under it. Widths are measured on the text, so painting never shifts a
// column.
func writeTable(w io.Writer, p palette, indent string, cols []column, rows [][]cell) {
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = utf8.RuneCountInString(c.title)
	}
	for _, row := range rows {
		for i, c := range row {
			widths[i] = max(widths[i], utf8.RuneCountInString(c.text))
		}
	}

	line := func(cells []cell) {
		var b strings.Builder
		b.WriteString(indent)
		for i, c := range cells {
			pad := strings.Repeat(" ", widths[i]-utf8.RuneCountInString(c.text))
			last := i == len(cells)-1
			switch {
			case cols[i].right:
				b.WriteString(pad)
				b.WriteString(p.paint(c.style, c.text))
			case last:
				b.WriteString(p.paint(c.style, c.text))
			default:
				b.WriteString(p.paint(c.style, c.text))
				b.WriteString(pad)
			}
			if !last {
				b.WriteString("  ")
			}
		}
		fmt.Fprintln(w, b.String())
	}

	head := make([]cell, len(cols))
	for i, c := range cols {
		head[i] = cell{text: c.title, style: styleBold}
	}
	line(head)
	rule := make([]cell, len(cols))
	for i := range cols {
		rule[i] = cell{text: strings.Repeat("─", widths[i]), style: styleDim}
	}
	line(rule)
	for _, row := range rows {
		line(row)
	}
}

// dropEmptyColumns removes the columns after the first whose every cell is
// "-": a group that has no CPU time or subprocess count to show should not
// carry empty columns.
func dropEmptyColumns(cols []column, rows [][]cell) ([]column, [][]cell) {
	keep := []int{0}
	for i := 1; i < len(cols); i++ {
		for _, row := range rows {
			if row[i].text != "-" {
				keep = append(keep, i)
				break
			}
		}
	}
	if len(keep) == len(cols) {
		return cols, rows
	}
	outCols := make([]column, len(keep))
	for j, i := range keep {
		outCols[j] = cols[i]
	}
	outRows := make([][]cell, len(rows))
	for r, row := range rows {
		outRows[r] = make([]cell, len(keep))
		for j, i := range keep {
			outRows[r][j] = row[i]
		}
	}
	return outCols, outRows
}

// count is "1 scenario" or "3 scenarios".
func count(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// oneLine flattens s to a single line of at most 160 characters.
func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 160 {
		s = string(r[:160]) + "..."
	}
	return s
}
