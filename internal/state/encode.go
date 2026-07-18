package state

import (
	"encoding/json"
	"fmt"
)

type encodedDocument struct {
	Version int                             `json:"version"`
	Entries map[string]encodedEntry         `json:"entries"`
	RunOnce map[string]encodedRunOnceRecord `json:"run_once"`
}

type encodedEntry struct {
	Module    string `json:"module"`
	Kind      Kind   `json:"kind"`
	Source    string `json:"source"`
	LinkDest  string `json:"link_dest,omitempty"`
	Hash      string `json:"hash,omitempty"`
	AppliedAt string `json:"applied_at"`
}

type encodedRunOnceRecord struct {
	Hash       string `json:"hash"`
	ExecutedAt string `json:"executed_at"`
}

// Encode 把有效 Snapshot 确定性编码为完整 state v1 JSON 文档。
func Encode(snapshot Snapshot) ([]byte, error) {
	if !snapshot.valid || snapshot.version != 1 {
		return nil, fmt.Errorf("encode state v1: invalid Snapshot")
	}

	document := encodedDocument{
		Version: 1,
		Entries: make(map[string]encodedEntry, len(snapshot.entries)),
		RunOnce: make(map[string]encodedRunOnceRecord, len(snapshot.runOnce)),
	}
	for target, entry := range snapshot.entries {
		document.Entries[target] = encodedEntry{
			Module:    entry.module,
			Kind:      entry.kind,
			Source:    entry.source,
			LinkDest:  entry.linkDest,
			Hash:      entry.hash,
			AppliedAt: entry.appliedAt,
		}
	}
	for key, record := range snapshot.runOnce {
		document.RunOnce[key] = encodedRunOnceRecord{
			Hash:       record.hash,
			ExecutedAt: record.executedAt,
		}
	}

	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode state v1: %w", err)
	}
	data = append(data, '\n')
	roundTripped, err := Decode(data)
	if err != nil {
		return nil, fmt.Errorf("validate encoded state v1: %w", err)
	}
	if !snapshotsEqual(snapshot, roundTripped) {
		return nil, corruptf("state Snapshot cannot be encoded without loss")
	}
	return data, nil
}

func snapshotsEqual(left, right Snapshot) bool {
	if left.version != right.version || left.valid != right.valid ||
		len(left.entries) != len(right.entries) || len(left.runOnce) != len(right.runOnce) {
		return false
	}
	for key, leftEntry := range left.entries {
		rightEntry, exists := right.entries[key]
		if !exists || leftEntry != rightEntry {
			return false
		}
	}
	for key, leftRecord := range left.runOnce {
		rightRecord, exists := right.runOnce[key]
		if !exists || leftRecord != rightRecord {
			return false
		}
	}
	return true
}
