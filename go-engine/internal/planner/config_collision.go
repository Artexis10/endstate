// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package planner

import (
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/configtarget"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

type resolvedTargetKind uint8

const (
	resolvedFilesystemTarget resolvedTargetKind = iota
	resolvedRegistryTarget
)

type resolvedTargetClaim struct {
	planIndex int
	kind      resolvedTargetKind
	canonical string
	display   string
}

type resolvedTargetIdentity struct {
	kind      resolvedTargetKind
	canonical string
}

func resolvePlanCollisions(plan *ConfigPlan) {
	if plan == nil {
		return
	}
	selected := make([]int, 0, len(plan.Sets))
	claims := make([]resolvedTargetClaim, 0)
	for index := range plan.Sets {
		if !isViableCompatibility(plan.Sets[index].Resolution) || plan.Sets[index].Resolution.Status != "" {
			continue
		}
		setClaims, safe := resolvePlanSetTargets(&plan.Sets[index], index)
		if !safe {
			markTargetCollision(&plan.Sets[index])
			continue
		}
		selected = append(selected, index)
		claims = append(claims, setClaims...)
	}

	markTargetInstanceCompetition(plan, selected)
	for left := 0; left < len(claims); left++ {
		for right := left + 1; right < len(claims); right++ {
			if claims[left].planIndex == claims[right].planIndex || !targetClaimsOverlap(claims[left], claims[right]) {
				continue
			}
			markTargetCollision(&plan.Sets[claims[left].planIndex])
			markTargetCollision(&plan.Sets[claims[right].planIndex])
		}
	}
}

func resolvePlanSetTargets(set *PlanSet, planIndex int) ([]resolvedTargetClaim, bool) {
	target, ok := selectedTargetInstance(set)
	if !ok || set.TargetGenerationDef == nil {
		set.Resolution.ResolvedTargets = []string{}
		return nil, false
	}
	instance := modules.ConfigInstance{
		ID:         target.ID,
		ModuleID:   target.ModuleID,
		DetectorID: target.DetectorID,
		Root:       target.Root,
		Version:    targetVersionEvidence(target),
		Evidence: modules.InstanceEvidence{
			Type:     target.Evidence.Type,
			AppID:    target.Evidence.AppID,
			Backend:  target.Evidence.Backend,
			Platform: target.Evidence.Platform,
			Ref:      target.Evidence.Ref,
			Driver:   target.Evidence.Driver,
		},
	}

	claims := make([]resolvedTargetClaim, 0, len(set.TargetGenerationDef.Restore))
	displays := make(map[resolvedTargetIdentity]string)
	for _, restore := range set.TargetGenerationDef.Restore {
		claim, err := resolveRestoreTarget(restore, instance, planIndex)
		if err != nil {
			set.Resolution.ResolvedTargets = []string{}
			return nil, false
		}
		claims = append(claims, claim)
		key := resolvedTargetIdentity{kind: claim.kind, canonical: claim.canonical}
		if current, exists := displays[key]; !exists || stableTargetDisplayLess(claim.display, current) {
			displays[key] = claim.display
		}
	}
	set.Resolution.ResolvedTargets = sortedResolvedTargetDisplays(displays)
	return claims, true
}

func resolveRestoreTarget(
	restore modules.RestoreDef,
	instance modules.ConfigInstance,
	planIndex int,
) (resolvedTargetClaim, error) {
	switch restore.Type {
	case "copy", "merge-json", "merge-ini", "append", "delete-glob":
		target, err := modules.ExpandInstancePath(restore.Target, instance, modules.HostPath)
		if err != nil {
			return resolvedTargetClaim{}, err
		}
		return resolvedTargetClaim{
			planIndex: planIndex,
			kind:      resolvedFilesystemTarget,
			canonical: canonicalFilesystemTarget(target),
			display:   target,
		}, nil
	case "registry-set":
		key, err := modules.ExpandInstanceTemplate(restore.Key, instance)
		if err != nil {
			return resolvedTargetClaim{}, err
		}
		valueName, err := modules.ExpandInstanceTemplate(restore.ValueName, instance)
		if err != nil {
			return resolvedTargetClaim{}, err
		}
		return resolvedTargetClaim{
			planIndex: planIndex,
			kind:      resolvedRegistryTarget,
			canonical: canonicalRegistryTarget(key, valueName),
			display:   strings.TrimRight(key, `\/`) + `\` + valueName,
		}, nil
	default:
		return resolvedTargetClaim{}, &unsupportedRestoreTargetError{restoreType: restore.Type}
	}
}

type unsupportedRestoreTargetError struct{ restoreType string }

func (e *unsupportedRestoreTargetError) Error() string {
	return "unsupported restore target kind " + e.restoreType
}

func selectedTargetInstance(set *PlanSet) (TargetInstance, bool) {
	for _, target := range set.TargetInstances {
		if target.ID == set.Resolution.TargetInstanceID {
			return target, true
		}
	}
	return TargetInstance{}, false
}

func markTargetInstanceCompetition(plan *ConfigPlan, selected []int) {
	owners := make(map[string][]int, len(selected))
	for _, index := range selected {
		set := &plan.Sets[index]
		key := set.Source.ModuleID + "\x00" + set.Source.ConfigSetID + "\x00" + set.Resolution.TargetInstanceID
		owners[key] = append(owners[key], index)
	}
	for _, indices := range owners {
		if len(indices) < 2 {
			continue
		}
		for _, index := range indices {
			markTargetCollision(&plan.Sets[index])
		}
	}
}

func markTargetCollision(set *PlanSet) {
	set.Resolution.Reason = reasonPointerValue(ReasonTargetCollision)
	set.Resolution.Status = StatusFailed
}

func targetClaimsOverlap(left, right resolvedTargetClaim) bool {
	return configtarget.ClaimsOverlap(
		configtarget.Claim{Kind: configtarget.Kind(left.kind), Canonical: left.canonical},
		configtarget.Claim{Kind: configtarget.Kind(right.kind), Canonical: right.canonical},
	)
}

func canonicalFilesystemTarget(target string) string {
	return configtarget.CanonicalFilesystem(target)
}

func canonicalRegistryTarget(key, valueName string) string {
	return configtarget.CanonicalRegistry(key, valueName)
}

func sortedResolvedTargetDisplays(displays map[resolvedTargetIdentity]string) []string {
	resolved := make([]string, 0, len(displays))
	for _, display := range displays {
		resolved = append(resolved, display)
	}
	sort.Slice(resolved, func(left, right int) bool {
		return stableTargetDisplayLess(resolved[left], resolved[right])
	})
	return resolved
}

func stableTargetDisplayLess(left, right string) bool {
	leftFolded := strings.ToLower(left)
	rightFolded := strings.ToLower(right)
	if leftFolded != rightFolded {
		return leftFolded < rightFolded
	}
	return left < right
}
