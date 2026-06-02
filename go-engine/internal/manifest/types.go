// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package manifest provides JSONC manifest loading, include resolution, and
// profile validation for the Endstate engine.
package manifest

// Manifest represents a fully-loaded Endstate provisioning manifest. The
// Version field is declared as interface{} so the validator can distinguish
// between a missing field, a wrong type, and the correct numeric 1.
type Manifest struct {
	Version        interface{}    `json:"version"`
	Name           string         `json:"name,omitempty"`
	Captured       string         `json:"captured,omitempty"`
	Apps           []App          `json:"apps"`
	Includes       []string       `json:"includes,omitempty"`
	Restore        []RestoreEntry `json:"restore,omitempty"`
	Verify         []VerifyEntry  `json:"verify,omitempty"`
	ConfigModules  []string       `json:"configModules,omitempty"`
	ExcludeConfigs []string       `json:"excludeConfigs,omitempty"`

	// HomeManager declares a home-manager configuration the Nix realizer activates
	// as a config stage of apply (opt-in via --enable-restore). Absent ⇒ no config
	// stage (default apply unchanged). Realizer-only; the winget path ignores it.
	HomeManager *HomeManagerConfig `json:"homeManager,omitempty"`
}

// HomeManagerConfig is the manifest input to the home-manager config stage. Flake
// is a home-manager flakeref (e.g. "/home/me/dotfiles#hugo" or
// "github:me/dotfiles#hugo") that the engine activates with an engine-owned,
// pinned home-manager — a permanent power-user escape hatch. The orchestrator is
// input-agnostic, so engine-generated inputs (a home.nix wrapper, a programs.*
// catalog) layer on later by producing a flakeref this same stage consumes.
type HomeManagerConfig struct {
	Flake string `json:"flake"`
}

// App represents a single application entry in the manifest. The Refs map
// holds platform-specific package identifiers (e.g. "windows": "Vendor.App").
type App struct {
	ID          string            `json:"id"`
	Refs        map[string]string `json:"refs"`
	Driver      string            `json:"driver,omitempty"`
	Version     string            `json:"version,omitempty"`
	Manual      *ManualApp        `json:"manual,omitempty"`
	DisplayName string            `json:"displayName,omitempty"`
}

// ManualApp describes an app that cannot be installed automatically but can be
// verified as present via a filesystem path check.
type ManualApp struct {
	VerifyPath   string `json:"verifyPath"`
	Launch       string `json:"launch,omitempty"`
	Instructions string `json:"instructions,omitempty"`
	Fallback     string `json:"fallback,omitempty"`
}

// RestoreEntry describes a single configuration restore operation.
type RestoreEntry struct {
	Type       string   `json:"type"`
	Source     string   `json:"source"`
	Target     string   `json:"target"`
	Pattern    string   `json:"pattern,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	Backup     bool     `json:"backup,omitempty"`
	Optional   bool     `json:"optional,omitempty"`
	Exclude    []string `json:"exclude,omitempty"`
	FromModule string   `json:"fromModule,omitempty"`
}

// VerifyEntry describes a single state assertion.
type VerifyEntry struct {
	Type      string `json:"type"`
	Command   string `json:"command,omitempty"`
	Path      string `json:"path,omitempty"`
	ValueName string `json:"valueName,omitempty"`
}
