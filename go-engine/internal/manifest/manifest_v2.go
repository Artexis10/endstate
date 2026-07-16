// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path"
	"regexp"
	"strconv"
	"strings"
)

const (
	ManifestDiagnosticMissingVersion       = "MISSING_VERSION"
	ManifestDiagnosticInvalidVersionType   = "INVALID_VERSION_TYPE"
	ManifestDiagnosticUnsupportedVersion   = "UNSUPPORTED_VERSION"
	ManifestDiagnosticVersionMismatch      = "MANIFEST_VERSION_MISMATCH"
	ManifestDiagnosticCapturesRequireV2    = "CONFIG_CAPTURES_REQUIRE_V2"
	ManifestDiagnosticInvalidConfigCapture = "INVALID_CONFIG_CAPTURE"
)

var (
	manifestStableIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[-._][a-z0-9]+)*$`)
	lowerHex256Pattern      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	windowsVolumePattern    = regexp.MustCompile(`^[A-Za-z]:`)
)

// ManifestValidationError is a stable typed loader error for version dispatch
// and manifest-v2 provenance validation.
type ManifestValidationError struct {
	Code   string
	Path   string
	Detail string
}

func (e *ManifestValidationError) Error() string {
	if e.Path == "" {
		return "manifest: " + e.Detail
	}
	return fmt.Sprintf("manifest: validation error in %q: %s", e.Path, e.Detail)
}

// ManifestDiagnosticCode extracts a stable loader diagnostic code.
func ManifestDiagnosticCode(err error) string {
	var validationErr *ManifestValidationError
	if errors.As(err, &validationErr) {
		return validationErr.Code
	}
	return ""
}

func manifestValidationError(filePath, code, format string, args ...any) error {
	return &ManifestValidationError{Code: code, Path: filePath, Detail: fmt.Sprintf(format, args...)}
}

func parseManifestVersion(raw map[string]json.RawMessage, filePath string, inheritedVersion int) (int, error) {
	versionRaw, exists := raw["version"]
	if !exists {
		if inheritedVersion != 0 {
			return inheritedVersion, nil
		}
		return 0, manifestValidationError(filePath, ManifestDiagnosticMissingVersion, `"version" field is required`)
	}

	decoder := json.NewDecoder(bytes.NewReader(versionRaw))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return 0, manifestValidationError(filePath, ManifestDiagnosticInvalidVersionType, `"version" must be a numeric integer`)
	}
	number, ok := decoded.(json.Number)
	if !ok {
		return 0, manifestValidationError(filePath, ManifestDiagnosticInvalidVersionType, `"version" must be a numeric integer`)
	}
	value, err := strconv.ParseFloat(number.String(), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value {
		return 0, manifestValidationError(filePath, ManifestDiagnosticUnsupportedVersion, `"version" must be integer 1 or 2`)
	}
	version := int(value)
	if version < 1 || version > 2 {
		return 0, manifestValidationError(filePath, ManifestDiagnosticUnsupportedVersion, `unsupported manifest version %d; supported versions are 1 and 2`, version)
	}
	if inheritedVersion != 0 && version != inheritedVersion {
		return 0, manifestValidationError(filePath, ManifestDiagnosticVersionMismatch, "included manifest version %d does not match parent version %d", version, inheritedVersion)
	}
	return version, nil
}

func validateRawConfigCaptures(raw map[string]json.RawMessage, version int, filePath string) error {
	capturesRaw, exists := raw["configCaptures"]
	if !exists {
		return nil
	}
	var captures []json.RawMessage
	if err := json.Unmarshal(capturesRaw, &captures); err != nil {
		return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, `"configCaptures" must be an array`)
	}
	if version == 1 {
		if len(captures) > 0 {
			return manifestValidationError(filePath, ManifestDiagnosticCapturesRequireV2, "nonempty configCaptures requires manifest version 2")
		}
		return nil
	}
	for index, captureRaw := range captures {
		if err := validateRawConfigCapture(captureRaw); err != nil {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "configCaptures[%d]: %v", index, err)
		}
	}
	return nil
}

func validateRawConfigCapture(captureRaw json.RawMessage) error {
	var capture map[string]json.RawMessage
	if err := json.Unmarshal(captureRaw, &capture); err != nil || capture == nil {
		return fmt.Errorf("must be an object")
	}
	if err := requireRawFields(capture,
		"captureId", "moduleId", "configSetId", "sourceInstance",
		"sourceGeneration", "sourceGenerationFingerprint", "captureModule",
		"payloadRoot", "payloadManifest"); err != nil {
		return err
	}

	var source map[string]json.RawMessage
	if err := json.Unmarshal(capture["sourceInstance"], &source); err != nil || source == nil {
		return fmt.Errorf("sourceInstance must be an object")
	}
	if err := requireRawFields(source, "id", "detectorId", "rawVersion", "normalizedVersion", "evidence"); err != nil {
		return fmt.Errorf("sourceInstance: %w", err)
	}
	var evidence map[string]json.RawMessage
	if err := json.Unmarshal(source["evidence"], &evidence); err != nil || evidence == nil {
		return fmt.Errorf("sourceInstance.evidence must be an object")
	}
	if err := requireRawFields(evidence, "type"); err != nil {
		return fmt.Errorf("sourceInstance.evidence: %w", err)
	}

	var captureModule map[string]json.RawMessage
	if err := json.Unmarshal(capture["captureModule"], &captureModule); err != nil || captureModule == nil {
		return fmt.Errorf("captureModule must be an object")
	}
	if err := requireRawFields(captureModule, "schemaVersion", "contentHash", "snapshotPath"); err != nil {
		return fmt.Errorf("captureModule: %w", err)
	}

	var payloadEntries []json.RawMessage
	if err := json.Unmarshal(capture["payloadManifest"], &payloadEntries); err != nil {
		return fmt.Errorf("payloadManifest must be an array")
	}
	for index, entryRaw := range payloadEntries {
		var entry map[string]json.RawMessage
		if err := json.Unmarshal(entryRaw, &entry); err != nil || entry == nil {
			return fmt.Errorf("payloadManifest[%d] must be an object", index)
		}
		if err := requireRawFields(entry, "relativePath", "size", "sha256"); err != nil {
			return fmt.Errorf("payloadManifest[%d]: %w", index, err)
		}
	}

	var typed ConfigCapture
	if err := json.Unmarshal(captureRaw, &typed); err != nil {
		return fmt.Errorf("has invalid field type: %w", err)
	}
	return nil
}

func requireRawFields(object map[string]json.RawMessage, fields ...string) error {
	for _, field := range fields {
		value, exists := object[field]
		if !exists || len(bytes.TrimSpace(value)) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("required field %q is missing", field)
		}
	}
	return nil
}

func validateConfigCaptures(captures []ConfigCapture, filePath string, requireAny bool) error {
	if requireAny && len(captures) == 0 {
		return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "manifest version 2 requires at least one configCapture")
	}
	seenCaptures := make(map[string]struct{}, len(captures))
	for index := range captures {
		capture := &captures[index]
		if err := validateConfigCapture(capture); err != nil {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "configCaptures[%d]: %v", index, err)
		}
		if _, exists := seenCaptures[capture.CaptureID]; exists {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "duplicate captureId %q", capture.CaptureID)
		}
		seenCaptures[capture.CaptureID] = struct{}{}
	}
	return nil
}

func validateConfigCapture(capture *ConfigCapture) error {
	for field, value := range map[string]string{
		"captureId":                 capture.CaptureID,
		"moduleId":                  capture.ModuleID,
		"configSetId":               capture.ConfigSetID,
		"sourceInstance.id":         capture.SourceInstance.ID,
		"sourceInstance.detectorId": capture.SourceInstance.DetectorID,
		"sourceGeneration":          capture.SourceGeneration,
	} {
		if !manifestStableIDPattern.MatchString(value) {
			return fmt.Errorf("%s %q is not stable lowercase identifier syntax", field, value)
		}
	}
	if !lowerHex256Pattern.MatchString(capture.SourceGenerationFingerprint) {
		return fmt.Errorf("sourceGenerationFingerprint must be lowercase 64-hex SHA-256")
	}
	expectedNormalizedVersion, rawIsNumeric := normalizeNumericManifestVersion(capture.SourceInstance.RawVersion)
	if rawIsNumeric {
		if capture.SourceInstance.NormalizedVersion != expectedNormalizedVersion {
			return fmt.Errorf(
				"sourceInstance.normalizedVersion must be canonical %q for numeric rawVersion %q",
				expectedNormalizedVersion,
				capture.SourceInstance.RawVersion,
			)
		}
	} else if capture.SourceInstance.NormalizedVersion != "" {
		return fmt.Errorf("sourceInstance.normalizedVersion must be empty when rawVersion is not numeric dotted")
	}
	if capture.SourceInstance.Evidence == nil {
		return fmt.Errorf("sourceInstance.evidence is required")
	}
	evidence := capture.SourceInstance.Evidence
	switch evidence.Type {
	case "package":
		if strings.TrimSpace(evidence.Backend) == "" || strings.TrimSpace(evidence.Ref) == "" {
			return fmt.Errorf("package evidence requires backend and ref")
		}
	case "path":
		// Machine-local roots are intentionally not persisted.
	default:
		return fmt.Errorf("sourceInstance.evidence.type %q is unsupported", evidence.Type)
	}
	if capture.CaptureModule.SchemaVersion != 2 {
		return fmt.Errorf("captureModule.schemaVersion must be 2")
	}
	if !lowerHex256Pattern.MatchString(capture.CaptureModule.ContentHash) {
		return fmt.Errorf("captureModule.contentHash must be lowercase 64-hex SHA-256")
	}
	if err := validatePortableManifestPath(capture.CaptureModule.SnapshotPath); err != nil {
		return fmt.Errorf("captureModule.snapshotPath: %w", err)
	}
	if !strings.HasPrefix(capture.CaptureModule.SnapshotPath, "provenance/modules/") {
		return fmt.Errorf("captureModule.snapshotPath must be contained under provenance/modules/")
	}
	if err := validatePortableManifestPath(capture.PayloadRoot); err != nil {
		return fmt.Errorf("payloadRoot: %w", err)
	}
	expectedRoot := path.Join("configs", capture.CaptureID)
	if capture.PayloadRoot != expectedRoot {
		return fmt.Errorf("payloadRoot must be %q", expectedRoot)
	}

	previousPath := ""
	for index, entry := range capture.PayloadManifest {
		if err := validatePortableManifestPath(entry.RelativePath); err != nil {
			return fmt.Errorf("payloadManifest[%d].relativePath: %w", index, err)
		}
		if previousPath != "" && entry.RelativePath <= previousPath {
			return fmt.Errorf("payloadManifest entries must be unique and sorted by relativePath")
		}
		previousPath = entry.RelativePath
		if entry.Size < 0 {
			return fmt.Errorf("payloadManifest[%d].size must be nonnegative", index)
		}
		if !lowerHex256Pattern.MatchString(entry.SHA256) {
			return fmt.Errorf("payloadManifest[%d].sha256 must be lowercase 64-hex SHA-256", index)
		}
	}
	return nil
}

// validateNoGenerationLegacyFallback preserves the structural safety boundary
// between explicitly attributed legacy restore actions and generation-aware
// payloads. A v2 payload can only be consumed by the generation resolver; it
// must never also be reachable through the flat restore list.
func validateNoGenerationLegacyFallback(manifest *Manifest, filePath string) error {
	roots := make([]string, 0, len(manifest.ConfigCaptures))
	generationModules := make(map[string]struct{}, len(manifest.ConfigCaptures))
	for _, capture := range manifest.ConfigCaptures {
		roots = append(roots, capture.PayloadRoot)
		generationModules[capture.ModuleID] = struct{}{}
	}
	listedModules := make(map[string]struct{}, len(manifest.ConfigModules))
	for _, moduleID := range manifest.ConfigModules {
		listedModules[moduleID] = struct{}{}
	}
	for index, restore := range manifest.Restore {
		if !manifestStableIDPattern.MatchString(restore.FromModule) {
			return manifestValidationError(
				filePath,
				ManifestDiagnosticInvalidConfigCapture,
				"restore[%d].fromModule must be a nonempty stable lowercase module ID",
				index,
			)
		}
		if _, listed := listedModules[restore.FromModule]; !listed {
			return manifestValidationError(
				filePath,
				ManifestDiagnosticInvalidConfigCapture,
				"restore[%d].fromModule %q is not listed in configModules",
				index,
				restore.FromModule,
			)
		}
		if _, generationAware := generationModules[restore.FromModule]; generationAware {
			return manifestValidationError(
				filePath,
				ManifestDiagnosticInvalidConfigCapture,
				"restore[%d].fromModule %q is generation-aware and cannot use a flat restore lane",
				index,
				restore.FromModule,
			)
		}
		if strings.TrimSpace(restore.Source) == "" {
			continue
		}
		source := path.Clean(strings.ReplaceAll(strings.TrimSpace(restore.Source), `\`, "/"))
		for _, root := range roots {
			if source == root || strings.HasPrefix(source, root+"/") {
				return manifestValidationError(
					filePath,
					ManifestDiagnosticInvalidConfigCapture,
					"restore[%d].source enters generation payload root %q",
					index,
					root,
				)
			}
		}
	}
	return nil
}

func normalizeNumericManifestVersion(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	components := strings.Split(trimmed, ".")
	for index, component := range components {
		if component == "" {
			return "", false
		}
		for _, character := range component {
			if character < '0' || character > '9' {
				return "", false
			}
		}
		component = strings.TrimLeft(component, "0")
		if component == "" {
			component = "0"
		}
		components[index] = component
	}
	for len(components) > 1 && components[len(components)-1] == "0" {
		components = components[:len(components)-1]
	}
	return strings.Join(components, "."), true
}

func validatePortableManifestPath(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "." {
		return fmt.Errorf("path is empty")
	}
	if strings.Contains(trimmed, `\`) {
		return fmt.Errorf("path must use portable forward slashes")
	}
	if strings.HasPrefix(trimmed, "/") || windowsVolumePattern.MatchString(trimmed) {
		return fmt.Errorf("path is absolute or volume-qualified")
	}
	if strings.HasPrefix(trimmed, "~") || strings.ContainsAny(trimmed, "$%") {
		return fmt.Errorf("path contains host expansion")
	}
	for _, component := range strings.Split(trimmed, "/") {
		if component == ".." {
			return fmt.Errorf("path contains parent traversal")
		}
	}
	if path.Clean(trimmed) != trimmed {
		return fmt.Errorf("path is not canonical")
	}
	return nil
}
