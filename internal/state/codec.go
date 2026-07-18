package state

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"strings"
	"time"
)

var (
	moduleNamePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	integerPattern     = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)
	sha256Pattern      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	environmentPattern = regexp.MustCompile(`\$[A-Za-z_][A-Za-z0-9_]*|\$\{[A-Za-z_][A-Za-z0-9_]*\}`)
)

type rawDocument struct {
	Version *int                         `json:"version"`
	Entries *map[string]rawEntry         `json:"entries"`
	RunOnce *map[string]rawRunOnceRecord `json:"run_once"`
}

type rawEntry struct {
	Module    *string `json:"module"`
	Kind      *string `json:"kind"`
	Source    *string `json:"source"`
	LinkDest  *string `json:"link_dest"`
	Hash      *string `json:"hash"`
	AppliedAt *string `json:"applied_at"`
}

type rawRunOnceRecord struct {
	Hash       *string `json:"hash"`
	ExecutedAt *string `json:"executed_at"`
}

// Decode 严格解码一个完整 state JSON 文档。它不读取文件系统，也不验证当前 target identity。
func Decode(data []byte) (Snapshot, error) {
	if err := rejectDuplicateMembers(data); err != nil {
		return Snapshot{}, corruptf("%v", err)
	}
	version, err := probeVersion(data)
	if err != nil {
		return Snapshot{}, err
	}
	if version.Cmp(big.NewInt(1)) > 0 {
		return Snapshot{}, fmt.Errorf("%w: found version %s, maximum supported is 1", ErrTooNew, version)
	}

	var raw rawDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return Snapshot{}, corruptf("decode state v1: %v", err)
	}
	if raw.Version == nil || *raw.Version != 1 {
		return Snapshot{}, corruptf("required top-level version must equal 1")
	}
	if raw.Entries == nil {
		return Snapshot{}, corruptf("required top-level entries must be an object")
	}
	if raw.RunOnce == nil {
		return Snapshot{}, corruptf("required top-level run_once must be an object")
	}

	entries := make(map[string]Entry, len(*raw.Entries))
	hasRendered := false
	for target, rawEntry := range *raw.Entries {
		if err := validateTargetKey(target); err != nil {
			return Snapshot{}, corruptf("entry target %q: %v", target, err)
		}
		entry, err := validateEntry(rawEntry)
		if err != nil {
			return Snapshot{}, corruptf("entry target %q: %v", target, err)
		}
		entries[target] = entry
		hasRendered = hasRendered || entry.kind == KindRendered
	}

	runOnce := make(map[string]RunOnceRecord, len(*raw.RunOnce))
	for key, rawRecord := range *raw.RunOnce {
		if err := validateRunOnceKey(key); err != nil {
			return Snapshot{}, corruptf("run_once key %q: %v", key, err)
		}
		record, err := validateRunOnceRecord(rawRecord)
		if err != nil {
			return Snapshot{}, corruptf("run_once key %q: %v", key, err)
		}
		runOnce[key] = record
	}

	if hasRendered {
		return Snapshot{}, ErrUnsupportedRendered
	}
	return Snapshot{version: 1, entries: entries, runOnce: runOnce, valid: true}, nil
}

func probeVersion(data []byte) (*big.Int, error) {
	var probe struct {
		Version json.RawMessage `json:"version"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, corruptf("decode state envelope: %v", err)
	}
	if probe.Version == nil {
		return nil, corruptf("required top-level version is missing")
	}
	var number json.Number
	decoder := json.NewDecoder(bytes.NewReader(probe.Version))
	decoder.UseNumber()
	if err := decoder.Decode(&number); err != nil || !integerPattern.MatchString(number.String()) {
		return nil, corruptf("version must be a positive integer")
	}
	version, ok := new(big.Int).SetString(number.String(), 10)
	if !ok || version.Sign() <= 0 {
		return nil, corruptf("version must be a positive integer")
	}
	return version, nil
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

func validateEntry(raw rawEntry) (Entry, error) {
	if raw.Module == nil || raw.Kind == nil || raw.Source == nil || raw.AppliedAt == nil {
		return Entry{}, fmt.Errorf("module, kind, source, and applied_at are required")
	}
	if !moduleNamePattern.MatchString(*raw.Module) {
		return Entry{}, fmt.Errorf("invalid module %q", *raw.Module)
	}
	if err := validateSource(*raw.Source, *raw.Module); err != nil {
		return Entry{}, err
	}
	if err := validateRFC3339("applied_at", *raw.AppliedAt); err != nil {
		return Entry{}, err
	}

	entry := Entry{
		module:    *raw.Module,
		kind:      Kind(*raw.Kind),
		source:    *raw.Source,
		appliedAt: *raw.AppliedAt,
	}
	switch entry.kind {
	case KindSymlink:
		if raw.LinkDest == nil || *raw.LinkDest == "" || strings.ContainsRune(*raw.LinkDest, '\x00') {
			return Entry{}, fmt.Errorf("symlink requires a non-empty link_dest without NUL")
		}
		if raw.Hash != nil {
			return Entry{}, fmt.Errorf("symlink must not contain hash")
		}
		entry.linkDest = *raw.LinkDest
	case KindRendered:
		if raw.Hash == nil || !sha256Pattern.MatchString(*raw.Hash) {
			return Entry{}, fmt.Errorf("rendered requires a supported sha256 hash")
		}
		if raw.LinkDest != nil {
			return Entry{}, fmt.Errorf("rendered must not contain link_dest")
		}
		entry.hash = *raw.Hash
	case KindScaffold:
		if raw.LinkDest != nil || raw.Hash != nil {
			return Entry{}, fmt.Errorf("scaffold must not contain ownership evidence")
		}
	default:
		return Entry{}, fmt.Errorf("unsupported kind %q", entry.kind)
	}
	return entry, nil
}

func validateRunOnceRecord(raw rawRunOnceRecord) (RunOnceRecord, error) {
	if raw.Hash == nil || raw.ExecutedAt == nil {
		return RunOnceRecord{}, fmt.Errorf("hash and executed_at are required")
	}
	if !sha256Pattern.MatchString(*raw.Hash) {
		return RunOnceRecord{}, fmt.Errorf("hash must use supported sha256 format")
	}
	if err := validateRFC3339("executed_at", *raw.ExecutedAt); err != nil {
		return RunOnceRecord{}, err
	}
	return RunOnceRecord{hash: *raw.Hash, executedAt: *raw.ExecutedAt}, nil
}

func validateTargetKey(target string) error {
	if !strings.HasPrefix(target, "~/") || environmentPattern.MatchString(target) {
		return fmt.Errorf("must be a canonical ~/ path without environment references")
	}
	return validateNormalizedRelativePath(strings.TrimPrefix(target, "~/"))
}

func validateSource(source, module string) error {
	components := strings.Split(source, "/")
	if len(components) < 3 || components[0] != "modules" || components[1] != module {
		return fmt.Errorf("source %q must be under modules/%s", source, module)
	}
	return validateNormalizedRelativePath(source)
}

func validateRunOnceKey(key string) error {
	module, script, found := strings.Cut(key, "/")
	if !found || !moduleNamePattern.MatchString(module) {
		return fmt.Errorf("must start with a valid module name and slash")
	}
	if err := validateNormalizedRelativePath(script); err != nil {
		return fmt.Errorf("invalid script path: %w", err)
	}
	return nil
}

func validateNormalizedRelativePath(path string) error {
	if path == "" || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") || strings.ContainsRune(path, '\x00') {
		return fmt.Errorf("path %q is not a non-empty normalized relative path", path)
	}
	for _, component := range strings.Split(path, "/") {
		if component == "" || component == "." || component == ".." {
			return fmt.Errorf("path %q contains a non-normal component", path)
		}
	}
	return nil
}

func validateRFC3339(name, value string) error {
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		return fmt.Errorf("%s %q is not RFC3339: %w", name, value, err)
	}
	return nil
}

func corruptf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrCorrupt, fmt.Sprintf(format, args...))
}
