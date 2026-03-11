// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// IniSection represents a single INI section with ordered keys.
type IniSection struct {
	Name string
	Keys []string
	Vals map[string]string
}

// IniFile represents a parsed INI file with a global section and named
// sections.
type IniFile struct {
	Sections []*IniSection
}

// RestoreMergeIni implements the merge-ini restore strategy. It performs a
// section-aware merge of source INI into target INI, preserving existing keys
// not in source.
func RestoreMergeIni(entry RestoreAction, source, target string, opts RestoreOptions) (*RestoreResult, error) {
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

	// Read source INI.
	sourceData, err := os.ReadFile(source)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("cannot read source: %v", err)
		return result, nil
	}
	sourceIni := ParseIni(string(sourceData))

	// Read target INI (empty if doesn't exist).
	var targetIni *IniFile
	if _, statErr := os.Stat(target); statErr == nil {
		targetData, readErr := os.ReadFile(target)
		if readErr != nil {
			result.Status = "failed"
			result.Error = fmt.Sprintf("cannot read target: %v", readErr)
			return result, nil
		}
		targetIni = ParseIni(string(targetData))
	} else {
		targetIni = &IniFile{}
	}

	// Merge.
	merged := MergeIni(targetIni, sourceIni)
	mergedContent := FormatIni(merged)

	// Check if up-to-date.
	if _, statErr := os.Stat(target); statErr == nil {
		existingData, readErr := os.ReadFile(target)
		if readErr == nil {
			existingIni := ParseIni(string(existingData))
			existingContent := FormatIni(existingIni)
			if existingContent == mergedContent {
				result.Status = "skipped_up_to_date"
				return result, nil
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

	// Write merged INI atomically.
	targetDir := filepath.Dir(target)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("cannot create target directory: %v", err)
		return result, nil
	}

	tmpPath := target + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(mergedContent), 0644); err != nil {
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

// ParseIni parses INI content into an IniFile. Keys before any [section]
// header are placed in a section with empty name (the global section).
func ParseIni(content string) *IniFile {
	ini := &IniFile{}

	// Create the global section.
	global := &IniSection{Name: "", Vals: make(map[string]string)}
	ini.Sections = append(ini.Sections, global)
	current := global

	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimRight(line, "\r"))

		// Skip empty lines and comments.
		if trimmed == "" || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") {
			continue
		}

		// Section header.
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			sectionName := trimmed[1 : len(trimmed)-1]
			// Find existing section or create new one.
			found := false
			for _, s := range ini.Sections {
				if s.Name == sectionName {
					current = s
					found = true
					break
				}
			}
			if !found {
				current = &IniSection{Name: sectionName, Vals: make(map[string]string)}
				ini.Sections = append(ini.Sections, current)
			}
			continue
		}

		// Key=Value.
		eqIdx := strings.Index(trimmed, "=")
		if eqIdx > 0 {
			key := strings.TrimSpace(trimmed[:eqIdx])
			value := strings.TrimSpace(trimmed[eqIdx+1:])
			if _, exists := current.Vals[key]; !exists {
				current.Keys = append(current.Keys, key)
			}
			current.Vals[key] = value
		}
	}

	return ini
}

// MergeIni merges source sections and keys into target. Keys in matching
// sections overwrite; new sections and keys are added. Existing keys not in
// source are preserved.
func MergeIni(target, source *IniFile) *IniFile {
	result := &IniFile{}

	// Copy all target sections.
	sectionMap := make(map[string]*IniSection)
	for _, sec := range target.Sections {
		newSec := &IniSection{
			Name: sec.Name,
			Keys: make([]string, len(sec.Keys)),
			Vals: make(map[string]string),
		}
		copy(newSec.Keys, sec.Keys)
		for k, v := range sec.Vals {
			newSec.Vals[k] = v
		}
		result.Sections = append(result.Sections, newSec)
		sectionMap[sec.Name] = newSec
	}

	// Merge source sections.
	for _, srcSec := range source.Sections {
		destSec, exists := sectionMap[srcSec.Name]
		if !exists {
			destSec = &IniSection{
				Name: srcSec.Name,
				Vals: make(map[string]string),
			}
			result.Sections = append(result.Sections, destSec)
			sectionMap[srcSec.Name] = destSec
		}

		for _, key := range srcSec.Keys {
			if _, keyExists := destSec.Vals[key]; !keyExists {
				destSec.Keys = append(destSec.Keys, key)
			}
			destSec.Vals[key] = srcSec.Vals[key]
		}
	}

	return result
}

// FormatIni serializes an IniFile back to INI format with sorted sections
// and sorted keys within each section.
func FormatIni(data *IniFile) string {
	var sb strings.Builder

	// Separate global section from named sections.
	var global *IniSection
	var named []*IniSection
	for _, sec := range data.Sections {
		if sec.Name == "" {
			global = sec
		} else {
			named = append(named, sec)
		}
	}

	// Sort named sections by name.
	sort.Slice(named, func(i, j int) bool {
		return named[i].Name < named[j].Name
	})

	// Output global keys first (no section header).
	hasGlobal := global != nil && len(global.Vals) > 0
	if hasGlobal {
		keys := make([]string, 0, len(global.Vals))
		for k := range global.Vals {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sb.WriteString(k + "=" + global.Vals[k] + "\n")
		}
		if len(named) > 0 {
			sb.WriteString("\n")
		}
	}

	// Output each named section.
	for i, sec := range named {
		sb.WriteString("[" + sec.Name + "]\n")

		keys := make([]string, 0, len(sec.Vals))
		for k := range sec.Vals {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			sb.WriteString(k + "=" + sec.Vals[k] + "\n")
		}

		// Blank line between sections (but not after last).
		if i < len(named)-1 {
			sb.WriteString("\n")
		}
	}

	return strings.TrimRight(sb.String(), "\r\n")
}
