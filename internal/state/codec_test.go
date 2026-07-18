package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestDecode_ValidV1(t *testing.T) {
	document := testDocument()
	document["entries"] = map[string]any{
		"~/.config/zsh/.zshrc": testSymlinkEntry(),
		"~/.config/app/settings": map[string]any{
			"module":     "app",
			"kind":       "scaffold",
			"source":     "modules/app/.config/app/settings.template",
			"applied_at": "2026-07-14T10:00:00.123Z",
		},
	}
	document["run_once"] = map[string]any{
		"macos/hooks/setup.sh": map[string]any{
			"hash":        "sha256:" + strings.Repeat("a", 64),
			"executed_at": "2026-07-14T10:00:01+08:00",
		},
	}

	snapshot, err := Decode(marshalDocument(t, document))
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if snapshot.Version() != 1 {
		t.Errorf("Snapshot.Version() = %d, want 1", snapshot.Version())
	}
	entry, ok := snapshot.Entry("~/.config/zsh/.zshrc")
	if !ok {
		t.Fatal("Snapshot.Entry() found = false, want true")
	}
	if entry.Module() != "zsh" || entry.Kind() != KindSymlink || entry.Source() != "modules/zsh/.config/zsh/.zshrc" {
		t.Errorf("Snapshot.Entry() = (%q, %q, %q), want symlink metadata", entry.Module(), entry.Kind(), entry.Source())
	}
	if entry.LinkDest() != "/repo/modules/zsh/.config/zsh/.zshrc" || entry.Hash() != "" {
		t.Errorf("Snapshot.Entry() evidence = (%q, %q), want link_dest only", entry.LinkDest(), entry.Hash())
	}
	keys := snapshot.EntryKeys()
	if len(keys) != 2 || keys[0] != "~/.config/app/settings" || keys[1] != "~/.config/zsh/.zshrc" {
		t.Errorf("Snapshot.EntryKeys() = %v, want stable sorted keys", keys)
	}
	run, ok := snapshot.RunOnce("macos/hooks/setup.sh")
	if !ok || !strings.HasPrefix(run.Hash(), "sha256:") || run.ExecutedAt() == "" {
		t.Errorf("Snapshot.RunOnce() = (%#v, %t), want valid record", run, ok)
	}
}

func TestDecode_RejectsDuplicateMemberAtAnyObjectLevel(t *testing.T) {
	entry := `{"module":"zsh","kind":"symlink","source":"modules/zsh/file","link_dest":"/repo/file","applied_at":"2026-07-14T10:00:00Z"}`
	run := `{"hash":"sha256:` + strings.Repeat("a", 64) + `","executed_at":"2026-07-14T10:00:01Z"}`
	tests := []struct {
		name string
		raw  string
	}{
		{name: "top level", raw: `{"version":1,"version":1,"entries":{},"run_once":{}}`},
		{name: "escaped top level", raw: `{"version":1,"\u0076ersion":1,"entries":{},"run_once":{}}`},
		{name: "entries map", raw: `{"version":1,"entries":{"~/file":` + entry + `,"~/file":` + entry + `},"run_once":{}}`},
		{name: "entry record", raw: `{"version":1,"entries":{"~/file":{"module":"zsh","\u006dodule":"zsh","kind":"symlink","source":"modules/zsh/file","link_dest":"/repo/file","applied_at":"2026-07-14T10:00:00Z"}},"run_once":{}}`},
		{name: "run once map", raw: `{"version":1,"entries":{},"run_once":{"zsh/hooks/x":` + run + `,"zsh/hooks/x":` + run + `}}`},
		{name: "run once record", raw: `{"version":1,"entries":{},"run_once":{"zsh/hooks/x":{"hash":"sha256:` + strings.Repeat("a", 64) + `","\u0068ash":"sha256:` + strings.Repeat("a", 64) + `","executed_at":"2026-07-14T10:00:01Z"}}}`},
		{name: "unknown nested object", raw: `{"version":1,"entries":{},"run_once":{},"future":{"a":1,"\u0061":2}}`},
		{name: "unknown object in array", raw: `{"version":1,"entries":{},"run_once":{},"future":[{"a":1,"a":2}]}`},
		{name: "too new still duplicate", raw: `{"version":2,"entries":{},"run_once":{},"future":{"a":1,"a":2}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode([]byte(tt.raw))
			if !errors.Is(err, ErrCorrupt) || !strings.Contains(err.Error(), "duplicate JSON member") {
				t.Fatalf("Decode() error = %v, want ErrCorrupt duplicate member", err)
			}
		})
	}
}

func TestDecode_ClassifiesVersionAndSchemaErrors(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want error
	}{
		{name: "empty", raw: ``, want: ErrCorrupt},
		{name: "root array", raw: `[]`, want: ErrCorrupt},
		{name: "missing version", raw: `{"entries":{},"run_once":{}}`, want: ErrCorrupt},
		{name: "string version", raw: `{"version":"1","entries":{},"run_once":{}}`, want: ErrCorrupt},
		{name: "fractional version", raw: `{"version":1.0,"entries":{},"run_once":{}}`, want: ErrCorrupt},
		{name: "zero version", raw: `{"version":0,"entries":{},"run_once":{}}`, want: ErrCorrupt},
		{name: "negative version", raw: `{"version":-1,"entries":{},"run_once":{}}`, want: ErrCorrupt},
		{name: "too new", raw: `{"version":2,"future":true}`, want: ErrTooNew},
		{name: "huge too new", raw: `{"version":999999999999999999999999999999,"future":true}`, want: ErrTooNew},
		{name: "missing entries", raw: `{"version":1,"run_once":{}}`, want: ErrCorrupt},
		{name: "null entries", raw: `{"version":1,"entries":null,"run_once":{}}`, want: ErrCorrupt},
		{name: "entries wrong type", raw: `{"version":1,"entries":[],"run_once":{}}`, want: ErrCorrupt},
		{name: "missing run once", raw: `{"version":1,"entries":{}}`, want: ErrCorrupt},
		{name: "run once wrong type", raw: `{"version":1,"entries":{},"run_once":false}`, want: ErrCorrupt},
		{name: "entry field wrong type", raw: `{"version":1,"entries":{"~/file":{"module":1,"kind":"symlink","source":"modules/app/file","link_dest":"/repo/file","applied_at":"2026-07-14T10:00:00Z"}},"run_once":{}}`, want: ErrCorrupt},
		{name: "run once field wrong type", raw: `{"version":1,"entries":{},"run_once":{"app/hooks/x":{"hash":1,"executed_at":"2026-07-14T10:00:00Z"}}}`, want: ErrCorrupt},
		{name: "unknown top level", raw: `{"version":1,"entries":{},"run_once":{},"future":true}`, want: ErrCorrupt},
		{name: "trailing value", raw: `{"version":1,"entries":{},"run_once":{}} true`, want: ErrCorrupt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode([]byte(tt.raw))
			if !errors.Is(err, tt.want) {
				t.Fatalf("Decode() error = %v, want errors.Is(%v)", err, tt.want)
			}
			if tt.want == ErrTooNew && errors.Is(err, ErrCorrupt) {
				t.Fatalf("Decode() error = %v, too-new must not be corrupt", err)
			}
		})
	}
}

func TestDecode_RejectsInvalidEntrySemantics(t *testing.T) {
	valid := testSymlinkEntry()
	tests := []struct {
		name   string
		target string
		mutate func(map[string]any)
	}{
		{name: "invalid target root", target: "~", mutate: noEntryMutation},
		{name: "absolute target", target: "/tmp/file", mutate: noEntryMutation},
		{name: "target dot component", target: "~/.config/./file", mutate: noEntryMutation},
		{name: "target parent component", target: "~/.config/../file", mutate: noEntryMutation},
		{name: "target empty component", target: "~/.config//file", mutate: noEntryMutation},
		{name: "target trailing slash", target: "~/.config/file/", mutate: noEntryMutation},
		{name: "target environment reference", target: "~/$HOME/file", mutate: noEntryMutation},
		{name: "target braced environment reference", target: "~/${HOME}/file", mutate: noEntryMutation},
		{name: "invalid module", target: "~/file", mutate: setEntry("module", "../zsh")},
		{name: "empty module", target: "~/file", mutate: setEntry("module", "")},
		{name: "source outside modules", target: "~/file", mutate: setEntry("source", "zsh/file")},
		{name: "source module mismatch", target: "~/file", mutate: setEntry("source", "modules/git/file")},
		{name: "source dot component", target: "~/file", mutate: setEntry("source", "modules/zsh/./file")},
		{name: "empty source leaf", target: "~/file", mutate: setEntry("source", "modules/zsh")},
		{name: "invalid time", target: "~/file", mutate: setEntry("applied_at", "yesterday")},
		{name: "unknown kind", target: "~/file", mutate: setEntry("kind", "managed")},
		{name: "missing link dest", target: "~/file", mutate: deleteEntry("link_dest")},
		{name: "empty link dest", target: "~/file", mutate: setEntry("link_dest", "")},
		{name: "symlink hash forbidden", target: "~/file", mutate: setEntry("hash", "sha256:"+strings.Repeat("a", 64))},
		{name: "unknown entry field", target: "~/file", mutate: setEntry("future", true)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := cloneMap(valid)
			tt.mutate(entry)
			document := testDocument()
			document["entries"] = map[string]any{tt.target: entry}
			_, err := Decode(marshalDocument(t, document))
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("Decode() error = %v, want ErrCorrupt", err)
			}
		})
	}
}

func TestDecode_ValidatesKindSpecificEvidenceBeforeRenderedClassification(t *testing.T) {
	tests := []struct {
		name  string
		entry map[string]any
		want  error
	}{
		{
			name: "valid scaffold",
			entry: map[string]any{
				"module": "app", "kind": "scaffold", "source": "modules/app/file.template",
				"applied_at": "2026-07-14T10:00:00Z",
			},
		},
		{
			name: "scaffold link evidence",
			entry: map[string]any{
				"module": "app", "kind": "scaffold", "source": "modules/app/file.template",
				"link_dest": "/repo/file", "applied_at": "2026-07-14T10:00:00Z",
			},
			want: ErrCorrupt,
		},
		{
			name: "valid rendered unsupported",
			entry: map[string]any{
				"module": "app", "kind": "rendered", "source": "modules/app/file.tmpl",
				"hash": "sha256:" + strings.Repeat("b", 64), "applied_at": "2026-07-14T10:00:00Z",
			},
			want: ErrUnsupportedRendered,
		},
		{
			name: "rendered missing hash corrupt",
			entry: map[string]any{
				"module": "app", "kind": "rendered", "source": "modules/app/file.tmpl",
				"applied_at": "2026-07-14T10:00:00Z",
			},
			want: ErrCorrupt,
		},
		{
			name: "rendered malformed hash corrupt",
			entry: map[string]any{
				"module": "app", "kind": "rendered", "source": "modules/app/file.tmpl",
				"hash": "sha1:abcd", "applied_at": "2026-07-14T10:00:00Z",
			},
			want: ErrCorrupt,
		},
		{
			name: "rendered link evidence corrupt",
			entry: map[string]any{
				"module": "app", "kind": "rendered", "source": "modules/app/file.tmpl",
				"hash": "sha256:" + strings.Repeat("b", 64), "link_dest": "/repo/file",
				"applied_at": "2026-07-14T10:00:00Z",
			},
			want: ErrCorrupt,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			document := testDocument()
			document["entries"] = map[string]any{"~/file": tt.entry}
			_, err := Decode(marshalDocument(t, document))
			if tt.want == nil {
				if err != nil {
					t.Fatalf("Decode() error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("Decode() error = %v, want errors.Is(%v)", err, tt.want)
			}
			if tt.want == ErrCorrupt && errors.Is(err, ErrUnsupportedRendered) {
				t.Fatalf("Decode() error = %v, malformed rendered must be corrupt", err)
			}
		})
	}

	t.Run("later invalid entry wins over rendered classification", func(t *testing.T) {
		document := testDocument()
		document["entries"] = map[string]any{
			"~/rendered": tests[2].entry,
			"~/invalid":  map[string]any{"module": "app", "kind": "scaffold"},
		}
		_, err := Decode(marshalDocument(t, document))
		if !errors.Is(err, ErrCorrupt) || errors.Is(err, ErrUnsupportedRendered) {
			t.Fatalf("Decode() error = %v, want only ErrCorrupt", err)
		}
	})
}

func TestDecode_ValidatesRunOnceRecords(t *testing.T) {
	valid := map[string]any{
		"hash":        "sha256:" + strings.Repeat("c", 64),
		"executed_at": "2026-07-14T10:00:01Z",
	}
	tests := []struct {
		name   string
		key    string
		mutate func(map[string]any)
	}{
		{name: "invalid module", key: "../zsh/hooks/x", mutate: noEntryMutation},
		{name: "missing script", key: "zsh", mutate: noEntryMutation},
		{name: "absolute script", key: "zsh//hooks/x", mutate: noEntryMutation},
		{name: "dot script", key: "zsh/hooks/./x", mutate: noEntryMutation},
		{name: "parent script", key: "zsh/hooks/../x", mutate: noEntryMutation},
		{name: "missing hash", key: "zsh/hooks/x", mutate: deleteEntry("hash")},
		{name: "invalid hash", key: "zsh/hooks/x", mutate: setEntry("hash", "sha256:abcd")},
		{name: "invalid time", key: "zsh/hooks/x", mutate: setEntry("executed_at", "soon")},
		{name: "unknown field", key: "zsh/hooks/x", mutate: setEntry("future", true)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := cloneMap(valid)
			tt.mutate(record)
			document := testDocument()
			document["run_once"] = map[string]any{tt.key: record}
			_, err := Decode(marshalDocument(t, document))
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("Decode() error = %v, want ErrCorrupt", err)
			}
		})
	}

	document := testDocument()
	document["run_once"] = map[string]any{"zsh/hooks/missing.sh": valid}
	if _, err := Decode(marshalDocument(t, document)); err != nil {
		t.Fatalf("Decode() historical missing script error = %v, want nil", err)
	}
}

func testDocument() map[string]any {
	return map[string]any{
		"version":  1,
		"entries":  map[string]any{},
		"run_once": map[string]any{},
	}
}

func testSymlinkEntry() map[string]any {
	return map[string]any{
		"module":     "zsh",
		"kind":       "symlink",
		"source":     "modules/zsh/.config/zsh/.zshrc",
		"link_dest":  "/repo/modules/zsh/.config/zsh/.zshrc",
		"applied_at": "2026-07-14T10:00:00Z",
	}
}

func marshalDocument(t *testing.T, document map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return data
}

func cloneMap(input map[string]any) map[string]any {
	cloned := make(map[string]any, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}

func noEntryMutation(map[string]any) {}

func setEntry(key string, value any) func(map[string]any) {
	return func(entry map[string]any) {
		entry[key] = value
	}
}

func deleteEntry(key string) func(map[string]any) {
	return func(entry map[string]any) {
		delete(entry, key)
	}
}

func ExampleDecode() {
	snapshot, err := Decode([]byte(`{"version":1,"entries":{},"run_once":{}}`))
	fmt.Println(snapshot.Version(), err)
	// Output: 1 <nil>
}
