// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package provision owns the engine's Provisioning Generation: a numbered,
// inspectable, install-only record of the package set committed by a successful
// apply, written for both package backends (the winget driver and the Nix
// realizer). It is the unification layer above the two backends — the engine
// owns the state and UX; backends differ only in advertised capabilities.
//
// Separation of concerns (inviolable): this package records package facts only.
// It MUST NOT read or write the config backup directory (state/backups/) or the
// restore revert journal, and it MUST NOT import internal/restore. A guard test
// enforces the import constraint.
package provision

// SchemaVersion is the schema version of the Generation record. It is owned by
// the provisioning layer and is independent of the manifest and envelope schema
// versions, so the record format can migrate on its own cadence.
const SchemaVersion = "1.0"

// ProvItem is a single package fact within a Generation.
type ProvItem struct {
	ID      string `json:"id"`
	Ref     string `json:"ref"`
	Status  string `json:"status"`            // "installed" | "present"
	Version string `json:"version,omitempty"` // best-effort; "" when the backend exposes none
}

// Generation is an engine-owned record of the package set committed by a
// successful apply. AddedRefs lists only the refs installed this run (status
// "installed"); already-present refs are recorded in Items but never in
// AddedRefs.
type Generation struct {
	SchemaVersion string     `json:"schemaVersion"`
	Number        int        `json:"number"`
	RunID         string     `json:"runId"`
	Timestamp     string     `json:"timestamp,omitempty"`
	Backend       string     `json:"backend"` // "nix" | "winget"
	Items         []ProvItem `json:"items"`
	AddedRefs     []string   `json:"addedRefs"`
	Native        string     `json:"native,omitempty"` // backend-native anchor (nix generation number); "" if none
	Partial       bool       `json:"partial"`          // true when a non-atomic backend committed a partial set
}

// Capabilities describes what a package backend can do. It is discovered at
// runtime by type-asserting a backend to CapabilityReporter, exactly like
// driver.BatchDetector.
type Capabilities struct {
	AtomicSet      bool
	NativeRollback bool
	Transactional  bool
	BatchInstall   bool
}

// CapabilityReporter is implemented by backends that advertise their
// capabilities.
type CapabilityReporter interface {
	Capabilities() Capabilities
}

// Rollbacker is implemented by backends that can roll back to a prior
// generation. It is declared here to pin the contract for a later phase; no
// backend implements it yet (native rollback is out of scope for this change).
type Rollbacker interface {
	Rollback(to int) error
}
