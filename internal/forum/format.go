package forum

import (
	"fmt"
	"strings"

	"github.com/isaias-alt/vexillum/internal/cmdname"
)

// FormatPoll renders a poll response in the stable text format agents parse
// (documented in skills/forum/SKILL.md):
//
//	session: <key>
//	file: <absolute path>
//	status: feedback | ended | browser_disconnected | timeout
//	prompts[N]:
//	  - uid: <id>
//	    tag: <tag>
//	    prompt: <text>
//	    selector: <css selector>      (omitted when empty)
//	    text: <element text>          (omitted when empty)
//	    target: <compact JSON>        (omitted when empty)
//	    attachments[N]:               (omitted when none)
//	      - path: <absolute local path of an image the user attached>
//	        type: <image/png | image/jpeg | image/gif | image/webp>
//	        bytes: <size>
//	next_step: <what to do now>
//
// A multi-line value is written as `key: |` followed by its lines indented
// two spaces deeper than the key; everything else is `key: value` on one
// line. file is a path
// the user chose, written verbatim.
func FormatPoll(file string, res PollResponse) string {
	var b strings.Builder
	fmt.Fprintf(&b, "session: %s\n", res.Session)
	writeField(&b, "", "file", file)
	fmt.Fprintf(&b, "status: %s\n", res.Status)
	if res.EndedBy != "" {
		fmt.Fprintf(&b, "ended_by: %s\n", res.EndedBy)
	}
	fmt.Fprintf(&b, "prompts[%d]:\n", len(res.Prompts))
	for _, p := range res.Prompts {
		b.WriteString("  - ")
		writeField(&b, "", "uid", p.UID)
		writeField(&b, "    ", "tag", p.Tag)
		writeField(&b, "    ", "prompt", p.Prompt)
		if p.Selector != "" {
			writeField(&b, "    ", "selector", p.Selector)
		}
		if p.Text != "" {
			writeField(&b, "    ", "text", p.Text)
		}
		if len(p.Target) > 0 {
			writeField(&b, "    ", "target", string(p.Target))
		}
		if len(p.Attachments) > 0 {
			fmt.Fprintf(&b, "    attachments[%d]:\n", len(p.Attachments))
			for _, a := range p.Attachments {
				b.WriteString("      - ")
				writeField(&b, "", "path", a.Path)
				writeField(&b, "        ", "type", a.Mime)
				writeField(&b, "        ", "bytes", fmt.Sprint(a.Bytes))
			}
		}
	}
	fmt.Fprintf(&b, "next_step: %s\n", PollNextStep(file, res))
	return b.String()
}

func writeField(b *strings.Builder, indent, key, value string) {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	if !strings.Contains(value, "\n") {
		fmt.Fprintf(b, "%s%s: %s\n", indent, key, value)
		return
	}
	fmt.Fprintf(b, "%s%s: |\n", indent, key)
	for _, line := range strings.Split(value, "\n") {
		if line == "" {
			b.WriteString("\n")
			continue
		}
		fmt.Fprintf(b, "%s  %s\n", indent, line)
	}
}

// PollNextStep is the one-sentence instruction printed after a poll: what
// the agent should do now, per status.
func PollNextStep(file string, res PollResponse) string {
	poll := cmdname.Name + " forum poll " + shellQuote(file)
	switch res.Status {
	case PollFeedback:
		if hasAttachments(res.Prompts) {
			return "Some prompts carry attachments: read each image from its path (your file-reading tool can open images) before acting. Apply this feedback, then run `" + poll + " --reply \"<what you did>\"` to answer in the browser and keep waiting for more."
		}
		return "Apply this feedback, then run `" + poll + " --reply \"<what you did>\"` to answer in the browser and keep waiting for more."
	case PollEnded:
		who := "The session ended"
		if res.EndedBy == EndedByUser {
			who = "The user ended the session from the browser"
		}
		if len(res.Prompts) > 0 {
			return who + ". This was the final feedback, delivered once: apply it. Do not poll again and do not reopen the session unless the user asks."
		}
		return who + ". Stop polling and do not reopen the session unless the user asks."
	case PollBrowserDisconnected:
		return "The browser window went away but the session is still resumable. Ask the user whether to reopen it (`" + cmdname.Name + " forum " + shellQuote(file) + "`) or end it (`" + cmdname.Name + " forum end " + shellQuote(file) + "`); do neither uninvited."
	case PollTimeout:
		return "No feedback yet. Run `" + poll + "` again to keep waiting."
	default:
		return "Run `" + poll + "` again."
	}
}

// OpenNextStep is the instruction printed after `vx forum <file>`.
func OpenNextStep(file string, res OpenResponse) string {
	poll := cmdname.Name + " forum poll " + shellQuote(file)
	switch res.Status {
	case OpenUserEnded:
		return "The user ended this session from the browser, so it was not reopened. Only if the user asks for further review, run `" + cmdname.Name + " forum " + shellQuote(file) + " --reopen`."
	}
	if res.Pending > 0 {
		return fmt.Sprintf("The user already sent %d prompt(s). Run `%s` now to receive them; keep polling in a loop after that.", res.Pending, poll)
	}
	return "Tell the user the review is open at the URL above, then run `" + poll + "` to wait for their feedback (keep polling in a loop; never kill the poll)."
}

// shellQuote quotes s for a POSIX shell only when it needs it.
func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/._-+=:@%", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func hasAttachments(prompts []Prompt) bool {
	for _, p := range prompts {
		if len(p.Attachments) > 0 {
			return true
		}
	}
	return false
}
