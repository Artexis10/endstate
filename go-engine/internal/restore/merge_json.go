// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// RestoreMergeJson implements the merge-json restore strategy. It deep-merges
// source JSON into target JSON, with sorted keys and 2-space indentation.
func RestoreMergeJson(entry RestoreAction, source, target string, opts RestoreOptions) (*RestoreResult, error) {
	result := &RestoreResult{
		Source: source,
		Target: target,
	}

	// Check source exists.
	if _, err := os.Stat(source); os.IsNotExist(err) {
		result.Status = "failed"
		result.Error = fmt.Sprintf("source not found: %s", source)
		return result, nil
	}

	// Read source JSON.
	sourceData, err := os.ReadFile(source)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("cannot read source: %v", err)
		return result, nil
	}

	var sourceObj interface{}
	if err := json.Unmarshal(sourceData, &sourceObj); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("source JSON parse error: %v", err)
		return result, nil
	}

	// Read target JSON (empty object if file doesn't exist).
	var targetObj interface{} = map[string]interface{}{}
	if _, err := os.Stat(target); err == nil {
		targetData, readErr := os.ReadFile(target)
		if readErr != nil {
			result.Status = "failed"
			result.Error = fmt.Sprintf("cannot read target: %v", readErr)
			return result, nil
		}
		if len(targetData) > 0 {
			if err := json.Unmarshal(targetData, &targetObj); err != nil {
				// Target exists but isn't valid JSON — will be overwritten.
				targetObj = map[string]interface{}{}
			}
		}
	}

	// Perform deep merge.
	merged := DeepMerge(targetObj, sourceObj)

	// Serialize with sorted keys and 2-space indent.
	mergedBytes, err := marshalSorted(merged)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("JSON marshal error: %v", err)
		return result, nil
	}

	// Check if already up-to-date.
	if _, err := os.Stat(target); err == nil {
		existingData, readErr := os.ReadFile(target)
		if readErr == nil {
			var existingObj interface{}
			if json.Unmarshal(existingData, &existingObj) == nil {
				existingBytes, marshalErr := marshalSorted(existingObj)
				if marshalErr == nil && string(existingBytes) == string(mergedBytes) {
					result.Status = "skipped_up_to_date"
					return result, nil
				}
			}
		}
	}

	// Dry-run.
	if opts.DryRun {
		result.Status = "restored"
		return result, nil
	}

	// Backup target if exists and backup requested.
	if entry.Backup {
		if _, statErr := os.Stat(target); statErr == nil {
			backupDir := opts.BackupDir
			if backupDir == "" {
				backupDir = filepath.Join("state", "backups", opts.RunID)
			}
			backupPath, backupErr := CreateBackup(target, backupDir)
			if backupErr != nil {
				result.Status = "failed"
				result.Error = fmt.Sprintf("backup failed: %v", backupErr)
				return result, nil
			}
			result.BackupPath = backupPath
			result.BackupCreated = true
		}
	}

	// Write merged JSON atomically.
	targetDir := filepath.Dir(target)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("cannot create target directory: %v", err)
		return result, nil
	}

	tmpPath := target + ".tmp"
	if err := os.WriteFile(tmpPath, append(mergedBytes, '\n'), 0644); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("write failed: %v", err)
		return result, nil
	}
	if err := os.Rename(tmpPath, target); err != nil {
		os.Remove(tmpPath)
		result.Status = "failed"
		result.Error = fmt.Sprintf("atomic rename failed: %v", err)
		return result, nil
	}

	result.Status = "restored"
	return result, nil
}

// DeepMerge recursively merges source into target. Objects merge keys;
// arrays replace; scalars overwrite from source.
func DeepMerge(target, source interface{}) interface{} {
	if source == nil {
		return target
	}
	if target == nil {
		return source
	}

	targetMap, targetIsMap := target.(map[string]interface{})
	sourceMap, sourceIsMap := source.(map[string]interface{})

	if targetIsMap && sourceIsMap {
		result := make(map[string]interface{})
		// Copy all target keys.
		for k, v := range targetMap {
			result[k] = v
		}
		// Merge source keys.
		for k, v := range sourceMap {
			if existing, exists := result[k]; exists {
				result[k] = DeepMerge(existing, v)
			} else {
				result[k] = v
			}
		}
		return result
	}

	// For arrays and scalars, source wins.
	return source
}

// marshalSorted serializes an object as JSON with sorted keys and 2-space
// indentation.
func marshalSorted(v interface{}) ([]byte, error) {
	sorted := sortKeys(v)
	return json.MarshalIndent(sorted, "", "  ")
}

// sortKeys recursively sorts map keys for deterministic output.
func sortKeys(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		sorted := newOrderedMap()
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sorted.keys = append(sorted.keys, k)
			sorted.values[k] = sortKeys(val[k])
		}
		return sorted
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, item := range val {
			result[i] = sortKeys(item)
		}
		return result
	default:
		return v
	}
}

// orderedMap is a JSON-serializable map that preserves key insertion order.
type orderedMap struct {
	keys   []string
	values map[string]interface{}
}

func newOrderedMap() *orderedMap {
	return &orderedMap{values: make(map[string]interface{})}
}

// MarshalJSON implements json.Marshaler for orderedMap, producing a JSON
// object with keys in their insertion order.
func (om *orderedMap) MarshalJSON() ([]byte, error) {
	var buf []byte
	buf = append(buf, '{')
	for i, key := range om.keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		keyBytes, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		buf = append(buf, keyBytes...)
		buf = append(buf, ':')
		valBytes, err := json.Marshal(om.values[key])
		if err != nil {
			return nil, err
		}
		buf = append(buf, valBytes...)
	}
	buf = append(buf, '}')
	return buf, nil
}
