package herdr

import "testing"

func TestParseVersion(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "happy path", in: "herdr 0.9.0", want: "0.9.0"},
		{name: "trailing newline", in: "herdr 0.9.0\n", want: "0.9.0"},
		{name: "surrounding whitespace", in: "  herdr 0.10.2  \n", want: "0.10.2"},
		{name: "no space", in: "herdr", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseVersion(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseVersion(%q) = %q, nil; want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseVersion(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("parseVersion(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsSupportedVersion(t *testing.T) {
	tests := []struct {
		v    string
		want bool
	}{
		{"0.9.0", true},
		{"0.9.5", true},
		{"0.10.0", false},
		{"1.0.0", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsSupportedVersion(tt.v); got != tt.want {
			t.Errorf("IsSupportedVersion(%q) = %v, want %v", tt.v, got, tt.want)
		}
	}
}
