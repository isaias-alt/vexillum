package prbody

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSanitizerSensitive(t *testing.T) {
	prompt := "You are a soldier in a vexillum camp (a git worktree of the vexillum repo)."
	s := NewSanitizer(prompt)
	for name, tc := range map[string]struct {
		line string
		want bool
	}{
		"plain subject":        {"feat: add a --title flag to vx ship", false},
		"repo relative path":   {"internal/cli/ship.go and site/content/docs", false},
		"path with home word":  {"update internal/home/page.go", false},
		"mac home path":        {"see /Users/macuser/github/x", true},
		"linux home path":      {"cat /home/runner/work/file", true},
		"quoted home path":     {`path "/Users/me/x"`, true},
		"windows home path":    {`C:\Users\me\x`, true},
		"localhost port":       {"the dev server on localhost:3000", true},
		"loopback port":        {"curl http://127.0.0.1:8080/x", true},
		"localhost no port":    {"binds to localhost only", false},
		"github token":         {"token ghp_abcdefghijklmnopqrstuvwxyz0123456789", true},
		"fine grained pat":     {"github_pat_11ABCDEFG0123456789abcdefgh", true},
		"api key prefix":       {"key sk-ant-api03-abcdefghijklmnopqrstuvwxyz", true},
		"aws key":              {"AKIAABCDEFGHIJKLMNOP", true},
		"slack token":          {"xoxb-1234567890-abcdefghij", true},
		"jwt":                  {"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abcdefghijklmnop", true},
		"bearer header":        {"Authorization: Bearer abcdefghijklmnopqrstuvwxyz0123", true},
		"private key":          {"-----BEGIN RSA PRIVATE KEY-----", true},
		"url credentials":      {"https://user:hunter2@example.com/repo", true},
		"assignment":           {"API_KEY=abcd1234efgh5678", true},
		"yaml secret":          {`password: "correct-horse-battery"`, true},
		"word token in prose":  {"fix: tokens are counted per step", false},
		"short assignment":     {"token: none", false},
		"prompt line":          {prompt, true},
		"prompt line embedded": {"- " + prompt + " Read AGENTS.md", true},
		"long prompt fragment": {"a soldier in a vexillum camp (a git worktree", true},
		"short fragment":       {"a soldier in a vexillum camp", false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := s.Sensitive(tc.line); got != tc.want {
				t.Errorf("Sensitive(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

func TestSanitizerText(t *testing.T) {
	s := NewSanitizer()
	in := "first\n\n\nsecond /Users/me/x\nthird localhost:3000\n\nfourth\r\nfifth   \n"
	got, dropped := s.Text(in)
	want := "first\n\nfourth\nfifth"
	if got != want || dropped != 2 {
		t.Errorf("Text = %q, %d dropped; want %q, 2", got, dropped, want)
	}
	if got, dropped := s.Text(""); got != "" || dropped != 0 {
		t.Errorf("empty text = %q, %d", got, dropped)
	}
}

func TestTitle(t *testing.T) {
	long := "feat: " + strings.Repeat("a very long subject ", 8)
	s := NewSanitizer()
	for name, tc := range map[string]struct {
		subjects []string
		override string
		want     string
	}{
		"single commit":            {[]string{"fix: stop leaking the prompt"}, "", "fix: stop leaking the prompt"},
		"single commit unprefixed": {[]string{"change"}, "", "change"},
		"newest conventional":      {[]string{"feat: first", "chore: tidy", "fix: second", "test: more"}, "", "fix: second"},
		"scoped prefix":            {[]string{"chore: a", "feat(cli): b", "chore: c"}, "", "feat(cli): b"},
		"breaking prefix":          {[]string{"chore: a", "feat!: b"}, "", "feat!: b"},
		"falls back to first":      {[]string{"chore: one", "test: two"}, "", "chore: one"},
		"no commits":               {nil, "", "fallback"},
		"blank subjects":           {[]string{"  ", ""}, "", "fallback"},
		"skips sensitive":          {[]string{"fix: ok", "feat: see /Users/me/x"}, "", "fix: ok"},
		"only sensitive":           {[]string{"feat: localhost:3000"}, "", "fallback"},
		"override wins":            {[]string{"fix: derived"}, "feat: chosen", "feat: chosen"},
		"override one line":        {nil, "feat:\n  two  lines", "feat: two lines"},
		"truncated":                {[]string{long}, "", ""},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Title(s, tc.subjects, tc.override, "fallback")
			if err != nil {
				t.Fatalf("Title: %v", err)
			}
			if name == "truncated" {
				if len(got) > maxTitleLen || !strings.HasSuffix(got, "...") || !strings.HasPrefix(got, "feat: a very long") {
					t.Errorf("expected a title cut to %d with an ellipsis, got %q (%d)", maxTitleLen, got, len(got))
				}
				return
			}
			if got != tc.want {
				t.Errorf("Title = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTitleRejectsSensitiveOverride(t *testing.T) {
	s := NewSanitizer("Work only in this camp and never touch the general's dev server.")
	for _, override := range []string{"fix: see /Users/me/x", "feat: localhost:3000", "Work only in this camp and never touch the general's dev server."} {
		if _, err := Title(s, nil, override, "fallback"); err == nil {
			t.Errorf("expected an error for override %q", override)
		}
	}
}

func TestBody(t *testing.T) {
	facts := Facts{
		Subjects: []string{"feat: one", "fix: two", "feat: one", "docs: see /Users/me/x"},
		Files:    3, Added: 40, Deleted: 5,
		Areas: []string{"internal/", "site/"},
	}
	in := Input{
		TaskID: "abc123", TribunalName: "tribunal", Facts: facts,
		Steps: []string{"lint", "tests", "review", "docs"}, FixRounds: 1,
		Notes: []string{"`a.go:1` - minor", "`b.go:2` - see localhost:3000"},
	}
	s := NewSanitizer("Secret mission statement that must never be published anywhere.")

	got, dropped := Body(s, in)
	want := "## What\n\n- feat: one\n- fix: two\n\n" +
		"## Changes\n\n3 files changed, +40 -5. Areas touched: `internal/`, `site/`.\n\n" +
		"## Verification\n\nTribunal steps passed: lint, tests, review, docs. 2 review rounds, 1 fix round.\n\n" +
		"## Tribunal notes\n\nNon-blocking findings from the adversarial review:\n\n- `a.go:1` - minor\n\n" +
		"---\nvexillum mission abc123, verified by tribunal: lint, tests, review, and docs all passed."
	if got != want {
		t.Errorf("body mismatch\n got: %q\nwant: %q", got, want)
	}
	if dropped != 2 {
		t.Errorf("expected 2 dropped lines, got %d", dropped)
	}

	t.Run("description replaces What only", func(t *testing.T) {
		in := in
		in.Description = "My own summary.\nIt leaked /home/me/x.\nSecret mission statement that must never be published anywhere."
		got, dropped := Body(s, in)
		for _, want := range []string{"## What\n\nMy own summary.\n\n## Changes", "## Verification", "## Tribunal notes", "verified by tribunal"} {
			if !strings.Contains(got, want) {
				t.Errorf("expected %q in:\n%s", want, got)
			}
		}
		for _, banned := range []string{"- feat: one", "/home/me", "Secret mission"} {
			if strings.Contains(got, banned) {
				t.Errorf("did not expect %q in:\n%s", banned, got)
			}
		}
		if dropped != 3 {
			t.Errorf("expected 3 dropped lines (2 from the description, 1 note), got %d", dropped)
		}
	})

	t.Run("fully sanitized description falls back to commits", func(t *testing.T) {
		in := in
		in.Description = "/Users/me/x"
		got, _ := Body(s, in)
		if !strings.Contains(got, "## What\n\n- feat: one") {
			t.Errorf("expected the generated What section, got:\n%s", got)
		}
	})

	t.Run("minimal", func(t *testing.T) {
		got, _ := Body(s, Input{TaskID: "x", TribunalName: "tribunal"})
		want := "---\nvexillum mission x, verified by tribunal: lint, tests, review, and docs all passed."
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("single file singular", func(t *testing.T) {
		got, _ := Body(s, Input{TaskID: "x", TribunalName: "tribunal", Facts: Facts{Files: 1, Added: 1}, Steps: []string{"lint"}})
		for _, want := range []string{"1 file changed, +1 -0.", "1 review round, 0 fix rounds."} {
			if !strings.Contains(got, want) {
				t.Errorf("expected %q in:\n%s", want, got)
			}
		}
	})
}

func TestParseNumstat(t *testing.T) {
	files, added, deleted, areas := parseNumstat("10\t2\tinternal/cli/ship.go\n-\t-\tsite/public/logo.png\n3\t0\tREADME.md\n1\t1\tinternal/prbody/x.go\n")
	if files != 4 || added != 14 || deleted != 3 {
		t.Errorf("totals = %d files, +%d -%d", files, added, deleted)
	}
	if want := []string{"(repo root)", "internal/", "site/"}; !reflect.DeepEqual(areas, want) {
		t.Errorf("areas = %v, want %v", areas, want)
	}
	if files, _, _, areas := parseNumstat(""); files != 0 || areas != nil {
		t.Errorf("empty numstat = %d, %v", files, areas)
	}
}

func TestGather(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=T", "-c", "user.email=t@example.com"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main")
	write("base.txt", "base\n")
	run("add", "-A")
	run("commit", "-q", "-m", "base")
	run("checkout", "-q", "-b", "work")
	write("a/one.txt", "1\n2\n")
	run("add", "-A")
	run("commit", "-q", "-m", "feat: add a")
	write("top.txt", "x\n")
	run("add", "-A")
	run("commit", "-q", "-m", "fix: add top")

	f, err := Gather(dir, "main")
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if want := []string{"feat: add a", "fix: add top"}; !reflect.DeepEqual(f.Subjects, want) {
		t.Errorf("subjects = %v, want %v", f.Subjects, want)
	}
	if f.Files != 2 || f.Added != 3 || f.Deleted != 0 || !reflect.DeepEqual(f.Areas, []string{"(repo root)", "a/"}) {
		t.Errorf("unexpected facts: %+v", f)
	}

	if _, err := Gather(dir, "no-such-branch"); err == nil {
		t.Error("expected an error for an unknown base")
	}
}

func TestReviewerTitle(t *testing.T) {
	s := NewSanitizer("Secret mission statement that must never be published anywhere.")
	long := "feat: " + strings.Repeat("word ", 20)
	for name, tc := range map[string]struct {
		in   string
		want string
		ok   bool
	}{
		"valid":              {"feat: add a thing", "feat: add a thing", true},
		"trimmed":            {"  fix(cli): stop leaking  ", "fix(cli): stop leaking", true},
		"breaking":           {"feat!: drop the old flag", "feat!: drop the old flag", true},
		"other type":         {"refactor: split the module", "refactor: split the module", true},
		"cut to 72":          {long, "", true},
		"empty":              {"", "", false},
		"not conventional":   {"Add a thing", "", false},
		"no space after":     {"feat:add", "", false},
		"multi line":         {"feat: a\nsecond line", "", false},
		"far too long":       {"feat: " + strings.Repeat("x", 130), "", false},
		"home path":          {"fix: handle /Users/me/x", "", false},
		"localhost port":     {"fix: serve on localhost:3000", "", false},
		"secret":             {"fix: rotate ghp_abcdefghijklmnopqrstuvwxyz0123456789", "", false},
		"quotes the mission": {"feat: Secret mission statement that must never be published anywhere.", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := ReviewerTitle(s, tc.in)
			if ok != tc.ok {
				t.Fatalf("ReviewerTitle(%q) ok = %v, want %v (%q)", tc.in, ok, tc.ok, got)
			}
			if name == "cut to 72" {
				if len(got) > maxTitleLen || !strings.HasSuffix(got, "...") {
					t.Errorf("expected a cut title, got %q", got)
				}
				return
			}
			if ok && got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReviewerDescription(t *testing.T) {
	s := NewSanitizer("Secret mission statement that must never be published anywhere.")
	for name, tc := range map[string]struct {
		in   string
		want string
		ok   bool
	}{
		"valid":          {"Adds x.\nBecause y.\n", "Adds x.\nBecause y.", true},
		"bullets":        {"- one\n- two\n- three", "- one\n- two\n- three", true},
		"crlf":           {"a\r\nb", "a\nb", true},
		"empty":          {"  \n", "", false},
		"heading":        {"## What\nAdds x.", "", false},
		"too many lines": {strings.Repeat("line\n", 25), "", false},
		"too long":       {strings.Repeat("x", 2100), "", false},
		"home path line": {"Adds x.\nSee /Users/me/x for more.", "", false},
		"port line":      {"Adds x.\nTested on localhost:8080.", "", false},
		"quotes mission": {"Adds x.\nSecret mission statement that must never be published anywhere.", "", false},
		"secret line":    {"Adds x.\nAPI_KEY=abcd1234efgh5678", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := ReviewerDescription(s, tc.in)
			if ok != tc.ok || (ok && got != tc.want) {
				t.Errorf("ReviewerDescription(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}
