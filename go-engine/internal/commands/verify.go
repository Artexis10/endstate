// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/driver/winget"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/verifier"
)

// newDriverFn is the factory used to create the package manager driver. It
// defaults to winget.New() and can be replaced in tests to inject a mock.
var newDriverFn func() driver.Driver = func() driver.Driver { return winget.New() }

// VerifyFlags holds the parsed CLI flags for the verify command.
type VerifyFlags struct {
	// Manifest is the path to the .jsonc manifest file.
	Manifest string
	// Events controls streaming event output. "jsonl" enables it; "" disables.
	Events string
}

// VerifyResult is the data payload for the verify command JSON envelope.
// Shape matches docs/contracts/cli-json-contract.md section "Command: verify".
type VerifyResult struct {
	Manifest VerifyManifestRef `json:"manifest"`
	Summary  VerifySummary     `json:"summary"`
	Results  []VerifyItem      `json:"results"`
}

// VerifyManifestRef identifies the manifest that was verified.
type VerifyManifestRef struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// VerifySummary aggregates pass/fail counts across all checked items.
type VerifySummary struct {
	Total int `json:"total"`
	Pass  int `json:"pass"`
	Fail  int `json:"fail"`
}

// VerifyItem is a single result entry in the verify response. Fields that do
// not apply to the item type are omitted from JSON output.
type VerifyItem struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Name    string `json:"name,omitempty"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// RunVerify executes the verify command with the provided flags.
//
// Algorithm (three steps matching engine/verify.ps1 and Invoke-VerifyCore in
// bin/endstate.ps1):
//
//  1. Load and parse the manifest.  Hard errors return an *envelope.Error with
//     a stable code (MANIFEST_NOT_FOUND / MANIFEST_PARSE_ERROR).
//  2. Emit a PhaseEvent("verify"), then detect each app via winget, emitting
//     an ItemEvent per app.
//  3. Emit a SummaryEvent("verify", ...) and return the VerifyResult.
//
// Partial failures (missing apps) are encoded in the result data — they do NOT
// produce a non-nil *envelope.Error; the caller sets success=false in the
// envelope based on the fail count.
func RunVerify(flags VerifyFlags) (interface{}, *envelope.Error) {
	// Build a run-scoped emitter. Streaming is enabled only when --events jsonl
	// was passed. The emitter is a no-op when disabled, so no guard is needed.
	runID := buildRunID("verify")
	emitter := events.NewEmitter(runID, flags.Events == "jsonl")

	// --- 1. Load manifest ---
	mf, envelopeErr := loadManifest(flags.Manifest)
	if envelopeErr != nil {
		return nil, envelopeErr
	}

	// --- 2. Create driver ---
	d := newDriverFn()

	// --- 3. Emit phase event (first event in stream per event-contract.md) ---
	emitter.EmitPhase("verify")

	var results []VerifyItem
	passCount := 0
	failCount := 0

	for _, app := range mf.Apps {
		ref := resolveWindowsRef(app)
		isManual := ref == "" && app.Manual != nil && app.Manual.VerifyPath != ""

		if ref == "" && !isManual {
			// No Windows ref and no manual verifyPath — skip silently.
			continue
		}

		if isManual {
			expanded, exists := checkVerifyPath(app.Manual.VerifyPath)
			item := VerifyItem{
				Type: "app",
				ID:   app.ID,
			}
			if exists {
				item.Status = "pass"
				item.Message = fmt.Sprintf("Verified at %s", expanded)
				emitter.EmitItem(app.ID, "manual", "present", "", item.Message, "")
				passCount++
			} else {
				item.Status = "fail"
				item.Reason = driver.ReasonMissing
				item.Message = fmt.Sprintf("Missing at %s", expanded)
				emitter.EmitItem(app.ID, "manual", "failed", driver.ReasonMissing, item.Message, "")
				failCount++
			}
			results = append(results, item)
			continue
		}

		installed, displayName, detectErr := d.Detect(ref)

		item := VerifyItem{
			Type: "app",
			ID:   app.ID,
			Ref:  ref,
		}

		if detectErr != nil {
			// Infrastructure error (e.g. winget not on PATH). Report as failed
			// and continue checking remaining apps.
			item.Status = "fail"
			item.Reason = driver.ReasonMissing
			item.Message = detectErr.Error()
			emitter.EmitItem(ref, d.Name(), "failed", driver.ReasonMissing, item.Message, "")
			failCount++
		} else if installed {
			item.Status = "pass"
			item.Name = displayName
			emitter.EmitItem(ref, d.Name(), "present", "", "Verified installed", displayName)
			passCount++
		} else {
			item.Status = "fail"
			item.Reason = driver.ReasonMissing
			item.Message = "Missing - not installed"
			emitter.EmitItem(ref, d.Name(), "failed", driver.ReasonMissing, item.Message, "")
			failCount++
		}

		results = append(results, item)
	}

	// --- 4. Run manifest verify entries through verifier dispatcher ---
	if len(mf.Verify) > 0 {
		verifyResults := verifier.RunVerify(mf.Verify)
		for _, vr := range verifyResults {
			item := VerifyItem{
				Type:    vr.Type,
				Status:  "fail",
				Message: vr.Message,
			}
			if vr.Pass {
				item.Status = "pass"
				passCount++
			} else {
				failCount++
			}
			results = append(results, item)
		}
	}

	total := passCount + failCount

	// --- 5. Summary event (last event in stream per event-contract.md) ---
	emitter.EmitSummary("verify", total, passCount, 0, failCount)

	return &VerifyResult{
		Manifest: VerifyManifestRef{
			Path: flags.Manifest,
			Name: mf.Name,
		},
		Summary: VerifySummary{
			Total: total,
			Pass:  passCount,
			Fail:  failCount,
		},
		Results: results,
	}, nil
}

// ---------------------------------------------------------------------------
// Shared helpers (also used by apply.go)
// ---------------------------------------------------------------------------

// loadManifest reads, JSONC-strips, and parses the manifest at path. It
// returns a structured *envelope.Error when the file is missing or malformed
// so the caller can surface a stable error code in the JSON envelope.
func loadManifest(path string) (*manifest.Manifest, *envelope.Error) {
	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		return nil, envelope.NewError(
			envelope.ErrManifestNotFound,
			"The specified manifest file does not exist.",
		).WithDetail(map[string]string{"path": path}).
			WithRemediation("Check the file path and ensure the manifest exists.")
	}

	mf, err := manifest.LoadManifest(path)
	if err != nil {
		return nil, envelope.NewError(
			envelope.ErrManifestParseError,
			"Failed to parse the manifest file.",
		).WithDetail(map[string]string{"path": path, "error": err.Error()}).
			WithRemediation("Ensure the manifest is valid JSONC.")
	}

	return mf, nil
}

// resolveWindowsRef returns the Windows-platform package ref for an app.
// It prefers app.Refs["windows"]; if absent it falls back to the first
// available ref in map iteration order. Returns "" if the map is empty.
func resolveWindowsRef(app manifest.App) string {
	if ref, ok := app.Refs["windows"]; ok && ref != "" {
		return ref
	}
	for _, ref := range app.Refs {
		if ref != "" {
			return ref
		}
	}
	return ""
}

// buildRunID constructs a simple run identifier for use with the emitter.
// Format matches envelope.BuildRunID: <command>-YYYYMMDD-HHMMSS.
func buildRunID(command string) string {
	return command + "-" + time.Now().UTC().Format("20060102-150405")
}
