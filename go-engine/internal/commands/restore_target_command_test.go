// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

func TestRunApplyRejectsDuplicateRestoreTargetBeforePlanning(t *testing.T) {
	manifestPath := restoreTargetCommandManifest(t)

	_, envErr := RunApply(ApplyFlags{
		Manifest: manifestPath,
		DryRun:   true,
		RestoreTargets: []string{
			"capture-target=instance-one",
			"capture-target=instance-two",
		},
	})
	assertRestoreTargetCommandError(t, envErr, "duplicate_capture_id")
}

func TestRunRestoreRejectsMalformedRestoreTargetBeforeEmptyRestoreReturn(t *testing.T) {
	manifestPath := restoreTargetCommandManifest(t)

	_, envErr := RunRestore(RestoreFlags{
		Manifest:       manifestPath,
		DryRun:         true,
		RestoreTargets: []string{""},
	})
	assertRestoreTargetCommandError(t, envErr, "malformed_mapping")
}

func TestRunRebuildRejectsUnknownRestoreTargetBeforeApply(t *testing.T) {
	manifestPath := restoreTargetCommandManifest(t)

	_, envErr := RunRebuild(RebuildFlags{
		From:           manifestPath,
		DryRun:         true,
		RestoreTargets: []string{"capture-missing=instance-one"},
	})
	assertRestoreTargetCommandError(t, envErr, "unknown_capture_id")
}

func TestRunCapabilitiesAdvertisesRestoreTargetForRestoreCapableCommands(t *testing.T) {
	raw, envErr := RunCapabilities()
	if envErr != nil {
		t.Fatalf("RunCapabilities: %+v", envErr)
	}
	capabilities := raw.(CapabilitiesData)
	for _, command := range []string{"apply", "restore", "rebuild"} {
		info, exists := capabilities.Commands[command]
		if !exists {
			t.Fatalf("commands.%s is missing", command)
		}
		if countCapabilityFlag(info.Flags, "--restore-target") != 1 {
			t.Fatalf("commands.%s.flags does not advertise one --restore-target: %v", command, info.Flags)
		}
	}
	if countCapabilityFlag(capabilities.Commands["rebuild"].Flags, "--restore-filter") != 1 {
		t.Fatalf("commands.rebuild.flags lost module-level --restore-filter: %v", capabilities.Commands["rebuild"].Flags)
	}
}

func restoreTargetCommandManifest(t *testing.T) string {
	t.Helper()
	manifestDir := t.TempDir()
	capture := commandTestConfigCapture(t, manifestDir, "capture-target", "apps.target", "preferences")
	value := manifest.Manifest{
		Version:        2,
		Name:           "restore-target-command",
		Apps:           []manifest.App{},
		ConfigCaptures: []manifest.ConfigCapture{capture},
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	manifestPath := filepath.Join(manifestDir, "manifest.jsonc")
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return manifestPath
}

func assertRestoreTargetCommandError(t *testing.T, envErr *envelope.Error, wantReason string) {
	t.Helper()
	if envErr == nil || envErr.Code != envelope.ErrInvalidRestoreTarget {
		t.Fatalf("error = %+v, want %s", envErr, envelope.ErrInvalidRestoreTarget)
	}
	detail, ok := envErr.Detail.(RestoreTargetErrorDetail)
	if !ok || detail.Reason != wantReason {
		t.Fatalf("detail = %#v, want reason %q", envErr.Detail, wantReason)
	}
	if envErr.Remediation == "" {
		t.Fatal("restore-target error remediation is empty")
	}
}

func countCapabilityFlag(flags []string, wanted string) int {
	count := 0
	for _, flag := range flags {
		if flag == wanted {
			count++
		}
	}
	return count
}
