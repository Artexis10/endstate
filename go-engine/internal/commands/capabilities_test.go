// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPlatformInfoFor_UsesStableRegistryOrder(t *testing.T) {
	tests := []struct {
		goos    string
		drivers []string
	}{
		{goos: "windows", drivers: []string{"winget", "chocolatey"}},
		{goos: "linux", drivers: []string{"nix"}},
		{goos: "darwin", drivers: []string{"nix", "brew"}},
		{goos: "plan9", drivers: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			got := platformInfoFor(tt.goos)
			if got.OS != tt.goos {
				t.Errorf("platformInfoFor(%q).OS = %q", tt.goos, got.OS)
			}
			if !reflect.DeepEqual(got.Drivers, tt.drivers) {
				t.Errorf("platformInfoFor(%q).Drivers = %v, want %v", tt.goos, got.Drivers, tt.drivers)
			}
		})
	}
}

// TestRunCapabilities_HostedBackupIfChangedAdvertised verifies that the
// capabilities envelope includes features.hostedBackup.ifChanged = true,
// which is the canonical GUI gate for the conditional auto-backup flow.
func TestRunCapabilities_HostedBackupIfChangedAdvertised(t *testing.T) {
	data, err := RunCapabilities()
	if err != nil {
		t.Fatalf("RunCapabilities() returned error: %v", err)
	}

	// Marshal to JSON and unmarshal into a generic map to test the wire shape.
	b, jsonErr := json.Marshal(data)
	if jsonErr != nil {
		t.Fatalf("json.Marshal capabilities data: %v", jsonErr)
	}

	var envelope map[string]interface{}
	if jsonErr = json.Unmarshal(b, &envelope); jsonErr != nil {
		t.Fatalf("json.Unmarshal capabilities data: %v", jsonErr)
	}

	features, ok := envelope["features"].(map[string]interface{})
	if !ok {
		t.Fatalf("capabilities.features is not an object, got: %T", envelope["features"])
	}

	hostedBackup, ok := features["hostedBackup"].(map[string]interface{})
	if !ok {
		t.Fatalf("capabilities.features.hostedBackup is not an object, got: %T", features["hostedBackup"])
	}

	ifChanged, ok := hostedBackup["ifChanged"]
	if !ok {
		t.Fatal("capabilities.features.hostedBackup.ifChanged is missing")
	}

	ifChangedBool, ok := ifChanged.(bool)
	if !ok {
		t.Fatalf("capabilities.features.hostedBackup.ifChanged is not a bool, got: %T", ifChanged)
	}

	if !ifChangedBool {
		t.Error("capabilities.features.hostedBackup.ifChanged = false, want true")
	}
}

// TestRunCapabilities_CaptureFlags_IncludesPin verifies that the capabilities
// envelope advertises --pin in commands.capture.flags so clients can gate a
// pinned-capture option on engine support.
func TestRunCapabilities_CaptureFlags_IncludesPin(t *testing.T) {
	result, err := RunCapabilities()
	if err != nil {
		t.Fatalf("RunCapabilities returned error: %v", err)
	}
	data := result.(CapabilitiesData)
	captureCmd, ok := data.Commands["capture"]
	if !ok {
		t.Fatal("capture command not found in capabilities")
	}
	found := false
	for _, f := range captureCmd.Flags {
		if f == "--pin" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("commands.capture.flags does not contain --pin; got %v", captureCmd.Flags)
	}
}

func TestRunCapabilities_MultiDriverCLIFlags(t *testing.T) {
	result, err := RunCapabilities()
	if err != nil {
		t.Fatalf("RunCapabilities returned error: %v", err)
	}
	data := result.(CapabilitiesData)

	for _, tc := range []struct {
		command string
		flags   []string
	}{
		{command: "apply", flags: []string{"--bootstrap-backends", "--no-bootstrap"}},
		{command: "capture", flags: []string{"--driver"}},
		{command: "rebuild", flags: []string{"--bootstrap-backends", "--no-bootstrap"}},
	} {
		got := map[string]bool{}
		for _, flag := range data.Commands[tc.command].Flags {
			got[flag] = true
		}
		for _, want := range tc.flags {
			if !got[want] {
				t.Errorf("commands.%s.flags missing %q: %v", tc.command, want, data.Commands[tc.command].Flags)
			}
		}
	}
}

// TestRunCapabilities_HostedBackupShape verifies the full shape of the
// hostedBackup features block so regressions in existing fields are caught.
func TestRunCapabilities_HostedBackupShape(t *testing.T) {
	data, err := RunCapabilities()
	if err != nil {
		t.Fatalf("RunCapabilities() returned error: %v", err)
	}

	caps, ok := data.(CapabilitiesData)
	if !ok {
		t.Fatalf("RunCapabilities() returned %T, want CapabilitiesData", data)
	}

	hb := caps.Features.HostedBackup

	if !hb.Supported {
		t.Error("hostedBackup.supported = false, want true")
	}
	if hb.MinSchemaVersion == "" {
		t.Error("hostedBackup.minSchemaVersion is empty")
	}
	if !hb.Rename {
		t.Error("hostedBackup.rename = false, want true")
	}
	if !hb.IfChanged {
		t.Error("hostedBackup.ifChanged = false, want true")
	}
}
