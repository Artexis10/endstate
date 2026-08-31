// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package discovery models read-only host inventory and its reconciliation into
// portable Endstate package and settings intent.
package discovery

// SchemaVersion is the additive capture-envelope discovery schema emitted by
// this engine release.
const SchemaVersion = "1.0"

// SourceState distinguishes an absent source from a source that was available
// but failed. Treating both as an empty inventory would make partial captures
// look complete.
type SourceState string

const (
	SourceSucceeded    SourceState = "succeeded"
	SourceNotAvailable SourceState = "not_available"
	SourceFailed       SourceState = "failed"
)

// EvidenceKind describes the bounded fact an inventory source contributed.
type EvidenceKind string

const (
	EvidencePackage    EvidenceKind = "package"
	EvidenceDesktop    EvidenceKind = "desktop_entry"
	EvidenceExecutable EvidenceKind = "executable"
	EvidenceConfig     EvidenceKind = "config_path"
)

// PortabilityState reports whether an inventory/settings candidate has one
// reviewed portable package identity. Settings can remain useful when the
// package itself is unresolved.
type PortabilityState string

const (
	PortabilityResolved   PortabilityState = "resolved"
	PortabilityUnresolved PortabilityState = "unresolved"
	PortabilityConfigOnly PortabilityState = "config_only"
)

// SettingsAction is one action the engine can safely offer for a settings
// candidate. Absence of restore is how capture-only support remains explicit.
type SettingsAction string

const (
	SettingsCapture SettingsAction = "capture"
	SettingsRestore SettingsAction = "restore"
	SettingsVerify  SettingsAction = "verify"
	SettingsRevert  SettingsAction = "revert"
)

// Request carries user selection into host discovery. Adapters receive only
// normalized values; backend assembly and filesystem/command seams live in the
// discovery implementation rather than in GUI clients.
type Request struct {
	SelectedSources []string `json:"selectedSources,omitempty"`
	Only            []string `json:"only,omitempty"`
	IncludeRuntimes bool     `json:"includeRuntimes,omitempty"`
}

// SourceStatus reports one adapter's bounded outcome.
type SourceStatus struct {
	ID         string      `json:"id"`
	State      SourceState `json:"state"`
	Discovered int         `json:"discovered"`
	Ignored    int         `json:"ignored"`
	Retries    int         `json:"retries,omitempty"`
	Reason     string      `json:"reason,omitempty"`
	Message    string      `json:"message,omitempty"`
}

// Evidence is normalized inventory provenance. Raw command output and
// machine-local absolute roots deliberately have no representation here.
type Evidence struct {
	Source      string       `json:"source"`
	Kind        EvidenceKind `json:"kind,omitempty"`
	Ref         string       `json:"ref"`
	Version     string       `json:"version,omitempty"`
	DisplayName string       `json:"displayName,omitempty"`
	UserFacing  bool         `json:"userFacing,omitempty"`
}

// InventoryItem is the normalized, source-qualified output of one adapter
// before reviewed catalog resolution.
type InventoryItem struct {
	Source      string       `json:"source"`
	Kind        EvidenceKind `json:"kind"`
	Ref         string       `json:"ref"`
	Version     string       `json:"version,omitempty"`
	DisplayName string       `json:"displayName,omitempty"`
	UserFacing  bool         `json:"userFacing,omitempty"`
	Explicit    bool         `json:"explicit,omitempty"`
}

// ResolvedIntent keeps desired Nix realization separate from the native
// package evidence that led to it.
type ResolvedIntent struct {
	ID              string     `json:"id"`
	DisplayName     string     `json:"displayName"`
	Attribute       string     `json:"attribute"`
	Input           string     `json:"input"`
	Installable     string     `json:"installable"`
	MappingRevision string     `json:"mappingRevision"`
	Evidence        []Evidence `json:"evidence"`
}

// UnresolvedItem is user-facing inventory for which no safe package intent was
// established. Reason is a stable product code such as mapping_missing.
type UnresolvedItem struct {
	ID          string     `json:"id"`
	DisplayName string     `json:"displayName"`
	Reason      string     `json:"reason"`
	Message     string     `json:"message"`
	Evidence    []Evidence `json:"evidence"`
}

// SettingsCandidate describes a trusted settings lane independently from
// package normalization.
type SettingsCandidate struct {
	ID          string           `json:"id"`
	ModuleID    string           `json:"moduleId,omitempty"`
	DisplayName string           `json:"displayName"`
	PackageID   string           `json:"packageId,omitempty"`
	Portability PortabilityState `json:"portability"`
	Actions     []SettingsAction `json:"actions"`
	Evidence    []Evidence       `json:"evidence,omitempty"`
}

// SettingsFilePlan is private execution data joining a reviewed portable
// target to the live host file that supplied it. Source is intentionally
// excluded from every public capture/discovery schema.
type SettingsFilePlan struct {
	CandidateID  string `json:"-"`
	AdapterID    string `json:"-"`
	Target       string `json:"-"`
	Source       string `json:"-"`
	Optional     bool   `json:"-"`
	ObservedSize int64  `json:"-"`
	// ObservedSHA256 binds collection to the exact bounded bytes discovery saw.
	// It is private execution state and never enters the public envelope.
	ObservedSHA256 string `json:"-"`
}

// Warning is a source- or item-scoped product-language diagnostic. Backend
// diagnostics may identify the source but never retain arbitrary raw output.
type Warning struct {
	Code    string `json:"code"`
	Source  string `json:"source,omitempty"`
	ItemID  string `json:"itemId,omitempty"`
	Message string `json:"message"`
}

// Counts makes filtering and unresolved state visible to CLI and GUI clients.
type Counts struct {
	Discovered int `json:"discovered"`
	Resolved   int `json:"resolved"`
	Selected   int `json:"selected"`
	Unresolved int `json:"unresolved"`
	Ignored    int `json:"ignored"`
	Settings   int `json:"settings"`
}

// Result is the deterministic additive discovery object returned by capture.
type Result struct {
	SchemaVersion string              `json:"schemaVersion"`
	Sources       []SourceStatus      `json:"sources"`
	Resolved      []ResolvedIntent    `json:"resolved"`
	Settings      []SettingsCandidate `json:"settings"`
	Unresolved    []UnresolvedItem    `json:"unresolved"`
	Warnings      []Warning           `json:"warnings"`
	Counts        Counts              `json:"counts"`
	SettingsFiles []SettingsFilePlan  `json:"-"`
}
