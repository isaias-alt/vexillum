package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrompterConfirm(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		def     bool
		want    bool
		wantErr bool
		wantOut string
	}{
		{"empty takes yes default", "\n", true, true, false, "[Y/n]"},
		{"empty takes no default", "\n", false, false, false, "[y/N]"},
		{"y", "y\n", false, true, false, ""},
		{"YES", "YES\n", false, true, false, ""},
		{"n", "n\n", true, false, false, ""},
		{"retry after garbage", "maybe\ny\n", false, true, false, "Please answer y or n."},
		{"gives up after three", "a\nb\nc\n", true, false, true, ""},
		{"closed input", "", true, false, true, ""},
		{"last line without newline", "n", true, false, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			p := newPrompter(strings.NewReader(tc.in), &out)
			got, err := p.confirm("Go?", tc.def)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Errorf("got (%v, %v), want (%v, err=%v)", got, err, tc.want, tc.wantErr)
			}
			if !strings.Contains(out.String(), tc.wantOut) || !strings.HasPrefix(out.String(), "Go? ") {
				t.Errorf("output = %q", out.String())
			}
		})
	}
}
