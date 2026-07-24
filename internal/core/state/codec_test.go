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
		Modules: map[string]corestate.Module{
			"git": {
				Placements: map[string]corestate.Placement{
					"config": {
						Kind:            corestate.KindLink,
						Target:          filepath.Join(home, ".gitconfig"),
						ResolvedTarget:  filepath.Join(home, ".gitconfig"),
						LinkDestination: filepath.Join(home, "dotfiles", "modules", "git", "gitconfig"),
					},
					"local": {
						Kind:   corestate.KindLocal,
						Target: filepath.Join(home, ".config", "git", "config.local"),
					},
				},
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
	decoded, err := corestate.Decode(first, home)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, snapshot) {
		t.Fatalf("Decode(Marshal(snapshot)) = %#v, want %#v", decoded, snapshot)
	}
}

func TestDecode_AcceptsMissingOptionalContainersAsEmpty(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	decoded, err := corestate.Decode(
		[]byte(fmt.Sprintf(`{"version":2,"home":%q}`, home)),
		home,
	)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if decoded.Home != home || decoded.Modules == nil || len(decoded.Modules) != 0 {
		t.Fatalf("Decode() = %#v, want empty normalized modules", decoded)
	}
}

func TestDecode_RejectsUnsafeOrAmbiguousDocuments(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	target := filepath.Join(home, ".config", "app")
	linkDestination := filepath.Join(home, "repo", "modules", "app", "config")
	validLink := fmt.Sprintf(
		`{"kind":"link","target":%q,"resolved_target":%q,"link_destination":%q}`,
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
			document: fmt.Sprintf(`{"version":2,"version":2,"home":%q}`, home),
			want:     corestate.ErrInvalid,
		},
		{
			name:     "trailing JSON",
			document: fmt.Sprintf(`{"version":2,"home":%q}{}`, home),
			want:     corestate.ErrInvalid,
		},
		{
			name:     "modules null",
			document: fmt.Sprintf(`{"version":2,"home":%q,"modules":null}`, home),
			want:     corestate.ErrInvalid,
		},
		{
			name: "module null",
			document: fmt.Sprintf(
				`{"version":2,"home":%q,"modules":{"app":null}}`,
				home,
			),
			want: corestate.ErrInvalid,
		},
		{
			name: "placements null",
			document: fmt.Sprintf(
				`{"version":2,"home":%q,"modules":{"app":{"placements":null}}}`,
				home,
			),
			want: corestate.ErrInvalid,
		},
		{
			name: "unknown placement field",
			document: fmt.Sprintf(
				`{"version":2,"home":%q,"modules":{"app":{"placements":{"config":{"kind":"link","target":%q,"resolved_target":%q,"link_destination":%q,"extra":true}}}}}`,
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
				`{"version":2,"home":%q,"modules":{"app":{"placements":{"config":{"Kind":"link","target":%q,"resolved_target":%q,"link_destination":%q}}}}}`,
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
				`{"version":2,"home":%q,"modules":{"Bad":{"placements":{}}}}`,
				home,
			),
			want: corestate.ErrInvalid,
		},
		{
			name: "invalid placement ID",
			document: fmt.Sprintf(
				`{"version":2,"home":%q,"modules":{"app":{"placements":{"Bad":%s}}}}`,
				home,
				validLink,
			),
			want: corestate.ErrInvalid,
		},
		{
			name: "target outside home",
			document: fmt.Sprintf(
				`{"version":2,"home":%q,"modules":{"app":{"placements":{"config":{"kind":"local","target":%q}}}}}`,
				home,
				filepath.Join(filepath.Dir(home), "outside"),
			),
			want: corestate.ErrInvalid,
		},
		{
			name: "relative resolved target",
			document: fmt.Sprintf(
				`{"version":2,"home":%q,"modules":{"app":{"placements":{"config":{"kind":"link","target":%q,"resolved_target":"relative","link_destination":%q}}}}}`,
				home,
				target,
				linkDestination,
			),
			want: corestate.ErrInvalid,
		},
		{
			name: "local with link ownership fields",
			document: fmt.Sprintf(
				`{"version":2,"home":%q,"modules":{"app":{"placements":{"config":{"kind":"local","target":%q,"resolved_target":%q}}}}}`,
				home,
				target,
				target,
			),
			want: corestate.ErrInvalid,
		},
		{
			name: "local with null resolved target",
			document: fmt.Sprintf(
				`{"version":2,"home":%q,"modules":{"app":{"placements":{"config":{"kind":"local","target":%q,"resolved_target":null}}}}}`,
				home,
				target,
			),
			want: corestate.ErrInvalid,
		},
		{
			name: "local with null link destination",
			document: fmt.Sprintf(
				`{"version":2,"home":%q,"modules":{"app":{"placements":{"config":{"kind":"local","target":%q,"link_destination":null}}}}}`,
				home,
				target,
			),
			want: corestate.ErrInvalid,
		},
		{
			name: "invalid UTF-8",
			document: string(append(
				[]byte(fmt.Sprintf(`{"version":2,"home":%q,"modules":{"`, home)),
				0xff,
			)),
			want: corestate.ErrInvalid,
		},
		{
			name: "unpaired UTF-16 surrogate",
			document: fmt.Sprintf(
				`{"version":2,"home":%q,"modules":{"\ud800":{"placements":{}}}}`,
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
			if decoded.Home != "" || decoded.Modules != nil {
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
		{version: "3", want: corestate.ErrTooNew},
		{version: "999999999999999999999999999999", want: corestate.ErrTooNew},
		{version: "0", want: corestate.ErrInvalid},
		{version: "2.0", want: corestate.ErrInvalid},
		{version: `"1"`, want: corestate.ErrInvalid},
		{version: `"3"`, want: corestate.ErrInvalid},
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
	tests := []struct {
		name     string
		snapshot corestate.Snapshot
	}{
		{
			name: "relative home",
			snapshot: corestate.Snapshot{
				Home:    "relative",
				Modules: map[string]corestate.Module{},
			},
		},
		{
			name: "local with link evidence",
			snapshot: corestate.Snapshot{
				Home: home,
				Modules: map[string]corestate.Module{
					"app": {
						Placements: map[string]corestate.Placement{
							"local": {
								Kind:            corestate.KindLocal,
								Target:          filepath.Join(home, ".config", "app"),
								ResolvedTarget:  filepath.Join(home, ".config", "app"),
								LinkDestination: filepath.Join(home, "repo", "app"),
							},
						},
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
