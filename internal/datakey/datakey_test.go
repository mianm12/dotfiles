package datakey

import "testing"

func TestValid(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{name: "single lowercase letter", key: "a", want: true},
		{name: "mixed suffix", key: "email_Address2", want: true},
		{name: "empty", key: "", want: false},
		{name: "uppercase prefix", key: "Email", want: false},
		{name: "digit prefix", key: "2fa", want: false},
		{name: "underscore prefix", key: "_email", want: false},
		{name: "hyphen", key: "git-email", want: false},
		{name: "dot", key: "git.email", want: false},
		{name: "non-ASCII prefix", key: "émail", want: false},
		{name: "non-ASCII suffix", key: "emailé", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Valid(tt.key); got != tt.want {
				t.Errorf("Valid(%q) = %t, want %t", tt.key, got, tt.want)
			}
		})
	}
}
