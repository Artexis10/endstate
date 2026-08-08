// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"sort"
	"strings"
	"testing"
)

// TestValidateModule_CaptureModuleNeedsAConsultedMatcher locks the validation
// rule itself, independent of what the catalog happens to contain today.
func TestValidateModule_CaptureModuleNeedsAConsultedMatcher(t *testing.T) {
	withCapture := func(m MatchCriteria) *Module {
		return &Module{
			ID:          "apps.example",
			DisplayName: "Example",
			Matches:     m,
			Capture:     &CaptureDef{Files: []CaptureFile{{Source: "s", Dest: "d"}}},
		}
	}

	tests := []struct {
		name    string
		mod     *Module
		wantErr bool
	}{
		{
			name:    "exe only is rejected once the module captures",
			mod:     withCapture(MatchCriteria{Exe: []string{"app.exe"}}),
			wantErr: true,
		},
		{
			name:    "uninstallDisplayName only is rejected once the module captures",
			mod:     withCapture(MatchCriteria{UninstallDisplayName: []string{"^App"}}),
			wantErr: true,
		},
		{
			name:    "pathExists is sufficient",
			mod:     withCapture(MatchCriteria{Exe: []string{"app.exe"}, PathExists: []string{"%APPDATA%\\App"}}),
			wantErr: false,
		},
		{
			name:    "winget is sufficient",
			mod:     withCapture(MatchCriteria{Winget: []string{"Vendor.App"}}),
			wantErr: false,
		},
		{
			name:    "chocolatey is sufficient",
			mod:     withCapture(MatchCriteria{Chocolatey: []string{"app"}}),
			wantErr: false,
		},
		{
			name: "restore-only modules are held to the same rule",
			mod: &Module{
				ID:          "apps.example",
				DisplayName: "Example",
				Matches:     MatchCriteria{Exe: []string{"app.exe"}},
				Restore:     []RestoreDef{{Type: "copy", Source: "s", Target: "t"}},
			},
			wantErr: true,
		},
		{
			name: "an inert stub with neither capture nor restore is left alone",
			mod: &Module{
				ID:          "apps.example",
				DisplayName: "Example",
				Matches:     MatchCriteria{Exe: []string{"app.exe"}},
			},
			wantErr: false,
		},
		{
			name: "a module with no matcher at all is still rejected",
			mod: &Module{
				ID:          "apps.example",
				DisplayName: "Example",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateModule(tt.mod, "module.jsonc")
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateModule() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), "match") {
				t.Errorf("expected a matcher-related message, got %q", err.Error())
			}
		})
	}
}

// TestCatalogIntegrity_EveryCaptureModuleIsDetectable guards against modules
// that pass validation but can never attach to anything.
//
// validateModule accepts matches.exe or matches.uninstallDisplayName as
// satisfying "at least one matcher", but neither is consulted when matching:
// matchModulesForApps only reads winget, chocolatey and pathExists, exe feeds
// the planner's running-process guard, and uninstallDisplayName only breaks
// precedence ties between modules that already matched. A module declaring
// nothing but those two loads cleanly, appears in the catalog, and is silently
// undetectable — the failure mode reported in Artexis10/endstate#208.
//
// Schema-v2 modules are exempt: they are discovered through
// config.instanceDetectors rather than the matches block.
func TestCatalogIntegrity_EveryCaptureModuleIsDetectable(t *testing.T) {
	root := productionModulesRoot()

	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}
	if len(catalog) < 70 {
		t.Fatalf("loaded only %d modules — wrong path or catastrophic catalog loss?", len(catalog))
	}

	var undetectable []string
	for id, mod := range catalog {
		// Only modules that actually capture something can strand a user.
		if mod.Capture == nil || (len(mod.Capture.Files) == 0 && len(mod.Capture.RegistryKeys) == 0) {
			continue
		}
		// The three matchers matchModulesForApps actually consults.
		if len(mod.Matches.Winget) > 0 || len(mod.Matches.Chocolatey) > 0 || len(mod.Matches.PathExists) > 0 {
			continue
		}
		// Schema-v2 discovery runs through detectors, not the matches block.
		if mod.Config != nil && len(mod.Config.InstanceDetectors) > 0 {
			continue
		}
		undetectable = append(undetectable, id)
	}

	if len(undetectable) > 0 {
		sort.Strings(undetectable)
		t.Errorf("%d module(s) declare a capture section but no matcher the engine consults, so they can never attach:\n  %v\n"+
			"Give each one a matches.pathExists entry pointing at a file the app always writes.",
			len(undetectable), undetectable)
	}
}
