package banner

import (
	"strings"
	"testing"
)

func TestGeneratePasswordFormat(t *testing.T) {
	pw, err := GeneratePassword()
	if err != nil {
		t.Fatalf("GeneratePassword: %v", err)
	}
	groups := strings.Split(pw, "-")
	if len(groups) != 3 {
		t.Fatalf("expected 3 dash-separated groups, got %d (%q)", len(groups), pw)
	}
	for _, g := range groups {
		if len(g) != 4 {
			t.Errorf("expected each group to have 4 characters, got %d (%q)", len(g), g)
		}
		for _, r := range g {
			if !strings.ContainsRune(passwordAlphabet, r) {
				t.Errorf("character %q is outside the unambiguous alphabet %q", r, passwordAlphabet)
			}
		}
	}
}

func TestGeneratePasswordAlphabetExcludesAmbiguousCharacters(t *testing.T) {
	for _, r := range "0o1li" {
		if strings.ContainsRune(passwordAlphabet, r) {
			t.Errorf("alphabet should not contain ambiguous character %q", r)
		}
	}
}

func TestGeneratePasswordIsRandom(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		pw, err := GeneratePassword()
		if err != nil {
			t.Fatalf("GeneratePassword: %v", err)
		}
		if seen[pw] {
			t.Fatalf("got a repeated password across 20 draws: %q", pw)
		}
		seen[pw] = true
	}
}
