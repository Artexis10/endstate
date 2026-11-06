// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// An owner declared outside the settings catalog keeps its configuration; the
// independently detected files are dropped and the user is told why.
func TestProjectCaptureHomeManagerPreservesDeclaredExternalOwner(t *testing.T) {
	old := captureGOOSFn
	t.Cleanup(func() { captureGOOSFn = old })
	captureGOOSFn = func() string { return "linux" }
	home := &manifest.HomeManagerConfig{Flake: "github:owner/dots#me"}
	flags := &CaptureFlags{linuxConfigFiles: []bundle.SettingsFileCapturePlan{{CandidateID: "apps.ripgrep", Target: "${home}/.ripgreprc"}}}

	got, warnings := projectCaptureHomeManager(home, flags, captureSelection{})

	if got == nil || got.Flake != "github:owner/dots#me" || got.Settings != nil {
		t.Fatalf("external owner was changed: %+v", got)
	}
	if len(flags.linuxConfigFiles) != 0 {
		t.Fatalf("detected files were added beside the external owner: %+v", flags.linuxConfigFiles)
	}
	if len(warnings) != 1 || warnings[0].Code != "home_manager_ownership_preserved" {
		t.Fatalf("warnings = %+v", warnings)
	}
}
