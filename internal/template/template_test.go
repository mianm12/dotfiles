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
	functions := []string{"printf", "len", "index", "call", "html", "js", "urlquery", "env"}
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
