package forum

import (
	"fmt"
	"strings"

	"github.com/isaias-alt/vexillum/internal/cmdname"
)

// FormatPoll renders a poll response in the stable text format agents parse
// (documented in skills/forum/SKILL.md):
//
//	session: <key>                    (omitted when no session is meant)
//	file: <absolute path>             (omitted when no session is meant)
//	status: feedback | ended | browser_disconnected | timeout | no_sessions
//	other_sessions_pending: <n>       (only with poll --all, when n > 0)
//	prompts[N]:
//	  - uid: <id>
//	    redelivered: true             (omitted unless an earlier delivery went unconfirmed)
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
// the user chose, written verbatim; an empty file means the result names its
// own (a multiplexed poll).
func FormatPoll(file string, res PollResponse) string {
	if file == "" {
		file = res.File
	}
	var b strings.Builder
	if res.Session != "" {
		fmt.Fprintf(&b, "session: %s\n", res.Session)
	}
	if file != "" {
		writeField(&b, "", "file", file)
	}
	fmt.Fprintf(&b, "status: %s\n", res.Status)
	if res.EndedBy != "" {
		fmt.Fprintf(&b, "ended_by: %s\n", res.EndedBy)
	}
	if res.OtherPending > 0 {
		fmt.Fprintf(&b, "other_sessions_pending: %d\n", res.OtherPending)
	}
	fmt.Fprintf(&b, "prompts[%d]:\n", len(res.Prompts))
	for _, p := range res.Prompts {
		writePrompt(&b, "", p)
	}
	fmt.Fprintf(&b, "next_step: %s\n", PollNextStep(file, res))
	return b.String()
}

// writePrompt writes one prompt as a list item of a prompts[N] block, its "- "
// marker indented by lead.
func writePrompt(b *strings.Builder, lead string, p Prompt) {
	b.WriteString(lead + "  - ")
	writeField(b, "", "uid", p.UID)
	in := lead + "    "
	if p.Redelivered {
		writeField(b, in, "redelivered", "true")
	}
	writeField(b, in, "tag", p.Tag)
	writeField(b, in, "prompt", p.Prompt)
	if p.Selector != "" {
		writeField(b, in, "selector", p.Selector)
	}
	if p.Text != "" {
		writeField(b, in, "text", p.Text)
	}
	if len(p.Target) > 0 {
		writeField(b, in, "target", string(p.Target))
	}
	if len(p.Attachments) > 0 {
		fmt.Fprintf(b, "%sattachments[%d]:\n", in, len(p.Attachments))
		for _, a := range p.Attachments {
			b.WriteString(in + "  - ")
			writeField(b, "", "path", a.Path)
			writeField(b, in+"    ", "type", a.Mime)
			writeField(b, in+"    ", "bytes", fmt.Sprint(a.Bytes))
		}
	}
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
// the agent should do now, per status. A multiplexed poll (res.All) keeps the
// agent on `poll --all`, naming the session to reply to.
func PollNextStep(file string, res PollResponse) string {
	if file == "" {
		file = res.File
	}
	again := cmdname.Name + " forum poll " + shellQuote(file)
	reply := again + " --reply \"<what you did>\""
	if res.All {
		again = cmdname.Name + " forum poll --all"
		reply = again + " --reply-to " + shellQuote(file) + " --reply \"<what you did>\""
	}
	step := pollStatusStep(file, res, again, reply)
	if res.Status != PollNoSessions && hasRedelivered(res.Prompts) {
		step += " Prompts marked redelivered were delivered by an earlier poll that never confirmed it: skip any uid you already applied."
	}
	return step
}

func pollStatusStep(file string, res PollResponse, again, reply string) string {
	switch res.Status {
	case PollFeedback:
		lead := "Apply this feedback, then run `" + reply + "` to answer in the browser and keep waiting for more."
		if res.All && res.OtherPending > 0 {
			lead = fmt.Sprintf("Apply this feedback, then run `%s` to answer in the browser and keep waiting; %d other session(s) already have feedback waiting, the next poll delivers it.", reply, res.OtherPending)
		}
		if hasAttachments(res.Prompts) {
			return "Some prompts carry attachments: read each image from its path (your file-reading tool can open images) before acting. " + lead
		}
		return lead
	case PollEnded:
		who := "This session is over"
		if res.EndedBy == EndedByUser {
			who = "The user closed this session from the browser"
		}
		if res.All {
			tail := " Leave it closed unless the user requests otherwise. To keep covering the sessions that are still open, run `" + again + "` once more (no_sessions means none remain)."
			if len(res.Prompts) > 0 {
				return who + ". Its last feedback comes with this result and will not be sent again: apply it." + tail
			}
			return who + "." + tail
		}
		if len(res.Prompts) > 0 {
			return who + ". Its last feedback comes with this result and will not be sent again: apply it. Polling this session is finished, and it stays closed unless the user requests otherwise."
		}
		return who + ". Polling this session is finished, and it stays closed unless the user requests otherwise."
	case PollBrowserDisconnected:
		if res.All {
			return "No review window is open any more, yet the sessions can still be resumed. Let the user choose between bringing them back (`" + cmdname.Name + " forum <file>`) and closing them (`" + cmdname.Name + " forum end <file>`), and take neither step before they answer."
		}
		return "The browser window is gone, yet the session can still be resumed. Let the user choose between bringing it back (`" + cmdname.Name + " forum " + shellQuote(file) + "`) and closing it (`" + cmdname.Name + " forum end " + shellQuote(file) + "`), and take neither step before they answer."
	case PollNoSessions:
		return "No forum session is open, so there is nothing to listen to. Stop polling; open an artifact with `" + cmdname.Name + " forum <file>` when there is something to review."
	case PollTimeout:
		return "No feedback yet. Run `" + again + "` again to keep waiting."
	default:
		return "Run `" + again + "` again."
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
	return "Tell the user the review is open at the URL above, then run `" + poll + "` to wait for their feedback (keep polling in a loop; never kill the poll). With several sessions open, run one `" + cmdname.Name + " forum poll --all` instead of a poll per file."
}

// OpenNextStepForwarded is the instruction printed after `vx forum <file>` when
// a listener is running for the session: the commander does not poll, feedback
// reaches it through the inbox and a forum wake.
func OpenNextStepForwarded(file string, res OpenResponse) string {
	if res.Status == OpenUserEnded {
		return OpenNextStep(file, res)
	}
	step := "Tell the user the review is open at the URL above. Do not poll: a listener forwards their feedback to this project's inbox and wakes you through the Stop hook when you end a turn. On a forum wake run `" +
		cmdname.Name + " forum inbox`, act on it, then answer with `" + cmdname.Name + " forum reply " + shellQuote(file) + " --reply \"<what you did>\"` and confirm with `" + cmdname.Name + " forum inbox --ack <uid>...`."
	if res.Pending > 0 {
		step = fmt.Sprintf("The user already sent %d prompt(s); the listener is forwarding them, so `%s forum inbox` shows them shortly. ", res.Pending, cmdname.Name) + step
	}
	return step + " (Fallback if no listener runs: `" + cmdname.Name + " forum poll " + shellQuote(file) + "`.)"
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

func hasRedelivered(prompts []Prompt) bool {
	for _, p := range prompts {
		if p.Redelivered {
			return true
		}
	}
	return false
}
