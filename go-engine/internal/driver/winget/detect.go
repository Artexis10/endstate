// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package winget

import (
	"errors"
	"os/exec"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/snapshot"
)

// ErrWingetNotAvailable is returned when the winget binary cannot be found on
// PATH. Callers should surface this as WINGET_NOT_AVAILABLE in the JSON
// envelope rather than treating it as a per-package failure.
var ErrWingetNotAvailable = errors.New("winget is not installed or not available on PATH")

// Detect checks whether the package identified by ref is currently installed
// and returns the human-readable display name when available.
//
// It runs:
//
//	winget list --id <ref> -e --accept-source-agreements
//
// and interprets the exit code:
//   - 0  → installed (true, displayName, nil)
//   - non-zero → not installed (false, "", nil)
//   - binary not found → (false, "", ErrWingetNotAvailable)
//
// The display name is extracted from the Name column of the tabular output.
// If the output cannot be parsed, the display name is returned as "".
func (w *WingetDriver) Detect(ref string) (bool, string, error) {
	cmd := w.ExecCommand(
		"winget",
		"list",
		"--id", ref,
		"-e",
		"--accept-source-agreements",
	)

	var stdout strings.Builder
	cmd.Stdout = &stdout

	err := cmd.Run()
	if err == nil {
		name := parseDisplayName(stdout.String())
		return true, name, nil
	}

	// Distinguish "winget not found" from "package not found".
	var execErr *exec.Error
	if errors.As(err, &execErr) && execErr.Err == exec.ErrNotFound {
		return false, "", ErrWingetNotAvailable
	}

	// Any non-zero exit code from winget means the package is not listed.
	return false, "", nil
}

// parseDisplayName extracts the display name from winget list tabular output.
// The output contains a header line with column names (Name, Id, Version, ...)
// separated by spaces. The Name column value is the substring from position 0
// to the start of the Id column, trimmed of whitespace.
//
// Returns "" if the header cannot be parsed.
func parseDisplayName(output string) string {
	// Winget writes progress spinner output using \r to overwrite lines in
	// the terminal. When captured to a pipe, each \n-delimited line may
	// contain multiple \r-separated segments from the spinner followed by
	// the actual content. Simulate terminal behaviour: for each line, split
	// on \r and take the last non-empty segment.
	rawLines := strings.Split(output, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, raw := range rawLines {
		line := resolveCarriageReturns(raw)
		lines = append(lines, line)
	}

	// Find the header line containing "Name" and "Id" columns.
	headerIdx := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Name") && strings.Contains(trimmed, "Id") {
			headerIdx = i
			break
		}
	}
	if headerIdx < 0 {
		return ""
	}

	header := lines[headerIdx]
	idCol := strings.Index(header, "Id")
	if idCol <= 0 {
		return ""
	}

	// Find the first data line after the header. Skip the dash separator line.
	dataStart := headerIdx + 1
	for dataStart < len(lines) {
		trimmed := strings.TrimSpace(lines[dataStart])
		if trimmed == "" || allDashes(trimmed) {
			dataStart++
			continue
		}
		break
	}
	if dataStart >= len(lines) {
		return ""
	}

	dataLine := lines[dataStart]
	if len(dataLine) < idCol {
		return ""
	}

	name := strings.TrimSpace(dataLine[:idCol])
	return name
}

// resolveCarriageReturns simulates terminal \r behaviour: when a line
// contains carriage returns, later segments overwrite earlier ones.
// Returns the last non-empty \r-separated segment.
func resolveCarriageReturns(line string) string {
	if !strings.Contains(line, "\r") {
		return line
	}
	parts := strings.Split(line, "\r")
	// Walk backwards to find the last non-empty segment.
	for i := len(parts) - 1; i >= 0; i-- {
		s := strings.TrimSpace(parts[i])
		if s != "" {
			return parts[i]
		}
	}
	return ""
}

// allDashes returns true if s consists entirely of dash characters and spaces.
func allDashes(s string) bool {
	for _, c := range s {
		if c != '-' && c != ' ' && c != '\t' {
			return false
		}
	}
	return len(s) > 0
}

// DetectBatch checks multiple package refs in a single winget list call.
// It runs `winget list --source winget` once via snapshot.TakeSnapshot(),
// then matches each ref against the results. This is dramatically faster
// than calling Detect() per ref (1 process spawn vs N).
func (w *WingetDriver) DetectBatch(refs []string) (map[string]driver.DetectResult, error) {
	apps, err := snapshot.TakeSnapshot()
	if err != nil {
		return nil, err
	}

	// Build case-insensitive lookup: winget ID → display name.
	installed := make(map[string]string, len(apps))
	for _, app := range apps {
		installed[strings.ToLower(app.ID)] = app.Name
	}

	results := make(map[string]driver.DetectResult, len(refs))
	for _, ref := range refs {
		name, found := installed[strings.ToLower(ref)]
		results[ref] = driver.DetectResult{
			Installed:   found,
			DisplayName: name,
		}
	}
	return results, nil
}
