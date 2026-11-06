// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"path/filepath"
	"slices"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// Historical declarations describe prior intent; they never authorize live reads.
func projectCaptureHomeManager(home *manifest.HomeManagerConfig, flags *CaptureFlags, selection captureSelection) (*manifest.HomeManagerConfig, []CommandWarning) {
	if home == nil || captureGOOSFn() != "linux" {
		return home, nil
	}
	omission := []CommandWarning{{Code: "historical_settings_omitted", Message: "Some previously recorded settings were omitted because they could not be scoped or bundled for this capture."}}
	if flags.Sanitize {
		return nil, nil
	}
	// The share collector processes payload files, not inline historical declarations.
	if flags.Share {
		return nil, omission
	}
	if home.Config != "" || home.Flake != "" {
		if selection.active() {
			return nil, omission
		}
		if len(flags.linuxConfigFiles)+len(flags.linuxHomeManagerFiles) > 0 {
			flags.linuxConfigFiles, flags.linuxHomeManagerFiles = nil, nil
			return home, []CommandWarning{{Code: "home_manager_ownership_preserved", Message: "The previously recorded settings configuration was kept; independently detected settings were not added beside it."}}
		}
		return home, nil
	}
	if home.Settings == nil {
		return nil, nil
	}
	data, err := json.Marshal(home.Settings)
	if err != nil {
		return nil, omission
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, omission
	}
	entries := map[string][]hmregistry.Entry{}
	if root := resolveRepoRootFn(); root != "" {
		if registry, err := hmregistry.Load(filepath.Join(root, "catalog", "home-manager", "registry.jsonc")); err == nil {
			for _, entry := range registry.Entries {
				entries[entry.Program] = append(entries[entry.Program], entry)
			}
		}
	}
	owners := map[string]bool{}
	keepProgram := func(program string) bool {
		keep := !selection.active()
		for _, entry := range entries[program] {
			keep = keep || slices.Contains(selection.appIDs, entry.PackageID) || slices.Contains(selection.appIDs, entry.ID) || slices.Contains(selection.moduleIDs, entry.ModuleID)
		}
		if keep {
			for _, entry := range entries[program] {
				owners[entry.ID] = true
				if entry.ModuleID != "" {
					owners[entry.ModuleID] = true
				}
			}
		}
		return keep
	}
	omitted := len(home.Settings.Files) > 0
	programs := map[string]json.RawMessage{}
	if raw := fields["programs"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &programs); err != nil {
			return nil, omission
		}
	}
	// These two keys are fixed by HomeManagerSettings; program ownership is registry data.
	delete(fields, "files")
	delete(fields, "programs")
	for program := range fields {
		if !keepProgram(program) {
			delete(fields, program)
			omitted = true
		}
	}
	for program := range programs {
		if !keepProgram(program) {
			delete(programs, program)
			omitted = true
		}
	}
	if len(programs) > 0 {
		fields["programs"], err = json.Marshal(programs)
		if err != nil {
			return nil, omission
		}
	}
	settings := &manifest.HomeManagerSettings{}
	data, err = json.Marshal(fields)
	if err != nil {
		return nil, omission
	}
	if err := json.Unmarshal(data, settings); err != nil {
		return nil, omission
	}
	projected := &manifest.HomeManagerConfig{Settings: settings, Secrets: home.Secrets}
	if selection.active() {
		omitted = omitted || len(projected.Secrets) > 0
		projected.Secrets = nil
	}
	filterPlans := func(plans []bundle.SettingsFileCapturePlan) []bundle.SettingsFileCapturePlan {
		kept := make([]bundle.SettingsFileCapturePlan, 0, len(plans))
		for _, plan := range plans {
			if owners[plan.CandidateID] {
				omitted = true
				continue
			}
			kept = append(kept, plan)
		}
		return kept
	}
	flags.linuxConfigFiles = filterPlans(flags.linuxConfigFiles)
	flags.linuxHomeManagerFiles = filterPlans(flags.linuxHomeManagerFiles)
	if len(fields) == 0 && len(projected.Secrets) == 0 {
		projected = nil
	}
	if omitted {
		return projected, omission
	}
	return projected, nil
}
