package state_test

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	corestate "github.com/mianm12/dotfiles/internal/core/state"
)

func TestMarshalDecodeRoundTripsV5Deterministically(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	snapshot := corestate.Snapshot{
		Home: home,
		Links: map[corestate.Key]corestate.LinkRecord{
			{ModuleID: "git", PlacementID: "local"}: {
				Target: filepath.Join(".config", "git", "local"),
				Dest:   filepath.Join(home, "repo", "git", "local"),
			},
			{ModuleID: "git", PlacementID: "config"}: {
				Target: ".gitconfig",
				Dest:   filepath.Join(home, "repo", "git", "gitconfig"),
			},
			{ModuleID: "app", PlacementID: "config"}: {
				Target: filepath.Join(".config", "app"),
				Dest:   filepath.Join(home, "repo", "app", "config"),
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
	for _, field := range []string{`"module"`, `"placement"`, `"target"`, `"dest"`} {
		if !bytes.Contains(first, []byte(field)) {
			t.Fatalf("Marshal() omitted %s: %s", field, first)
		}
	}
	if bytes.Contains(first, []byte("module_id")) || bytes.Contains(first, []byte(home+"/.config")) {
		t.Fatalf("Marshal() used obsolete schema or absolute target: %s", first)
	}
	decoded, err := corestate.Decode(first, home)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if !corestate.Equal(decoded, snapshot) {
		t.Fatalf("Decode(Marshal(snapshot)) = %#v, want %#v", decoded, snapshot)
	}
}

func TestDecodeClassifiesVersionBeforeStrictV5(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	tests := []struct {
		document string
		want     error
	}{
		{document: `{"version":1,"legacy_field":true}`, want: corestate.ErrLegacyVersion},
		{document: `{"version":4,"unknown":true}`, want: corestate.ErrLegacyVersion},
		{document: `{"version":6,"future":true}`, want: corestate.ErrTooNew},
		{document: `{"version":999999999999999999999999999999999999,"future":true}`, want: corestate.ErrTooNew},
		{document: `{"version":5,"home":"/tmp","links":[],"extra":true}`, want: corestate.ErrInvalid},
		{document: `{"version":5.0}`, want: corestate.ErrInvalid},
		{document: `{"version":0}`, want: corestate.ErrInvalid},
		{document: `{}`, want: corestate.ErrInvalid},
	}
	for _, test := range tests {
		t.Run(test.document, func(t *testing.T) {
			decoded, err := corestate.Decode([]byte(test.document), home)
			if !errors.Is(err, test.want) {
				t.Fatalf("Decode() error = %v, want %v", err, test.want)
			}
			if decoded.Home != "" || decoded.Links != nil {
				t.Fatalf("Decode(error) returned partial snapshot %#v", decoded)
			}
		})
	}
}

func TestDecodeRejectsInvalidV5ShapeAndTargetAntichain(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	dest := filepath.Join(home, "repo", "app")
	link := func(module, placement, target string) string {
		return fmt.Sprintf(
			`{"module":%q,"placement":%q,"target":%q,"dest":%q}`,
			module,
			placement,
			target,
			dest,
		)
	}
	tests := []string{
		fmt.Sprintf(`{"version":5,"home":%q}`, home),
		fmt.Sprintf(`{"version":5,"home":%q,"links":null}`, home),
		fmt.Sprintf(`{"version":5,"home":%q,"links":[%s],"extra":true}`, home, link("app", "config", ".app")),
		fmt.Sprintf(`{"version":5,"home":%q,"links":[%s,%s]}`, home, link("app", "config", ".app"), link("app", "config", ".other")),
		fmt.Sprintf(`{"version":5,"home":%q,"links":[%s]}`, home, link("Bad", "config", ".app")),
		fmt.Sprintf(`{"version":5,"home":%q,"links":[%s]}`, home, link("app", "config", filepath.Join(home, ".app"))),
		fmt.Sprintf(`{"version":5,"home":%q,"links":[%s]}`, home, link("app", "config", "app/../other")),
		fmt.Sprintf(`{"version":5,"home":%q,"links":[%s,%s]}`, home, link("app", "one", ".app"), link("other", "two", ".app")),
		fmt.Sprintf(`{"version":5,"home":%q,"links":[%s,%s]}`, home, link("app", "one", ".app"), link("other", "two", ".app/child")),
		fmt.Sprintf(`{"version":5,"home":%q,"links":[{"module":"app","placement":"config","target":".app","dest":"relative"}]}`, home),
		fmt.Sprintf(`{"version":5,"home":%q,"links":[]} trailing`, home),
	}
	for _, document := range tests {
		decoded, err := corestate.Decode([]byte(document), home)
		if !errors.Is(err, corestate.ErrInvalid) {
			t.Fatalf("Decode(%s) error = %v, want ErrInvalid", document, err)
		}
		if decoded.Home != "" || decoded.Links != nil {
			t.Fatalf("Decode(error) returned partial snapshot %#v", decoded)
		}
	}
}

func TestDecodeHomeMismatch(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	other := filepath.Join(t.TempDir(), "other")
	_, err := corestate.Decode(
		[]byte(fmt.Sprintf(`{"version":5,"home":%q,"links":[]}`, other)),
		home,
	)
	if !errors.Is(err, corestate.ErrHomeMismatch) {
		t.Fatalf("Decode() error = %v, want ErrHomeMismatch", err)
	}
}

func TestMarshalRejectsInvalidConstructedSnapshot(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	tests := []corestate.Snapshot{
		{Home: home},
		{
			Home: home,
			Links: map[corestate.Key]corestate.LinkRecord{
				{ModuleID: "app", PlacementID: "one"}: {Target: ".app", Dest: filepath.Join(home, "one")},
				{ModuleID: "app", PlacementID: "two"}: {Target: ".app/child", Dest: filepath.Join(home, "two")},
			},
		},
	}
	for _, snapshot := range tests {
		if _, err := corestate.Marshal(snapshot); !errors.Is(err, corestate.ErrInvalid) {
			t.Fatalf("Marshal(%#v) error = %v, want ErrInvalid", snapshot, err)
		}
	}
}
