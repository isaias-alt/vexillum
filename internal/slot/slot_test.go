package slot

import (
	"errors"
	"strings"
	"testing"
)

const tmpl = "Core rules.\n\nSecond paragraph."

func block(body string) string { return Render(body) }

func TestHash(t *testing.T) {
	if got := Hash("abc"); got != "ba7816bf" {
		t.Errorf("Hash(abc) = %q, want ba7816bf", got)
	}
	if Hash("a\r\nb\n") != Hash("a\nb") {
		t.Error("hash must ignore CRLF and trailing newlines")
	}
}

func TestRender(t *testing.T) {
	got := Render("x\ny")
	want := "<!-- BEGIN VEXILLUM v:1 hash:" + Hash("x\ny") + " -->\nx\ny\n<!-- END VEXILLUM -->\n"
	if got != want {
		t.Errorf("Render =\n%q\nwant\n%q", got, want)
	}
}

func TestInspect(t *testing.T) {
	cur := block(tmpl)
	oldBody := "Older rules."
	oldBlock := block(oldBody)
	edited := strings.Replace(cur, "Core rules.", "My rules.", 1)
	fence := "```md\n" + cur + "```\n"

	tests := []struct {
		name    string
		content string
		want    State
	}{
		{"empty", "", StateAbsent},
		{"no markers", "# Title\n\ntext\n", StateAbsent},
		{"current", cur, StateCurrent},
		{"current with user text around", "before\n\n" + cur + "\nafter\n", StateCurrent},
		{"current CRLF", strings.ReplaceAll(cur, "\n", "\r\n"), StateCurrent},
		{"stale old template", oldBlock, StateStale},
		{"stale marker only (version bump)", strings.Replace(cur, "v:1", "v:0", 1), StateStale},
		{"drifted", edited, StateDrifted},
		{"drifted: missing hash", strings.Replace(edited, " hash:"+Hash(tmpl), "", 1), StateDrifted},
		{"body is template but hash wrong", strings.Replace(cur, Hash(tmpl), "deadbeef", 1), StateStale},
		{"BEGIN only", "<!-- BEGIN VEXILLUM v:1 hash:abcd1234 -->\ntext\n", StateMalformed},
		{"END only", "text\n<!-- END VEXILLUM -->\n", StateMalformed},
		{"END before BEGIN", "<!-- END VEXILLUM -->\n<!-- BEGIN VEXILLUM v:1 hash:abcd1234 -->\n", StateMalformed},
		{"duplicate blocks", cur + "\n" + cur, StateMalformed},
		{"BEGIN BEGIN END", "<!-- BEGIN VEXILLUM v:1 -->\n<!-- BEGIN VEXILLUM v:1 -->\n<!-- END VEXILLUM -->\n", StateMalformed},
		{"markers only inside code fence", fence, StateAbsent},
		{"tilde fence", "~~~\n" + cur + "~~~\n", StateAbsent},
		{"real block plus quoted block", cur + "\n" + fence, StateCurrent},
		{"indented marker is not a marker", "  " + strings.ReplaceAll(cur, "\n", "\n  "), StateAbsent},
		{"marker with trailing text is not END", "<!-- BEGIN VEXILLUM v:1 -->\nx\n<!-- END VEXILLUM --> trailing\n", StateMalformed},
		{"lookalike prefix", "<!-- BEGIN VEXILLUMX -->\nx\n<!-- END VEXILLUM -->\n", StateMalformed},
		{"inline mention is not a marker", "see `<!-- BEGIN VEXILLUM -->` in the docs\n", StateAbsent},
		{"future metadata field stays readable", strings.Replace(cur, " -->\n", " lang:es -->\n", 1), StateStale},
		{"empty body block", "<!-- BEGIN VEXILLUM v:1 hash:" + Hash("") + " -->\n<!-- END VEXILLUM -->\n", StateStale},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Inspect(tc.content, tmpl)
			if got.State != tc.want {
				t.Fatalf("state = %s (%s), want %s", got.State, got.Reason, tc.want)
			}
			if tc.want == StateMalformed && got.Reason == "" {
				t.Error("malformed without a reason")
			}
		})
	}
}

func TestInspectParsesBody(t *testing.T) {
	c := "intro\n\n<!-- BEGIN VEXILLUM v:1 hash:abcd1234 -->\r\nline1\r\nline2\r\n<!-- END VEXILLUM -->\r\n"
	got := Inspect(c, tmpl)
	if got.Body != "line1\nline2" || got.MarkerHash != "abcd1234" || got.MarkerVersion != "1" || got.BodyHash != Hash("line1\nline2") {
		t.Errorf("unexpected inspection: %+v", got)
	}
}

func TestUpsert(t *testing.T) {
	cur := block(tmpl)
	old := block("Older rules.")
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"empty file", "", cur},
		{"whitespace only file", "\n\n", cur},
		{"user text with trailing newline", "# Mine\n", "# Mine\n\n" + cur},
		{"user text without trailing newline", "# Mine", "# Mine\n\n" + cur},
		{"user text already ends with blank line", "# Mine\n\n", "# Mine\n\n" + cur},
		{"CRLF file", "# Mine\r\n", "# Mine\r\n\r\n" + strings.ReplaceAll(cur, "\n", "\r\n")},
		{"already current is untouched", "a\n\n" + cur + "\nz\n", "a\n\n" + cur + "\nz\n"},
		{"stale replaced in place, user text kept", "a\n\n" + old + "\nz\n", "a\n\n" + cur + "\nz\n"},
		{"stale CRLF keeps CRLF", strings.ReplaceAll("a\n"+old+"z\n", "\n", "\r\n"), strings.ReplaceAll("a\n"+cur+"z\n", "\n", "\r\n")},
		{"block at EOF without final newline", strings.TrimSuffix(old, "\n"), strings.TrimSuffix(cur, "\n")},
		{"quoted markers in a fence are ignored", "```\n" + cur + "```\n", "```\n" + cur + "```\n\n" + cur},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Upsert(tc.content, tmpl, false)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got\n%q\nwant\n%q", got, tc.want)
			}
			again, err := Upsert(got, tmpl, false)
			if err != nil || again != got {
				t.Errorf("not idempotent: err=%v\nonce  %q\ntwice %q", err, got, again)
			}
			if s := Inspect(got, tmpl).State; s != StateCurrent {
				t.Errorf("state after upsert = %s", s)
			}
		})
	}
}

func TestUpsertRefusals(t *testing.T) {
	cur := block(tmpl)
	drifted := strings.Replace(cur, "Core rules.", "My rules.", 1)

	t.Run("drifted without force", func(t *testing.T) {
		_, err := Upsert(drifted, tmpl, false)
		if !errors.Is(err, ErrDrifted) {
			t.Fatalf("err = %v, want ErrDrifted", err)
		}
	})
	t.Run("drifted with force", func(t *testing.T) {
		got, err := Upsert("top\n"+drifted+"bottom\n", tmpl, true)
		if err != nil {
			t.Fatal(err)
		}
		if got != "top\n"+cur+"bottom\n" {
			t.Errorf("got %q", got)
		}
	})
	for name, c := range map[string]string{
		"BEGIN only":       "<!-- BEGIN VEXILLUM v:1 -->\nx\n",
		"END before BEGIN": "<!-- END VEXILLUM -->\n<!-- BEGIN VEXILLUM v:1 -->\n",
		"duplicate":        cur + cur,
	} {
		t.Run("malformed "+name, func(t *testing.T) {
			for _, force := range []bool{false, true} {
				if _, err := Upsert(c, tmpl, force); !errors.Is(err, ErrMalformed) {
					t.Errorf("force=%v err = %v, want ErrMalformed", force, err)
				}
			}
		})
	}
}

func TestRemove(t *testing.T) {
	cur := block(tmpl)
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"absent unchanged", "# Mine\n", "# Mine\n"},
		{"only block", cur, ""},
		{"appended block removes its separator", "# Mine\n\n" + cur, "# Mine\n"},
		{"block in the middle", "a\n\n" + cur + "\nb\n", "a\n\nb\n"},
		{"block in the middle no blanks", "a\n" + cur + "b\n", "a\nb\n"},
		{"block at top drops following blank", cur + "\nb\n", "b\n"},
		{"block at EOF without final newline", "a\n\n" + strings.TrimSuffix(cur, "\n"), "a\n"},
		{"CRLF", strings.ReplaceAll("# Mine\n\n"+cur, "\n", "\r\n"), "# Mine\r\n"},
		{"quoted block in fence is kept", "```\n" + cur + "```\n", "```\n" + cur + "```\n"},
		{"drifted block is removed too", strings.Replace(cur, "Core", "My", 1), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Remove(tc.content)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	for name, c := range map[string]string{
		"BEGIN only": "<!-- BEGIN VEXILLUM v:1 -->\n",
		"duplicate":  cur + cur,
	} {
		if _, err := Remove(c); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: err = %v, want ErrMalformed", name, err)
		}
	}
}

func TestRemoveUpsertRoundTrip(t *testing.T) {
	for _, base := range []string{"", "# Mine\n", "# Mine\n\nmore\n", "a\r\nb\r\n"} {
		with, err := Upsert(base, tmpl, false)
		if err != nil {
			t.Fatal(err)
		}
		without, err := Remove(with)
		if err != nil {
			t.Fatal(err)
		}
		if without != base {
			t.Errorf("Remove(Upsert(%q)) = %q", base, without)
		}
		back, err := Upsert(without, tmpl, false)
		if err != nil || back != with {
			t.Errorf("round trip changed output for %q: %q vs %q (err %v)", base, back, with, err)
		}
	}
}

func TestRepair(t *testing.T) {
	cur := block(tmpl)
	begin := "<!-- BEGIN VEXILLUM v:1 hash:abcd1234 -->\n"
	end := "<!-- END VEXILLUM -->\n"
	tests := []struct {
		name        string
		content     string
		want        string
		wantActions int
	}{
		{"healthy untouched", "a\n\n" + cur, "a\n\n" + cur, 0},
		{"absent untouched", "a\n", "a\n", 0},
		{"orphan BEGIN", "a\n" + begin + "b\n", "a\nb\n", 1},
		{"orphan END", "a\n" + end + "b\n", "a\nb\n", 1},
		{"END before BEGIN", end + "a\n" + begin, "a\n", 2},
		{"duplicate pairs keep first", "a\n\n" + cur + "\n" + cur + "z\n", "a\n\n" + cur + "z\n", 1},
		{"orphan BEGIN before real pair", begin + "x\n" + cur, "x\n" + cur, 1},
		{"orphan plus duplicate", cur + "\n" + cur + end, cur, 2},
		{"only quoted markers are left alone", "```\n" + begin + "```\n", "```\n" + begin + "```\n", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, actions := Repair(tc.content)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			if len(actions) != tc.wantActions {
				t.Errorf("actions = %v, want %d", actions, tc.wantActions)
			}
			if tc.wantActions > 0 && Inspect(got, tmpl).State == StateMalformed {
				t.Error("still malformed after repair")
			}
			again, more := Repair(got)
			if again != got || len(more) != 0 {
				t.Errorf("repair not idempotent: %q %v", again, more)
			}
		})
	}
}
