package state

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mianm12/dotfiles/internal/core/identifier"
	corepaths "github.com/mianm12/dotfiles/internal/core/paths"
)

type encodedDocument struct {
	Version int           `json:"version"`
	Home    string        `json:"home"`
	Links   []encodedLink `json:"links"`
}

type encodedLink struct {
	ModuleID    string `json:"module"`
	PlacementID string `json:"placement"`
	Target      string `json:"target"`
	Dest        string `json:"dest"`
}

type versionEnvelope struct {
	Version json.RawMessage `json:"version"`
}

// Decode strictly decodes state v5 and binds it to expectedHome.
func Decode(data []byte, expectedHome string) (Snapshot, error) {
	return decode(data, expectedHome)
}

func decode(data []byte, expectedHome string) (Snapshot, error) {
	home, err := cleanExpectedHome(expectedHome)
	if err != nil {
		return Snapshot{}, err
	}
	version, err := probeVersion(data)
	if err != nil {
		return Snapshot{}, err
	}
	supported := big.NewInt(Version)
	switch version.Cmp(supported) {
	case 1:
		return Snapshot{}, fmt.Errorf(
			"%w: found version %s, maximum supported is %d",
			ErrTooNew,
			version.String(),
			Version,
		)
	case -1:
		return Snapshot{}, fmt.Errorf(
			"%w: version %s is unsupported; remove or archive it before using version %d",
			ErrLegacyVersion,
			version.String(),
			Version,
		)
	}
	snapshot, err := decodeV5Document(data)
	if err != nil {
		return Snapshot{}, err
	}
	if snapshot.Home != home {
		return Snapshot{}, fmt.Errorf(
			"%w: state is bound to %q, current home is %q",
			ErrHomeMismatch,
			snapshot.Home,
			home,
		)
	}
	return snapshot, nil
}

func probeVersion(data []byte) (*big.Int, error) {
	var envelope versionEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, invalidf("decode state JSON: %v", err)
	}
	if len(envelope.Version) == 0 {
		return nil, invalidf("required top-level version is missing")
	}
	versionText := string(envelope.Version)
	for _, character := range versionText {
		if character < '0' || character > '9' {
			return nil, invalidf("version must be a positive integer")
		}
	}
	version, ok := new(big.Int).SetString(versionText, 10)
	if !ok || version.Sign() <= 0 {
		return nil, invalidf("version must be a positive integer")
	}
	return version, nil
}

// Marshal validates and encodes one state v5 document.
func Marshal(snapshot Snapshot) ([]byte, error) {
	if err := validateSnapshot(snapshot); err != nil {
		return nil, err
	}
	document := encodedDocument{
		Version: Version,
		Home:    snapshot.Home,
		Links:   make([]encodedLink, 0, len(snapshot.Links)),
	}
	for _, key := range sortedStateKeys(snapshot.Links) {
		link := snapshot.Links[key]
		document.Links = append(document.Links, encodedLink{
			ModuleID:    key.ModuleID,
			PlacementID: key.PlacementID,
			Target:      link.Target,
			Dest:        link.Dest,
		})
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode state v5: %w", err)
	}
	return append(data, '\n'), nil
}

func decodeV5Document(data []byte) (Snapshot, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document encodedDocument
	if err := decoder.Decode(&document); err != nil {
		return Snapshot{}, invalidf("%v", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return Snapshot{}, invalidf("%v", err)
	}
	if document.Links == nil {
		return Snapshot{}, invalidf("required top-level links is missing")
	}

	snapshot := Snapshot{
		Home:  document.Home,
		Links: make(map[Key]LinkRecord, len(document.Links)),
	}
	for index, link := range document.Links {
		key := Key{ModuleID: link.ModuleID, PlacementID: link.PlacementID}
		if _, exists := snapshot.Links[key]; exists {
			return Snapshot{}, invalidf(
				"link %d duplicates identity %q/%q",
				index,
				key.ModuleID,
				key.PlacementID,
			)
		}
		snapshot.Links[key] = LinkRecord{Target: link.Target, Dest: link.Dest}
	}
	if err := validateSnapshot(snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return fmt.Errorf("unexpected trailing JSON token %v", token)
}

func validateSnapshot(snapshot Snapshot) error {
	_, err := cleanStoredAbsolute("home", snapshot.Home)
	if err != nil {
		return err
	}
	if snapshot.Links == nil {
		return invalidf("links map must not be nil")
	}
	keys := sortedStateKeys(snapshot.Links)
	targets := make([]corepaths.Target, 0, len(keys))
	for _, key := range keys {
		if !identifier.Valid(key.ModuleID) {
			return invalidf("invalid module ID %q", key.ModuleID)
		}
		if !identifier.Valid(key.PlacementID) {
			return invalidf(
				"module %q has invalid placement ID %q",
				key.ModuleID,
				key.PlacementID,
			)
		}
		target, err := validateLink(key, snapshot.Links[key])
		if err != nil {
			return err
		}
		targets = append(targets, target)
	}
	for left := range targets {
		for right := left + 1; right < len(targets); right++ {
			if !corepaths.TargetsConflict(targets[left], targets[right]) {
				continue
			}
			return invalidf(
				"links %q/%q target %q conflicts with %q/%q target %q",
				keys[left].ModuleID,
				keys[left].PlacementID,
				snapshot.Links[keys[left]].Target,
				keys[right].ModuleID,
				keys[right].PlacementID,
				snapshot.Links[keys[right]].Target,
			)
		}
	}
	return nil
}

func validateLink(key Key, link LinkRecord) (corepaths.Target, error) {
	target, err := corepaths.ResolveStoredTarget(link.Target)
	if err != nil {
		return corepaths.Target{}, invalidf(
			"module %q placement %q target: %v",
			key.ModuleID,
			key.PlacementID,
			err,
		)
	}
	if _, err := cleanStoredAbsolute("dest", link.Dest); err != nil {
		return corepaths.Target{}, invalidf(
			"module %q link %q: %v",
			key.ModuleID,
			key.PlacementID,
			err,
		)
	}
	return target, nil
}

func cleanExpectedHome(home string) (string, error) {
	if home == "" ||
		strings.ContainsRune(home, '\x00') ||
		!utf8.ValidString(home) ||
		!filepath.IsAbs(home) {
		return "", invalidf(
			"home %q must be a non-empty absolute path without NUL and with valid UTF-8",
			home,
		)
	}
	return filepath.Clean(home), nil
}

func cleanStoredAbsolute(name, value string) (string, error) {
	if value == "" ||
		strings.ContainsRune(value, '\x00') ||
		!utf8.ValidString(value) ||
		!filepath.IsAbs(value) {
		return "", invalidf(
			"%s %q must be a non-empty absolute path without NUL and with valid UTF-8",
			name,
			value,
		)
	}
	cleaned := filepath.Clean(value)
	if cleaned != value {
		return "", invalidf("%s %q must be normalized", name, value)
	}
	return cleaned, nil
}

func sortedStateKeys(links map[Key]LinkRecord) []Key {
	keys := make([]Key, 0, len(links))
	for key := range links {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(left, right Key) int {
		if byModule := strings.Compare(left.ModuleID, right.ModuleID); byModule != 0 {
			return byModule
		}
		return strings.Compare(left.PlacementID, right.PlacementID)
	})
	return keys
}

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}
