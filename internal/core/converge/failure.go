package converge

import (
	"errors"
	"fmt"
	"strconv"
)

// Failure is the minimal runtime failure shared with the CLI boundary.
type Failure struct {
	MayHaveChanged bool
	Line           *Line
	Cause          error
}

func (failure *Failure) Error() string {
	if failure == nil {
		return "<nil>"
	}
	message := fmt.Sprintf("failure may_have_changed=%t", failure.MayHaveChanged)
	if failure.Line != nil {
		if failure.Line.Op == OpChmod {
			message += fmt.Sprintf(
				" line=%s control=%s path=%s mode=%s",
				failure.Line.Op,
				failure.Line.Control,
				strconv.Quote(failure.Line.Path),
				failure.Line.Mode,
			)
		} else {
			message += fmt.Sprintf(
				" line=%s module=%s placement=%s target=%s",
				failure.Line.Op,
				failure.Line.ModuleID,
				failure.Line.PlacementID,
				strconv.Quote(failure.Line.Target),
			)
		}
	}
	if failure.Cause != nil {
		message += " reason=" + strconv.Quote(failure.Cause.Error())
	}
	return message
}

func (failure *Failure) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Cause
}

func newFailure(mayHaveChanged bool, line *Line, cause error) error {
	if cause == nil {
		return nil
	}
	var existing *Failure
	if errors.As(cause, &existing) {
		if !mayHaveChanged || existing.MayHaveChanged {
			return cause
		}
		copy := *existing
		copy.MayHaveChanged = true
		return &copy
	}
	var owned *Line
	if line != nil {
		copy := *line
		owned = &copy
	}
	return &Failure{
		MayHaveChanged: mayHaveChanged,
		Line:           owned,
		Cause:          cause,
	}
}

// FailureMayHaveChanged reports whether a failed command may have persisted work.
func FailureMayHaveChanged(err error) bool {
	var failure *Failure
	return errors.As(err, &failure) && failure.MayHaveChanged
}
