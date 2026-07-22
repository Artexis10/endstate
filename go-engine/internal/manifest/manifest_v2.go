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

func validateRawLegacyAssociations(raw map[string]json.RawMessage, version int, filePath string) error {
	if lanesRaw, exists := raw["legacyConfigLanes"]; exists {
		if version == 2 && bytes.Equal(bytes.TrimSpace(lanesRaw), []byte("null")) {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, `"legacyConfigLanes" must be an array`)
		}
		var lanes []json.RawMessage
		if err := json.Unmarshal(lanesRaw, &lanes); err != nil {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, `"legacyConfigLanes" must be an array`)
		}
		if version == 1 && len(lanes) > 0 {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "nonempty legacyConfigLanes requires manifest version 2")
		}
		if version == 2 {
			for index, laneRaw := range lanes {
				var lane map[string]json.RawMessage
				if err := json.Unmarshal(laneRaw, &lane); err != nil || lane == nil {
					return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "legacyConfigLanes[%d] must be an object", index)
				}
				if err := requireRawFields(lane, "captureId", "moduleId", "moduleSchemaVersion", "payloadRoot"); err != nil {
					return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "legacyConfigLanes[%d]: %v", index, err)
				}
			}
		}
	}

	if version != 1 {
		return nil
	}
	restoreRaw, exists := raw["restore"]
	if !exists {
		return nil
	}
	var restores []map[string]json.RawMessage
	if err := json.Unmarshal(restoreRaw, &restores); err != nil {
		return nil // the typed decoder reports a malformed restore shape
	}
	for index, restore := range restores {
		legacyRaw, exists := restore["legacyCaptureId"]
		if !exists || bytes.Equal(bytes.TrimSpace(legacyRaw), []byte("null")) {
			continue
		}
		var legacyCaptureID string
		if err := json.Unmarshal(legacyRaw, &legacyCaptureID); err != nil {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "restore[%d].legacyCaptureId must be a string", index)
		}
		if strings.TrimSpace(legacyCaptureID) != "" {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "restore[%d].legacyCaptureId requires manifest version 2", index)
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
	seenRoots := make(map[string]struct{}, len(captures))
	for index := range captures {
		capture := &captures[index]
		if err := validateConfigCapture(capture); err != nil {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "configCaptures[%d]: %v", index, err)
		}
		if _, exists := seenCaptures[capture.CaptureID]; exists {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "duplicate captureId %q", capture.CaptureID)
		}
		// Readable payload roots no longer mirror the (unique) captureId, so
		// guard their uniqueness directly: two captures must never resolve to
		// the same on-disk directory.
		if _, exists := seenRoots[capture.PayloadRoot]; exists {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "duplicate payloadRoot %q", capture.PayloadRoot)
		}
		seenCaptures[capture.CaptureID] = struct{}{}
		seenRoots[capture.PayloadRoot] = struct{}{}
	}
	return nil
}

// validateConfigPayloadRoot enforces that a staged config payload lives in a
// single directory directly under configs/. The directory name is a
// human-readable label plus a short hash suffix (or, for bundles written before
// readable names, the full opaque capture identity) — either way its exact
// spelling is not load-bearing, because every consumer resolves payloads through
// this PayloadRoot pointer rather than by parsing the folder name back into an
// identity. Keeping this permissive is what lets old hash-named bundles keep
// validating and restoring unchanged.
func validateConfigPayloadRoot(payloadRoot string) error {
	if err := validatePortableManifestPath(payloadRoot); err != nil {
		return err
	}
	const prefix = "configs/"
	if !strings.HasPrefix(payloadRoot, prefix) {
		return fmt.Errorf("must be a directory under configs/")
	}
	segment := payloadRoot[len(prefix):]
	if segment == "" || strings.Contains(segment, "/") {
		return fmt.Errorf("must be a single directory under configs/")
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
	if err := validateConfigPayloadRoot(capture.PayloadRoot); err != nil {
		return fmt.Errorf("payloadRoot: %w", err)
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
	return validateLegacyConfigLanes(manifest, filePath)
}

func validateLegacyConfigLanes(manifest *Manifest, filePath string) error {
	laneByID := make(map[string]LegacyConfigLane, len(manifest.LegacyConfigLanes))
	legacyModules := make(map[string]struct{}, len(manifest.LegacyConfigLanes))
	generationModules := make(map[string]struct{}, len(manifest.ConfigCaptures))
	allCaptureIDs := make(map[string]struct{}, len(manifest.ConfigCaptures)+len(manifest.LegacyConfigLanes))
	roots := make([]string, 0, len(manifest.ConfigCaptures)+len(manifest.LegacyConfigLanes))
	for _, capture := range manifest.ConfigCaptures {
		generationModules[capture.ModuleID] = struct{}{}
		allCaptureIDs[capture.CaptureID] = struct{}{}
		roots = append(roots, capture.PayloadRoot)
	}
	for index, lane := range manifest.LegacyConfigLanes {
		if !manifestStableIDPattern.MatchString(lane.CaptureID) || !manifestStableIDPattern.MatchString(lane.ModuleID) {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "legacyConfigLanes[%d] has an invalid captureId or moduleId", index)
		}
		if lane.ModuleSchemaVersion != 1 {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "legacyConfigLanes[%d].moduleSchemaVersion must be exactly 1", index)
		}
		if err := validateConfigPayloadRoot(lane.PayloadRoot); err != nil {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "legacyConfigLanes[%d].payloadRoot %q is invalid: %v", index, lane.PayloadRoot, err)
		}
		if _, duplicate := allCaptureIDs[lane.CaptureID]; duplicate {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "duplicate captureId %q across config captures and legacy lanes", lane.CaptureID)
		}
		if _, duplicate := legacyModules[lane.ModuleID]; duplicate {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "duplicate legacy lane moduleId %q", lane.ModuleID)
		}
		if _, generationAware := generationModules[lane.ModuleID]; generationAware {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "module %q cannot have both generation and legacy lanes", lane.ModuleID)
		}
		for _, existingRoot := range roots {
			if portableRootsOverlap(existingRoot, lane.PayloadRoot) {
				return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "legacy payload root %q overlaps %q", lane.PayloadRoot, existingRoot)
			}
		}
		allCaptureIDs[lane.CaptureID] = struct{}{}
		legacyModules[lane.ModuleID] = struct{}{}
		laneByID[lane.CaptureID] = lane
		roots = append(roots, lane.PayloadRoot)
	}

	listedModules := make(map[string]struct{}, len(manifest.ConfigModules))
	for index, moduleID := range manifest.ConfigModules {
		if !manifestStableIDPattern.MatchString(moduleID) {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "configModules[%d] is not a stable module ID", index)
		}
		if _, duplicate := listedModules[moduleID]; duplicate {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "configModules contains duplicate module %q", moduleID)
		}
		if _, expected := legacyModules[moduleID]; !expected {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "configModules contains module %q without a legacy lane", moduleID)
		}
		listedModules[moduleID] = struct{}{}
	}
	if len(listedModules) != len(legacyModules) {
		return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "configModules must exactly equal the legacy lane module set")
	}

	used := make(map[string]struct{}, len(laneByID))
	for index, restore := range manifest.Restore {
		if restore.LegacyCaptureID == "" && restore.FromModule == "" {
			if strings.TrimSpace(restore.Source) != "" {
				source, err := normalizeLegacyRestoreSource(restore.Source)
				if err != nil {
					return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "restore[%d].source is not a portable ordinary restore source", index)
				}
				for _, protectedRoot := range roots {
					if portableRootsOverlap(source, protectedRoot) {
						return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "restore[%d].source must not overlap config payload root %q", index, protectedRoot)
					}
				}
			}
			continue
		}
		if !manifestStableIDPattern.MatchString(restore.LegacyCaptureID) {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "restore[%d].legacyCaptureId must identify one legacy lane", index)
		}
		lane, exists := laneByID[restore.LegacyCaptureID]
		if !exists {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "restore[%d].legacyCaptureId %q does not resolve to a legacy lane", index, restore.LegacyCaptureID)
		}
		if restore.FromModule != lane.ModuleID {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "restore[%d].fromModule %q does not match legacy lane module %q", index, restore.FromModule, lane.ModuleID)
		}
		if strings.TrimSpace(restore.Source) != "" {
			source, err := normalizeLegacyRestoreSource(restore.Source)
			if err != nil || (source != lane.PayloadRoot && !strings.HasPrefix(source, lane.PayloadRoot+"/")) {
				return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "restore[%d].source must remain under legacy payload root %q", index, lane.PayloadRoot)
			}
		}
		used[lane.CaptureID] = struct{}{}
	}
	for _, lane := range manifest.LegacyConfigLanes {
		if _, exists := used[lane.CaptureID]; !exists {
			return manifestValidationError(filePath, ManifestDiagnosticInvalidConfigCapture, "legacy lane %q is not used by any flat restore entry", lane.CaptureID)
		}
	}
	return nil
}

func normalizeLegacyRestoreSource(source string) (string, error) {
	portable := strings.ReplaceAll(strings.TrimSpace(source), `\`, "/")
	portable = strings.TrimPrefix(portable, "./")
	if err := validatePortableManifestPath(portable); err != nil {
		return "", err
	}
	return portable, nil
}

func portableRootsOverlap(left, right string) bool {
	left = strings.ToLower(left)
	right = strings.ToLower(right)
	return left == right || strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/")
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
	if strings.HasPrefix(trimmed, "/") || windowsVolumePattern.MatchString(trimmed) || strings.Contains(trimmed, ":") {
		return fmt.Errorf("path is absolute, volume-qualified, or names an alternate data stream")
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
