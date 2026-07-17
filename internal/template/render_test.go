package template

import (
	"bytes"
	"strings"
	"testing"
)

func TestTemplateRender_UsesOnlyExplicitContextAndPreservesBytes(t *testing.T) {
	parsed, err := Parse("scaffold", []byte(
		"os={{ .OS }}\narch={{ .Arch }}\nhost={{ .Hostname }}\n"+
			"profile={{ .Profile }}\nhome={{ .Home }}\nemail={{ default \"unset\" .email }}\x00\n",
	))
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	context := Context{
		OS:       "linux",
		Arch:     "arm64",
		Hostname: "workstation.example",
		Profile:  "base",
		Home:     "/tmp/isolated-home",
		Data: map[string]string{
			"email": "",
			"stale": "must not be visible",
		},
	}
	want := []byte("os=linux\narch=arm64\nhost=workstation.example\n" +
		"profile=base\nhome=/tmp/isolated-home\nemail=unset\x00\n")

	first, err := parsed.Render([]string{"email"}, context)
	if err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}
	second, err := parsed.Render([]string{"email"}, context)
	if err != nil {
		t.Fatalf("Render() second error = %v, want nil", err)
	}
	if !bytes.Equal(first, want) {
		t.Fatalf("Render() = %q, want exact bytes %q", first, want)
	}
	if !bytes.Equal(second, first) {
		t.Fatalf("repeated Render() = %q, want %q", second, first)
	}
	first[0] = 'X'
	third, err := parsed.Render([]string{"email"}, context)
	if err != nil {
		t.Fatalf("Render() third error = %v, want nil", err)
	}
	if !bytes.Equal(third, second) {
		t.Fatalf("mutating result changed later Render(): got %q, want %q", third, second)
	}
}

func TestTemplateRender_FiltersUndeclaredMachineData(t *testing.T) {
	parsed, err := Parse("root", []byte(`{{ range $key, $value := . }}{{ $key }}={{ $value }};{{ end }}`))
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	context := Context{
		OS:       "darwin",
		Arch:     "amd64",
		Hostname: "host",
		Profile:  "base",
		Home:     "/home/test",
		Data:     map[string]string{"email": "me@example.com", "stale": "secret"},
	}

	got, err := parsed.Render([]string{"email"}, context)
	if err != nil {
		t.Fatalf("Render() error = %v, want nil", err)
	}
	want := "Arch=amd64;Home=/home/test;Hostname=host;OS=darwin;Profile=base;email=me@example.com;"
	if string(got) != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
}

func TestTemplateRender_DoesNotUseEnvironmentOrImplicitFallback(t *testing.T) {
	t.Setenv("EMAIL", "environment@example.com")
	parsed, err := Parse("missing", []byte(`{{ .email }}`))
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rendered, err := parsed.Render([]string{"email"}, Context{Data: map[string]string{}})
	if err == nil || !strings.Contains(err.Error(), `data key "email" is missing`) {
		t.Fatalf("Render() = %q, %v; want missing explicit data error", rendered, err)
	}
}

func TestTemplateRender_ReportsExecutionErrorWithoutContent(t *testing.T) {
	parsed, err := Parse("invalid-call", []byte(`prefix{{ default "fallback" 1 }}suffix`))
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}

	rendered, err := parsed.Render(nil, Context{})
	if err == nil || !strings.Contains(err.Error(), `render template "invalid-call"`) {
		t.Fatalf("Render() = %q, %v; want execution error", rendered, err)
	}
	if rendered != nil {
		t.Fatalf("Render() content = %q, want nil on error", rendered)
	}
}
