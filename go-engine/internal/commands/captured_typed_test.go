// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

func declareExplicitConfiguration(t *testing.T, path, config string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "explicit.nix"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	declareInManifest(t, path, map[string]any{"homeManager": map[string]any{"config": "./explicit.nix"}})
}

func TestCapturedTypedSettingsApplyThroughTheGeneratedModuleAndKeepTheBuiltBytes(t *testing.T) {
	path, _, home, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{typed: true})
	// Home Manager's own TOML writer lays the file out differently from the canonical payload.
	backend.renderTyped = func(payload []byte) []byte {
		return []byte("[virtualenvs]\ncreate = false\nin-project = true\n\n")
	}
	declareExplicitConfiguration(t, path, "{ ... }: { programs.git.enable = true; }\n")
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true})
	if err != nil {
		t.Fatalf("apply: %+v", err)
	}
	result := raw.(*ApplyResult)
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.poetry"), planner.StatusRestored, "")
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.ripgrep"), planner.StatusRestored, "")
	target := filepath.Join(home, ".config", "pypoetry", "config.toml")
	built, readErr := os.ReadFile(target)
	if readErr != nil || !strings.Contains(string(built), "in-project = true") {
		t.Fatalf("poetry target was not activated from the built file: %q %v", built, readErr)
	}
	// The activation is a link into the build, so the settings were rendered, not placed as a captured copy.
	if info, err := os.Lstat(target); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("poetry target is not a Home Manager link: %v", err)
	}
	if backend.activated != 1 {
		t.Fatalf("typed and placed settings did not share one activation: %d", backend.activated)
	}
	generated := backend.flakes[len(backend.flakes)-1]
	if _, err := os.Stat(filepath.Join(generated, "typed.nix")); err != nil {
		t.Fatalf("typed settings were not generated as a separate module: %v", err)
	}
	if placements, _ := os.ReadFile(filepath.Join(generated, "placements.nix")); strings.Contains(string(placements), "pypoetry") {
		t.Fatalf("typed settings were also placed as a file: %s", placements)
	}
}

func TestCapturedTypedSettingsWithholdActivationWhenBuiltTargetDiffers(t *testing.T) {
	path, _, home, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{typed: true})
	// Home Manager builds a different value than the capture holds.
	backend.renderTyped = func([]byte) []byte { return []byte("[virtualenvs]\ncreate = false\nin-project = false\n") }
	declareExplicitConfiguration(t, path, "{ ... }: { programs.git.enable = true; }\n")
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true})
	if err != nil {
		t.Fatalf("apply: %+v", err)
	}
	result := raw.(*ApplyResult)
	target := filepath.Join(home, ".config", "pypoetry", "config.toml")
	for _, module := range []string{"apps.poetry", "apps.ripgrep"} {
		resolution := resolutionFor(t, result.ConfigResultFields, module)
		assertResolution(t, resolution, planner.StatusSkipped, planner.ReasonSettingsStrategyUnavailable)
		if !strings.Contains(resolution.Message, target) {
			t.Errorf("%s message does not name the target %s: %q", module, target, resolution.Message)
		}
	}
	if result.HomeManager == nil || !result.HomeManager.Activated || backend.activated != 1 {
		t.Fatalf("the explicit activation did not continue: %+v activated=%d", result.HomeManager, backend.activated)
	}
	assertNoFile(t, append(backend.targets, target)...)
}

func TestCapturedTypedSettingsWithholdOnlyTypedSetWhenExplicitConfigDefinesProgram(t *testing.T) {
	path, _, home, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{typed: true})
	declareExplicitConfiguration(t, path, "{ ... }: { programs.poetry.enable = true; }\n")
	// The projection of that file reports poetry as defined outside Home Manager's modules.
	backend.definedOutside = []string{"poetry"}
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true})
	if err != nil && err.Code != envelope.ErrRestoreFailed {
		t.Fatalf("apply: %+v", err)
	}
	result := raw.(*ApplyResult)
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.poetry"), planner.StatusFailed, planner.ReasonTargetCollision)
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.ripgrep"), planner.StatusRestored, "")
	if result.HomeManager == nil || !result.HomeManager.Activated || backend.activated != 1 {
		t.Fatalf("the activation did not continue: %+v activated=%d", result.HomeManager, backend.activated)
	}
	assertNoFile(t, filepath.Join(home, ".config", "pypoetry", "config.toml"))
	generated := backend.flakes[len(backend.flakes)-1]
	if _, err := os.Stat(filepath.Join(generated, "typed.nix")); !os.IsNotExist(err) {
		t.Fatalf("the removed typed set still reached the combined module: %v", err)
	}
}
