// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package commands contains the handler implementations for each Endstate CLI
// command. Each handler returns (data interface{}, err *envelope.Error) so that
// main.go can wrap the result in a standard envelope.
package commands

import (
	"github.com/Artexis10/endstate/go-engine/internal/backup"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// CapabilitiesData is the data payload returned by the capabilities command.
// It matches the shape defined in docs/contracts/cli-json-contract.md and
// docs/contracts/gui-integration-contract.md.
type CapabilitiesData struct {
	SupportedSchemaVersions SchemaVersionRange        `json:"supportedSchemaVersions"`
	Commands                map[string]CommandInfo    `json:"commands"`
	Features                FeaturesInfo              `json:"features"`
	Platform                PlatformInfo              `json:"platform"`
	GitCommit               *string                   `json:"gitCommit"`
	GitDirty                bool                      `json:"gitDirty"`
	BootstrapTimestamp      *string                   `json:"bootstrapTimestamp"`
}

// SchemaVersionRange expresses the inclusive range of JSON schema versions this
// CLI supports.
type SchemaVersionRange struct {
	Min string `json:"min"`
	Max string `json:"max"`
}

// CommandInfo describes a single supported command and the flags it accepts.
type CommandInfo struct {
	Supported bool     `json:"supported"`
	Flags     []string `json:"flags"`
}

// FeaturesInfo is the features capability map returned in the capabilities response.
type FeaturesInfo struct {
	Streaming       bool                 `json:"streaming"`
	ParallelInstall bool                 `json:"parallelInstall"`
	ConfigModules   bool                 `json:"configModules"`
	JSONOutput      bool                 `json:"jsonOutput"`
	ManualApps      bool                 `json:"manualApps"`
	HostedBackup    HostedBackupFeature  `json:"hostedBackup"`
}

// HostedBackupFeature is the GUI-facing capability advertisement for the
// Hosted Backup feature. The GUI gates its hosted-backup UI on this block
// (contract §11). Issuer/Audience are populated at runtime so a self-host
// configuration shows up correctly without rebuilding the engine.
type HostedBackupFeature struct {
	Supported        bool   `json:"supported"`
	MinSchemaVersion string `json:"minSchemaVersion"`
	IssuerURL        string `json:"issuerUrl"`
	Audience         string `json:"audience"`
}

// PlatformInfo describes the host operating system and available package manager
// drivers.
type PlatformInfo struct {
	OS      string   `json:"os"`
	Drivers []string `json:"drivers"`
}

// RunCapabilities executes the capabilities command and returns the data payload.
// It never fails; any future dynamic enrichment (git SHA, bootstrap timestamp)
// that errors is silently omitted so the handshake always succeeds.
func RunCapabilities() (interface{}, *envelope.Error) {
	data := CapabilitiesData{
		SupportedSchemaVersions: SchemaVersionRange{
			Min: "1.0",
			Max: "1.0",
		},
		Commands: map[string]CommandInfo{
			"capabilities": {
				Supported: true,
				Flags:     []string{"--json"},
			},
			"apply": {
				Supported: true,
				Flags:     []string{"--manifest", "--dry-run", "--enable-restore", "--restore-filter", "--json", "--events"},
			},
			"verify": {
				Supported: true,
				Flags:     []string{"--manifest", "--json", "--events"},
			},
			"capture": {
				Supported: true,
				Flags:     []string{"--profile", "--out", "--name", "--sanitize", "--discover", "--update", "--include-runtimes", "--include-store-apps", "--minimize", "--manifest", "--json", "--events"},
			},
			"plan": {
				Supported: true,
				Flags:     []string{"--manifest", "--json", "--events"},
			},
			"restore": {
				Supported: true,
				Flags:     []string{"--manifest", "--restore-filter", "--json", "--events", "--filter"},
			},
			"report": {
				Supported: true,
				Flags:     []string{"--run-id", "--latest", "--last", "--json"},
			},
			"doctor": {
				Supported: true,
				Flags:     []string{"--json"},
			},
			"profile": {
				Supported: true,
				Flags:     []string{"--json"},
			},
			"bootstrap": {
				Supported: true,
				Flags:     []string{"--json"},
			},
			"export-config": {
				Supported: true,
				Flags:     []string{"--manifest", "--export", "--dry-run", "--json", "--events"},
			},
			"validate-export": {
				Supported: true,
				Flags:     []string{"--manifest", "--export", "--json", "--events"},
			},
			"revert": {
				Supported: true,
				Flags:     []string{"--json", "--events"},
			},
			"backup": {
				Supported: true,
				Flags:     []string{"--email", "--backup-id", "--version-id", "--profile", "--name", "--to", "--confirm", "--json", "--events"},
			},
			"account": {
				Supported: true,
				Flags:     []string{"--confirm", "--json"},
			},
		},
		Features: FeaturesInfo{
			Streaming:       false,
			ParallelInstall: true,
			ConfigModules:   true,
			JSONOutput:      true,
			ManualApps:      true,
			HostedBackup: HostedBackupFeature{
				Supported:        true,
				MinSchemaVersion: "1.0",
				IssuerURL:        backup.IssuerURL(),
				Audience:         backup.Audience(),
			},
		},
		Platform: PlatformInfo{
			OS:      "windows",
			Drivers: []string{"winget"},
		},
		GitCommit:          nil,
		GitDirty:           false,
		BootstrapTimestamp: nil,
	}

	return data, nil
}
