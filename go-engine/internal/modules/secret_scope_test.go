// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"reflect"
	"testing"
)

// TestResolveRegistrySecretScope pins the coordinate semantics that decide
// what capture filters out and what it refuses. A wrong answer either leaks a
// declared secret into a bundle or drops a whole module the user wanted.
func TestResolveRegistrySecretScope(t *testing.T) {
	type valueProbe struct {
		key, name string
		excluded  bool
	}
	tests := []struct {
		name       string
		secrets    SecretsDef
		capture    CaptureDef
		conflicts  []string
		intersects map[string]bool
		values     []valueProbe
	}{
		{
			name:       "a string coordinate below a whole-key capture is filtered by both readings",
			secrets:    SecretsDef{Files: []string{`HKCU\Software\App\Name`}},
			capture:    CaptureDef{RegistryKeys: []CaptureRegistryKey{{Key: `HKCU\Software\App`}}},
			intersects: map[string]bool{`HKCU\Software\App`: true, `HKCU\Software\Other`: false},
			values: []valueProbe{
				{key: `HKEY_CURRENT_USER\Software\App`, name: "name", excluded: true},
				{key: `HKCU\Software\App\Name\Child`, name: "Anything", excluded: true},
				{key: `HKCU\Software\App`, name: "Theme", excluded: false},
				{key: `HKCU\Software\App\Name2`, name: "Value", excluded: false},
			},
		},
		{
			name:      "a capture equal to a secret key is a contradiction",
			secrets:   SecretsDef{RegistryKeys: []string{`HKCU\Software\App\Vault`}},
			capture:   CaptureDef{RegistryKeys: []CaptureRegistryKey{{Key: `hkcu\software\app\vault`}}},
			conflicts: []string{"capture.registryKeys[0].key"},
		},
		{
			name:       "a capture inside a secret key is a contradiction",
			secrets:    SecretsDef{RegistryKeys: []string{`HKCU\Software\App`}},
			capture:    CaptureDef{RegistryKeys: []CaptureRegistryKey{{Key: `HKCU\Software\App\Settings`}}},
			conflicts:  []string{"capture.registryKeys[0].key"},
			intersects: map[string]bool{`HKCU\Software\App\Settings`: true},
		},
		{
			name:       "a named secret value applies only to its own key",
			secrets:    SecretsDef{RegistryValues: []RegistrySecretValue{{Key: `HKCU\Software\App`, ValueName: "Password"}}},
			capture:    CaptureDef{RegistryKeys: []CaptureRegistryKey{{Key: `HKCU\Software\App`}}},
			intersects: map[string]bool{`HKCU\Software\App`: true, `HKCU\Software\App\Child`: false},
			values: []valueProbe{
				{key: `HKCU\Software\App`, name: "PASSWORD", excluded: true},
				{key: `HKCU\Software\App\Child`, name: "Password", excluded: false},
				{key: `HKCU\Software\App`, name: "Theme", excluded: false},
			},
		},
		{
			name:       "an omitted value name covers every value directly on the key",
			secrets:    SecretsDef{RegistryValues: []RegistrySecretValue{{Key: `HKCU\Software\App`}}},
			capture:    CaptureDef{RegistryKeys: []CaptureRegistryKey{{Key: `HKCU\Software\App\HotKeys`}}},
			intersects: map[string]bool{`HKCU\Software\App\HotKeys`: false, `HKCU\Software`: true},
			values: []valueProbe{
				{key: `HKCU\Software\App`, name: "License", excluded: true},
				{key: `HKCU\Software\App`, name: "", excluded: true},
				{key: `HKCU\Software\App\HotKeys`, name: "HotKey_1", excluded: false},
			},
		},
		{
			name:       "subkeys applies the value name at the key and every key below it",
			secrets:    SecretsDef{RegistryValues: []RegistrySecretValue{{Key: `HKCU\Software\App`, ValueName: "Password", Subkeys: true}}},
			capture:    CaptureDef{RegistryKeys: []CaptureRegistryKey{{Key: `HKCU\Software\App\Connections`}}},
			intersects: map[string]bool{`HKCU\Software\App\Connections`: true},
			values: []valueProbe{
				{key: `HKCU\Software\App\Connections\office`, name: "password", excluded: true},
				{key: `HKCU\Software\App\Connections\office`, name: "Host", excluded: false},
				{key: `HKCU\Software\Apple`, name: "Password", excluded: false},
			},
		},
		{
			name:    "a value name containing a star is matched literally",
			secrets: SecretsDef{RegistryValues: []RegistrySecretValue{{Key: `HKCU\Software\App`, ValueName: "Pass*"}}},
			capture: CaptureDef{RegistryKeys: []CaptureRegistryKey{{Key: `HKCU\Software\App`}}},
			values: []valueProbe{
				{key: `HKCU\Software\App`, name: "Pass*", excluded: true},
				{key: `HKCU\Software\App`, name: "Password", excluded: false},
			},
		},
		{
			name:    "a named-value capture of a secret value is a contradiction",
			secrets: SecretsDef{RegistryValues: []RegistrySecretValue{{Key: `HKCU\Software\App`, ValueName: "Token"}}},
			capture: CaptureDef{RegistryValues: []CaptureRegistryValue{
				{Key: `HKCU\Software\App`, ValueName: "token"},
				{Key: `HKCU\Software\App`, ValueName: "Theme"},
			}},
			conflicts: []string{"capture.registryValues[0]"},
		},
		{
			name:    "a malformed registry secret contradicts every registry capture but no file capture",
			secrets: SecretsDef{Files: []string{`HKCU\Software\App\*\Password`}},
			capture: CaptureDef{
				Files:          []CaptureFile{{Source: `%APPDATA%\App\settings.json`, Dest: "apps/app/settings.json"}},
				RegistryKeys:   []CaptureRegistryKey{{Key: `HKCU\Software\App`}},
				RegistryValues: []CaptureRegistryValue{{Key: `HKCU\Software\Other`, ValueName: "Mode"}},
			},
			conflicts:  []string{"capture.registryKeys[0].key", "capture.registryValues[0]"},
			intersects: map[string]bool{`HKCU\Software\Unrelated`: true},
		},
		{
			name:       "a secret in another hive never touches an HKCU capture",
			secrets:    SecretsDef{RegistryKeys: []string{`HKLM\Software\App`}},
			capture:    CaptureDef{RegistryKeys: []CaptureRegistryKey{{Key: `HKCU\Software\App`}}},
			intersects: map[string]bool{`HKCU\Software\App`: false},
			values:     []valueProbe{{key: `HKCU\Software\App`, name: "App", excluded: false}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			secrets, capture := test.secrets, test.capture
			scope, conflicts := ResolveRegistrySecretScope(&Module{ID: "apps.example", Secrets: &secrets, Capture: &capture})
			var coordinates []string
			for _, conflict := range conflicts {
				if conflict.ModuleID != "apps.example" || conflict.Reason == "" {
					t.Errorf("conflict %+v lacks module identity or reason", conflict)
				}
				coordinates = append(coordinates, conflict.Coordinate)
			}
			if !reflect.DeepEqual(coordinates, test.conflicts) {
				t.Errorf("conflicts = %v, want %v", coordinates, test.conflicts)
			}
			for key, want := range test.intersects {
				if got := scope.Intersects(key); got != want {
					t.Errorf("Intersects(%s) = %t, want %t", key, got, want)
				}
			}
			for _, probe := range test.values {
				if got := scope.ExcludesValue(probe.key, probe.name); got != probe.excluded {
					t.Errorf("ExcludesValue(%s, %q) = %t, want %t", probe.key, probe.name, got, probe.excluded)
				}
			}
		})
	}
}
