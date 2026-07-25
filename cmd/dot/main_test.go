package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestProcessVersionSmoke(t *testing.T) {
	command := exec.Command("go", "run", ".", "version")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . version error = %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "version=") {
		t.Fatalf("go run . version output = %q, want version field", output)
	}
}
