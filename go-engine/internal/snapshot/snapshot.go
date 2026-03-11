// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package snapshot captures the current system state by running winget list
// and parsing the tabular output into structured SnapshotApp entries.
package snapshot

import (
	"bufio"
	"bytes"
	"errors"
	"os/exec"
	"regexp"
	"strings"
)

// SnapshotApp represents one installed application from winget list.
type SnapshotApp struct {
	Name    string
	ID      string
	Version string
	Source  string
}

// ExecCommand is injectable for tests. Default runs actual command.
var ExecCommand = defaultExecCommand

// defaultExecCommand runs the real OS command. It captures stdout only;
// winget writes progress spinners to stderr which would corrupt the tabular
// output if captured together.
func defaultExecCommand(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil && len(out) > 0 {
		// winget may exit non-zero but still produce valid stdout.
		return out, nil
	}
	return out, err
}

// RuntimePatterns lists winget IDs that represent runtime/framework packages
// rather than user-installed applications. Capture filters these out unless
// --include-runtimes is set.
var RuntimePatterns = []string{
	"Microsoft.VCRedist.*",
	"Microsoft.VCLibs.*",
	"Microsoft.UI.Xaml.*",
	"Microsoft.DotNet.*",
	"Microsoft.WindowsAppRuntime.*",
	"Microsoft.DirectX.*",
}

// StoreIdPatterns lists regex patterns for Microsoft Store IDs (not winget-sourced).
// Capture filters these out unless --include-store-apps is set.
var StoreIdPatterns = []string{
	`^9[A-Z0-9]{10,}$`,
	`^XP[A-Z0-9]{10,}$`,
}

// compiledRuntimePatterns caches compiled regexps for RuntimePatterns.
var compiledRuntimePatterns []*regexp.Regexp

// compiledStorePatterns caches compiled regexps for StoreIdPatterns.
var compiledStorePatterns []*regexp.Regexp

func init() {
	for _, p := range RuntimePatterns {
		compiledRuntimePatterns = append(compiledRuntimePatterns, regexp.MustCompile("^"+p+"$"))
	}
	for _, p := range StoreIdPatterns {
		compiledStorePatterns = append(compiledStorePatterns, regexp.MustCompile(p))
	}
}

// IsRuntimePackage returns true if the winget ID matches a runtime/framework pattern.
func IsRuntimePackage(wingetID string) bool {
	for _, re := range compiledRuntimePatterns {
		if re.MatchString(wingetID) {
			return true
		}
	}
	return false
}

// IsStoreID returns true if the winget ID matches a Microsoft Store ID pattern.
func IsStoreID(wingetID string) bool {
	for _, re := range compiledStorePatterns {
		if re.MatchString(wingetID) {
			return true
		}
	}
	return false
}

// TakeSnapshot runs `winget list --source winget --accept-source-agreements`
// and parses the tabular output into a slice of SnapshotApp.
func TakeSnapshot() ([]SnapshotApp, error) {
	output, err := ExecCommand("winget", "list", "--source", "winget", "--accept-source-agreements")
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
			return nil, err
		}
		// winget may return non-zero exit code but still produce output.
		// If we got output, try to parse it.
		if len(output) == 0 {
			return nil, err
		}
	}

	return parseWingetList(output)
}

// GetDisplayNameMap builds a map from winget ID to display name by running
// a snapshot and extracting the Name and ID fields.
func GetDisplayNameMap() (map[string]string, error) {
	apps, err := TakeSnapshot()
	if err != nil {
		return nil, err
	}

	nameMap := make(map[string]string, len(apps))
	for _, app := range apps {
		nameMap[app.ID] = app.Name
	}
	return nameMap, nil
}

// parseWingetList parses the tabular output of `winget list`.
//
// The expected format is:
//
//	Name                             Id                                Version        Source
//	---------------------------------------------------------------------------------------------------------
//	Visual Studio Code               Microsoft.VisualStudioCode        1.85.0         winget
//	Git                              Git.Git                           2.43.0         winget
//
// The parser:
//  1. Scans for the header row containing "Name", "Id", "Version"
//  2. Records column start positions from the header
//  3. Skips the separator line (dashes)
//  4. Extracts fields by column positions for each data row
// cleanCR strips carriage-return based progress spinners from a line.
// Winget writes progress animations using \r to overwrite the same line on a
// terminal.  When captured programmatically the \r characters are preserved,
// leaving spinner garbage before the real content.  Taking everything after
// the last \r simulates what a terminal would display.
func cleanCR(line string) string {
	if i := strings.LastIndex(line, "\r"); i >= 0 {
		return line[i+1:]
	}
	return line
}

func parseWingetList(output []byte) ([]SnapshotApp, error) {
	scanner := bufio.NewScanner(bytes.NewReader(output))

	// Phase 1: Find the header row and record column positions.
	var nameCol, idCol, versionCol, sourceCol int
	headerFound := false
	var headerLine string

	for scanner.Scan() {
		line := cleanCR(scanner.Text())

		// Look for a line containing all required column headers.
		nameIdx := strings.Index(line, "Name")
		idIdx := strings.Index(line, "Id")
		versionIdx := strings.Index(line, "Version")

		if nameIdx >= 0 && idIdx > nameIdx && versionIdx > idIdx {
			headerLine = line
			nameCol = nameIdx
			idCol = idIdx
			versionCol = versionIdx

			// Source column is optional; find it if present.
			sourceIdx := strings.Index(line, "Source")
			if sourceIdx > versionIdx {
				sourceCol = sourceIdx
			}

			headerFound = true
			break
		}
	}

	if !headerFound {
		// No header found; return empty slice (not an error — winget may have no results).
		return nil, nil
	}

	_ = headerLine

	// Phase 2: Skip the separator line (all dashes/hyphens).
	if scanner.Scan() {
		sep := strings.TrimSpace(scanner.Text())
		// Verify it looks like a separator (all dashes). If not, we still continue.
		_ = sep
	}

	// Phase 3: Parse data rows.
	var apps []SnapshotApp
	for scanner.Scan() {
		line := cleanCR(scanner.Text())

		// Skip empty lines.
		if strings.TrimSpace(line) == "" {
			continue
		}

		// Skip lines shorter than the Id column start — they're malformed.
		if len(line) < idCol+1 {
			continue
		}

		name := extractColumn(line, nameCol, idCol)
		id := extractColumn(line, idCol, versionCol)

		var version, source string
		if sourceCol > 0 {
			version = extractColumn(line, versionCol, sourceCol)
			source = extractColumnToEnd(line, sourceCol)
		} else {
			version = extractColumnToEnd(line, versionCol)
		}

		// Skip entries with empty ID — they're not useful.
		if id == "" {
			continue
		}

		apps = append(apps, SnapshotApp{
			Name:    name,
			ID:      id,
			Version: version,
			Source:  source,
		})
	}

	return apps, nil
}

// extractColumn extracts text between column start and the next column start,
// trimming whitespace.
func extractColumn(line string, start, end int) string {
	if start >= len(line) {
		return ""
	}
	if end > len(line) {
		end = len(line)
	}
	return strings.TrimSpace(line[start:end])
}

// extractColumnToEnd extracts text from column start to end of line,
// trimming whitespace.
func extractColumnToEnd(line string, start int) string {
	if start >= len(line) {
		return ""
	}
	return strings.TrimSpace(line[start:])
}
