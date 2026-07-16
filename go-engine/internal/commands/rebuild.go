// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// RebuildFlags holds the parsed CLI flags for the rebuild command.
type RebuildFlags struct {
	// From is the required input: a local .zip capture bundle or a .jsonc
	// manifest path. URL input is rejected in v0.
	From string
	// DryRun previews the plan without installing, restoring, or verifying.
	DryRun bool
	// Confirm authorizes a live run (restore on, not a dry run). Without it a
	// live rebuild refuses with CONFIRMATION_REQUIRED before any mutation.
	Confirm bool
	// NoRestore installs and verifies but skips configuration restore. A
	// no-restore run is non-destructive and needs no confirmation.
	NoRestore bool
	// Events controls streaming event output. "jsonl" enables it; "" disables.
	Events string
	// BootstrapBackends authorizes installing and verifying selected absent
	// package backends before package mutation.
	BootstrapBackends bool
	// NoBootstrap forces selected absent backend lanes to be skipped.
	NoBootstrap bool
}

// RebuildBundleInfo describes the extracted capture bundle. It is nil for a
// bare-manifest rebuild (no extraction). Metadata fields are best-effort: they
// are populated from the bundle's metadata.json when present and readable, and
// omitted otherwise.
type RebuildBundleInfo struct {
	// Extracted is true when the input was a .zip that was extracted to a temp
	// directory for the duration of the pipeline.
	Extracted bool `json:"extracted"`
	// SchemaVersion is the bundle metadata schema version (best-effort).
	SchemaVersion string `json:"schemaVersion,omitempty"`
	// CapturedAt is the bundle capture timestamp (best-effort).
	CapturedAt string `json:"capturedAt,omitempty"`
	// MachineName is the machine the bundle was captured on (best-effort).
	MachineName string `json:"machineName,omitempty"`
	// EndstateVersion is the engine version that produced the bundle (best-effort).
	EndstateVersion string `json:"endstateVersion,omitempty"`
	// ConfigModulesIncluded lists the config modules bundled into the zip
	// (best-effort).
	ConfigModulesIncluded []string `json:"configModulesIncluded,omitempty"`
}

// RebuildResult is the data payload for the rebuild command JSON envelope. Apply
// and Verify carry the underlying command results (an *ApplyResult / *VerifyResult
// interface value); Verify is omitted on a dry run.
type RebuildResult struct {
	From    string             `json:"from"`
	Bundle  *RebuildBundleInfo `json:"bundle,omitempty"`
	DryRun  bool               `json:"dryRun"`
	Restore string             `json:"restore"` // "enabled" | "disabled"
	Apply   interface{}        `json:"apply"`
	Verify  interface{}        `json:"verify,omitempty"`
}

// RunRebuild executes the one-command fresh-machine flow: it resolves and
// validates the --from input, gates a live run behind --confirm (refusing
// before any mutation), extracts a bundle when needed, then composes the
// existing apply and verify pipelines.
//
// Restore is ON by default; pass --no-restore to install without touching
// configuration, or --dry-run to preview. Verify failures are data, not a
// command error: a rebuild whose post-install verification reports drift still
// returns a success envelope (precedent: schedule run).
func RunRebuild(flags RebuildFlags) (interface{}, *envelope.Error) {
	// --- 1. Validate the input (read-only checks; no side effects) ---
	from := strings.TrimSpace(flags.From)
	if from == "" {
		return nil, envelope.NewError(
			envelope.ErrManifestValidationError,
			"--from is required").
			WithRemediation("Provide a local .zip bundle or .jsonc manifest path, e.g. --from MyProfile.zip.")
	}
	if strings.Contains(from, "://") {
		return nil, envelope.NewError(
			envelope.ErrNotSupported,
			"URL input is not supported for rebuild").
			WithDetail(map[string]string{"from": from}).
			WithRemediation("URL input is not supported; download the bundle and pass a local path.")
	}
	if _, statErr := os.Stat(from); errors.Is(statErr, os.ErrNotExist) {
		return nil, envelope.NewError(
			envelope.ErrManifestNotFound,
			"The specified rebuild input does not exist.").
			WithDetail(map[string]string{"path": from}).
			WithRemediation("Check the file path and ensure the bundle or manifest exists.")
	}

	// --- 2. Confirmation gate — nothing has been mutated above this line ---
	// A live run (restore on, not a dry run) requires explicit consent. The
	// --dry-run (preview) and --no-restore (install-only) lanes are
	// non-destructive and proceed without it.
	if !flags.DryRun && !flags.NoRestore && !flags.Confirm {
		return nil, envelope.NewError(
			envelope.ErrConfirmationRequired,
			"rebuild installs apps and restores configuration; confirmation is required").
			WithRemediation("Re-run with --confirm to proceed, or --dry-run to preview.")
	}

	// --- 3. Resolve the manifest: extract a bundle, or use a bare manifest ---
	manifestPath := from
	var bundleInfo *RebuildBundleInfo
	if bundle.IsBundle(from) {
		extractedManifest, extractErr := bundle.ExtractBundle(from)
		if extractErr != nil {
			// A malformed/non-bundle zip (or one without manifest.jsonc). Surface
			// the underlying reason. ExtractBundle removes its own temp dir on
			// failure, so there is nothing to clean up here.
			return nil, envelope.NewError(
				envelope.ErrManifestParseError,
				extractErr.Error()).
				WithDetail(map[string]string{"path": from}).
				WithRemediation("Ensure the file is a valid Endstate capture bundle (.zip containing manifest.jsonc).")
		}
		manifestPath = extractedManifest
		// The extraction directory must outlive install + restore + verify; the
		// deferred cleanup runs when RunRebuild returns, on both the success and
		// the mid-pipeline error paths.
		defer os.RemoveAll(filepath.Dir(manifestPath))

		bundleInfo = &RebuildBundleInfo{Extracted: true}
		if md, ok := readBundleMetadata(filepath.Dir(manifestPath)); ok {
			bundleInfo.SchemaVersion = md.SchemaVersion
			bundleInfo.CapturedAt = md.CapturedAt
			bundleInfo.MachineName = md.MachineName
			bundleInfo.EndstateVersion = md.EndstateVersion
			bundleInfo.ConfigModulesIncluded = md.ConfigModulesIncluded
		}
	}

	// --- 4. Apply (plan → install → restore) ---
	restoreState := "enabled"
	if flags.NoRestore {
		restoreState = "disabled"
	}
	applyResult, applyErr := RunApply(rebuildApplyFlags(flags, manifestPath))
	if applyErr != nil {
		return nil, applyErr
	}

	// --- 5. Verify (skipped on dry-run) ---
	// Verify is a superset of apply's Phase 3: it re-detects apps and dispatches
	// the manifest's verify[] block and version-drift checks. Its per-item
	// failures are data (summary.fail), not an envelope error.
	var verifyResult interface{}
	if !flags.DryRun {
		vr, verifyErr := RunVerify(VerifyFlags{
			Manifest: manifestPath,
			Events:   flags.Events,
		})
		if verifyErr != nil {
			return nil, verifyErr
		}
		verifyResult = vr
	}

	// --- 6. Assemble — success even when apply/verify summaries carry failures ---
	return &RebuildResult{
		From:    from,
		Bundle:  bundleInfo,
		DryRun:  flags.DryRun,
		Restore: restoreState,
		Apply:   applyResult,
		Verify:  verifyResult,
	}, nil
}

func rebuildApplyFlags(flags RebuildFlags, manifestPath string) ApplyFlags {
	return ApplyFlags{
		Manifest:          manifestPath,
		DryRun:            flags.DryRun,
		EnableRestore:     !flags.NoRestore,
		Events:            flags.Events,
		BootstrapBackends: flags.BootstrapBackends,
		NoBootstrap:       flags.NoBootstrap,
	}
}

// readBundleMetadata best-effort reads metadata.json from an extracted bundle
// directory. metadata.json is plain JSON (not JSONC), so a direct unmarshal is
// correct here. Returns ok=false when the file is absent or unparseable; the
// caller then omits the metadata fields.
func readBundleMetadata(dir string) (*bundle.BundleMetadata, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		return nil, false
	}
	var md bundle.BundleMetadata
	if err := json.Unmarshal(data, &md); err != nil {
		return nil, false
	}
	return &md, true
}
