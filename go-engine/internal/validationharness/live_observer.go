// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const maxLiveObserverOutputBytes = 16 * 1024

type LiveProcessClassification string

const (
	LiveProcessCompleted   LiveProcessClassification = "completed"
	LiveProcessNoInstalled LiveProcessClassification = "no-installed-package"
)

// LiveProcessResult deliberately leaves non-zero exit result interpretation to
// a reviewed host contract; winget list's human output is not a stable API.
type LiveProcessResult struct {
	ExitCode       int
	Stdout         []byte
	Classification LiveProcessClassification
}

type LiveProcess interface {
	Run(context.Context, string, ...string) (LiveProcessResult, error)
}

type LiveRegistryView string

const (
	LiveRegistryHKLM64 LiveRegistryView = "hklm-64"
	LiveRegistryHKLM32 LiveRegistryView = "hklm-32"
	LiveRegistryHKCU   LiveRegistryView = "hkcu"
)

type LiveUninstallRecord struct {
	View            LiveRegistryView
	DisplayName     string
	DisplayVersion  string
	InstallLocation string
	DisplayIcon     string
	Publisher       string
	UninstallString string
}

type LiveRegistry interface {
	UninstallRecords(context.Context) ([]LiveUninstallRecord, error)
}

// LivePath reconstructs the machine and user PATH from host authority. It
// must not return the current process PATH as a substitute.
type LivePath interface {
	MachineAndUserPath(context.Context) ([]string, error)
}

type LiveFileInfo struct {
	Regular      bool
	ReparsePoint bool
}

type LiveFiles interface {
	Stat(string) (LiveFileInfo, error)
	FileVersion(string) (string, error)
}

// LiveObserverDefinition is derived by a later caller from a compiled module.
// It is deliberately narrower than a module and grants no command authority.
type LiveObserverDefinition struct {
	WingetRef            string   `json:"wingetRef"`
	UninstallDisplayName []string `json:"uninstallDisplayName"`
	ExecutableNames      []string `json:"executableNames"`
}

type LiveObservationStatus string

const (
	LiveObservationAbsent          LiveObservationStatus = "absent"
	LiveObservationPresent         LiveObservationStatus = "present"
	LiveObservationMixed           LiveObservationStatus = "mixed"
	LiveObservationAmbiguous       LiveObservationStatus = "ambiguous"
	LiveObservationVersionMismatch LiveObservationStatus = "version-mismatch"
	LiveObservationFailed          LiveObservationStatus = "failed"
)

// LiveObservation contains only summary state. It intentionally excludes host
// paths, raw registry data, command output, environment values, and errors.
type LiveObservation struct {
	Status            LiveObservationStatus `json:"status"`
	WingetPresent     bool                  `json:"wingetPresent"`
	RegistryPresent   bool                  `json:"registryPresent"`
	ExecutablePresent bool                  `json:"executablePresent"`
	Ref               string                `json:"ref"`
	WingetVersion     string                `json:"wingetVersion,omitempty"`
	RegistryVersion   string                `json:"registryVersion,omitempty"`
	ExecutableVersion string                `json:"executableVersion,omitempty"`
}

// LiveObserver combines independent, injected read-only observations. It does
// not import the production winget driver, verifier, or snapshot parser.
type LiveObserver struct {
	Process  LiveProcess
	Registry LiveRegistry
	Path     LivePath
	Files    LiveFiles
}

func (observer LiveObserver) Observe(ctx context.Context, definition LiveObserverDefinition) LiveObservation {
	result := LiveObservation{Status: LiveObservationFailed, Ref: definition.WingetRef}
	if err := validateLiveObserverDefinition(definition); err != nil || observer.Process == nil || observer.Registry == nil || observer.Path == nil || observer.Files == nil {
		return result
	}

	wingetPresent, wingetVersion, wingetErr := observer.observeWinget(ctx, definition.WingetRef)
	if wingetErr != nil {
		return result
	}
	result.WingetPresent, result.WingetVersion = wingetPresent, wingetVersion

	records, err := observer.Registry.UninstallRecords(ctx)
	if err != nil {
		return result
	}
	record, registryPresent, registryAmbiguous, err := matchingLiveUninstallRecord(records, definition.UninstallDisplayName)
	if err != nil {
		return result
	}
	if registryAmbiguous {
		result.Status = LiveObservationAmbiguous
		return result
	}
	result.RegistryPresent = registryPresent
	if registryPresent {
		result.RegistryVersion, err = NormalizeLiveVersion(record.DisplayVersion)
		if err != nil {
			return result
		}
		executablePresent, executableVersion, executableAmbiguous, executableErr := observer.observeExecutable(ctx, record, definition.ExecutableNames)
		if executableErr != nil {
			return result
		}
		if executableAmbiguous {
			result.Status = LiveObservationAmbiguous
			return result
		}
		result.ExecutablePresent, result.ExecutableVersion = executablePresent, executableVersion
	}

	if !result.WingetPresent && !result.RegistryPresent && !result.ExecutablePresent {
		result.Status = LiveObservationAbsent
		return result
	}
	if !result.WingetPresent || !result.RegistryPresent || !result.ExecutablePresent {
		result.Status = LiveObservationMixed
		return result
	}
	if result.WingetVersion == "" || result.RegistryVersion == "" || result.ExecutableVersion == "" || result.WingetVersion != result.RegistryVersion || result.WingetVersion != result.ExecutableVersion {
		result.Status = LiveObservationVersionMismatch
		return result
	}
	result.Status = LiveObservationPresent
	return result
}

func (observer LiveObserver) observeWinget(ctx context.Context, ref string) (bool, string, error) {
	process, err := observer.Process.Run(ctx, "winget", "list", "--id", ref, "--exact", "--source", "winget", "--accept-source-agreements", "--disable-interactivity")
	if err != nil {
		return false, "", err
	}
	if process.Classification == LiveProcessNoInstalled {
		return false, "", nil
	}
	if process.Classification != "" && process.Classification != LiveProcessCompleted || process.ExitCode != 0 {
		return false, "", fmt.Errorf("winget result is not a reviewed completed observation")
	}
	version, err := ParseLiveWingetTable(process.Stdout, ref)
	if err != nil {
		return false, "", err
	}
	return true, version, nil
}

// ParseLiveWingetTable parses only the fixed-width layout declared by the
// separator row. Header labels are intentionally ignored because they localize.
func ParseLiveWingetTable(output []byte, ref string) (string, error) {
	if len(output) == 0 || len(output) > maxLiveObserverOutputBytes || !validLiveObserverText(string(output)) || !validLiveObserverValue(ref) {
		return "", fmt.Errorf("winget output is unsafe or bounded")
	}
	lines := strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n")
	separator, starts, ends := -1, []int(nil), []int(nil)
	for index, line := range lines {
		columnStarts, columnEnds, ok := liveWingetSeparator(line)
		if ok {
			if separator >= 0 {
				return "", fmt.Errorf("winget output has ambiguous layouts")
			}
			separator, starts, ends = index, columnStarts, columnEnds
		}
	}
	if separator < 0 {
		return "", fmt.Errorf("winget output has no tabular layout")
	}
	var rows [][]string
	for _, line := range lines[separator+1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cells, ok := liveWingetCells(line, starts, ends)
		if !ok {
			return "", fmt.Errorf("winget output is truncated or malformed")
		}
		rows = append(rows, cells)
	}
	if len(rows) != 1 {
		return "", fmt.Errorf("winget output must contain exactly one data row")
	}
	var idIndex, sourceIndex = -1, -1
	for index, cell := range rows[0] {
		if cell == ref {
			if idIndex >= 0 {
				return "", fmt.Errorf("winget row has ambiguous reference")
			}
			idIndex = index
		}
		if cell == "winget" {
			if sourceIndex >= 0 {
				return "", fmt.Errorf("winget row has ambiguous source")
			}
			sourceIndex = index
		}
	}
	if idIndex < 1 || sourceIndex < 0 || idIndex+1 >= len(rows[0]) || sourceIndex <= idIndex+1 {
		return "", fmt.Errorf("winget row does not prove exact id and source")
	}
	return NormalizeLiveVersion(rows[0][idIndex+1])
}

func liveWingetSeparator(line string) ([]int, []int, bool) {
	var starts, ends []int
	for index := 0; index < len(line); {
		if line[index] != '-' {
			index++
			continue
		}
		start := index
		for index < len(line) && line[index] == '-' {
			index++
		}
		if index-start < 3 {
			return nil, nil, false
		}
		starts, ends = append(starts, start), append(ends, index)
	}
	return starts, ends, len(starts) >= 3
}

func liveWingetCells(line string, starts, ends []int) ([]string, bool) {
	if len(line) < ends[len(ends)-1] {
		return nil, false
	}
	cells := make([]string, len(starts))
	for index := range starts {
		end := len(line)
		if index+1 < len(starts) {
			end = starts[index+1]
		}
		if end > len(line) {
			return nil, false
		}
		cells[index] = strings.TrimSpace(line[starts[index]:end])
		if !validLiveObserverValue(cells[index]) {
			return nil, false
		}
	}
	return cells, true
}

func matchingLiveUninstallRecord(records []LiveUninstallRecord, patterns []string) (LiveUninstallRecord, bool, bool, error) {
	compiled := make([]*regexp.Regexp, len(patterns))
	for index, value := range patterns {
		if !validLiveObserverValue(value) {
			return LiveUninstallRecord{}, false, false, fmt.Errorf("uninstall matcher is unsafe")
		}
		pattern, err := regexp.Compile(value)
		if err != nil {
			return LiveUninstallRecord{}, false, false, err
		}
		compiled[index] = pattern
	}
	var matches []LiveUninstallRecord
	for _, record := range records {
		if !validLiveObserverValue(record.DisplayName) {
			continue
		}
		for _, pattern := range compiled {
			if pattern.MatchString(record.DisplayName) {
				matches = append(matches, record)
				break
			}
		}
	}
	if len(matches) == 0 {
		return LiveUninstallRecord{}, false, false, nil
	}
	canonical := matches[0]
	for _, candidate := range matches[1:] {
		if !sameLiveUninstallRecord(canonical, candidate) {
			return LiveUninstallRecord{}, false, true, nil
		}
	}
	return canonical, true, false, nil
}

func sameLiveUninstallRecord(left, right LiveUninstallRecord) bool {
	return sameLiveRecordValue(left.DisplayName, right.DisplayName) && sameLiveRecordValue(left.DisplayVersion, right.DisplayVersion) &&
		sameLiveRecordValue(left.InstallLocation, right.InstallLocation) && sameLiveRecordValue(left.DisplayIcon, right.DisplayIcon) &&
		sameLiveRecordValue(left.Publisher, right.Publisher) && sameLiveRecordValue(left.UninstallString, right.UninstallString)
}

func sameLiveRecordValue(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func (observer LiveObserver) observeExecutable(ctx context.Context, record LiveUninstallRecord, names []string) (bool, string, bool, error) {
	paths, err := observer.Path.MachineAndUserPath(ctx)
	if err != nil {
		return false, "", false, err
	}
	candidates := liveExecutableCandidates(record, names, paths)
	var found []string
	for _, candidate := range candidates {
		info, statErr := observer.Files.Stat(candidate)
		if statErr == nil && info.Regular && !info.ReparsePoint {
			found = append(found, candidate)
		}
	}
	if len(found) == 0 {
		return false, "", false, nil
	}
	if len(found) != 1 {
		return false, "", true, nil
	}
	version, err := observer.Files.FileVersion(found[0])
	if err != nil {
		return false, "", false, err
	}
	normalized, err := NormalizeLiveVersion(version)
	if err != nil {
		return false, "", false, err
	}
	return true, normalized, false, nil
}

func liveExecutableCandidates(record LiveUninstallRecord, names, paths []string) []string {
	allowed := make(map[string]string, len(names))
	for _, name := range names {
		if validLiveExecutableName(name) {
			allowed[strings.ToLower(name)] = name
		}
	}
	if len(allowed) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	add := func(value string, candidates *[]string) {
		key := strings.ToLower(value)
		if _, exists := seen[key]; !exists {
			seen[key] = struct{}{}
			*candidates = append(*candidates, value)
		}
	}
	var candidates []string
	if root, ok := cleanLiveWindowsDirectory(record.InstallLocation); ok {
		for _, name := range allowed {
			add(root+`\`+name, &candidates)
		}
	}
	if icon, ok := parseLiveDisplayIcon(record.DisplayIcon); ok {
		if _, allowedIcon := allowed[strings.ToLower(liveWindowsBase(icon))]; allowedIcon {
			add(icon, &candidates)
		}
	}
	for _, entry := range paths {
		if root, ok := cleanLiveWindowsDirectory(entry); ok {
			for _, name := range allowed {
				add(root+`\`+name, &candidates)
			}
		}
	}
	sort.Slice(candidates, func(left, right int) bool {
		return strings.ToLower(candidates[left]) < strings.ToLower(candidates[right])
	})
	return candidates
}

func parseLiveDisplayIcon(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.Contains(trimmed, ",") || strings.ContainsAny(trimmed, "\r\n\t") {
		return "", false
	}
	if strings.HasPrefix(trimmed, `"`) {
		if len(trimmed) < 3 || !strings.HasSuffix(trimmed, `"`) {
			return "", false
		}
		trimmed = trimmed[1 : len(trimmed)-1]
	} else if strings.ContainsAny(trimmed, `" `) {
		return "", false
	}
	if !cleanLiveWindowsPath(trimmed) || !strings.EqualFold(strings.TrimSpace(trimmed), trimmed) || !strings.HasSuffix(strings.ToLower(trimmed), ".exe") {
		return "", false
	}
	return trimmed, true
}

func validLiveExecutableName(value string) bool {
	return validLiveObserverValue(value) && strings.EqualFold(liveWindowsBase(value), value) && strings.HasSuffix(strings.ToLower(value), ".exe")
}

func cleanLiveWindowsDirectory(value string) (string, bool) {
	value = strings.TrimSpace(value)
	value = strings.TrimRight(strings.ReplaceAll(value, "/", `\`), `\`)
	if !cleanLiveWindowsPath(value) {
		return "", false
	}
	return value, true
}

func cleanLiveWindowsPath(value string) bool {
	if len(value) < 4 || value[1] != ':' || (value[2] != '\\' && value[2] != '/') || !((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')) || strings.ContainsAny(value, `<>|?*"`) {
		return false
	}
	for _, component := range strings.Split(strings.ReplaceAll(value[3:], "/", `\`), `\`) {
		if component == "" || component == "." || component == ".." || strings.TrimRight(component, " .") != component || !validLiveObserverValue(component) {
			return false
		}
	}
	return true
}

func liveWindowsBase(value string) string {
	if index := strings.LastIndexAny(value, `\\/`); index >= 0 {
		return value[index+1:]
	}
	return value
}

// NormalizeLiveVersion permits the observer's explicitly narrow vendor format:
// numeric dotted components, one optional leading v, and insignificant zeroes.
func NormalizeLiveVersion(value string) (string, error) {
	if len(value) == 0 || len(value) > maxLiveStringBytes || strings.TrimSpace(value) != value {
		return "", fmt.Errorf("version is unsafe")
	}
	if len(value) > 0 && (value[0] == 'v' || value[0] == 'V') {
		value = value[1:]
	}
	if value == "" {
		return "", fmt.Errorf("version is empty")
	}
	parts := strings.Split(value, ".")
	for index, part := range parts {
		if part == "" {
			return "", fmt.Errorf("version is not numeric")
		}
		for _, character := range part {
			if character < '0' || character > '9' {
				return "", fmt.Errorf("version is not numeric")
			}
		}
		part = strings.TrimLeft(part, "0")
		if part == "" {
			part = "0"
		}
		parts[index] = part
	}
	for len(parts) > 1 && parts[len(parts)-1] == "0" {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, "."), nil
}

func validateLiveObserverDefinition(definition LiveObserverDefinition) error {
	if !validLiveObserverValue(definition.WingetRef) || len(definition.UninstallDisplayName) == 0 || len(definition.UninstallDisplayName) > maxLiveMappings || len(definition.ExecutableNames) == 0 || len(definition.ExecutableNames) > maxLiveMappings {
		return fmt.Errorf("live observer definition is invalid")
	}
	for _, name := range definition.ExecutableNames {
		if !validLiveExecutableName(name) {
			return fmt.Errorf("live observer executable is invalid")
		}
	}
	return nil
}

func validLiveObserverText(value string) bool {
	for _, character := range value {
		if character < 0x20 && character != '\n' && character != '\r' && character != '\t' || character == 0x7f {
			return false
		}
	}
	return true
}

func validLiveObserverValue(value string) bool {
	return value != "" && len(value) <= maxLiveStringBytes && strings.TrimSpace(value) == value && validLiveObserverText(value)
}
