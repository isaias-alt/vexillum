package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// errNoAnswer is returned by the prompter when input ends (or keeps being
// unintelligible) before a question gets an answer.
var errNoAnswer = errors.New("no answer (input closed or not understood)")

// maxAnswerTries is how many times a yes/no question is repeated after an
// unintelligible answer before giving up.
const maxAnswerTries = 3

// prompter asks questions on w and reads answers line by line from r.
type prompter struct {
	r *bufio.Reader
	w io.Writer
}

func newPrompter(in io.Reader, out io.Writer) *prompter {
	return &prompter{r: bufio.NewReader(in), w: out}
}

// readLine prints question and returns the trimmed line typed in answer. A
// closed input with nothing typed is errNoAnswer.
func (p *prompter) readLine(question string) (string, error) {
	fmt.Fprint(p.w, question)
	line, err := p.r.ReadString('\n')
	if err != nil && line == "" {
		fmt.Fprintln(p.w)
		return "", errNoAnswer
	}
	return strings.TrimSpace(line), nil
}

// confirm asks a yes/no question. An empty answer takes the default, shown
// as the capital letter in the [Y/n] or [y/N] hint.
func (p *prompter) confirm(question string, def bool) (bool, error) {
	hint := "[Y/n]"
	if !def {
		hint = "[y/N]"
	}
	for i := 0; i < maxAnswerTries; i++ {
		ans, err := p.readLine(question + " " + hint + " ")
		if err != nil {
			return false, err
		}
		switch strings.ToLower(ans) {
		case "":
			return def, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
		fmt.Fprintln(p.w, "Please answer y or n.")
	}
	return false, errNoAnswer
}

// isTerminal reports whether f is an interactive terminal (a character
// device), the stdlib-only way: pipes, files and /dev/null are not.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
