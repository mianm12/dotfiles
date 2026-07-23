package planner

import "testing"

func TestFileActionClone_DoesNotShareDesiredBytes(t *testing.T) {
	action := FileAction{
		Verb: FileScaffold,
		Desired: Desired{
			Kind:    DesiredScaffold,
			Content: []byte("desired"),
		},
		Precondition: Precondition{Leaf: LeafCondition{Kind: LeafPresent}},
		OnSuccess:    StateEffect{Kind: StateUpsert},
		OnFailure:    StateEffect{Kind: StatePreserve},
	}
	cloned := action.Clone()
	cloned.Desired.Content[0] = 'D'
	cloned.Precondition.Leaf.Kind = LeafMissing

	if string(action.Desired.Content) != "desired" || action.Precondition.Leaf.Kind != LeafPresent {
		t.Fatalf("mutating clone changed source action: %#v", action)
	}
}

func TestFileVerbExecutionClass(t *testing.T) {
	tests := []struct {
		verb FileVerb
		want FileExecutionClass
	}{
		{verb: FileSkip, want: FilePlanOnly},
		{verb: FileConflict, want: FilePlanOnly},
		{verb: FileAdopt, want: FileStateOnly},
		{verb: FileCreateLink, want: FileTargetMutation},
		{verb: FileScaffold, want: FileTargetMutation},
		{verb: FileVerb("future"), want: ""},
	}
	for _, test := range tests {
		if got := test.verb.ExecutionClass(); got != test.want {
			t.Errorf("FileVerb(%q).ExecutionClass() = %q, want %q", test.verb, got, test.want)
		}
	}
}

func TestLeafConditionMatchesOnlyRequiredEvidence(t *testing.T) {
	regular := Observation{Kind: ObjectRegular, Mode: 0o640, Hash: "sha256:regular"}
	tests := []struct {
		name      string
		condition LeafCondition
		observed  Observation
		want      bool
	}{
		{name: "any", condition: LeafCondition{Kind: LeafAny}, observed: regular, want: true},
		{name: "missing", condition: LeafCondition{Kind: LeafMissing}, observed: Observation{Kind: ObjectMissing}, want: true},
		{name: "present regular", condition: LeafCondition{Kind: LeafPresent}, observed: regular, want: true},
		{name: "present rejects missing", condition: LeafCondition{Kind: LeafPresent}, observed: Observation{Kind: ObjectMissing}},
		{
			name:      "exact symlink",
			condition: LeafCondition{Kind: LeafExactSymlink, LinkDest: "raw"},
			observed:  Observation{Kind: ObjectSymlink, LinkDest: "raw"},
			want:      true,
		},
		{
			name:      "exact symlink rejects retarget",
			condition: LeafCondition{Kind: LeafExactSymlink, LinkDest: "raw"},
			observed:  Observation{Kind: ObjectSymlink, LinkDest: "changed"},
		},
		{
			name:      "not owned accepts other kind",
			condition: LeafCondition{Kind: LeafNotOwnedSymlink, LinkDest: "owned"},
			observed:  regular,
			want:      true,
		},
		{
			name:      "not owned rejects restored link",
			condition: LeafCondition{Kind: LeafNotOwnedSymlink, LinkDest: "owned"},
			observed:  Observation{Kind: ObjectSymlink, LinkDest: "owned"},
		},
		{
			name:      "invalid condition",
			condition: LeafCondition{Kind: LeafPresent, LinkDest: "unexpected"},
			observed:  regular,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.condition.Matches(test.observed); got != test.want {
				t.Fatalf("Matches(%#v) = %t, want %t for %#v", test.observed, got, test.want, test.condition)
			}
		})
	}
}
