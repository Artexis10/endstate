// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/packagesource"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

const maxArtifactBytes = 100 << 20

func inspectCaptureArtifact(runtime *scenarioRuntime, zipPath string) (captureEvidence, *Failure) {
	if err := runtime.Plan.context.ValidateSandboxPath(zipPath); err != nil {
		return captureEvidence{}, fail(CodeIsolationFailure, "capture", "artifact", "capture artifact left validation authority")
	}
	entries, failure := readCaptureArtifactEntries(zipPath)
	if failure != nil {
		return captureEvidence{}, failure
	}
	if failure := validateArtifactConfigPayloadSet(runtime, entries); failure != nil {
		return captureEvidence{}, failure
	}
	manifestBytes := entries["manifest.jsonc"]
	if len(manifestBytes) == 0 || leaked(manifestBytes, runtime.forbiddenOutputValues()...) {
		return captureEvidence{}, fail(CodeArtifactContract, "capture", "manifest", "capture manifest is absent or leaks validation authority")
	}
	var captured manifest.Manifest
	if failure := decodeCapturedManifest(manifestBytes, &captured); failure != nil {
		return captureEvidence{}, failure
	}
	if failure := validateCapturedManifestIdentity(runtime, &captured); failure != nil {
		return captureEvidence{}, failure
	}

	counts := map[string]int{validationmatrix.AssertionProvenance: 1}
	for _, target := range runtime.Plan.Targets {
		root := targetArtifactRoot(target)
		expected, ok := targetArtifactPayloadName(target)
		if !ok {
			return captureEvidence{}, fail(CodeIsolationFailure, "capture", target.Coordinate, "directory payload left fixture authority")
		}
		payload, exists := entries[strings.ToLower(expected)]
		if !exists || string(payload) != target.Captured {
			return captureEvidence{}, fail(CodeArtifactContract, "capture", target.Coordinate, "capture ZIP lacks the exact fixture payload")
		}
		counts[validationmatrix.AssertionCaptured]++
		counts[validationmatrix.AssertionPayload]++
		if target.Directory {
			nested := strings.ToLower(root + "/" + filepath.Base(target.Resolved) + "/")
			for name := range entries {
				if strings.HasPrefix(name, nested) {
					return captureEvidence{}, fail(CodeArtifactContract, "capture", target.Coordinate, "directory payload nested itself")
				}
			}
		}
		for _, excluded := range append(append([]FixtureExcluded(nil), target.CaptureExcluded...), target.OverlappingExcluded...) {
			excludedName := strings.ToLower(root + "/" + filepath.ToSlash(excluded.Relative))
			if _, exists := entries[excludedName]; exists {
				return captureEvidence{}, fail(CodeArtifactContract, "capture", target.Coordinate, "capture-excluded fixture descendant entered the artifact")
			}
			for _, data := range entries {
				if bytes.Contains(data, []byte(excluded.Captured)) || bytes.Contains(data, []byte(excluded.Mutated)) {
					return captureEvidence{}, fail(CodeArtifactContract, "capture", target.Coordinate, "capture-excluded fixture sentinel entered the artifact")
				}
			}
		}
		for _, excluded := range target.RestoreExcluded {
			restoreName := strings.ToLower(root + "/" + filepath.ToSlash(excluded.Relative))
			payload, exists := entries[restoreName]
			if !exists || string(payload) != excluded.Captured {
				return captureEvidence{}, fail(CodeArtifactContract, "capture", target.Coordinate, "restore-excluded witness was not captured exactly")
			}
		}

		rewritten := "./configs/" + strings.TrimPrefix(filepath.ToSlash(target.Destination), "apps/")
		matched := 0
		for _, restore := range captured.Restore {
			if restore.FromModule == runtime.Module.ID && restore.Target == target.Authored && filepath.ToSlash(restore.Source) == rewritten {
				matched++
			}
		}
		if matched != 1 {
			return captureEvidence{}, fail(CodeArtifactContract, "capture", target.Coordinate, "rewritten restore entry is absent or ambiguous")
		}
		counts[validationmatrix.AssertionRewrittenRestore]++
	}
	if failure := validateCapturedProjection(runtime, &captured); failure != nil {
		return captureEvidence{}, failure
	}

	verifyManifest := filepath.Join(runtime.Root, "manifests", "captured-verify.jsonc")
	if err := safepath.AtomicWriteFile(verifyManifest, manifestBytes, 0o600); err != nil {
		return captureEvidence{}, fail(CodeIsolationFailure, "capture", "verifyManifest", "persist verification manifest")
	}
	return captureEvidence{
		ArtifactPath: zipPath, VerifyManifest: verifyManifest, AssertionCounts: counts,
	}, nil
}

func readCaptureArtifactEntries(zipPath string) (map[string][]byte, *Failure) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP is absent or malformed")
	}
	defer reader.Close()
	entries := make(map[string][]byte, len(reader.File))
	var total uint64
	for _, file := range reader.File {
		name := filepath.ToSlash(file.Name)
		if !safeArtifactName(name) || file.Mode()&os.ModeSymlink != 0 {
			return nil, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP contains an unsafe member")
		}
		if strings.HasSuffix(name, "/") {
			if file.UncompressedSize64 != 0 {
				return nil, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP directory member contains data")
			}
			continue
		}
		key := strings.ToLower(name)
		if _, duplicate := entries[key]; duplicate {
			return nil, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP contains duplicate members")
		}
		total += file.UncompressedSize64
		if total > maxArtifactBytes || file.UncompressedSize64 > maxArtifactBytes {
			return nil, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP exceeds the validation limit")
		}
		stream, err := file.Open()
		if err != nil {
			return nil, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP member cannot be opened")
		}
		data, err := io.ReadAll(io.LimitReader(stream, maxArtifactBytes+1))
		_ = stream.Close()
		if err != nil || len(data) > maxArtifactBytes || uint64(len(data)) != file.UncompressedSize64 {
			return nil, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP member is truncated or oversized")
		}
		entries[key] = data
	}
	return entries, nil
}

func validateCapturedManifestIdentity(runtime *scenarioRuntime, captured *manifest.Manifest) *Failure {
	if runtime == nil || captured == nil {
		return fail(CodeArtifactContract, "capture", "manifest", "capture manifest identity is absent")
	}
	if len(captured.Apps) != 1 || captured.Apps[0].ID != runtime.Inventory.AppID || captured.Apps[0].Refs["windows"] != runtime.Inventory.Ref || !capturedAppSourceMatches(captured.Apps[0], runtime.Inventory) {
		return fail(CodeArtifactContract, "capture", "manifest.apps", "test-mode inventory did not drive exact app selection")
	}
	appDriver := captured.Apps[0].Driver
	if appDriver == "" {
		appDriver = "winget"
	}
	if !strings.EqualFold(appDriver, runtime.Inventory.Driver) {
		return fail(CodeArtifactContract, "capture", "manifest.apps.driver", "captured app driver differs from inventory")
	}
	if len(captured.ConfigModules) != 1 || captured.ConfigModules[0] != runtime.Module.ID {
		return fail(CodeArtifactContract, "capture", "manifest.configModules", "capture manifest lacks exact module provenance")
	}
	return nil
}

func decodeCapturedManifest(raw []byte, captured *manifest.Manifest) *Failure {
	if captured == nil || json.Unmarshal(manifest.StripJsoncComments(raw), captured) != nil {
		return fail(CodeArtifactContract, "capture", "manifest", "capture manifest is malformed")
	}
	return nil
}

func validateArtifactConfigPayloadSet(runtime *scenarioRuntime, entries map[string][]byte) *Failure {
	if runtime == nil || runtime.Plan == nil {
		return fail(CodeArtifactContract, "capture", "configs", "fixture plan is absent")
	}
	expected := make(map[string]struct{}, len(runtime.Plan.Targets))
	for _, target := range runtime.Plan.Targets {
		root := targetArtifactRoot(target)
		name, ok := targetArtifactPayloadName(target)
		if !ok {
			return fail(CodeIsolationFailure, "capture", target.Coordinate, "directory payload left fixture authority")
		}
		if target.Directory {
			for _, excluded := range target.RestoreExcluded {
				expected[strings.ToLower(root+"/"+filepath.ToSlash(excluded.Relative))] = struct{}{}
			}
		}
		expected[strings.ToLower(name)] = struct{}{}
	}
	seen := 0
	for name := range entries {
		if !strings.HasPrefix(name, "configs/") {
			continue
		}
		if _, ok := expected[name]; !ok {
			return fail(CodeArtifactContract, "capture", "configs", "capture ZIP contains an unexpected config payload")
		}
		seen++
	}
	if seen != len(expected) {
		return fail(CodeArtifactContract, "capture", "configs", "capture ZIP omits an expected config payload")
	}
	return nil
}

func validateOptionalAbsentArtifactEntries(runtime *scenarioRuntime, entries map[string][]byte) *Failure {
	if runtime == nil || runtime.Plan == nil || runtime.Module == nil {
		return fail(CodeArtifactContract, "capture", "optional", "optional-absence fixture plan is absent")
	}
	expected := map[string]string{}
	for _, target := range runtime.Plan.Targets {
		if target.Optional {
			continue
		}
		root := targetArtifactRoot(target)
		name, ok := targetArtifactPayloadName(target)
		if !ok {
			return fail(CodeIsolationFailure, "capture", target.Coordinate, "directory payload left fixture authority")
		}
		if target.Directory {
			for _, excluded := range target.RestoreExcluded {
				expected[strings.ToLower(root+"/"+filepath.ToSlash(excluded.Relative))] = excluded.Captured
			}
		}
		expected[strings.ToLower(name)] = target.Captured
	}
	seen := 0
	for name, data := range entries {
		if !strings.HasPrefix(name, "configs/") {
			continue
		}
		want, ok := expected[name]
		if !ok || string(data) != want {
			return fail(CodeArtifactContract, "capture", "optional", "optional-absence ZIP contains a foreign, optional, or inexact config payload")
		}
		seen++
	}
	if seen != len(expected) {
		return fail(CodeArtifactContract, "capture", "optional", "optional-absence ZIP omits an exact required config payload")
	}
	manifestBytes := entries["manifest.jsonc"]
	if len(manifestBytes) == 0 || leaked(manifestBytes, runtime.forbiddenOutputValues()...) {
		return fail(CodeArtifactContract, "capture", "optional", "optional-absence manifest is absent or leaks validation authority")
	}
	var captured manifest.Manifest
	if failure := decodeCapturedManifest(manifestBytes, &captured); failure != nil {
		return failure
	}
	if len(expected) == 0 {
		if len(captured.ConfigModules) != 0 || len(captured.Restore) != 0 || len(captured.Verify) != 0 {
			return fail(CodeArtifactContract, "capture", "optional", "all-optional absence artifact contains a config projection")
		}
		return nil
	}
	if failure := validateCapturedManifestIdentity(runtime, &captured); failure != nil {
		return failure
	}
	return validateCapturedProjection(runtime, &captured)
}

func targetArtifactRoot(target FixtureTarget) string {
	return "configs/" + strings.TrimPrefix(filepath.ToSlash(target.Destination), "apps/")
}

func targetArtifactPayloadName(target FixtureTarget) (string, bool) {
	root := targetArtifactRoot(target)
	if !target.Directory {
		return root, true
	}
	relative, ok := targetPayloadRelative(target)
	if !ok {
		return "", false
	}
	return root + "/" + relative, true
}

func capturedAppSourceMatches(app manifest.App, inventory validationmode.Inventory) bool {
	source := strings.ToLower(strings.TrimSpace(app.Source))
	if strings.EqualFold(inventory.Driver, "winget") {
		source = packagesource.ResolveWinget(app.Refs["windows"], source)
	}
	return source == strings.ToLower(strings.TrimSpace(inventory.Source))
}

func validateCapturedProjection(runtime *scenarioRuntime, captured *manifest.Manifest) *Failure {
	if runtime == nil || runtime.Module == nil || runtime.Plan == nil || captured == nil ||
		len(captured.Restore) != len(runtime.Module.Restore) || len(captured.Verify) != len(runtime.Module.Verify) {
		return fail(CodeArtifactContract, "capture", "manifest", "captured restore or verifier projection differs from production module")
	}

	usedVerifiers := make([]bool, len(captured.Verify))
	for _, expected := range runtime.Module.Verify {
		matched := -1
		for index, actual := range captured.Verify {
			if !usedVerifiers[index] && exactVerifyProjection(actual, expected) {
				if matched >= 0 {
					return fail(CodeArtifactContract, "capture", "manifest.verify", "captured verifier projection is ambiguous")
				}
				matched = index
			}
		}
		if matched < 0 {
			return fail(CodeArtifactContract, "capture", "manifest.verify", "captured verifier projection differs from production module")
		}
		usedVerifiers[matched] = true
	}

	usedRestores := make([]bool, len(captured.Restore))
	for _, expected := range runtime.Module.Restore {
		rewritten, ok := expectedCapturedRestoreSource(runtime.Plan, expected)
		if !ok {
			return fail(CodeArtifactContract, "capture", "manifest.restore", "production restore has no exact fixture projection")
		}
		matched := -1
		for index, actual := range captured.Restore {
			if !usedRestores[index] && exactRestoreProjection(actual, expected, runtime.Module.ID, rewritten) {
				if matched >= 0 {
					return fail(CodeArtifactContract, "capture", "manifest.restore", "captured restore projection is ambiguous")
				}
				matched = index
			}
		}
		if matched < 0 {
			return fail(CodeArtifactContract, "capture", "manifest.restore", "captured restore projection differs from production module")
		}
		usedRestores[matched] = true
	}
	return nil
}

func expectedCapturedRestoreSource(plan *FixturePlan, restore modules.RestoreDef) (string, bool) {
	var rewritten string
	for _, target := range plan.Targets {
		if target.Authored != restore.Target || filepath.ToSlash(target.Destination) != payloadDestination(restore.Source) {
			continue
		}
		candidate := "./configs/" + strings.TrimPrefix(filepath.ToSlash(target.Destination), "apps/")
		if rewritten != "" {
			return "", false
		}
		rewritten = candidate
	}
	return rewritten, rewritten != ""
}

func exactVerifyProjection(actual manifest.VerifyEntry, expected modules.VerifyDef) bool {
	return actual.Type == expected.Type && actual.Path == expected.Path && actual.Command == expected.Command &&
		actual.ValueName == expected.ValueName && actual.ValueType == expected.ValueType && actual.Data == expected.Data
}

func exactRestoreProjection(actual manifest.RestoreEntry, expected modules.RestoreDef, moduleID, rewritten string) bool {
	return actual.Type == expected.Type && filepath.ToSlash(actual.Source) == rewritten && actual.Target == expected.Target &&
		actual.Pattern == expected.Pattern && actual.Reason == expected.Reason && actual.Backup == expected.Backup &&
		actual.Optional == expected.Optional && exactStrings(actual.Exclude, expected.Exclude) && actual.FromModule == moduleID &&
		actual.LegacyCaptureID == "" && actual.Key == expected.Key && actual.ValueName == expected.ValueName &&
		actual.ValueType == expected.ValueType && actual.Data == expected.Data
}

func exactStrings(actual, expected []string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for index := range expected {
		if actual[index] != expected[index] {
			return false
		}
	}
	return true
}

func safeArtifactName(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, `\`) {
		return false
	}
	trimmed := strings.TrimSuffix(name, "/")
	if trimmed == "" || strings.HasSuffix(trimmed, "/") {
		return false
	}
	for _, component := range strings.Split(trimmed, "/") {
		if component == "" || component == "." || component == ".." || strings.Contains(component, ":") {
			return false
		}
	}
	return true
}
