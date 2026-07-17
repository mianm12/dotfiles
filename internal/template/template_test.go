package template

import (
	"strings"
	"testing"
)

func TestParse_AllowsM1FunctionsAndNativeActions(t *testing.T) {
	source := []byte(`
{{ define "choice" }}{{ if and (eq .OS "darwin") (not (ne .Arch "arm64")) }}{{ default "fallback" .email }}{{ else }}other{{ end }}{{ end }}
{{ template "choice" . }}
{{ range .items }}{{ with . }}{{ . }}{{ end }}{{ else }}empty{{ end }}
{{ if or true false }}allowed{{ end }}
`)

	if _, err := Parse("allowed", source); err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
}

func TestParse_RejectsFunctionsOutsideWhitelist(t *testing.T) {
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
			parsed, err := Parse("rejected", source)
			if err == nil {
				t.Fatalf("Parse() = %#v, nil; want function rejection", parsed)
			}
		})
	}
}

func TestParse_RejectsDisallowedFunctionInsideChain(t *testing.T) {
	parsed, err := Parse("chain", []byte(`{{ (printf "%s" "value").Missing }}`))
	if err == nil || !strings.Contains(err.Error(), `function "printf" is not allowed`) {
		t.Fatalf("Parse() = %#v, %v; want nested printf rejection", parsed, err)
	}
}

func TestParse_RejectsDisallowedFunctionInNamedTemplate(t *testing.T) {
	parsed, err := Parse("root", []byte(`{{ define "hidden" }}{{ len .value }}{{ end }}`))
	if err == nil || !strings.Contains(err.Error(), `function "len" is not allowed`) {
		t.Fatalf("Parse() = %#v, %v; want len rejection", parsed, err)
	}
}

func TestParse_RejectsSyntaxError(t *testing.T) {
	parsed, err := Parse("broken", []byte(`{{ if }}`))
	if err == nil || !strings.Contains(err.Error(), `parse template "broken"`) {
		t.Fatalf("Parse() = %#v, %v; want parse error", parsed, err)
	}
}

func TestTemplateValidateVariables_AllowsBuiltInsDeclaredDataAndLocals(t *testing.T) {
	parsed, err := Parse("valid", []byte(`
{{ .OS }} {{ .Arch }} {{ .Hostname }} {{ .Profile }} {{ $.Home }}
{{ $email := .email }}{{ $email }}
`))
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if err := parsed.ValidateVariables([]string{"email"}); err != nil {
		t.Fatalf("ValidateVariables() error = %v, want nil", err)
	}
}

func TestTemplateValidateVariables_RejectsInvalidReferences(t *testing.T) {
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
			parsed, err := Parse("invalid", []byte(tt.source))
			if err != nil {
				t.Fatalf("Parse() error = %v, want nil", err)
			}
			err = parsed.ValidateVariables(tt.declared)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateVariables() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestTemplateValidateVariables_RejectsInvalidDeclaredKey(t *testing.T) {
	parsed, err := Parse("valid", []byte("literal"))
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	if err := parsed.ValidateVariables([]string{"Upper"}); err == nil {
		t.Fatal("ValidateVariables() error = nil, want invalid declared key error")
	}
}
