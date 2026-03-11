// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/snapshot"
)

// CaptureFlags holds parsed CLI flags for the capture command.
type CaptureFlags struct {
	Manifest         string // existing manifest to update
	Out              string // output path
	Profile          string // profile name
	Name             string // manifest name
	Sanitize         bool
	Discover         bool
	Update           bool
	IncludeRuntimes  bool
	IncludeStoreApps bool
	Minimize         bool
	Events           string // "jsonl" or ""
}

// CaptureResult is the data payload for the capture command.
type CaptureResult struct {
	OutputPath string          `json:"outputPath"`
	AppCount   int             `json:"appCount"`
	Sanitized  bool            `json:"sanitized"`
	Manifest   CaptureManifest `json:"manifest"`
	Counts     CaptureCounts   `json:"counts"`
}

// CaptureManifest identifies the manifest that was produced.
type CaptureManifest struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// CaptureCounts aggregates filtering statistics for the capture run.
type CaptureCounts struct {
	TotalFound       int `json:"totalFound"`
	Included         int `json:"included"`
	Skipped          int `json:"skipped"`
	FilteredRuntimes int `json:"filteredRuntimes,omitempty"`
	FilteredStore    int `json:"filteredStoreApps,omitempty"`
}

// takeSnapshotFn is the function used to take a system snapshot. It defaults
// to snapshot.TakeSnapshot and can be replaced in tests to inject fake data.
var takeSnapshotFn = snapshot.TakeSnapshot

// capturedApp is an internal representation of a captured application entry
// before it is written to the output manifest.
type capturedApp struct {
	ID   string            `json:"id"`
	Refs map[string]string `json:"refs"`
	Name string            `json:"_name,omitempty"`
}

// cleanApp is the sanitized version of capturedApp without underscore-prefixed fields.
type cleanApp struct {
	ID   string            `json:"id"`
	Refs map[string]string `json:"refs"`
}

// captureManifestOutput is the manifest structure written to disk.
type captureManifestOutput struct {
	Version  int         `json:"version"`
	Name     string      `json:"name,omitempty"`
	Captured string      `json:"captured,omitempty"`
	Apps     interface{} `json:"apps"`
}

// RunCapture executes the capture command with the provided flags.
//
// The algorithm:
//  1. Emit phase("capture")
//  2. Take system snapshot via winget list
//  3. Convert snapshot apps to manifest app entries
//  4. Filter runtime packages and store IDs
//  5. If --update and --manifest: merge with existing manifest
//  6. If --sanitize: strip underscore fields, sort by id
//  7. Determine output path and write manifest
//  8. Verify file exists and is non-empty (INV-CAPTURE-2)
//  9. Emit artifact and summary events
func RunCapture(flags CaptureFlags) (interface{}, *envelope.Error) {
	runID := buildRunID("capture")
	emitter := events.NewEmitter(runID, flags.Events == "jsonl")

	// --- 1. Emit phase event (first event per event-contract.md) ---
	emitter.EmitPhase("capture")

	// --- 2. Take system snapshot ---
	snapshotApps, snapErr := takeSnapshotFn()
	if snapErr != nil {
		var execErr *exec.Error
		if errors.As(snapErr, &execErr) && errors.Is(execErr.Err, exec.ErrNotFound) {
			return nil, envelope.NewError(
				envelope.ErrWingetNotAvailable,
				"winget is not installed or not available on PATH.",
			).WithRemediation("Install winget from https://aka.ms/winget or ensure it is on your PATH.")
		}
		return nil, envelope.NewError(
			envelope.ErrCaptureFailed,
			fmt.Sprintf("Failed to take system snapshot: %v", snapErr),
		)
	}

	totalFound := len(snapshotApps)

	// --- 3. Convert and filter snapshot apps ---
	var captured []capturedApp
	filteredRuntimes := 0
	filteredStore := 0
	skipped := 0

	for _, sApp := range snapshotApps {
		// Filter runtime packages unless --include-runtimes.
		if !flags.IncludeRuntimes && snapshot.IsRuntimePackage(sApp.ID) {
			filteredRuntimes++
			skipped++
			continue
		}

		// Filter store IDs unless --include-store-apps.
		if !flags.IncludeStoreApps && snapshot.IsStoreID(sApp.ID) {
			filteredStore++
			skipped++
			continue
		}

		appID := wingetIDToManifestID(sApp.ID)

		app := capturedApp{
			ID: appID,
			Refs: map[string]string{
				"windows": sApp.ID,
			},
			Name: sApp.Name,
		}

		captured = append(captured, app)
	}

	// --- 4. Emit item events for each included app ---
	for _, app := range captured {
		emitter.EmitItem(app.Refs["windows"], "winget", "captured", "", fmt.Sprintf("Captured %s", app.Name))
	}

	// --- 5. If --update and --manifest: merge with existing manifest ---
	if flags.Update && flags.Manifest != "" {
		existingMf, loadErr := loadManifest(flags.Manifest)
		if loadErr != nil {
			return nil, loadErr
		}

		// Build set of existing windows refs for dedup.
		existingRefs := make(map[string]bool)
		for _, app := range existingMf.Apps {
			if ref, ok := app.Refs["windows"]; ok {
				existingRefs[ref] = true
			}
		}

		// Convert existing apps to capturedApp format.
		var merged []capturedApp
		for _, app := range existingMf.Apps {
			merged = append(merged, capturedApp{
				ID:   app.ID,
				Refs: app.Refs,
			})
		}

		// Append newly discovered apps that aren't already present.
		for _, app := range captured {
			winRef := app.Refs["windows"]
			if !existingRefs[winRef] {
				merged = append(merged, app)
			}
		}

		captured = merged
	}

	included := len(captured)

	// --- 6. Sanitize ---
	var outputApps interface{}
	if flags.Sanitize {
		sorted := make([]cleanApp, len(captured))
		for i, app := range captured {
			sorted[i] = cleanApp{
				ID:   app.ID,
				Refs: app.Refs,
			}
		}
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].ID < sorted[j].ID
		})
		outputApps = sorted
	} else {
		sort.Slice(captured, func(i, j int) bool {
			return captured[i].ID < captured[j].ID
		})
		outputApps = captured
	}

	// --- 7. Determine output path ---
	outputPath := resolveOutputPath(flags)

	// Determine manifest name.
	manifestName := "captured"
	if flags.Name != "" {
		manifestName = flags.Name
	} else if flags.Profile != "" {
		manifestName = flags.Profile
	}

	// Build the output manifest.
	capturedTimestamp := time.Now().UTC().Format(time.RFC3339)
	outManifest := captureManifestOutput{
		Version:  1,
		Name:     manifestName,
		Captured: capturedTimestamp,
		Apps:     outputApps,
	}

	// Write manifest as pretty-printed JSON (JSONC-compatible).
	data, marshalErr := json.MarshalIndent(outManifest, "", "  ")
	if marshalErr != nil {
		return nil, envelope.NewError(
			envelope.ErrCaptureFailed,
			fmt.Sprintf("Failed to marshal manifest: %v", marshalErr),
		)
	}

	// Ensure parent directory exists.
	dir := filepath.Dir(outputPath)
	if dir != "" && dir != "." {
		if mkdirErr := os.MkdirAll(dir, 0755); mkdirErr != nil {
			return nil, envelope.NewError(
				envelope.ErrManifestWriteFailed,
				fmt.Sprintf("Failed to create output directory: %v", mkdirErr),
			).WithRemediation("Check directory permissions and ensure the path is writable.")
		}
	}

	if writeErr := os.WriteFile(outputPath, data, 0644); writeErr != nil {
		return nil, envelope.NewError(
			envelope.ErrManifestWriteFailed,
			fmt.Sprintf("Failed to write manifest file: %v", writeErr),
		).WithRemediation("Check directory permissions and ensure the path is writable.")
	}

	// --- 8. INV-CAPTURE-2: Verify file exists and is non-empty ---
	info, statErr := os.Stat(outputPath)
	if statErr != nil || info.Size() == 0 {
		return nil, envelope.NewError(
			envelope.ErrManifestWriteFailed,
			"Manifest file is empty or does not exist after write.",
		).WithRemediation("Check disk space and directory permissions.")
	}

	// Resolve to absolute path for the artifact event.
	absPath, absErr := filepath.Abs(outputPath)
	if absErr != nil {
		absPath = outputPath
	}

	// --- 9. Emit artifact and summary events ---
	emitter.EmitArtifact("capture", "manifest", absPath)
	emitter.EmitSummary("capture", totalFound, included, skipped, 0)

	return &CaptureResult{
		OutputPath: absPath,
		AppCount:   included,
		Sanitized:  flags.Sanitize,
		Manifest: CaptureManifest{
			Name: manifestName,
			Path: absPath,
		},
		Counts: CaptureCounts{
			TotalFound:       totalFound,
			Included:         included,
			Skipped:          skipped,
			FilteredRuntimes: filteredRuntimes,
			FilteredStore:    filteredStore,
		},
	}, nil
}

// resolveOutputPath determines the output file path based on flags.
//
// Priority:
//  1. --profile: <ProfileDir>/<profile>.jsonc
//  2. --out: use as-is
//  3. Default: captured-<timestamp>.jsonc in current directory
func resolveOutputPath(flags CaptureFlags) string {
	if flags.Profile != "" {
		profileDir := config.ProfileDir()
		if profileDir != "" {
			return filepath.Join(profileDir, flags.Profile+".jsonc")
		}
	}

	if flags.Out != "" {
		return flags.Out
	}

	timestamp := time.Now().UTC().Format("20060102-150405")
	return fmt.Sprintf("captured-%s.jsonc", timestamp)
}

// wingetIDToManifestID converts a winget package ID to a manifest app ID.
// The ID is lowercased and dots are replaced with hyphens.
// Example: "Microsoft.VisualStudioCode" -> "microsoft-visualstudiocode"
func wingetIDToManifestID(wingetID string) string {
	return strings.ToLower(strings.ReplaceAll(wingetID, ".", "-"))
}
