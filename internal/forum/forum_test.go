package forum_test

import (
	"testing"

	"github.com/isaias-alt/vexillum/internal/forum"
)

func TestSessionKey_StableAndShaped(t *testing.T) {
	key := forum.SessionKey("/Users/general/artifacts/plan.html")
	if !forum.ValidSessionKey(key) {
		t.Fatalf("SessionKey produced %q, which ValidSessionKey rejects", key)
	}
	again := forum.SessionKey("/Users/general/artifacts/plan.html")
	if key != again {
		t.Errorf("SessionKey not stable: %q != %q", key, again)
	}
}

func TestSessionKey_DifferentFilesDifferentKeys(t *testing.T) {
	a := forum.SessionKey("/Users/general/artifacts/plan.html")
	b := forum.SessionKey("/Users/general/artifacts/other.html")
	if a == b {
		t.Errorf("two different files produced the same session key %q", a)
	}
}

func TestSessionKey_CleansPath(t *testing.T) {
	a := forum.SessionKey("/Users/general/artifacts/plan.html")
	b := forum.SessionKey("/Users/general/artifacts/./plan.html")
	if a != b {
		t.Errorf("SessionKey should normalize equivalent paths: %q != %q", a, b)
	}
}

func TestValidSessionKey(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"0123456789abcdef", true},
		{"0123456789ABCDEF", false}, // uppercase hex not produced by SessionKey
		{"", false},
		{"short", false},
		{"0123456789abcdef00", false}, // too long
		{"../../../etc/passwd", false},
	}
	for _, c := range cases {
		if got := forum.ValidSessionKey(c.key); got != c.want {
			t.Errorf("ValidSessionKey(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}

func TestValidDiagramIndex(t *testing.T) {
	cases := []struct {
		index int
		want  bool
	}{
		{0, true},
		{999, true},
		{-1, false},
		{1000, false},
	}
	for _, c := range cases {
		if got := forum.ValidDiagramIndex(c.index); got != c.want {
			t.Errorf("ValidDiagramIndex(%d) = %v, want %v", c.index, got, c.want)
		}
	}
}
