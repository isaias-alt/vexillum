package slot

import (
	"strings"
	"testing"
)

func TestUnifiedDiff(t *testing.T) {
	tests := []struct {
		name, a, b, want string
	}{
		{"equal", "a\nb", "a\nb", ""},
		{"equal modulo CRLF", "a\r\nb\r\n", "a\nb", ""},
		{"change", "a\nb\nc", "a\nB\nc", "--- A\n+++ B\n@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n"},
		{"add line", "a", "a\nb", "--- A\n+++ B\n@@ -1,1 +1,2 @@\n a\n+b\n"},
		{"from empty", "", "a", "--- A\n+++ B\n@@ -0,0 +1,1 @@\n+a\n"},
		{"to empty", "a", "", "--- A\n+++ B\n@@ -1,1 +0,0 @@\n-a\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := UnifiedDiff(tc.a, tc.b, "A", "B"); got != tc.want {
				t.Errorf("got\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

func TestUnifiedDiffHunks(t *testing.T) {
	var a, b []string
	for i := 1; i <= 30; i++ {
		l := "line" + string(rune('a'+i%26)) + string(rune('0'+i%10))
		a = append(a, l)
		b = append(b, l)
	}
	b[2] = "CHANGED-EARLY"
	b[27] = "CHANGED-LATE"
	got := UnifiedDiff(strings.Join(a, "\n"), strings.Join(b, "\n"), "A", "B")
	if n := strings.Count(got, "@@ -"); n != 2 {
		t.Fatalf("want 2 separate hunks, got %d:\n%s", n, got)
	}
	if !strings.Contains(got, "-"+a[2]) || !strings.Contains(got, "+CHANGED-LATE") {
		t.Errorf("missing changes:\n%s", got)
	}
}
