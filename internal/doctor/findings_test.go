package doctor

import (
	"reflect"
	"testing"
)

func TestResult_ExitCode(t *testing.T) {
	tests := []struct {
		name     string
		findings []Finding
		want     int
	}{
		{name: "clean", want: 0},
		{
			name: "error",
			findings: []Finding{
				{Severity: SeverityError, Check: "manifest", Message: "broken"},
			},
			want: 1,
		},
		{
			name: "warning only",
			findings: []Finding{
				{Severity: SeverityWarning, Check: "future", Message: "attention"},
			},
			want: 2,
		},
		{
			name: "error wins over warning",
			findings: []Finding{
				{Severity: SeverityWarning, Check: "future", Message: "attention"},
				{Severity: SeverityError, Check: "manifest", Message: "broken"},
			},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := newResult(tt.findings, nil).ExitCode(); got != tt.want {
				t.Fatalf("Result.ExitCode() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestResult_StableOrderAndCopies(t *testing.T) {
	result := newResult([]Finding{
		{Severity: SeverityWarning, Check: "z", Message: "warning"},
		{Severity: SeverityError, Check: "b", Message: "second"},
		{Severity: SeverityError, Check: "a", Message: "last"},
		{Severity: SeverityError, Check: "a", Message: "first"},
	}, []string{"z notice", "a notice"})

	wantFindings := []Finding{
		{Severity: SeverityError, Check: "a", Message: "first"},
		{Severity: SeverityError, Check: "a", Message: "last"},
		{Severity: SeverityError, Check: "b", Message: "second"},
		{Severity: SeverityWarning, Check: "z", Message: "warning"},
	}
	if got := result.Findings(); !reflect.DeepEqual(got, wantFindings) {
		t.Fatalf("Result.Findings() = %#v, want %#v", got, wantFindings)
	}
	if got := result.Notices(); !reflect.DeepEqual(got, []string{"a notice", "z notice"}) {
		t.Fatalf("Result.Notices() = %#v, want stable notices", got)
	}

	result.Findings()[0].Message = "changed"
	result.Notices()[0] = "changed"
	if !reflect.DeepEqual(result.Findings(), wantFindings) || result.Notices()[0] != "a notice" {
		t.Fatal("mutating returned slices changed Result")
	}
}
