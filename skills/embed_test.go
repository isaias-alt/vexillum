package skills

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestNames(t *testing.T) {
	want := []string{"forum", "muster", "vexillum"}
	if got := Names(); !reflect.DeepEqual(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
}

var nameLine = regexp.MustCompile(`(?m)^name: (.+)$`)

func TestEverySkillIsValid(t *testing.T) {
	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			skill, err := ReadFile(name, "SKILL.md")
			if err != nil {
				t.Fatal(err)
			}
			text := string(skill)
			if !strings.HasPrefix(text, "---\n") {
				t.Fatal("SKILL.md has no frontmatter")
			}
			end := strings.Index(text[4:], "\n---\n")
			if end < 0 {
				t.Fatal("frontmatter is not closed")
			}
			front := text[4 : 4+end]
			m := nameLine.FindStringSubmatch(front)
			if m == nil || m[1] != name {
				t.Errorf("frontmatter name = %v, want %q (the directory name)", m, name)
			}
			if !strings.Contains(front, "\ndescription: ") {
				t.Error("frontmatter has no description")
			}
			if strings.Contains(text, "\u2014") {
				t.Error("SKILL.md contains an em dash")
			}
		})
	}
}

// TestEmbeddedCopiesDoNotDrift compares the embedded files against the
// working tree. The skills are embedded directly from this directory so
// there is nothing to keep in sync by hand, but a skill added on disk and
// left out of the //go:embed line (or a file the embed rules skip, such as
// one starting with "." or "_") would silently ship incomplete: this fails
// then.
func TestEmbeddedCopiesDoNotDrift(t *testing.T) {
	onDisk := map[string][]byte{}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		root := e.Name()
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			onDisk[filepath.ToSlash(p)] = data
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	embedded := map[string][]byte{}
	for _, name := range Names() {
		files, err := Files(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			embedded[name+"/"+f.Path] = f.Content
		}
	}

	for p, data := range onDisk {
		got, ok := embedded[p]
		if !ok {
			t.Errorf("%s exists on disk but is not embedded (add its skill to the //go:embed line, and avoid names starting with . or _)", p)
			continue
		}
		if string(got) != string(data) {
			t.Errorf("%s differs between disk and the embedded copy", p)
		}
	}
	for p := range embedded {
		if _, ok := onDisk[p]; !ok {
			t.Errorf("%s is embedded but missing on disk", p)
		}
	}
}

func TestFilesIncludesPlaybooks(t *testing.T) {
	files, err := Files("forum")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	if len(paths) < 2 || paths[0] != "SKILL.md" {
		t.Fatalf("paths = %v", paths)
	}
	found := false
	for _, p := range paths {
		if p == "playbooks/plan.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("playbooks/plan.md not in %v", paths)
	}
}

func TestUnknownSkill(t *testing.T) {
	if _, err := Files("nope"); err == nil {
		t.Error("Files: expected an error")
	}
	if _, err := ReadFile("nope", "SKILL.md"); err == nil {
		t.Error("ReadFile: expected an error")
	}
	if _, err := ReadFile("forum", "missing.md"); err == nil {
		t.Error("ReadFile: expected an error for a missing file")
	}
	if _, err := Hash("nope"); err == nil {
		t.Error("Hash: expected an error")
	}
}

func TestHash(t *testing.T) {
	seen := map[string]string{}
	for _, name := range Names() {
		h, err := Hash(name)
		if err != nil {
			t.Fatal(err)
		}
		if len(h) != 64 {
			t.Errorf("%s: hash %q is not a sha256 hex digest", name, h)
		}
		if other, dup := seen[h]; dup {
			t.Errorf("%s and %s share a hash", name, other)
		}
		seen[h] = name
		again, _ := Hash(name)
		if again != h {
			t.Errorf("%s: hash is not stable", name)
		}
	}

	files, _ := Files("forum")
	base := HashFiles(files)
	if got, _ := Hash("forum"); got != base {
		t.Error("HashFiles(Files) != Hash")
	}

	// Order-independent, content-sensitive, path-sensitive, add/remove-sensitive.
	reversed := append([]File(nil), files...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	if HashFiles(reversed) != base {
		t.Error("hash depends on file order")
	}
	edited := append([]File(nil), files...)
	edited[0] = File{Path: edited[0].Path, Content: append(append([]byte(nil), edited[0].Content...), 'x')}
	if HashFiles(edited) == base {
		t.Error("hash ignores a content edit")
	}
	renamed := append([]File(nil), files...)
	renamed[0] = File{Path: "OTHER.md", Content: renamed[0].Content}
	if HashFiles(renamed) == base {
		t.Error("hash ignores a rename")
	}
	if HashFiles(files[1:]) == base {
		t.Error("hash ignores a removed file")
	}
}

func TestFilesReturnsCopies(t *testing.T) {
	a, _ := ReadFile("vexillum", "SKILL.md")
	a[0] = 'X'
	b, _ := ReadFile("vexillum", "SKILL.md")
	if b[0] == 'X' {
		t.Error("ReadFile exposes the embedded bytes")
	}
}

// TestVexillumSkillContract pins what the always-on core relies on: the skill
// sends the commander to the models table and links the files it splits the
// operational detail into.
func TestVexillumSkillContract(t *testing.T) {
	skill, err := ReadFile("vexillum", "SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(skill)
	for _, want := range []string{"`vx models`", "--profile", "vx dispatch", "vx land", "vx ship", "vx strike", "sentinel"} {
		if !strings.Contains(text, want) {
			t.Errorf("SKILL.md does not mention %q", want)
		}
	}
	for _, linked := range []string{"sentinel.md", "landing.md", "shipping.md"} {
		if !strings.Contains(text, "]("+linked+")") {
			t.Errorf("SKILL.md does not link %s", linked)
		}
		if _, err := ReadFile("vexillum", linked); err != nil {
			t.Error(err)
		}
	}
}
