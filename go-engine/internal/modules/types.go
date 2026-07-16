// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package modules provides config module catalog loading, app matching, and
// manifest expansion for the Endstate Go engine. Config modules define reusable
// restore/verify/capture configurations for applications and are stored as
// module.jsonc files under modules/apps/<id>/.
package modules

import (
	"bytes"
	"encoding/json"
)

// Module represents a parsed config module definition from module.jsonc.
type Module struct {
	ModuleSchemaVersion int           `json:"moduleSchemaVersion,omitempty"`
	ID                  string        `json:"id"`
	DisplayName         string        `json:"displayName"`
	Sensitivity         string        `json:"sensitivity"`
	Matches             MatchCriteria `json:"matches"`
	Verify              []VerifyDef   `json:"verify,omitempty"`
	Restore             []RestoreDef  `json:"restore,omitempty"`
	Capture             *CaptureDef   `json:"capture,omitempty"`
	Secrets             *SecretsDef   `json:"secrets,omitempty"`
	Notes               string        `json:"notes,omitempty"`
	Config              *ConfigDef    `json:"config,omitempty"`
	Curation            *CurationDef  `json:"curation,omitempty"`

	// FilePath is the absolute path to the module.jsonc file (set at load time).
	FilePath string `json:"-"`
	// ModuleDir is the directory containing the module.jsonc file (set at load time).
	ModuleDir string `json:"-"`
	// Revision is the SHA-256 hash of the canonical parsed module JSON.
	Revision string `json:"-"`
	// Unversioned distinguishes schema-v1 modules from generation-aware modules.
	Unversioned bool `json:"-"`
	// canonicalSnapshot pins the parsed declarative module bytes at catalog load.
	// It is intentionally private so callers cannot mutate the catalog snapshot.
	canonicalSnapshot []byte
}

// CurationDef carries repository-maintenance metadata used to reproduce and
// audit curated module fixtures. It is declarative metadata, not executable
// restore behavior.
type CurationDef struct {
	Seed            *CurationSeedDef `json:"seed,omitempty"`
	SnapshotRoots   []string         `json:"snapshotRoots,omitempty"`
	ExcludePatterns []string         `json:"excludePatterns,omitempty"`
}

type CurationSeedDef struct {
	Type   string `json:"type"`
	Script string `json:"script"`
}

// EffectiveSchemaVersion returns the module's interpreted schema version.
// Omitted moduleSchemaVersion is the backward-compatible schema-v1 form.
func (m *Module) EffectiveSchemaVersion() int {
	if m == nil || m.ModuleSchemaVersion == 0 {
		return 1
	}
	return m.ModuleSchemaVersion
}

// CanonicalSnapshot returns a defensive copy of the immutable canonical module
// definition pinned when this Module was parsed.
func (m *Module) CanonicalSnapshot() []byte {
	if m == nil {
		return nil
	}
	return append([]byte(nil), m.canonicalSnapshot...)
}

// ConfigDef contains the optional generation-aware schema-v2 declarations.
type ConfigDef struct {
	InstanceDetectors []InstanceDetectorDef `json:"instanceDetectors,omitempty"`
	Sets              []ConfigSetDef        `json:"sets"`
}

// InstanceDetectorDef declares one engine-owned source of application/config
// instances. The first schema-v2 release supports package and path detectors.
type InstanceDetectorDef struct {
	ID             string `json:"id"`
	Type           string `json:"type"`
	Glob           string `json:"glob,omitempty"`
	VersionPattern string `json:"versionPattern,omitempty"`
}

// ConfigSetDef is an independently evolving family of application settings.
// Generation IDs are scoped to the containing module and config set.
type ConfigSetDef struct {
	ID          string             `json:"id"`
	DisplayName string             `json:"displayName,omitempty"`
	Generations []GenerationDef    `json:"generations"`
	Migrations  []MigrationEdgeDef `json:"migrations,omitempty"`
}

// GenerationDef describes one stable configuration layout/meaning.
type GenerationDef struct {
	ID                        string               `json:"id"`
	Order                     int                  `json:"order"`
	Matches                   []VersionSelectorDef `json:"matches,omitempty"`
	AcceptsSourceFingerprints []string             `json:"acceptsSourceFingerprints,omitempty"`
	Capture                   *CaptureDef          `json:"capture,omitempty"`
	Restore                   []RestoreDef         `json:"restore,omitempty"`
	Validate                  []ValidationDef      `json:"validate,omitempty"`
	RequiresAppClosed         bool                 `json:"requiresAppClosed,omitempty"`

	// Fingerprint is computed from the canonical generation definition. The
	// accepted-history list and other runtime/computed fields are excluded.
	Fingerprint string `json:"-"`
}

// VersionSelectorDef maps preserved vendor-version evidence to a generation.
// Exactly one of VersionRange and VersionPattern may be set per selector.
type VersionSelectorDef struct {
	VersionRange   string `json:"versionRange,omitempty"`
	VersionPattern string `json:"versionPattern,omitempty"`
}

// MigrationEdgeDef declares an explicit forward edge within one config set.
type MigrationEdgeDef struct {
	From       string                  `json:"from"`
	To         string                  `json:"to"`
	Operations []MigrationOperationDef `json:"operations"`
	Validate   []ValidationDef         `json:"validate"`
}

// MigrationOperationDef is the declarative data accepted by the engine-owned
// migration operation registry. Fields are shared across the file/JSON/INI
// operation families; validation enforces the allowlist and staging paths.
type MigrationOperationDef struct {
	Type        string `json:"type"`
	Source      string `json:"source,omitempty"`
	Target      string `json:"target,omitempty"`
	Path        string `json:"path,omitempty"`
	JSONPath    string `json:"jsonPath,omitempty"`
	From        string `json:"from,omitempty"`
	To          string `json:"to,omitempty"`
	Section     string `json:"section,omitempty"`
	Key         string `json:"key,omitempty"`
	FromSection string `json:"fromSection,omitempty"`
	FromKey     string `json:"fromKey,omitempty"`
	ToSection   string `json:"toSection,omitempty"`
	ToKey       string `json:"toKey,omitempty"`
	Value       any    `json:"value,omitempty"`
	valueSet    bool
}

// UnmarshalJSON preserves whether value was explicitly supplied. JSON null is
// a valid json-set value; an omitted value is malformed declarative intent.
func (operation *MigrationOperationDef) UnmarshalJSON(data []byte) error {
	type wire MigrationOperationDef
	var decoded wire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*operation = MigrationOperationDef(decoded)
	_, operation.valueSet = fields["value"]
	return nil
}

// MarshalJSON retains an explicitly supplied null value in canonical module
// snapshots and generation fingerprints.
func (operation MigrationOperationDef) MarshalJSON() ([]byte, error) {
	type wire MigrationOperationDef
	data, err := json.Marshal(wire(operation))
	if err != nil || !operation.valueSet || operation.Value != nil {
		return data, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	fields["value"] = json.RawMessage("null")
	return json.Marshal(fields)
}

// ValidationDef declares an engine-owned validation primitive for a staged or
// committed generation.
type ValidationDef struct {
	Type     string `json:"type"`
	Path     string `json:"path"`
	JSONPath string `json:"jsonPath,omitempty"`
	Section  string `json:"section,omitempty"`
	Key      string `json:"key,omitempty"`
}

// MatchCriteria defines how a module is matched to installed applications.
type MatchCriteria struct {
	Winget               []string `json:"winget,omitempty"`
	Exe                  []string `json:"exe,omitempty"`
	UninstallDisplayName []string `json:"uninstallDisplayName,omitempty"`
	PathExists           []string `json:"pathExists,omitempty"`
}

// RestoreDef describes a single configuration restore operation within a module.
//
// For the value-level registry-set restore type, Source/Target are unused; the
// operation is fully described by Key, ValueName, ValueType, and Data.
type RestoreDef struct {
	Type     string   `json:"type"`
	Source   string   `json:"source"`
	Target   string   `json:"target"`
	Pattern  string   `json:"pattern,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	Backup   bool     `json:"backup,omitempty"`
	Optional bool     `json:"optional,omitempty"`
	Exclude  []string `json:"exclude,omitempty"`

	// registry-set fields (value-level Windows OS-settings ops). Key is an HKCU
	// key path; ValueName/ValueType/Data describe the single named value to set.
	Key       string `json:"key,omitempty"`
	ValueName string `json:"valueName,omitempty"`
	ValueType string `json:"valueType,omitempty"`
	Data      string `json:"data,omitempty"`
}

// VerifyDef describes a single state assertion within a module.
//
// For the value-level registry-value-equals verify type, ValueType/Data carry
// the expected named-value type and data to compare against.
type VerifyDef struct {
	Type      string `json:"type"`
	Path      string `json:"path,omitempty"`
	Command   string `json:"command,omitempty"`
	ValueName string `json:"valueName,omitempty"`
	ValueType string `json:"valueType,omitempty"`
	Data      string `json:"data,omitempty"`
}

// CaptureDef describes the capture configuration for a module.
type CaptureDef struct {
	Files          []CaptureFile          `json:"files"`
	RegistryKeys   []CaptureRegistryKey   `json:"registryKeys,omitempty"`
	RegistryValues []CaptureRegistryValue `json:"registryValues,omitempty"`
	ExcludeGlobs   []string               `json:"excludeGlobs,omitempty"`
}

// CaptureFile describes a single file mapping for capture operations.
type CaptureFile struct {
	Source   string `json:"source"`
	Dest     string `json:"dest"`
	Optional bool   `json:"optional,omitempty"`
}

// CaptureRegistryKey describes a single registry key mapping for capture operations.
type CaptureRegistryKey struct {
	Key      string `json:"key"`
	Dest     string `json:"dest"`
	Optional bool   `json:"optional,omitempty"`
}

// CaptureRegistryValue describes a single named registry value to read during
// capture (value-level, not whole-key). Key is an HKCU key path and ValueName
// is the value to read. Unlike CaptureRegistryKey, this reads a single value's
// current type/data rather than exporting the whole key.
type CaptureRegistryValue struct {
	Key       string `json:"key"`
	ValueName string `json:"valueName"`
	Optional  bool   `json:"optional,omitempty"`
}

// SecretsDef describes files that must never be bundled or auto-restored.
type SecretsDef struct {
	Files    []string `json:"files,omitempty"`
	Warning  string   `json:"warning,omitempty"`
	Restorer string   `json:"restorer,omitempty"`
}
