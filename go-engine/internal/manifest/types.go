// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package manifest provides JSONC manifest loading, include resolution, and
// profile validation for the Endstate engine.
package manifest

// Manifest represents a fully-loaded Endstate provisioning manifest. The
// Version field is declared as interface{} so the validator can distinguish
// between a missing field, a wrong type, and the correct numeric 1.
type Manifest struct {
	Version       interface{}    `json:"version"`
	Name          string         `json:"name,omitempty"`
	Captured      string         `json:"captured,omitempty"`
	Apps          []App          `json:"apps"`
	Includes      []string       `json:"includes,omitempty"`
	Restore       []RestoreEntry `json:"restore,omitempty"`
	Verify        []VerifyEntry  `json:"verify,omitempty"`
	ConfigModules []string       `json:"configModules,omitempty"`
}

// App represents a single application entry in the manifest. The Refs map
// holds platform-specific package identifiers (e.g. "windows": "Vendor.App").
type App struct {
	ID      string            `json:"id"`
	Refs    map[string]string `json:"refs"`
	Driver  string            `json:"driver,omitempty"`
	Version string            `json:"version,omitempty"`
	Manual  *ManualApp        `json:"manual,omitempty"`
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
	Type     string   `json:"type"`
	Source   string   `json:"source"`
	Target   string   `json:"target"`
	Backup   bool     `json:"backup,omitempty"`
	Optional bool     `json:"optional,omitempty"`
	Exclude  []string `json:"exclude,omitempty"`
}

// VerifyEntry describes a single state assertion.
type VerifyEntry struct {
	Type      string `json:"type"`
	Command   string `json:"command,omitempty"`
	Path      string `json:"path,omitempty"`
	ValueName string `json:"valueName,omitempty"`
}
