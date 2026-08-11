package state_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	corestate "github.com/mianm12/dotfiles/internal/core/state"
)

func TestMarshalDecode_RoundTripsLinkAndLocal(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	snapshot := corestate.Snapshot{
		Home: home,
		Records: map[corestate.Key]corestate.Record{
			{ModuleID: "app", PlacementID: "config"}: {
				Kind:   corestate.KindLocal,
				Target: filepath.Join(home, ".config", "app"),
			},
			{ModuleID: "git", PlacementID: "local"}: {
				Kind:   corestate.KindLocal,
				Target: filepath.Join(home, ".config", "git", "config.local"),
			},
			{ModuleID: "git", PlacementID: "config"}: {
				Kind:            corestate.KindLink,
				Target:          filepath.Join(home, ".gitconfig"),
				ResolvedTarget:  filepath.Join(home, ".gitconfig"),
				LinkDestination: filepath.Join(home, "dotfiles", "modules", "git", "gitconfig"),
			},
		},
	}

	first, err := corestate.Marshal(snapshot)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	second, err := corestate.Marshal(snapshot)
	if err != nil {
		t.Fatalf("Marshal(second) error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("Marshal() is not deterministic\nfirst=%s\nsecond=%s", first, second)
	}
	appIndex := bytes.Index(first, []byte(`"module_id": "app"`))
	gitIndex := bytes.Index(first, []byte(`"module_id": "git"`))
	if appIndex < 0 || gitIndex < 0 || appIndex >= gitIndex {
		t.Fatalf("Marshal() records are not sorted by module ID: %s", first)
	}
	configIndex := bytes.LastIndex(first, []byte(`"placement_id": "config"`))
	localIndex := bytes.Index(first, []byte(`"placement_id": "local"`))
	if configIndex < 0 || localIndex < 0 || configIndex >= localIndex {
		t.Fatalf("Marshal() records are not sorted by placement ID: %s", first)
	}
	decoded, err := corestate.Decode(first, home)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, snapshot) {
		t.Fatalf("Decode(Marshal(snapshot)) = %#v, want %#v", decoded, snapshot)
	}
}

func TestDecode_RequiresRecordsAndAcceptsExplicitEmptyArray(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	decoded, err := corestate.Decode(
		[]byte(fmt.Sprintf(`{"version":3,"home":%q,"records":[]}`, home)),
		home,
	)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if decoded.Home != home || decoded.Records == nil || len(decoded.Records) != 0 {
		t.Fatalf("Decode() = %#v, want explicit empty records", decoded)
	}
	for _, document := range []string{
		fmt.Sprintf(`{"version":3,"home":%q}`, home),
		fmt.Sprintf(`{"version":3,"home":%q,"records":null}`, home),
	} {
		if _, err := corestate.Decode([]byte(document), home); !errors.Is(err, corestate.ErrInvalid) {
			t.Fatalf("Decode(%s) error = %v, want ErrInvalid", document, err)
		}
	}
}

func TestDecode_RejectsUnsafeOrAmbiguousDocuments(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	target := filepath.Join(home, ".config", "app")
	linkDestination := filepath.Join(home, "repo", "modules", "app", "config")
	validLink := fmt.Sprintf(
		`{"module_id":"app","placement_id":"config","kind":"link","target":%q,"resolved_target":%q,"link_destination":%q}`,
		target,
		target,
		linkDestination,
	)
	tests := []struct {
		name     string
		document string
		want     error
	}{
		{
			name:     "duplicate member",
			document: fmt.Sprintf(`{"version":3,"version":3,"home":%q,"records":[]}`, home),
			want:     corestate.ErrInvalid,
		},
		{
			name:     "trailing JSON",
			document: fmt.Sprintf(`{"version":3,"home":%q,"records":[]}{}`, home),
			want:     corestate.ErrInvalid,
		},
		{
			name:     "record null",
			document: fmt.Sprintf(`{"version":3,"home":%q,"records":[null]}`, home),
			want:     corestate.ErrInvalid,
		},
		{
			name: "unknown record field",
			document: fmt.Sprintf(
				`{"version":3,"home":%q,"records":[{"module_id":"app","placement_id":"config","kind":"link","target":%q,"resolved_target":%q,"link_destination":%q,"extra":true}]}`,
				home,
				target,
				target,
				linkDestination,
			),
			want: corestate.ErrInvalid,
		},
		{
			name: "case variant field",
			document: fmt.Sprintf(
				`{"version":3,"home":%q,"records":[{"Module_ID":"app","placement_id":"config","kind":"link","target":%q,"resolved_target":%q,"link_destination":%q}]}`,
				home,
				target,
				target,
				linkDestination,
			),
			want: corestate.ErrInvalid,
		},
		{
			name: "invalid module ID",
			document: fmt.Sprintf(
				`{"version":3,"home":%q,"records":[{"module_id":"Bad","placement_id":"config","kind":"local","target":%q}]}`,
				home,
				target,
			),
			want: corestate.ErrInvalid,
		},
		{
			name: "invalid placement ID",
			document: fmt.Sprintf(
				`{"version":3,"home":%q,"records":[{"module_id":"app","placement_id":"Bad","kind":"local","target":%q}]}`,
				home,
				target,
			),
			want: corestate.ErrInvalid,
		},
		{
			name: "duplicate identity",
			document: fmt.Sprintf(
				`{"version":3,"home":%q,"records":[%s,%s]}`,
				home,
				validLink,
				validLink,
			),
			want: corestate.ErrInvalid,
		},
		{
			name: "target outside home",
			document: fmt.Sprintf(
				`{"version":3,"home":%q,"records":[{"module_id":"app","placement_id":"config","kind":"local","target":%q}]}`,
				home,
				filepath.Join(filepath.Dir(home), "outside"),
			),
			want: corestate.ErrInvalid,
		},
		{
			name: "relative resolved target",
			document: fmt.Sprintf(
				`{"version":3,"home":%q,"records":[{"module_id":"app","placement_id":"config","kind":"link","target":%q,"resolved_target":"relative","link_destination":%q}]}`,
				home,
				target,
				linkDestination,
			),
			want: corestate.ErrInvalid,
		},
		{
			name: "local with link ownership fields",
			document: fmt.Sprintf(
				`{"version":3,"home":%q,"records":[{"module_id":"app","placement_id":"config","kind":"local","target":%q,"resolved_target":%q}]}`,
				home,
				target,
				target,
			),
			want: corestate.ErrInvalid,
		},
		{
			name: "local with null resolved target",
			document: fmt.Sprintf(
				`{"version":3,"home":%q,"records":[{"module_id":"app","placement_id":"config","kind":"local","target":%q,"resolved_target":null}]}`,
				home,
				target,
			),
			want: corestate.ErrInvalid,
		},
		{
			name: "local with null link destination",
			document: fmt.Sprintf(
				`{"version":3,"home":%q,"records":[{"module_id":"app","placement_id":"config","kind":"local","target":%q,"link_destination":null}]}`,
				home,
				target,
			),
			want: corestate.ErrInvalid,
		},
		{
			name: "invalid UTF-8",
			document: string(append(
				[]byte(fmt.Sprintf(`{"version":3,"home":%q,"records":["`, home)),
				0xff,
			)),
			want: corestate.ErrInvalid,
		},
		{
			name: "unpaired UTF-16 surrogate",
			document: fmt.Sprintf(
				`{"version":3,"home":%q,"records":[{"module_id":"\ud800"}]}`,
				home,
			),
			want: corestate.ErrInvalid,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decoded, err := corestate.Decode([]byte(test.document), home)
			if !errors.Is(err, test.want) {
				t.Fatalf("Decode() = (%#v, %v), want %v", decoded, err, test.want)
			}
			if decoded.Home != "" || decoded.Records != nil {
				t.Fatalf("Decode(error) returned partial snapshot %#v", decoded)
			}
		})
	}
}

func TestDecode_ClassifiesVersionsBeforeLegacySchema(t *testing.T) {
	tests := []struct {
		version string
		want    error
	}{
		{version: "1", want: corestate.ErrLegacyVersion},
		{version: "2", want: corestate.ErrLegacyVersion},
		{version: "3", want: corestate.ErrInvalid},
		{version: "4", want: corestate.ErrTooNew},
		{version: "999999999999999999999999999999", want: corestate.ErrTooNew},
		{version: "0", want: corestate.ErrInvalid},
		{version: "2.0", want: corestate.ErrInvalid},
		{version: `"1"`, want: corestate.ErrInvalid},
		{version: `"4"`, want: corestate.ErrInvalid},
	}
	for _, test := range tests {
		t.Run(test.version, func(t *testing.T) {
			_, err := corestate.Decode(
				[]byte(fmt.Sprintf(`{"version":%s,"legacy_field":true}`, test.version)),
				filepath.Join(t.TempDir(), "home"),
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("Decode(version %s) error = %v, want %v", test.version, err, test.want)
			}
		})
	}
}

func TestMarshal_RejectsInvalidConstructedSnapshot(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	invalidComponent := "invalid-" + string([]byte{0xff})
	tests := []struct {
		name     string
		snapshot corestate.Snapshot
	}{
		{
			name: "relative home",
			snapshot: corestate.Snapshot{
				Home:    "relative",
				Records: map[corestate.Key]corestate.Record{},
			},
		},
		{
			name:     "nil records",
			snapshot: corestate.Snapshot{Home: home},
		},
		{
			name: "local with link evidence",
			snapshot: corestate.Snapshot{
				Home: home,
				Records: map[corestate.Key]corestate.Record{
					{ModuleID: "app", PlacementID: "local"}: {
						Kind:            corestate.KindLocal,
						Target:          filepath.Join(home, ".config", "app"),
						ResolvedTarget:  filepath.Join(home, ".config", "app"),
						LinkDestination: filepath.Join(home, "repo", "app"),
					},
				},
			},
		},
		{
			name: "invalid UTF-8 home",
			snapshot: corestate.Snapshot{
				Home:    filepath.Join(string(filepath.Separator), invalidComponent),
				Records: map[corestate.Key]corestate.Record{},
			},
		},
		{
			name: "invalid UTF-8 placement target",
			snapshot: corestate.Snapshot{
				Home: home,
				Records: map[corestate.Key]corestate.Record{
					{ModuleID: "app", PlacementID: "local"}: {
						Kind:   corestate.KindLocal,
						Target: filepath.Join(home, invalidComponent),
					},
				},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, err := corestate.Marshal(test.snapshot)
			if !errors.Is(err, corestate.ErrInvalid) {
				t.Fatalf("Marshal() = (%q, %v), want ErrInvalid", data, err)
			}
		})
	}
}

func TestDecodeRejectsInvalidUTF8ExpectedHome(t *testing.T) {
	invalidHome := filepath.Join(
		string(filepath.Separator),
		"home-"+string([]byte{0xff}),
	)
	decoded, err := corestate.Decode(
		[]byte(`{"version":3,"home":"/valid","records":[]}`),
		invalidHome,
	)
	if !errors.Is(err, corestate.ErrInvalid) ||
		decoded.Home != "" ||
		decoded.Records != nil {
		t.Fatalf(
			"Decode(invalid expected HOME) = (%#v, %v), want zero snapshot and ErrInvalid",
			decoded,
			err,
		)
	}
}

func TestLoad_DanglingSymlinkIsNotMissing(t *testing.T) {
	tests := []struct {
		name      string
		statePath func(*testing.T, string) string
	}{
		{
			name: "state leaf",
			statePath: func(t *testing.T, root string) string {
				path := filepath.Join(root, "state.json")
				if err := os.Symlink("missing.json", path); err != nil {
					t.Fatalf("os.Symlink(state) error = %v", err)
				}
				return path
			},
		},
		{
			name: "state ancestor",
			statePath: func(t *testing.T, root string) string {
				ancestor := filepath.Join(root, "state")
				if err := os.Symlink("missing", ancestor); err != nil {
					t.Fatalf("os.Symlink(state ancestor) error = %v", err)
				}
				return filepath.Join(ancestor, "state.json")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			if err := os.Mkdir(home, 0o700); err != nil {
				t.Fatalf("os.Mkdir(home) error = %v", err)
			}
			loaded, err := corestate.Load(test.statePath(t, root), home)
			if err == nil || loaded.Missing {
				t.Fatalf("Load(dangling symlink) = (%#v, %v), want read error", loaded, err)
			}
		})
	}
}
