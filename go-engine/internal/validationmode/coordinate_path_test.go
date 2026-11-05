// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlatformCoordinatesResolveInsideValidationRoots(t *testing.T) {
	context := activeTestContext(t, "platform-coordinates")
	for _, coordinate := range []struct{ authored, alias, relative string }{
		{"${xdg.config}", "XDG_CONFIG_HOME", ""},
		{"${xdg.data}", "XDG_DATA_HOME", ""},
		{"${xdg.state}", "XDG_STATE_HOME", ""},
		{"${xdg.cache}", "XDG_CACHE_HOME", ""},
		{"${darwin.applicationSupport}", "USERPROFILE", "Library/Application Support"},
	} {
		t.Run(coordinate.authored, func(t *testing.T) {
			got, err := context.ResolveHostPath(coordinate.authored+"/editor/preferences.json", HostPathPolicy{})
			if err != nil {
				t.Fatalf("platform coordinate has no sandbox path: %v", err)
			}
			root, ok := context.VirtualRoot(coordinate.alias)
			if !ok || got != filepath.Join(root, filepath.FromSlash(coordinate.relative), "editor", "preferences.json") {
				t.Fatalf("resolved path %q does not use its declared virtual root", got)
			}
			if err := context.ValidateSandboxPath(got); err != nil {
				t.Fatalf("resolved coordinate escaped validation authority: %v", err)
			}
		})
	}
}

func TestPlatformCoordinatesProtectOriginalHostRoots(t *testing.T) {
	home, windowsHome := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", windowsHome)
	t.Setenv("XDG_CONFIG_HOME", "relative-config")
	hostHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	context := activeTestContext(t, "original-platform-coordinates")
	for _, coordinate := range []struct{ authored, root string }{
		{"${xdg.config}/editor/preferences.json", filepath.Join(hostHome, ".config")},
		{"${home}/editor/preferences.json", hostHome},
		{"${darwin.applicationSupport}/editor/preferences.json", filepath.Join(hostHome, "Library", "Application Support")},
		{`%USERPROFILE%\editor\preferences.json`, windowsHome},
	} {
		t.Run(coordinate.authored, func(t *testing.T) {
			got, err := context.OriginalHostPath(coordinate.authored, HostPathPolicy{})
			want := filepath.Join(coordinate.root, "editor", "preferences.json")
			if err != nil || got != want {
				t.Fatalf("original host guard = %q, want %q; error=%v", got, want, err)
			}
		})
	}
}
