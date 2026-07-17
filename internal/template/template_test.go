package template

import (
	"strings"
	"testing"
)

func TestCompile_AllowsM1FunctionsNativeActionsAndVariables(t *testing.T) {
	source := []byte(`
{{ define "choice" }}{{ if and (eq .OS "darwin") (not (ne .Arch "arm64")) }}{{ default "fallback" .email }}{{ else }}other{{ end }}{{ end }}
{{ template "choice" . }}
{{ range .items }}{{ with . }}{{ . }}{{ end }}{{ else }}empty{{ end }}
{{ if or true false }}allowed{{ end }}
`)

	if _, err := Compile("allowed", source, []string{"email", "items"}); err != nil {
		t.Fatalf("Compile() error = %v, want nil", err)
	}
}

func TestCompile_RejectsFunctionsOutsideWhitelist(t *testing.T) {
	functions := []string{
		"printf", "print", "println",
		"len", "index", "slice", "call",
		"html", "js", "urlquery",
		"ge", "gt", "le", "lt",
		"env",
	}
	for _, function := range functions {
		t.Run(function, func(t *testing.T) {
			source := []byte("{{ " + function + " .value }}")
			parsed, err := Compile("rejected", source, []string{"value"})
			if err == nil {
				t.Fatalf("Compile() = %#v, nil; want function rejection", parsed)
			}
		})
	}
}

func TestCompile_RejectsDisallowedFunctionInsideChain(t *testing.T) {
	parsed, err := Compile("chain", []byte(`{{ (printf "%s" "value").Missing }}`), nil)
	if err == nil || !strings.Contains(err.Error(), `function "printf" is not allowed`) {
		t.Fatalf("Compile() = %#v, %v; want nested printf rejection", parsed, err)
	}
}

func TestCompile_RejectsDisallowedFunctionInNamedTemplate(t *testing.T) {
	parsed, err := Compile("root", []byte(`{{ define "hidden" }}{{ len .value }}{{ end }}`), []string{"value"})
	if err == nil || !strings.Contains(err.Error(), `function "len" is not allowed`) {
		t.Fatalf("Compile() = %#v, %v; want len rejection", parsed, err)
	}
}

func TestCompile_RejectsSyntaxError(t *testing.T) {
	parsed, err := Compile("broken", []byte(`{{ if }}`), nil)
	if err == nil || !strings.Contains(err.Error(), `parse template "broken"`) {
		t.Fatalf("Compile() = %#v, %v; want parse error", parsed, err)
	}
}

func TestCompile_RejectsInvalidReferences(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		declared []string
		want     string
	}{
		{name: "undeclared user data", source: `{{ .email }}`, want: `user variable ".email" is not declared`},
		{name: "unknown built in", source: `{{ .CPU }}`, want: `unknown built-in variable ".CPU"`},
		{name: "invalid user namespace", source: `{{ ._email }}`, want: `invalid user variable "._email"`},
		{name: "chained field", source: `{{ .email.domain }}`, declared: []string{"email"}, want: "must name one root value"},
		{name: "local root alias", source: `{{ $root := . }}{{ $root.email }}`, declared: []string{"email"}, want: "must name one root value"},
		{name: "root variable unknown", source: `{{ $.token }}`, want: `user variable ".token" is not declared`},
		{name: "named template", source: `{{ define "nested" }}{{ .token }}{{ end }}`, want: `user variable ".token" is not declared`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := Compile("invalid", []byte(tt.source), tt.declared)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Compile() = %#v, %v; want error containing %q", parsed, err, tt.want)
			}
		})
	}
}

func TestCompile_RejectsInvalidDeclaredKey(t *testing.T) {
	parsed, err := Compile("valid", []byte("literal"), []string{"Upper"})
	if err == nil {
		t.Fatalf("Compile() = %#v, nil; want invalid declared key error", parsed)
	}
}
