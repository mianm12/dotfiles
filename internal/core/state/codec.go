package state

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

var (
	idPattern      = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	integerPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
)

type stateDocument struct {
	Version *int              `json:"version"`
	Home    *string           `json:"home"`
	Records *[]recordDocument `json:"records"`
}

type recordDocument struct {
	ModuleID        *string `json:"module_id"`
	PlacementID     *string `json:"placement_id"`
	Kind            *string `json:"kind"`
	Target          *string `json:"target"`
	ResolvedTarget  *string `json:"resolved_target"`
	LinkDestination *string `json:"link_destination"`
}

type encodedDocument struct {
	Version int             `json:"version"`
	Home    string          `json:"home"`
	Records []encodedRecord `json:"records"`
}

type encodedRecord struct {
	ModuleID        string `json:"module_id"`
	PlacementID     string `json:"placement_id"`
	Kind            Kind   `json:"kind"`
	Target          string `json:"target"`
	ResolvedTarget  string `json:"resolved_target,omitempty"`
	LinkDestination string `json:"link_destination,omitempty"`
}

// Decode strictly decodes state v3 and binds it to expectedHome.
func Decode(data []byte, expectedHome string) (Snapshot, error) {
	return decode(data, expectedHome)
}

func decode(data []byte, expectedHome string) (Snapshot, error) {
	home, err := cleanExpectedHome(expectedHome)
	if err != nil {
		return Snapshot{}, err
	}
	if err := validateJSONText(data); err != nil {
		return Snapshot{}, invalidf("%v", err)
	}
	if err := rejectDuplicateMembers(data); err != nil {
		return Snapshot{}, invalidf("%v", err)
	}
	version, err := probeVersion(data)
	if err != nil {
		return Snapshot{}, err
	}
	switch {
	case version.Cmp(big.NewInt(Version)) > 0:
		return Snapshot{}, fmt.Errorf(
			"%w: found version %s, maximum supported is %d",
			ErrTooNew,
			version,
			Version,
		)
	case version.Cmp(big.NewInt(Version)) < 0:
		return Snapshot{}, fmt.Errorf(
			"%w: version %s is unsupported; remove or archive it before using version %d",
			ErrLegacyVersion,
			version,
			Version,
		)
	}
	if err := validateObjectShapes(data); err != nil {
		return Snapshot{}, invalidf("%v", err)
	}

	var document stateDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return Snapshot{}, invalidf("decode state v3: %v", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return Snapshot{}, invalidf("%v", err)
	}
	snapshot, err := snapshotFromDocument(document)
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

// Marshal validates and encodes one state v3 document.
func Marshal(snapshot Snapshot) ([]byte, error) {
	if err := validateSnapshot(snapshot); err != nil {
		return nil, err
	}
	document := encodedDocument{
		Version: Version,
		Home:    snapshot.Home,
		Records: make([]encodedRecord, 0, len(snapshot.Records)),
	}
	for _, key := range sortedStateKeys(snapshot.Records) {
		record := snapshot.Records[key]
		document.Records = append(document.Records, encodedRecord{
			ModuleID:        key.ModuleID,
			PlacementID:     key.PlacementID,
			Kind:            record.Kind,
			Target:          record.Target,
			ResolvedTarget:  record.ResolvedTarget,
			LinkDestination: record.LinkDestination,
		})
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode state v3: %w", err)
	}
	return append(data, '\n'), nil
}

func snapshotFromDocument(document stateDocument) (Snapshot, error) {
	if document.Version == nil || *document.Version != Version {
		return Snapshot{}, invalidf("required top-level version must equal %d", Version)
	}
	if document.Home == nil {
		return Snapshot{}, invalidf("required top-level home is missing")
	}
	if document.Records == nil {
		return Snapshot{}, invalidf("required top-level records are missing")
	}
	snapshot := Snapshot{
		Home:    *document.Home,
		Records: make(map[Key]Record, len(*document.Records)),
	}
	for index, document := range *document.Records {
		key, record, err := recordFromDocument(index, document)
		if err != nil {
			return Snapshot{}, err
		}
		if _, exists := snapshot.Records[key]; exists {
			return Snapshot{}, invalidf(
				"record %d duplicates identity %q/%q",
				index,
				key.ModuleID,
				key.PlacementID,
			)
		}
		snapshot.Records[key] = record
	}
	if err := validateSnapshot(snapshot); err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func recordFromDocument(index int, document recordDocument) (Key, Record, error) {
	if document.ModuleID == nil || document.PlacementID == nil ||
		document.Kind == nil || document.Target == nil {
		return Key{}, Record{}, invalidf(
			"record %d requires module_id, placement_id, kind, and target",
			index,
		)
	}
	key := Key{
		ModuleID:    *document.ModuleID,
		PlacementID: *document.PlacementID,
	}
	record := Record{
		Kind:   Kind(*document.Kind),
		Target: *document.Target,
	}
	switch record.Kind {
	case KindLink:
		if document.ResolvedTarget == nil || document.LinkDestination == nil {
			return Key{}, Record{}, invalidf(
				"record %d link %q/%q requires resolved_target and link_destination",
				index,
				key.ModuleID,
				key.PlacementID,
			)
		}
		record.ResolvedTarget = *document.ResolvedTarget
		record.LinkDestination = *document.LinkDestination
	case KindLocal:
		if document.ResolvedTarget != nil || document.LinkDestination != nil {
			return Key{}, Record{}, invalidf(
				"record %d local %q/%q must not contain link ownership fields",
				index,
				key.ModuleID,
				key.PlacementID,
			)
		}
	default:
		return Key{}, Record{}, invalidf(
			"record %d %q/%q has unsupported kind %q",
			index,
			key.ModuleID,
			key.PlacementID,
			record.Kind,
		)
	}
	return key, record, nil
}

func validateSnapshot(snapshot Snapshot) error {
	home, err := cleanStoredAbsolute("home", snapshot.Home)
	if err != nil {
		return err
	}
	if snapshot.Records == nil {
		return invalidf("records map must not be nil")
	}
	for _, key := range sortedStateKeys(snapshot.Records) {
		if !idPattern.MatchString(key.ModuleID) {
			return invalidf("invalid module ID %q", key.ModuleID)
		}
		if !idPattern.MatchString(key.PlacementID) {
			return invalidf(
				"module %q has invalid placement ID %q",
				key.ModuleID,
				key.PlacementID,
			)
		}
		if err := validateRecord(home, key, snapshot.Records[key]); err != nil {
			return err
		}
	}
	return nil
}

func validateRecord(home string, key Key, record Record) error {
	if err := validateTarget(home, record.Target); err != nil {
		return invalidf(
			"module %q placement %q target: %v",
			key.ModuleID,
			key.PlacementID,
			err,
		)
	}
	switch record.Kind {
	case KindLink:
		if _, err := cleanStoredAbsolute("resolved_target", record.ResolvedTarget); err != nil {
			return invalidf(
				"module %q link %q: %v",
				key.ModuleID,
				key.PlacementID,
				err,
			)
		}
		if _, err := cleanStoredAbsolute("link_destination", record.LinkDestination); err != nil {
			return invalidf(
				"module %q link %q: %v",
				key.ModuleID,
				key.PlacementID,
				err,
			)
		}
	case KindLocal:
		if record.ResolvedTarget != "" || record.LinkDestination != "" {
			return invalidf(
				"module %q local %q must not contain link ownership fields",
				key.ModuleID,
				key.PlacementID,
			)
		}
	default:
		return invalidf(
			"module %q placement %q has unsupported kind %q",
			key.ModuleID,
			key.PlacementID,
			record.Kind,
		)
	}
	return nil
}

func validateTarget(home, target string) error {
	cleanTarget, err := cleanStoredAbsolute("target", target)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(home, cleanTarget)
	if err != nil {
		return fmt.Errorf("compare target with home: %w", err)
	}
	if relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%q is outside home %q", target, home)
	}
	return nil
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

func probeVersion(data []byte) (*big.Int, error) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(data, &members); err != nil || members == nil {
		return nil, invalidf("state must be a JSON object")
	}
	raw, exists := members["version"]
	if !exists {
		return nil, invalidf("required top-level version is missing")
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, invalidf("version must be a positive integer")
	}
	number, ok := value.(json.Number)
	if !ok || !integerPattern.MatchString(number.String()) {
		return nil, invalidf("version must be a positive integer")
	}
	version, ok := new(big.Int).SetString(number.String(), 10)
	if !ok || version.Sign() <= 0 {
		return nil, invalidf("version must be a positive integer")
	}
	return version, nil
}

func validateObjectShapes(data []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil || root == nil {
		return fmt.Errorf("state must be a JSON object")
	}
	if err := rejectUnknownMembers(root, "top-level state", []string{
		"version",
		"home",
		"records",
	}); err != nil {
		return err
	}
	recordsRaw, exists := root["records"]
	if !exists {
		return nil
	}
	if bytes.Equal(bytes.TrimSpace(recordsRaw), []byte("null")) {
		return fmt.Errorf("records must be a JSON array")
	}
	var records []json.RawMessage
	if err := json.Unmarshal(recordsRaw, &records); err != nil || records == nil {
		return fmt.Errorf("records must be a JSON array")
	}
	for index, recordRaw := range records {
		name := fmt.Sprintf("record %d", index)
		record, err := decodeObject(recordRaw, name)
		if err != nil {
			return err
		}
		if err := rejectUnknownMembers(
			record,
			name,
			[]string{
				"module_id",
				"placement_id",
				"kind",
				"target",
				"resolved_target",
				"link_destination",
			},
		); err != nil {
			return err
		}
		for _, field := range sortedKeys(record) {
			if bytes.Equal(bytes.TrimSpace(record[field]), []byte("null")) {
				return fmt.Errorf("%s field %q must not be null", name, field)
			}
		}
	}
	return nil
}

func decodeObject(data []byte, name string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return nil, fmt.Errorf("%s must be a JSON object", name)
	}
	return object, nil
}

func validateJSONText(data []byte) error {
	if !utf8.Valid(data) {
		return fmt.Errorf("state JSON contains invalid UTF-8")
	}
	insideString := false
	for index := 0; index < len(data); {
		switch {
		case !insideString:
			insideString = data[index] == '"'
			index++
		case data[index] == '"':
			insideString = false
			index++
		case data[index] != '\\':
			index++
		default:
			if index+1 >= len(data) {
				return fmt.Errorf("state JSON ends in an incomplete escape")
			}
			if data[index+1] != 'u' {
				index += 2
				continue
			}
			unit, err := decodeUTF16Escape(data, index)
			if err != nil {
				return err
			}
			switch {
			case unit >= 0xd800 && unit <= 0xdbff:
				if index+12 > len(data) || data[index+6] != '\\' || data[index+7] != 'u' {
					return fmt.Errorf("high UTF-16 surrogate is not followed by a low surrogate")
				}
				low, err := decodeUTF16Escape(data, index+6)
				if err != nil {
					return err
				}
				if low < 0xdc00 || low > 0xdfff {
					return fmt.Errorf("high UTF-16 surrogate is followed by a non-low surrogate")
				}
				index += 12
			case unit >= 0xdc00 && unit <= 0xdfff:
				return fmt.Errorf("low UTF-16 surrogate has no preceding high surrogate")
			default:
				index += 6
			}
		}
	}
	return nil
}

func decodeUTF16Escape(data []byte, start int) (uint16, error) {
	if start+6 > len(data) {
		return 0, fmt.Errorf("incomplete UTF-16 escape")
	}
	var value uint16
	for _, character := range data[start+2 : start+6] {
		value <<= 4
		switch {
		case character >= '0' && character <= '9':
			value |= uint16(character - '0')
		case character >= 'a' && character <= 'f':
			value |= uint16(character-'a') + 10
		case character >= 'A' && character <= 'F':
			value |= uint16(character-'A') + 10
		default:
			return 0, fmt.Errorf("invalid UTF-16 escape")
		}
	}
	return value, nil
}

func rejectUnknownMembers(
	object map[string]json.RawMessage,
	name string,
	allowed []string,
) error {
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, field := range allowed {
		allowedSet[field] = struct{}{}
	}
	for _, field := range sortedKeys(object) {
		if _, exists := allowedSet[field]; !exists {
			return fmt.Errorf("%s has unknown field %q", name, field)
		}
	}
	return nil
}

func rejectDuplicateMembers(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	if token, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return fmt.Errorf("decode trailing JSON: %w", err)
		}
		return fmt.Errorf("unexpected trailing JSON token %v", token)
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode JSON token: %w", err)
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		members := make(map[string]struct{})
		for decoder.More() {
			nameToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("decode JSON member name: %w", err)
			}
			name, ok := nameToken.(string)
			if !ok {
				return fmt.Errorf("JSON object member name is not a string")
			}
			if _, exists := members[name]; exists {
				return fmt.Errorf("duplicate JSON member %q", name)
			}
			members[name] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		return consumeDelimiter(decoder, '}')
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		return consumeDelimiter(decoder, ']')
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
}

func consumeDelimiter(decoder *json.Decoder, want json.Delim) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode closing JSON delimiter: %w", err)
	}
	if token != want {
		return fmt.Errorf("closing JSON delimiter is %v, want %q", token, want)
	}
	return nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return fmt.Errorf("decode trailing JSON: %w", err)
		}
		return fmt.Errorf("state contains trailing JSON")
	}
	return nil
}

func sortedKeys[Value any](values map[string]Value) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func sortedStateKeys(records map[Key]Record) []Key {
	keys := make([]Key, 0, len(records))
	for key := range records {
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
