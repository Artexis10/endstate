// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package hmregistry

import (
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/packagecatalog"
	"github.com/Artexis10/endstate/go-engine/internal/settingscodec"
)

// Action names the lifecycle operations in the settings capability protocol.
type Action string

const (
	Capture Action = "capture"
	Restore Action = "restore"
	Verify  Action = "verify"
	Revert  Action = "revert"
)

// Authority keeps structural binding separate from executable capability.
// Reason is one bounded diagnostic code; authored paths never enter it.
type Authority struct {
	AdapterID    string
	ModuleID     string
	ConfigSetID  string
	GenerationID string
	Realization  string
	Bound        bool
	Actions      []Action
	Reason       string
}

func (authority Authority) Supports(action Action) bool {
	return slices.Contains(authority.Actions, action)
}

// Join consumes reviewed registry metadata and current authored catalogues.
// A rejected adapter withholds only its own payload, never unrelated inventory.
func Join(registry *Registry, catalog map[string]*modules.Module, packages []*packagecatalog.Entry) []Authority {
	result := []Authority{}
	entries := map[string]bool{}
	linked := map[string]bool{}
	if registry != nil {
		for _, entry := range registry.Entries {
			entries[entry.ID] = true
			authority := bindEntry(entry, catalog, packages)
			result = append(result, authority)
			linked[authority.ModuleID] = true
		}
	}
	for _, mod := range catalog {
		if mod == nil || mod.EffectiveSchemaVersion() != 3 {
			continue
		}
		binding := mod.Platforms["linux"].HomeManagerAdapter
		if binding != nil && (!entries[binding.ID] || !linked[mod.ID]) {
			reason := "settings_adapter_missing"
			if entries[binding.ID] {
				reason = "settings_module_mismatch"
			}
			result = append(result, Authority{AdapterID: binding.ID, ModuleID: mod.ID, Reason: reason})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].AdapterID != result[j].AdapterID {
			return result[i].AdapterID < result[j].AdapterID
		}
		return result[i].ModuleID < result[j].ModuleID
	})
	return result
}

func bindEntry(entry Entry, catalog map[string]*modules.Module, packages []*packagecatalog.Entry) Authority {
	authority := Authority{AdapterID: entry.ID, ModuleID: entry.ModuleID, Reason: "settings_adapter_unbound"}
	if authority.ModuleID == "" {
		for _, pkg := range packages {
			if pkg != nil && pkg.ID == entry.PackageID {
				authority.ModuleID = pkg.ConfigModule
				break
			}
		}
		if authority.ModuleID == "" && catalog["apps."+entry.ID] != nil {
			authority.ModuleID = "apps." + entry.ID
		}
	}
	if entry.Disposition == Excluded {
		authority.Reason = "settings_adapter_excluded"
		return authority
	}
	if entry.ModuleID == "" {
		return authority
	}
	mod := catalog[entry.ModuleID]
	if mod == nil {
		return authority
	}
	if mod.EffectiveSchemaVersion() != 3 {
		authority.Reason = "settings_linux_variant_missing"
		return authority
	}
	variant, ok := mod.Platforms["linux"]
	if !ok {
		authority.Reason = "settings_linux_variant_missing"
		return authority
	}
	binding := variant.HomeManagerAdapter
	if binding == nil || binding.ID != entry.ID {
		return authority
	}
	fingerprint, err := ReviewFingerprint(entry)
	if err != nil || entry.ReviewFingerprint != fingerprint || binding.ReviewFingerprint != fingerprint {
		authority.Reason = "settings_adapter_review_stale"
		return authority
	}
	matchedPackage := false
	for _, pkg := range packages {
		if pkg != nil && pkg.ID == entry.PackageID && pkg.ConfigModule == mod.ID {
			matchedPackage = true
			break
		}
	}
	if entry.PackageID == "" || !matchedPackage {
		authority.Reason = "settings_package_mismatch"
		return authority
	}
	projected := modules.ProjectModuleForPlatform(mod, "linux")
	if projected == nil || projected.Config == nil {
		authority.Reason = "settings_generation_missing"
		return authority
	}
	var generation *modules.GenerationDef
	for _, set := range projected.Config.Sets {
		if set.ID == binding.ConfigSetID {
			for index := range set.Generations {
				if set.Generations[index].ID == binding.GenerationID {
					generation = &set.Generations[index]
				}
			}
		}
	}
	if generation == nil {
		authority.Reason = "settings_generation_missing"
		return authority
	}
	authority.ConfigSetID = binding.ConfigSetID
	authority.GenerationID = binding.GenerationID
	authority.Realization = projected.Realization
	if reason := bindTargets(entry, *generation, projected.Secrets); reason != "" {
		authority.Reason = reason
		return authority
	}
	authority.Bound = true
	if !settingscodec.Supported(entry.Codec) {
		authority.Reason = "settings_codec_unavailable"
		return authority
	}
	if !settingscodec.Validates(entry.Codec) {
		// Byte preservation and Git sanitization do not prove portable semantics.
		authority.Reason = "settings_content_policy_unavailable"
		return authority
	}
	if projected.Realization != "endstate-restore" && projected.Realization != "home-manager" {
		authority.Reason = "settings_strategy_unavailable"
		return authority
	}
	for _, restore := range generation.Restore {
		covered := 0
		for _, validation := range generation.Validate {
			if validation.Type == "settings-codec" && validation.Codec == entry.Codec && path.Clean(validation.Path) == path.Clean(restore.Source) && validation.Optional == restore.Optional {
				covered++
			}
		}
		if restore.Type != "copy" || covered != 1 {
			authority.Reason = "settings_content_policy_unavailable"
			return authority
		}
	}
	authority.Reason = ""
	authority.Actions = []Action{Capture, Restore, Verify, Revert}
	return authority
}

func bindTargets(entry Entry, generation modules.GenerationDef, secrets *modules.SecretsDef) string {
	capture := generation.Capture
	if capture == nil || len(entry.Targets) == 0 || len(capture.Files) != len(entry.Targets) || len(generation.Restore) != len(entry.Targets) || len(capture.RegistryKeys) > 0 || len(capture.RegistryValues) > 0 {
		return "settings_target_mismatch"
	}
	captures := map[string]modules.CaptureFile{}
	destinations := map[string]bool{}
	for _, file := range capture.Files {
		source, err := canonicalCoordinate(file.Source)
		if err != nil || captures[source].Source != "" || destinations[path.Clean(file.Dest)] {
			return "settings_target_mismatch"
		}
		captures[source] = file
		destinations[path.Clean(file.Dest)] = true
	}
	restores := map[string]modules.RestoreDef{}
	for _, restore := range generation.Restore {
		target, err := canonicalCoordinate(restore.Target)
		if err != nil || restores[target].Target != "" {
			return "settings_target_mismatch"
		}
		restores[target] = restore
	}
	for _, target := range entry.Targets {
		coordinate, err := canonicalCoordinate(target.Coordinate)
		if err != nil {
			return "settings_target_mismatch"
		}
		file, hasCapture := captures[coordinate]
		restore, hasRestore := restores[coordinate]
		if !hasCapture || !hasRestore || file.Optional != target.Optional || restore.Optional != target.Optional || path.Clean(file.Dest) != path.Clean(strings.TrimPrefix(restore.Source, "./payload/")) {
			return "settings_target_mismatch"
		}
		if secrets != nil {
			patterns := make([]string, 0, len(secrets.Files))
			for _, pattern := range secrets.Files {
				normalized, err := canonicalCoordinate(pattern)
				if err != nil {
					return "settings_secret_boundary"
				}
				patterns = append(patterns, normalized)
			}
			for _, value := range []string{coordinate, file.Dest} {
				matches, err := bundle.ResolvedPathMatchesSecrets(value, patterns, true)
				if err != nil || matches {
					return "settings_secret_boundary"
				}
			}
		}
		for _, value := range []string{file.Source, file.Dest} {
			for _, pattern := range capture.ExcludeGlobs {
				matches, err := bundle.ConfigPathMatchesExcludeGlob(value, pattern)
				if err != nil || matches {
					return "settings_target_excluded"
				}
			}
		}
		for _, value := range []string{restore.Source, restore.Target} {
			for _, pattern := range restore.Exclude {
				matches, err := bundle.ConfigPathMatchesExcludeGlob(value, pattern)
				if err != nil || matches {
					return "settings_target_excluded"
				}
			}
		}
	}
	return ""
}

func canonicalCoordinate(value string) (string, error) {
	if err := config.ValidateEnginePath(value, "linux"); err != nil {
		return "", err
	}
	coordinate, suffix, _ := config.SplitEnginePath(value)
	// Keep coordinate namespaces distinct without passing their braces to a glob parser.
	return strings.TrimSuffix(strings.TrimPrefix(coordinate, "${"), "}") + path.Clean("/"+strings.TrimLeft(strings.ReplaceAll(suffix, `\`, "/"), "/")), nil
}
