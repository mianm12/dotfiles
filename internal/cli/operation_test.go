package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/mianm12/dotfiles/internal/core/executor"
	"github.com/spf13/cobra"
)

func TestFinishMutationSuppressesSuccessWhenSessionCloseFails(t *testing.T) {
	convergeErr := errors.New("synthetic convergence failure")
	closeErr := errors.New("synthetic close failure")
	runErr := joinMutationSessionErrors(convergeErr, closeErr, "dot apply")
	var stdout bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&stdout)

	err := finishMutation(
		command,
		mutationOutcome{selectionChanged: true},
		runErr,
		"dot apply",
	)

	if !errors.Is(err, convergeErr) || !errors.Is(err, closeErr) {
		t.Fatalf("finishMutation() error = %v, want both convergence and close failures", err)
	}
	if !strings.Contains(err.Error(), "release mutation lock") ||
		!strings.Contains(err.Error(), "rerun dot apply") {
		t.Fatalf("finishMutation() error = %q, want close recovery guidance", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("finishMutation() stdout = %q, want no success result", stdout.String())
	}
}

func TestRunMutationSessionClosesDuringPanic(t *testing.T) {
	fixture := newCLITestEnv(t, `base = []`)
	context, err := resolveContext(fixture.env)
	if err != nil {
		t.Fatalf("resolveContext() error = %v", err)
	}
	func() {
		defer func() {
			if recovered := recover(); recovered != "synthetic panic" {
				t.Fatalf("recovered panic = %#v, want synthetic panic", recovered)
			}
		}()
		_, _ = runMutationSession(
			context,
			fixture.repository,
			"dot apply",
			func(*executor.Session, *mutationOutcome) error {
				panic("synthetic panic")
			},
		)
	}()

	_, err = runMutationSession(
		context,
		fixture.repository,
		"dot apply",
		func(*executor.Session, *mutationOutcome) error { return nil },
	)
	if err != nil {
		t.Fatalf("runMutationSession(after panic) error = %v", err)
	}
}
