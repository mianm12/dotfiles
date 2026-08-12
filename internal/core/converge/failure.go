package converge

import (
	"errors"
	"fmt"
	"strconv"
)

// FailureStage identifies the operation boundary at which a runtime failure
// occurred. A complete blocked Plan is an ApplyResult, not a Failure.
type FailureStage string

// Failure stages form the fixed runtime failure vocabulary.
const (
	FailureStageInput           FailureStage = "input"
	FailureStageLock            FailureStage = "lock"
	FailureStageAnalysis        FailureStage = "analysis"
	FailureStageExecute         FailureStage = "execute"
	FailureStageStateCommit     FailureStage = "state-commit"
	FailureStageSelectionCommit FailureStage = "selection-commit"
	FailureStageLockRelease     FailureStage = "lock-release"
)

// Failure is the single runtime failure representation shared by core and
// CLI. Action is present only when an execution failure belongs to one
// semantic Plan action.
type Failure struct {
	Stage    FailureStage
	Partial  bool
	Recovery Recovery
	Action   *Action
	Cause    error
}

func (failure *Failure) Error() string {
	if failure == nil {
		return "<nil>"
	}
	line := fmt.Sprintf(
		"failure stage=%s partial=%t recovery=%s",
		failure.Stage,
		failure.Partial,
		failure.Recovery,
	)
	if failure.Action != nil {
		line += fmt.Sprintf(
			" action=%s module=%s placement=%s target=%s",
			failure.Action.Decision,
			failure.Action.ModuleID,
			failure.Action.PlacementID,
			strconv.Quote(failure.Action.Target),
		)
	}
	if failure.Cause != nil {
		line += " reason=" + strconv.Quote(failure.Cause.Error())
	}
	return line
}

func (failure *Failure) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Cause
}

func newFailure(
	stage FailureStage,
	partial bool,
	recovery Recovery,
	action *Action,
	cause error,
) error {
	if cause == nil {
		return nil
	}
	var existing *Failure
	if errors.As(cause, &existing) {
		return cause
	}
	var ownedAction *Action
	if action != nil {
		copy := *action
		ownedAction = &copy
	}
	return &Failure{
		Stage:    stage,
		Partial:  partial,
		Recovery: recovery,
		Action:   ownedAction,
		Cause:    cause,
	}
}

// RecoveryForFailure returns the typed recovery attached to a runtime failure.
// Non-Failure errors have no recovery contract.
func RecoveryForFailure(err error) Recovery {
	var failure *Failure
	if errors.As(err, &failure) {
		return failure.Recovery
	}
	return RecoveryNone
}
