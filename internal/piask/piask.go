// Package piask is vexillum's side of the ask_user tool a Pi soldier is given.
//
// Pi has no question selector vexillum can read the way it reads Claude Code's
// AskUserQuestion modal, so a Pi soldier is launched with a small extension of
// vexillum's own (ask_user.ts, embedded here) that registers an ask_user tool.
// The extension reports the soldier as blocked to herdr while it waits, and
// leaves the question in a file so "vx decide" can read it and answer it
// without scraping the pane or typing into it.
//
// The extension is written under the project's state directory, outside the
// camp, and loaded with "pi -e <path>": nothing is added to the worktree and
// Pi's project-trust gate, which only covers the project's own .pi, never
// applies. It needs nothing but Pi itself, which runs it with its own bundled
// loader.
//
// Protocol, in one directory per task (Dir):
//
//	question.json  {"version":1,"id","question","options","asked_at"}
//	               written by the extension, atomically, before it reports
//	               blocked, and removed once the question is closed.
//	answer.json    {"version":1,"id","answer","option"}
//	               written by vx (WriteAnswer), atomically. The extension takes
//	               it only when id matches its open question, so a leftover
//	               file can never answer a later question.
//
// Both files are mode 0600 in a 0700 directory and hold only what the soldier
// asked and what the general answered: no environment, no paths, no secrets of
// vexillum's. An answer typed into Pi's own dialog works too and closes the
// question the same way.
package piask

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/isaias-alt/vexillum/internal/atomicfile"
)

//go:embed ask_user.ts
var extensionSource []byte

const (
	// ToolName is the tool the extension registers, the name a soldier's
	// prompt tells it to call.
	ToolName = "ask_user"

	// Flag is the extension's CLI flag naming the task's protocol directory.
	Flag = "vx-ask-dir"

	extensionDirName = "pi"
	extensionFile    = "ask_user.ts"
	asksDirName      = "asks"
	questionFile     = "question.json"
	answerFile       = "answer.json"
	protocolVersion  = 1

	// maxFileBytes bounds what is read back from a file the soldier's
	// process wrote: far above the extension's own caps, far below a problem.
	maxFileBytes = 256 << 10

	consumePoll = 100 * time.Millisecond
)

// ConsumeTimeout is how long an answer is given to be picked up by the
// extension, which polls every 400ms: many missed polls. A variable so tests
// need not wait it out.
var ConsumeTimeout = 10 * time.Second

// Question is the open question the extension recorded.
type Question struct {
	Version  int       `json:"version"`
	ID       string    `json:"id"`
	Question string    `json:"question"`
	Options  []string  `json:"options,omitempty"`
	AskedAt  time.Time `json:"asked_at"`
}

// answer is the file WriteAnswer leaves for the extension.
type answer struct {
	Version    int       `json:"version"`
	ID         string    `json:"id"`
	Answer     string    `json:"answer"`
	Option     int       `json:"option,omitempty"`
	AnsweredAt time.Time `json:"answered_at"`
}

var idPattern = regexp.MustCompile(`^[0-9a-f]{16,64}$`)

// ExtensionPath is where the extension is installed for a project:
// <project root>/pi/ask_user.ts.
func ExtensionPath(projectRoot string) string {
	return filepath.Join(projectRoot, extensionDirName, extensionFile)
}

// Dir is the protocol directory of one task: <project root>/asks/<task id>.
// The task id is validated by state.ValidateID before it gets here.
func Dir(projectRoot, taskID string) string {
	return filepath.Join(projectRoot, asksDirName, taskID)
}

// Install writes the embedded extension to ExtensionPath, atomically, and
// returns that path. A copy that is already there with the same content is
// left alone, so a soldier that is running keeps the file it loaded.
func Install(projectRoot string) (string, error) {
	path := ExtensionPath(projectRoot)
	if cur, err := os.ReadFile(path); err == nil && string(cur) == string(extensionSource) {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := atomicfile.WriteMode(path, extensionSource, 0o600); err != nil {
		return "", fmt.Errorf("installing the pi ask_user extension: %w", err)
	}
	return path, nil
}

// Source returns the embedded extension, for tests and docs.
func Source() []byte {
	return append([]byte{}, extensionSource...)
}

// ErrNoQuestion means no question is open in the directory.
var ErrNoQuestion = errors.New("no open question")

// ReadQuestion reads the open question in dir. It returns ErrNoQuestion when
// there is none, and another error for a file that is there but not a valid
// question (unknown version, bad id, empty question): callers must treat that
// as no question rather than guess at one.
func ReadQuestion(dir string) (Question, error) {
	var q Question
	f, err := os.Open(filepath.Join(dir, questionFile))
	if err != nil {
		if os.IsNotExist(err) {
			return q, ErrNoQuestion
		}
		return q, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return q, err
	}
	if len(data) > maxFileBytes {
		return q, fmt.Errorf("%s is larger than %d bytes", questionFile, maxFileBytes)
	}
	if err := json.Unmarshal(data, &q); err != nil {
		return q, fmt.Errorf("decoding %s: %w", questionFile, err)
	}
	if q.Version != protocolVersion || !idPattern.MatchString(q.ID) || q.Question == "" {
		return Question{}, fmt.Errorf("%s is not a version %d question", questionFile, protocolVersion)
	}
	return q, nil
}

// WriteAnswer answers question q. option is the 1-based number of the option
// text matches, or 0 for free text. The file is replaced atomically.
func WriteAnswer(dir string, q Question, text string, option int) error {
	if text == "" {
		return errors.New("an empty answer cannot be delivered")
	}
	return atomicfile.WriteJSON(filepath.Join(dir, answerFile), answer{
		Version:    protocolVersion,
		ID:         q.ID,
		Answer:     text,
		Option:     option,
		AnsweredAt: time.Now().UTC(),
	})
}

// Withdraw removes an answer that was not picked up, so it cannot answer the
// question late.
func Withdraw(dir string) {
	_ = os.Remove(filepath.Join(dir, answerFile))
}

// WaitConsumed waits until question q is no longer open - the extension took
// the answer, or the pane answered it first - and reports whether it closed
// within timeout.
func WaitConsumed(dir string, q Question, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		cur, err := ReadQuestion(dir)
		if err != nil || cur.ID != q.ID {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(consumePoll)
	}
}
