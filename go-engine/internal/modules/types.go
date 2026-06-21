// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package modules provides config module catalog loading, app matching, and
// manifest expansion for the Endstate Go engine. Config modules define reusable
// restore/verify/capture configurations for applications and are stored as
// module.jsonc files under modules/apps/<id>/.
package modules

// Module represents a parsed config module definition from module.jsonc.
type Module struct {
	ID          string        `json:"id"`
	DisplayName string        `json:"displayName"`
	Sensitivity string        `json:"sensitivity"`
	Matches     MatchCriteria `json:"matches"`
	Verify      []VerifyDef   `json:"verify,omitempty"`
	Restore     []RestoreDef  `json:"restore,omitempty"`
	Capture     *CaptureDef   `json:"capture,omitempty"`
	Secrets     *SecretsDef   `json:"secrets,omitempty"`
	Notes       string        `json:"notes,omitempty"`

	// FilePath is the absolute path to the module.jsonc file (set at load time).
	FilePath string `json:"-"`
	// ModuleDir is the directory containing the module.jsonc file (set at load time).
	ModuleDir string `json:"-"`
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
