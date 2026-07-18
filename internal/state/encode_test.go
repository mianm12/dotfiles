package state

import (
	"bytes"
	"errors"
	"testing"
)

func TestEncode_DeterministicRoundTrip(t *testing.T) {
	first, err := Decode([]byte(`{
  "version": 1,
  "entries": {
    "~/.config/zsh/zshrc": {
      "module": "zsh",
      "kind": "symlink",
      "source": "modules/zsh/.config/zsh/zshrc",
      "link_dest": "/repo/modules/zsh/.config/zsh/zshrc",
      "applied_at": "2026-07-14T10:00:00Z"
    },
    "~/.config/app/settings": {
      "module": "app",
      "kind": "scaffold",
      "source": "modules/app/.config/app/settings.template",
      "applied_at": "2026-07-14T10:00:01.125Z"
    }
  },
  "run_once": {
    "zsh/hooks/setup.sh": {
      "hash": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "executed_at": "2026-07-14T10:00:02+08:00"
    }
  }
}`))
	if err != nil {
		t.Fatalf("Decode(first) error = %v", err)
	}
	second, err := Decode([]byte(`{
  "run_once": {
    "zsh/hooks/setup.sh": {
      "executed_at": "2026-07-14T10:00:02+08:00",
      "hash": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
    }
  },
  "entries": {
    "~/.config/app/settings": {
      "applied_at": "2026-07-14T10:00:01.125Z",
      "source": "modules/app/.config/app/settings.template",
      "kind": "scaffold",
      "module": "app"
    },
    "~/.config/zsh/zshrc": {
      "link_dest": "/repo/modules/zsh/.config/zsh/zshrc",
      "source": "modules/zsh/.config/zsh/zshrc",
      "module": "zsh",
      "applied_at": "2026-07-14T10:00:00Z",
      "kind": "symlink"
    }
  },
  "version": 1
}`))
	if err != nil {
		t.Fatalf("Decode(second) error = %v", err)
	}

	firstEncoded, err := Encode(first)
	if err != nil {
		t.Fatalf("Encode(first) error = %v", err)
	}
	secondEncoded, err := Encode(second)
	if err != nil {
		t.Fatalf("Encode(second) error = %v", err)
	}
	if !bytes.Equal(firstEncoded, secondEncoded) {
		t.Fatalf("equivalent snapshots encoded differently:\nfirst=%s\nsecond=%s", firstEncoded, secondEncoded)
	}
	if len(firstEncoded) == 0 || firstEncoded[len(firstEncoded)-1] != '\n' {
		t.Fatalf("Encode() output must be a complete newline-terminated document: %q", firstEncoded)
	}

	roundTripped, err := Decode(firstEncoded)
	if err != nil {
		t.Fatalf("Decode(Encode()) error = %v", err)
	}
	if roundTripped.Version() != 1 {
		t.Fatalf("round-tripped version = %d, want 1", roundTripped.Version())
	}
	if got := roundTripped.EntryKeys(); len(got) != 2 || got[0] != "~/.config/app/settings" || got[1] != "~/.config/zsh/zshrc" {
		t.Fatalf("round-tripped entry keys = %v", got)
	}
	if record, ok := roundTripped.RunOnce("zsh/hooks/setup.sh"); !ok || record.ExecutedAt() != "2026-07-14T10:00:02+08:00" {
		t.Fatalf("round-tripped run_once = (%#v, %t)", record, ok)
	}
}

func TestEncode_RejectsInvalidSnapshots(t *testing.T) {
	if _, err := Encode(Snapshot{}); err == nil {
		t.Fatal("Encode(zero Snapshot) error = nil, want invalid Snapshot error")
	}

	invalid := Snapshot{
		version: 1,
		entries: map[string]Entry{
			"relative": {
				module:    "app",
				kind:      KindScaffold,
				source:    "modules/app/file.template",
				appliedAt: "2026-07-14T10:00:00Z",
			},
		},
		runOnce: map[string]RunOnceRecord{},
		valid:   true,
	}
	if _, err := Encode(invalid); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Encode(internally invalid Snapshot) error = %v, want ErrCorrupt", err)
	}
}
