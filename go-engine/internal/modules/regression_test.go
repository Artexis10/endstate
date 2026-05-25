// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"path/filepath"
	"testing"
)

// TestPowerToysModule_ExcludesUpdatesDir is a regression guard for the
// PowerToys self-update installer bloat.
//
// Background: substrate PR #15's hosted-backup e2e verification surfaced a
// 377 MiB profile push. Inventory revealed a single 376 MiB file at
// configs/powertoys/powertoys/Updates/powertoysusersetup-0.98.1-x64.exe —
// PowerToys' downloaded self-update installer, captured as if it were
// config. The PowerToys module's excludeGlobs covered
// Logs/Temp/Cache/GPUCache/Crashpad but not the Updates/ dir where the
// installer accumulates.
//
// This test asserts the PowerToys module excludes the Updates/ dir so the
// same regression can't slip back in.
func TestPowerToysModule_ExcludesUpdatesDir(t *testing.T) {
	// Load the real production modules catalog (not testdata).
	modulesRoot := filepath.Join("..", "..", "..", "modules", "apps")
	catalog, err := LoadCatalog(modulesRoot)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	mod, ok := catalog["apps.powertoys"]
	if !ok {
		t.Fatal("apps.powertoys module not in catalog (did the module dir move?)")
	}
	if mod.Capture == nil {
		t.Fatal("apps.powertoys has no Capture section")
	}
	found := false
	for _, glob := range mod.Capture.ExcludeGlobs {
		// Match either backslash- or forward-slash-style globs.
		if glob == "**\\Updates\\**" || glob == "**/Updates/**" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf(
			"apps.powertoys module excludeGlobs missing **\\Updates\\** (or forward-slash equivalent).\n"+
				"got: %v\n"+
				"PowerToys' Updates/ dir contains downloaded self-update installers (~400 MiB) — see substrate PR #15 regression notes.",
			mod.Capture.ExcludeGlobs,
		)
	}
}
