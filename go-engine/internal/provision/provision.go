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

import (
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// SchemaVersion is the schema version of the Generation record. It is owned by
// the provisioning layer and is independent of the manifest and envelope schema
// versions, so the record format can migrate on its own cadence.
const SchemaVersion = "2.0"

// ProvItem is a single package fact within a Generation.
type ProvItem struct {
	Resolution *realizer.InputResolution `json:"resolution,omitempty"`
	ID         string                    `json:"id"`
	Ref        string                    `json:"ref"`
	Status     string                    `json:"status"`            // "installed" | "present"
	Version    string                    `json:"version,omitempty"` // best-effort; "" when the backend exposes none
}

// PackageRecord is the source-aware package coordinate retained alongside
// legacy ref-only arrays for rollback compatibility.
type PackageRecord struct {
	Ref    string `json:"ref"`
	Source string `json:"source"`
}

// Generation is an engine-owned record of the package set committed by a
// successful apply. AddedRefs lists only the refs installed this run (status
// "installed"); already-present refs are recorded in Items but never in
// AddedRefs.
type Generation struct {
	SchemaVersion   string          `json:"schemaVersion"`
	Number          int             `json:"number"`
	RunID           string          `json:"runId"`
	Timestamp       string          `json:"timestamp,omitempty"`
	Backend         string          `json:"backend"` // "nix" | "winget" | "brew"
	Items           []ProvItem      `json:"items"`
	AddedRefs       []string        `json:"addedRefs"`
	AddedPackages   []PackageRecord `json:"addedPackages,omitempty"`
	Native          string          `json:"native,omitempty"`      // backend-native anchor (nix generation number); "" if none
	Partial         bool            `json:"partial"`               // true when state committed but the apply did not complete
	Rollback        bool            `json:"rollback,omitempty"`    // true when this generation was produced by a rollback (AddedRefs is empty)
	RemovedRefs     []string        `json:"removedRefs,omitempty"` // refs uninstalled by a best-effort (winget/brew) rollback; empty for native/apply generations
	RemovedPackages []PackageRecord `json:"removedPackages,omitempty"`

	// HomeManager records a home-manager configuration activated by this apply's
	// config stage (realizer-only, opt-in via --enable-restore). nil when no
	// config was activated. Config is recorded alongside packages so it is part of
	// the same audit trail; it stays a pointer so package-only generations omit it.
	HomeManager *HomeGenRef `json:"homeManager,omitempty"`
}

// HomeGenRef records a home-manager configuration the engine activated: the
// flakeref applied and the resulting home-manager generation number. It is the
// config analogue of the package Native anchor.
type HomeGenRef struct {
	Kind             string                    `json:"kind,omitempty"` // settings | config | flake; empty for legacy records
	Generated        bool                      `json:"generated,omitempty"`
	Platform         string                    `json:"platform,omitempty"`
	System           string                    `json:"system,omitempty"`
	ConfigIdentity   string                    `json:"configIdentity,omitempty"`
	SourceManifest   string                    `json:"sourceManifest,omitempty"`
	SettingsDigest   string                    `json:"settingsDigest,omitempty"`
	Resolution       *realizer.InputResolution `json:"resolution,omitempty"`
	ActivationOutput string                    `json:"activationOutput,omitempty"`
	// A native profile commit can precede a failing settings activation stage.
	ActivationFailed bool `json:"activationFailed,omitempty"`

	Flake string `json:"flake"`
	// Config is the user's originally-declared homeManager.config (a path to a
	// home.nix) when the activated flake was engine-generated from it; empty for a
	// directly-declared homeManager.flake. Flake records what was actually
	// activated (the generated, machine-local flakeref in the config case);
	// Config records what the user declared, so capture can round-trip it.
	Config string `json:"config,omitempty"`
	// Settings retains the embedded declaration from legacy records. New
	// generated records use SourceManifest and SettingsDigest instead.
	Settings *manifest.HomeManagerSettings `json:"settings,omitempty"`
	// Secrets is the user's declared Phase-1 documented-boundary secret REFERENCES
	// (homeManager.secrets) activated by this apply. It carries references only —
	// path / env / backend — never secret material, so capture can round-trip the
	// references without the engine ever holding or persisting the secret itself.
	Secrets    []manifest.HomeManagerSecret `json:"secrets,omitempty"`
	Generation int                          `json:"generation"`
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
