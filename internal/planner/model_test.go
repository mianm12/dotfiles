package planner

import "testing"

func TestFileActionClone_DoesNotSharePlanBytes(t *testing.T) {
	action := FileAction{
		Verb: FileScaffold,
		Desired: Desired{
			Kind:    DesiredScaffold,
			Content: []byte("desired"),
		},
		Precondition: Precondition{
			Observed: Observation{Kind: ObjectRegular, Content: []byte("observed")},
		},
		OnSuccess: StateEffect{Kind: StateUpsert},
		OnFailure: StateEffect{Kind: StatePreserve},
	}
	cloned := action.Clone()
	cloned.Desired.Content[0] = 'D'
	cloned.Precondition.Observed.Content[0] = 'O'

	if string(action.Desired.Content) != "desired" || string(action.Precondition.Observed.Content) != "observed" {
		t.Fatalf("mutating clone changed source action: %#v", action)
	}
}
