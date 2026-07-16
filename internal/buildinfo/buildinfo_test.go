package buildinfo

import "testing"

func TestCurrent(t *testing.T) {
	originalVersion := Version
	originalCommit := Commit
	originalBuildTime := BuildTime
	t.Cleanup(func() {
		Version = originalVersion
		Commit = originalCommit
		BuildTime = originalBuildTime
	})

	Version = "v1.2.3"
	Commit = "abc123"
	BuildTime = "2026-07-16T10:00:00Z"

	got := Current()
	want := (Info{
		Version:   "v1.2.3",
		Commit:    "abc123",
		BuildTime: "2026-07-16T10:00:00Z",
	})
	if got != want {
		t.Fatalf("Current() = %#v, want %#v", got, want)
	}
}

func TestCurrentNormalizesEmptyValues(t *testing.T) {
	originalVersion := Version
	originalCommit := Commit
	originalBuildTime := BuildTime
	t.Cleanup(func() {
		Version = originalVersion
		Commit = originalCommit
		BuildTime = originalBuildTime
	})

	Version = ""
	Commit = ""
	BuildTime = ""

	got := Current()
	want := (Info{
		Version:   "dev",
		Commit:    "unknown",
		BuildTime: "unknown",
	})
	if got != want {
		t.Fatalf("Current() = %#v, want %#v", got, want)
	}
}
