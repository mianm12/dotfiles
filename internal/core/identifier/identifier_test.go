package identifier_test

import (
	"testing"

	"github.com/mianm12/dotfiles/internal/core/identifier"
)

func TestValid(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "empty", value: "", want: false},
		{name: "lowercase first", value: "app", want: true},
		{name: "digit first", value: "2fa", want: true},
		{name: "underscore after first", value: "nvim_2", want: true},
		{name: "hyphen after first", value: "shell-tools", want: true},
		{name: "repeated separators", value: "a__--", want: true},
		{name: "underscore first", value: "_app", want: false},
		{name: "hyphen first", value: "-app", want: false},
		{name: "uppercase", value: "App", want: false},
		{name: "non ASCII", value: "app-配置", want: false},
		{name: "invalid UTF-8", value: "app-" + string([]byte{0xff}), want: false},
		{name: "dot", value: "app.local", want: false},
		{name: "slash", value: "app/local", want: false},
		{name: "NUL", value: "app\x00local", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := identifier.Valid(test.value); got != test.want {
				t.Fatalf("Valid(%q) = %t, want %t", test.value, got, test.want)
			}
		})
	}
}
