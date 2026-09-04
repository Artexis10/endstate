// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/packagecatalog"
)

// AdapterInventory is one source's normalized output. It contains no raw
// command output and no machine-local roots.
type AdapterInventory struct {
	Items         []InventoryItem
	Resolved      []ResolvedIntent
	Settings      []SettingsCandidate
	SettingsFiles []SettingsFilePlan
	Warnings      []Warning
	Ignored       int
}

// Adapter is one read-only, source-specific inventory boundary.
type Adapter interface {
	ID() string
	Inventory(context.Context) (AdapterInventory, error)
}

// Orchestrator reconciles normalized inventory through the reviewed package
// catalog and immutable Nixpkgs input.
type Orchestrator struct {
	Adapters     []Adapter
	Catalog      *packagecatalog.Catalog
	NixpkgsInput string
}

type resolvedAccumulator struct {
	entry      *packagecatalog.Entry
	intent     *ResolvedIntent
	intentRank int
	evidence   []Evidence
}

// Discover runs selected adapters in deterministic order. An available source
// failure is scoped when another selected source succeeds; if none succeeds,
// returning a partial/empty result would be misleading and the run fails.
func (orchestrator Orchestrator) Discover(ctx context.Context, request Request) (Result, error) {
	result := emptyResult()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	adapters := append([]Adapter(nil), orchestrator.Adapters...)
	sort.Slice(adapters, func(i, j int) bool { return adapters[i].ID() < adapters[j].ID() })
	selectedSources := stringSet(request.SelectedSources)
	resolved := make(map[string]*resolvedAccumulator)
	settings := make(map[string]SettingsCandidate)
	settingsFiles := make(map[string][]SettingsFilePlan)
	var unresolved []UnresolvedItem
	succeeded := 0
	selectedAdapters := 0

	for _, adapter := range adapters {
		if adapter == nil {
			continue
		}
		if len(selectedSources) > 0 && !selectedSources[adapter.ID()] {
			continue
		}
		selectedAdapters++
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		inventory, err := adapter.Inventory(ctx)
		status := SourceStatus{ID: adapter.ID()}
		switch {
		case errors.Is(err, ErrNotAvailable):
			status.State = SourceNotAvailable
			status.Reason = "source_not_available"
			status.Message = "This application source is not installed or available on this machine."
		case err != nil:
			status.State = SourceFailed
			status.Reason = "source_failed"
			status.Message = "This application source could not be read; results from other sources were kept."
			result.Warnings = append(result.Warnings, Warning{
				Code: "source_failed", Source: adapter.ID(), Message: status.Message,
			})
		default:
			succeeded++
			status.State = SourceSucceeded
			status.Discovered = len(inventory.Items) + len(inventory.Resolved) + len(inventory.Settings)
			status.Ignored = inventory.Ignored
			result.Counts.Discovered += len(inventory.Items) + len(inventory.Resolved) + len(inventory.Settings)
			result.Counts.Ignored += inventory.Ignored
			result.Warnings = append(result.Warnings, inventory.Warnings...)
			for _, candidate := range inventory.Settings {
				mergeSettingsCandidate(settings, candidate)
			}
			for _, plan := range inventory.SettingsFiles {
				if strings.TrimSpace(plan.CandidateID) == "" {
					return Result{}, fmt.Errorf("source %s: settings file plan has no candidate id", adapter.ID())
				}
				settingsFiles[plan.CandidateID] = append(settingsFiles[plan.CandidateID], plan)
			}
			for _, intent := range inventory.Resolved {
				if err := mergeResolvedIntent(resolved, intent); err != nil {
					return Result{}, fmt.Errorf("source %s: %w", adapter.ID(), err)
				}
			}
			for _, item := range inventory.Items {
				entry, ok := orchestrator.Catalog.Resolve(item.Source, item.Ref)
				if !ok {
					if item.Explicit || item.UserFacing {
						unresolved = append(unresolved, unresolvedFromItem(item))
					}
					continue
				}
				accumulator := resolved[entry.ID]
				if accumulator == nil {
					accumulator = &resolvedAccumulator{entry: entry}
					resolved[entry.ID] = accumulator
				}
				accumulator.evidence = append(accumulator.evidence, evidenceFromItem(item))
			}
		}
		result.Sources = append(result.Sources, status)
	}

	if selectedAdapters > 0 && succeeded == 0 {
		return Result{}, fmt.Errorf("no trustworthy selected Linux application source could be read")
	}

	allowedApps, allowedModules := splitOnly(request.Only)
	resolvedIDs := make([]string, 0, len(resolved))
	for id := range resolved {
		if len(allowedApps) == 0 || allowedApps[id] {
			resolvedIDs = append(resolvedIDs, id)
		}
	}
	sort.Strings(resolvedIDs)
	for _, id := range resolvedIDs {
		accumulator := resolved[id]
		evidence := canonicalEvidence(accumulator.evidence)
		if accumulator.intent != nil {
			intent := *accumulator.intent
			intent.Evidence = evidence
			result.Resolved = append(result.Resolved, intent)
			continue
		}
		result.Resolved = append(result.Resolved, ResolvedIntent{
			ID: id, DisplayName: accumulator.entry.DisplayName,
			Attribute: accumulator.entry.Nix.Attribute, Input: orchestrator.NixpkgsInput,
			Installable:     orchestrator.NixpkgsInput + "#" + accumulator.entry.Nix.Attribute,
			MappingRevision: accumulator.entry.Revision, Evidence: evidence,
		})
	}

	sort.Slice(unresolved, func(i, j int) bool {
		if unresolved[i].ID != unresolved[j].ID {
			return unresolved[i].ID < unresolved[j].ID
		}
		return unresolved[i].Reason < unresolved[j].Reason
	})
	result.Unresolved = deduplicateUnresolved(unresolved)

	settingIDs := make([]string, 0, len(settings))
	for id, candidate := range settings {
		// A catalog association is capability metadata, not proof that live
		// settings exist. Only an adapter may create a settings candidate; package
		// reconciliation then upgrades that detected candidate's portability.
		if candidate.PackageID != "" {
			if _, ok := resolved[candidate.PackageID]; ok {
				candidate.Portability = PortabilityResolved
				settings[id] = candidate
			}
		}
		if len(allowedApps) == 0 && len(allowedModules) == 0 || allowedModules[id] || (candidate.PackageID != "" && allowedApps[candidate.PackageID]) {
			settingIDs = append(settingIDs, id)
		}
	}
	sort.Strings(settingIDs)
	for _, id := range settingIDs {
		candidate := settings[id]
		candidate.Actions = canonicalActions(candidate.Actions)
		candidate.Evidence = canonicalEvidence(candidate.Evidence)
		result.Settings = append(result.Settings, candidate)
		result.SettingsFiles = append(result.SettingsFiles, settingsFiles[id]...)
	}
	sort.Slice(result.SettingsFiles, func(i, j int) bool {
		left, right := result.SettingsFiles[i], result.SettingsFiles[j]
		if left.CandidateID != right.CandidateID {
			return left.CandidateID < right.CandidateID
		}
		return left.Target < right.Target
	})
	for index := 1; index < len(result.SettingsFiles); index++ {
		previous, current := result.SettingsFiles[index-1], result.SettingsFiles[index]
		if previous.Target == current.Target && previous.Source != current.Source {
			return Result{}, fmt.Errorf("settings target %q has multiple live owners", current.Target)
		}
	}
	sort.Slice(result.Warnings, func(i, j int) bool {
		if result.Warnings[i].Source != result.Warnings[j].Source {
			return result.Warnings[i].Source < result.Warnings[j].Source
		}
		if result.Warnings[i].Code != result.Warnings[j].Code {
			return result.Warnings[i].Code < result.Warnings[j].Code
		}
		return result.Warnings[i].ItemID < result.Warnings[j].ItemID
	})
	result.Counts.Resolved = len(result.Resolved)
	result.Counts.Selected = len(result.Resolved)
	result.Counts.Unresolved = len(result.Unresolved)
	result.Counts.Settings = len(result.Settings)
	return result, nil
}

func mergeResolvedIntent(target map[string]*resolvedAccumulator, intent ResolvedIntent) error {
	if strings.TrimSpace(intent.ID) == "" || strings.TrimSpace(intent.Attribute) == "" || strings.TrimSpace(intent.Input) == "" || strings.TrimSpace(intent.Installable) == "" {
		return fmt.Errorf("resolved profile intent is incomplete")
	}
	accumulator := target[intent.ID]
	incomingRank := resolvedIntentAuthority(intent)
	if accumulator == nil {
		copy := intent
		copy.Evidence = nil
		target[intent.ID] = &resolvedAccumulator{intent: &copy, intentRank: incomingRank, evidence: append([]Evidence(nil), intent.Evidence...)}
		return nil
	}
	if accumulator.intent != nil && (accumulator.intent.Attribute != intent.Attribute || accumulator.intent.Input != intent.Input || accumulator.intent.Installable != intent.Installable) {
		switch {
		case incomingRank > accumulator.intentRank:
			copy := intent
			copy.Evidence = nil
			accumulator.intent = &copy
			accumulator.intentRank = incomingRank
		case incomingRank == accumulator.intentRank:
			return fmt.Errorf("portable id %q has conflicting authoritative profile intents", intent.ID)
		}
	}
	if accumulator.intent == nil {
		copy := intent
		copy.Evidence = nil
		accumulator.intent = &copy
		accumulator.intentRank = incomingRank
	}
	accumulator.evidence = append(accumulator.evidence, intent.Evidence...)
	return nil
}

func resolvedIntentAuthority(intent ResolvedIntent) int {
	rank := 1
	for _, evidence := range intent.Evidence {
		switch evidence.Source {
		case "endstate-profile":
			return 3
		case "nix-user-profile":
			if rank < 2 {
				rank = 2
			}
		}
	}
	return rank
}

func emptyResult() Result {
	return Result{
		SchemaVersion: SchemaVersion,
		Sources:       []SourceStatus{}, Resolved: []ResolvedIntent{}, Settings: []SettingsCandidate{},
		Unresolved: []UnresolvedItem{}, Warnings: []Warning{},
	}
}

func evidenceFromItem(item InventoryItem) Evidence {
	return Evidence{Source: item.Source, Kind: item.Kind, Ref: item.Ref, Version: item.Version, DisplayName: item.DisplayName, UserFacing: item.UserFacing}
}

func unresolvedFromItem(item InventoryItem) UnresolvedItem {
	name := strings.TrimSpace(item.DisplayName)
	if name == "" {
		name = item.Ref
	}
	return UnresolvedItem{
		ID: item.Source + ":" + item.Ref, DisplayName: name, Reason: "mapping_missing",
		Message:  "No reviewed portable package mapping exists for this installed application.",
		Evidence: []Evidence{evidenceFromItem(item)},
	}
}

func canonicalEvidence(values []Evidence) []Evidence {
	sorted := append([]Evidence(nil), values...)
	sort.Slice(sorted, func(i, j int) bool {
		left, right := sorted[i], sorted[j]
		if left.Source != right.Source {
			return left.Source < right.Source
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Ref != right.Ref {
			return left.Ref < right.Ref
		}
		if left.Version != right.Version {
			return left.Version < right.Version
		}
		return left.DisplayName < right.DisplayName
	})
	result := make([]Evidence, 0, len(sorted))
	for _, value := range sorted {
		if len(result) > 0 && result[len(result)-1] == value {
			continue
		}
		result = append(result, value)
	}
	return result
}

func canonicalActions(values []SettingsAction) []SettingsAction {
	seen := make(map[SettingsAction]bool, len(values))
	for _, value := range values {
		seen[value] = true
	}
	order := []SettingsAction{SettingsCapture, SettingsRestore, SettingsVerify, SettingsRevert}
	result := make([]SettingsAction, 0, len(values))
	for _, value := range order {
		if seen[value] {
			result = append(result, value)
		}
	}
	return result
}

func mergeSettingsCandidate(target map[string]SettingsCandidate, incoming SettingsCandidate) {
	if strings.TrimSpace(incoming.ID) == "" {
		return
	}
	existing, ok := target[incoming.ID]
	if !ok {
		target[incoming.ID] = incoming
		return
	}
	existing.Actions = append(existing.Actions, incoming.Actions...)
	existing.Evidence = append(existing.Evidence, incoming.Evidence...)
	if existing.PackageID == "" {
		existing.PackageID = incoming.PackageID
	}
	if portabilityRank(incoming.Portability) > portabilityRank(existing.Portability) {
		existing.Portability = incoming.Portability
	}
	if existing.ModuleID == "" {
		existing.ModuleID = incoming.ModuleID
	}
	if existing.DisplayName == "" {
		existing.DisplayName = incoming.DisplayName
	}
	target[incoming.ID] = existing
}

func portabilityRank(state PortabilityState) int {
	switch state {
	case PortabilityResolved:
		return 3
	case PortabilityConfigOnly:
		return 2
	case PortabilityUnresolved:
		return 1
	default:
		return 0
	}
}

func deduplicateUnresolved(values []UnresolvedItem) []UnresolvedItem {
	result := make([]UnresolvedItem, 0, len(values))
	for _, value := range values {
		if len(result) > 0 && result[len(result)-1].ID == value.ID && result[len(result)-1].Reason == value.Reason {
			result[len(result)-1].Evidence = canonicalEvidence(append(result[len(result)-1].Evidence, value.Evidence...))
			continue
		}
		value.Evidence = canonicalEvidence(value.Evidence)
		result = append(result, value)
	}
	return result
}

func stringSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result[value] = true
		}
	}
	return result
}

func splitOnly(values []string) (map[string]bool, map[string]bool) {
	apps, modules := make(map[string]bool), make(map[string]bool)
	for _, value := range values {
		if strings.HasPrefix(value, "apps.") {
			modules[value] = true
		} else if value != "" {
			apps[value] = true
		}
	}
	return apps, modules
}
