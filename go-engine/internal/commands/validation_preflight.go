// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

// validationProductionModulePreflight contains only explicit authority and
// declarations. Command integration supplies durable/materialized targets in a
// later task; the static module walker never consults process-global state.
type validationProductionModulePreflight struct {
	Context        *validationmode.Context
	Session        *ValidationModeSession
	Catalog        map[string]*modules.Module
	Modules        []*modules.Module
	Manifest       *manifest.Manifest
	PortableRoot   string
	ConfigPlans    []validationProductionConfigPlan
	Instances      []modules.ConfigInstance
	HostTargets    []validationProductionHostTarget
	SandboxTargets []validationProductionSandboxTarget
}

type validationProductionConfigPlan struct {
	SetID        string
	GenerationID string
	Instance     modules.ConfigInstance
}

type validationProductionHostTarget struct {
	Coordinate string
	Authored   string
	InstanceID string
}

type validationProductionSandboxTarget struct {
	Coordinate string
	Path       string
}

func preflightValidationProductionModule(input validationProductionModulePreflight) error {
	if input.Context == nil || input.Session == nil || input.Manifest == nil {
		return errors.New("validation production-module preflight requires context, session, and manifest")
	}
	descriptor := input.Context.Descriptor()
	authority := input.Catalog[descriptor.ModuleID]
	if len(input.Modules) != 1 || input.Modules[0] == nil || authority == nil ||
		input.Modules[0] != authority || authority.ID != descriptor.ModuleID || !validationModuleAuthorityIsPinned(authority) {
		return validationPreflightFailure(input.Session, "modules", "module-selection", isolationReasonUnsafePath)
	}
	mod := input.Modules[0]
	if err := validateValidationManifestContracts(input.Session, mod, input.Manifest, input.ConfigPlans, input.PortableRoot); err != nil {
		return err
	}
	instancePolicies, err := deriveValidationInstancePolicies(input.Context, input.Session, mod, input.Instances)
	if err != nil {
		return err
	}
	policies := make([]validationmode.HostPathPolicy, 0, len(instancePolicies))
	for _, instance := range input.Instances {
		policies = append(policies, instancePolicies[instance.ID])
	}
	if err := walkValidationModuleDeclarations(input.Context, input.Session, mod, input.Manifest, input.PortableRoot, policies); err != nil {
		return err
	}
	for _, target := range input.HostTargets {
		policy := validationmode.HostPathPolicy{}
		if target.InstanceID != "" {
			var ok bool
			policy, ok = instancePolicies[target.InstanceID]
			if !ok {
				return validationPreflightFailure(input.Session, target.Coordinate, tokenizedValidationTarget("path", target.Authored), isolationReasonUnsafePath)
			}
		}
		if strings.Contains(strings.ToLower(target.Authored), "${instance.root}") && policy.InstanceRoot == "" {
			return validationPreflightFailure(input.Session, target.Coordinate, tokenizedValidationTarget("path", target.Authored), isolationReasonUnsafePath)
		}
		if err := preflightHostPath(input.Context, input.Session, target.Coordinate, target.Authored, policy); err != nil {
			return err
		}
	}
	for _, target := range input.SandboxTargets {
		if err := input.Context.ValidateSandboxPath(target.Path); err != nil {
			return validationPreflightFailure(input.Session, target.Coordinate, tokenizedValidationTarget("path", target.Path), isolationReasonUnsafePath)
		}
	}
	return nil
}

func validationModuleAuthorityIsPinned(mod *modules.Module) bool {
	if mod == nil || len(mod.CanonicalSnapshot()) == 0 || mod.Revision == "" || mod.Unversioned != (mod.EffectiveSchemaVersion() == 1) {
		return false
	}
	snapshotRevision, err := modules.ComputeModuleRevision(mod.CanonicalSnapshot())
	if err != nil || snapshotRevision != mod.Revision {
		return false
	}
	pinned, err := modules.ParseModuleJSON(mod.CanonicalSnapshot())
	if err != nil {
		return false
	}
	liveDeclarations, pinnedDeclarations := *mod, *pinned
	liveDeclarations.FilePath, liveDeclarations.ModuleDir = "", ""
	pinnedDeclarations.FilePath, pinnedDeclarations.ModuleDir = "", ""
	return reflect.DeepEqual(liveDeclarations, pinnedDeclarations)
}

func deriveValidationInstancePolicies(context *validationmode.Context, session *ValidationModeSession, mod *modules.Module, instances []modules.ConfigInstance) (map[string]validationmode.HostPathPolicy, error) {
	policies := make(map[string]validationmode.HostPathPolicy, len(instances))
	for index, instance := range instances {
		coordinate := fmt.Sprintf("instances[%d]", index)
		detector := validationInstanceDetector(mod, instance.DetectorID)
		if instance.ModuleID != mod.ID || detector == nil || detector.Type != instance.Evidence.Type {
			return nil, validationPreflightFailure(session, coordinate+".detectorId", "instance-provenance", isolationReasonUnsafePath)
		}
		if detector.Type == "package" {
			inventory := context.Descriptor().Inventory
			if inventory.InitialState != "present" || instance.Root != "" || instance.Evidence.Path != "" || instance.ID == "" {
				return nil, validationPreflightFailure(session, coordinate+".root", "instance-provenance", isolationReasonUnsafePath)
			}
			probe := *mod
			probe.Config = &modules.ConfigDef{InstanceDetectors: []modules.InstanceDetectorDef{*detector}}
			expected, discoverErr := modules.DiscoverInstances(&probe, []modules.PackageEvidence{{
				AppID: inventory.AppID, Backend: inventory.Driver, Platform: "windows",
				Ref: inventory.Ref, Driver: inventory.Driver, RawVersion: inventory.Version,
			}}, modules.DiscoveryOptions{})
			if discoverErr != nil || len(expected) != 1 || expected[0] != instance {
				return nil, validationPreflightFailure(session, coordinate+".id", "instance-provenance", isolationReasonUnsafePath)
			}
			if _, duplicate := policies[instance.ID]; duplicate {
				return nil, validationPreflightFailure(session, coordinate+".id", "instance-provenance", isolationReasonUnsafePath)
			}
			policies[instance.ID] = validationmode.HostPathPolicy{}
			continue
		}
		if detector.Type != "path" || instance.Evidence.Type != "path" {
			return nil, validationPreflightFailure(session, coordinate+".detectorId", "instance-provenance", isolationReasonUnsafePath)
		}
		if instance.Root == "" || instance.Root != instance.Evidence.Path || instance.ID == "" {
			return nil, validationPreflightFailure(session, coordinate+".root", "instance-provenance", isolationReasonUnsafePath)
		}
		alias, pattern, err := validationDetectorPattern(context, detector.Glob)
		if err != nil || context.ValidateSandboxPath(instance.Root) != nil || !validationGlobContains(context, pattern, instance.Root) {
			return nil, validationPreflightFailure(session, coordinate+".root", "instance-provenance", isolationReasonUnsafePath)
		}
		expected, err := validationDiscoveredPathInstance(context, mod, detector, pattern, instance.Root)
		if err != nil || expected == nil {
			return nil, validationPreflightFailure(session, coordinate+".id", "instance-provenance", isolationReasonUnsafePath)
		}
		if instance.ID != expected.ID || instance.ModuleID != expected.ModuleID || instance.DetectorID != expected.DetectorID ||
			instance.Root != expected.Root || instance.Evidence != expected.Evidence || instance.CanonicalLocator != expected.CanonicalLocator {
			return nil, validationPreflightFailure(session, coordinate+".id", "instance-provenance", isolationReasonUnsafePath)
		}
		if instance.Version != expected.Version {
			return nil, validationPreflightFailure(session, coordinate+".version", "instance-provenance", isolationReasonUnsafePath)
		}
		policy := validationmode.HostPathPolicy{InstanceRoot: instance.Root, InstanceAlias: alias}
		if _, err := context.ResolveHostPath(`${instance.root}\probe`, policy); err != nil {
			return nil, validationPreflightFailure(session, coordinate+".root", "instance-provenance", isolationReasonUnsafePath)
		}
		if _, duplicate := policies[instance.ID]; duplicate {
			return nil, validationPreflightFailure(session, coordinate+".id", "instance-provenance", isolationReasonUnsafePath)
		}
		policies[instance.ID] = policy
	}
	return policies, nil
}

func validationPathInstanceLocator(root string) string {
	canonical := filepath.ToSlash(filepath.Clean(root))
	if runtime.GOOS == "windows" {
		canonical = strings.ToLower(canonical)
	}
	return "path:" + canonical
}

func validationDetectorPattern(context *validationmode.Context, authored string) (string, string, error) {
	if _, err := context.ResolveHostPath(validationHostProbe(authored), validationmode.HostPathPolicy{}); err != nil {
		return "", "", err
	}
	normalized := authored
	if strings.HasPrefix(authored, `~\`) || strings.HasPrefix(authored, "~/") {
		normalized = `%USERPROFILE%\` + authored[2:]
	}
	if !strings.HasPrefix(normalized, "%") {
		return "", "", validationmode.ErrUnsafePath
	}
	closing := strings.Index(normalized[1:], "%")
	if closing < 0 {
		return "", "", validationmode.ErrUnsafePath
	}
	closing++
	alias := normalized[1:closing]
	root, ok := context.VirtualRoot(alias)
	if !ok {
		return "", "", validationmode.ErrUnsafePath
	}
	suffix := strings.TrimLeft(normalized[closing+1:], `\/`)
	return alias, filepath.Join(root, filepath.FromSlash(strings.ReplaceAll(suffix, `\`, "/"))), nil
}

func validationGlobContains(context *validationmode.Context, pattern, root string) bool {
	matches, err := context.GlobSandboxPattern(pattern)
	if err != nil {
		return false
	}
	cleanRoot := filepath.Clean(root)
	for _, match := range matches {
		if strings.EqualFold(filepath.Clean(match), cleanRoot) {
			return true
		}
	}
	return false
}

func validationDiscoveredPathInstance(context *validationmode.Context, mod *modules.Module, detector *modules.InstanceDetectorDef, pattern, root string) (*modules.ConfigInstance, error) {
	matches, err := context.GlobSandboxPattern(pattern)
	if err != nil {
		return nil, err
	}
	discovered, err := modules.DiscoverInstances(mod, nil, modules.DiscoveryOptions{Glob: func(requested string) ([]string, error) {
		if requested == detector.Glob || strings.EqualFold(filepath.Clean(requested), filepath.Clean(pattern)) {
			return append([]string(nil), matches...), nil
		}
		return nil, nil
	}})
	if err != nil {
		return nil, err
	}
	cleanRoot := filepath.Clean(root)
	for index := range discovered {
		if discovered[index].DetectorID == detector.ID && strings.EqualFold(filepath.Clean(discovered[index].Root), cleanRoot) {
			return &discovered[index], nil
		}
	}
	return nil, nil
}

func validateValidationManifestContracts(session *ValidationModeSession, mod *modules.Module, mf *manifest.Manifest, plans []validationProductionConfigPlan, portableRoot string) error {
	seenModule := false
	for index, moduleID := range mf.ConfigModules {
		if moduleID != mod.ID || seenModule {
			return validationPreflightFailure(session, fmt.Sprintf("configModules[%d]", index), "module-provenance", isolationReasonUnsafePath)
		}
		seenModule = true
	}
	legacyLayout, legacyCaptureID, err := validateValidationLegacyLanes(session, mod, mf)
	if err != nil {
		return err
	}
	captureOnlyProvenance := validationModuleDeclaresCaptureOnlyProvenance(mod)
	requiresModuleProvenance := len(mf.Restore) > 0 || legacyLayout != "" || captureOnlyProvenance && len(mf.Verify) > 0
	if requiresModuleProvenance && !seenModule || seenModule && !requiresModuleProvenance && !captureOnlyProvenance {
		return validationPreflightFailure(session, "configModules", "module-provenance", isolationReasonUnsafePath)
	}

	for index, restore := range mf.Restore {
		if restore.FromModule != mod.ID {
			return validationPreflightFailure(session, fmt.Sprintf("restore[%d].fromModule", index), "module-provenance", isolationReasonUnsafePath)
		}
	}
	expectedRestores := projectModuleRestores(mod, "", "")
	if legacyLayout != "" {
		expectedRestores = projectModuleRestores(mod, legacyLayout, legacyCaptureID)
	} else if seenModule {
		projected := projectModuleRestores(mod, validationModuleLayoutID(mod.ID), "")
		if firstUnexpectedMultisetCoordinate(projected, manifestRestoreKeys(mf.Restore), "restore") == "" {
			expectedRestores = projected
		}
	}
	if coordinate := firstUnexpectedMultisetCoordinate(expectedRestores, manifestRestoreKeys(mf.Restore), "restore"); coordinate != "" {
		return validationPreflightFailure(session, coordinate, "restore-contract", isolationReasonUnsafePath)
	}

	expectedVerifies := projectModuleVerifies(mod)
	if coordinate := firstUnexpectedMultisetCoordinate(expectedVerifies, manifestVerifyKeys(mf.Verify), "verify"); coordinate != "" {
		return validationPreflightFailure(session, coordinate, "verify-contract", isolationReasonUnsafePath)
	}
	return validateValidationConfigCaptures(session, mod, mf, plans, portableRoot)
}

func validationModuleDeclaresCaptureOnlyProvenance(mod *modules.Module) bool {
	if mod == nil || mod.EffectiveSchemaVersion() != 1 || mod.Capture == nil || len(mod.Restore) != 0 {
		return false
	}
	return len(mod.Capture.Files)+len(mod.Capture.RegistryKeys)+len(mod.Capture.RegistryValues) > 0
}

func projectModuleRestores(mod *modules.Module, layoutID, legacyCaptureID string) []string {
	result := make([]string, 0, len(mod.Restore))
	for _, value := range mod.Restore {
		source := value.Source
		if layoutID != "" {
			source = projectValidationLegacySource(source, layoutID)
		}
		result = append(result, semanticKey(manifest.RestoreEntry{
			Type: value.Type, Source: source, Target: value.Target,
			Pattern: value.Pattern, Reason: value.Reason, Backup: value.Backup,
			Optional: value.Optional, Exclude: value.Exclude, FromModule: mod.ID,
			Key: value.Key, ValueName: value.ValueName, ValueType: value.ValueType, Data: value.Data,
			LegacyCaptureID: legacyCaptureID,
		}))
	}
	return result
}

func validateValidationLegacyLanes(session *ValidationModeSession, mod *modules.Module, mf *manifest.Manifest) (string, string, error) {
	if len(mf.LegacyConfigLanes) == 0 {
		for _, restore := range mf.Restore {
			if restore.LegacyCaptureID != "" {
				return "", "", validationPreflightFailure(session, "legacyConfigLanes", "module-provenance", isolationReasonUnsafePath)
			}
		}
		return "", "", nil
	}
	if len(mf.LegacyConfigLanes) != 1 {
		return "", "", validationPreflightFailure(session, "legacyConfigLanes[1]", "module-provenance", isolationReasonUnsafePath)
	}
	lane := mf.LegacyConfigLanes[0]
	expectedID := bundle.LegacyCaptureID(mod.ID)
	checks := []struct {
		ok         bool
		coordinate string
	}{
		{lane.ModuleID == mod.ID, "legacyConfigLanes[0].moduleId"},
		{mod.EffectiveSchemaVersion() == 1 && lane.ModuleSchemaVersion == 1, "legacyConfigLanes[0].moduleSchemaVersion"},
		{lane.CaptureID == expectedID, "legacyConfigLanes[0].captureId"},
		{lane.PayloadRoot == bundle.ConfigPayloadRoot(mod.ID, expectedID) || lane.PayloadRoot == path.Join("configs", expectedID), "legacyConfigLanes[0].payloadRoot"},
	}
	for _, check := range checks {
		if !check.ok {
			return "", "", validationPreflightFailure(session, check.coordinate, "module-provenance", isolationReasonUnsafePath)
		}
	}
	return strings.TrimPrefix(lane.PayloadRoot, "configs/"), expectedID, nil
}

var validationSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func validateValidationConfigCaptures(session *ValidationModeSession, mod *modules.Module, mf *manifest.Manifest, plans []validationProductionConfigPlan, portableRoot string) error {
	seenCaptureIDs := make(map[string]struct{}, len(mf.ConfigCaptures))
	for index, capture := range mf.ConfigCaptures {
		if _, duplicate := seenCaptureIDs[capture.CaptureID]; duplicate {
			return validationPreflightFailure(session, fmt.Sprintf("configCaptures[%d].captureId", index), "module-provenance", isolationReasonUnsafePath)
		}
		seenCaptureIDs[capture.CaptureID] = struct{}{}
	}
	if len(mf.ConfigCaptures) != len(plans) {
		coordinate := "configCaptures"
		if len(mf.ConfigCaptures) > len(plans) {
			coordinate = fmt.Sprintf("configCaptures[%d]", len(plans))
		}
		return validationPreflightFailure(session, coordinate, "module-provenance", isolationReasonUnsafePath)
	}
	usedPlans := make(map[int]struct{}, len(plans))
	for index := range mf.ConfigCaptures {
		capture := &mf.ConfigCaptures[index]
		prefix := fmt.Sprintf("configCaptures[%d]", index)
		if capture.ModuleID != mod.ID {
			return validationPreflightFailure(session, prefix+".moduleId", "module-provenance", isolationReasonUnsafePath)
		}
		set := validationConfigSet(mod, capture.ConfigSetID)
		if set == nil {
			return validationPreflightFailure(session, prefix+".configSetId", "module-provenance", isolationReasonUnsafePath)
		}
		generation := validationGeneration(set, capture.SourceGeneration)
		if generation == nil {
			return validationPreflightFailure(session, prefix+".sourceGeneration", "module-provenance", isolationReasonUnsafePath)
		}
		if capture.SourceGenerationFingerprint != generation.Fingerprint {
			return validationPreflightFailure(session, prefix+".sourceGenerationFingerprint", "module-provenance", isolationReasonUnsafePath)
		}
		detector := validationInstanceDetector(mod, capture.SourceInstance.DetectorID)
		if detector == nil {
			return validationPreflightFailure(session, prefix+".sourceInstance.detectorId", "module-provenance", isolationReasonUnsafePath)
		}
		if capture.SourceInstance.Evidence == nil || capture.SourceInstance.Evidence.Type != detector.Type {
			return validationPreflightFailure(session, prefix+".sourceInstance.evidence.type", "module-provenance", isolationReasonUnsafePath)
		}
		planIndex := validationCapturePlanIndex(plans, capture.ConfigSetID, capture.SourceGeneration, capture.SourceInstance.ID)
		if planIndex < 0 {
			return validationPreflightFailure(session, prefix, "module-provenance", isolationReasonUnsafePath)
		}
		plan := plans[planIndex]
		if plan.Instance.ModuleID != mod.ID || plan.Instance.DetectorID != capture.SourceInstance.DetectorID {
			return validationPreflightFailure(session, prefix+".sourceInstance.detectorId", "module-provenance", isolationReasonUnsafePath)
		}
		if capture.SourceInstance.RawVersion != plan.Instance.Version.Raw {
			return validationPreflightFailure(session, prefix+".sourceInstance.rawVersion", "module-provenance", isolationReasonUnsafePath)
		}
		if capture.SourceInstance.NormalizedVersion != plan.Instance.Version.Normalized {
			return validationPreflightFailure(session, prefix+".sourceInstance.normalizedVersion", "module-provenance", isolationReasonUnsafePath)
		}
		if !validationCaptureEvidenceMatches(capture.SourceInstance.Evidence, plan.Instance.Evidence) {
			return validationPreflightFailure(session, prefix+".sourceInstance.evidence", "module-provenance", isolationReasonUnsafePath)
		}
		expectedID := bundle.CaptureID(mod.ID, set.ID, plan.Instance.ID)
		if capture.CaptureID != expectedID {
			return validationPreflightFailure(session, prefix+".captureId", "module-provenance", isolationReasonUnsafePath)
		}
		if capture.CaptureModule.SchemaVersion != 2 {
			return validationPreflightFailure(session, prefix+".captureModule.schemaVersion", "module-provenance", isolationReasonUnsafePath)
		}
		if capture.CaptureModule.ContentHash != mod.Revision {
			return validationPreflightFailure(session, prefix+".captureModule.contentHash", "module-provenance", isolationReasonUnsafePath)
		}
		if capture.CaptureModule.SnapshotPath != validationSnapshotPath(mod) {
			return validationPreflightFailure(session, prefix+".captureModule.snapshotPath", "module-provenance", isolationReasonUnsafePath)
		}
		if capture.PayloadRoot != bundle.ConfigPayloadRoot(mod.ID, expectedID) {
			return validationPreflightFailure(session, prefix+".payloadRoot", "module-provenance", isolationReasonUnsafePath)
		}
		previousPath := ""
		for payloadIndex, entry := range capture.PayloadManifest {
			entryPrefix := fmt.Sprintf("%s.payloadManifest[%d]", prefix, payloadIndex)
			if _, err := validationmode.ResolvePortablePath(portableRoot, entry.RelativePath); err != nil || (previousPath != "" && entry.RelativePath <= previousPath) {
				return validationPreflightFailure(session, entryPrefix+".relativePath", "module-provenance", isolationReasonUnsafePath)
			}
			if entry.Size < 0 {
				return validationPreflightFailure(session, entryPrefix+".size", "module-provenance", isolationReasonUnsafePath)
			}
			if !validationSHA256Pattern.MatchString(entry.SHA256) {
				return validationPreflightFailure(session, entryPrefix+".sha256", "module-provenance", isolationReasonUnsafePath)
			}
			previousPath = entry.RelativePath
		}
		usedPlans[planIndex] = struct{}{}
	}
	if len(usedPlans) != len(plans) {
		return validationPreflightFailure(session, "configCaptures", "module-provenance", isolationReasonUnsafePath)
	}
	return nil
}

func validationCaptureEvidenceMatches(capture *manifest.ConfigSourceInstanceEvidence, plan modules.InstanceEvidence) bool {
	if capture == nil {
		return false
	}
	return capture.Type == plan.Type && capture.AppID == plan.AppID && capture.Backend == plan.Backend && capture.Platform == plan.Platform && capture.Ref == plan.Ref && capture.Driver == plan.Driver
}

func validationConfigSet(mod *modules.Module, id string) *modules.ConfigSetDef {
	if mod.Config == nil {
		return nil
	}
	for index := range mod.Config.Sets {
		if mod.Config.Sets[index].ID == id {
			return &mod.Config.Sets[index]
		}
	}
	return nil
}

func validationGeneration(set *modules.ConfigSetDef, id string) *modules.GenerationDef {
	for index := range set.Generations {
		if set.Generations[index].ID == id {
			return &set.Generations[index]
		}
	}
	return nil
}

func validationInstanceDetector(mod *modules.Module, id string) *modules.InstanceDetectorDef {
	if mod.Config == nil {
		return nil
	}
	for index := range mod.Config.InstanceDetectors {
		if mod.Config.InstanceDetectors[index].ID == id {
			return &mod.Config.InstanceDetectors[index]
		}
	}
	return nil
}

func validationCapturePlanIndex(plans []validationProductionConfigPlan, setID, generationID, instanceID string) int {
	for index := range plans {
		if plans[index].SetID == setID && plans[index].GenerationID == generationID && plans[index].Instance.ID == instanceID {
			return index
		}
	}
	return -1
}

func validationSnapshotPath(mod *modules.Module) string {
	return path.Join("provenance", "modules", validationSafeModuleID(mod.ID)+"-"+mod.Revision+".json")
}

func validationSafeModuleID(moduleID string) string {
	var builder strings.Builder
	var pending rune
	for _, character := range strings.ToLower(moduleID) {
		if (character >= '0' && character <= '9') || (character >= 'a' && character <= 'z') {
			if pending != 0 && builder.Len() > 0 {
				builder.WriteRune(pending)
			}
			pending = 0
			builder.WriteRune(character)
			continue
		}
		if character == '.' || character == '_' || character == '-' {
			if pending == 0 {
				pending = character
			} else {
				pending = '-'
			}
		} else {
			pending = '-'
		}
	}
	if builder.Len() == 0 {
		return "module"
	}
	return builder.String()
}

func validationModuleLayoutID(moduleID string) string { return strings.TrimPrefix(moduleID, "apps.") }

func projectValidationLegacySource(source, layoutID string) string {
	normalized := strings.ReplaceAll(source, `\`, "/")
	const prefix = "./payload/apps/"
	if !strings.HasPrefix(normalized, prefix) {
		return source
	}
	remainder := strings.TrimPrefix(normalized, prefix)
	parts := strings.SplitN(remainder, "/", 2)
	leaf := parts[0]
	if len(parts) == 2 {
		leaf = path.Base(parts[1])
	}
	return "./configs/" + layoutID + "/" + leaf
}

func manifestRestoreKeys(values []manifest.RestoreEntry) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, semanticKey(value))
	}
	return result
}

func projectModuleVerifies(mod *modules.Module) []string {
	result := make([]string, 0, len(mod.Verify))
	for _, value := range mod.Verify {
		result = append(result, semanticKey(manifest.VerifyEntry{
			Type: value.Type, Command: value.Command, Path: value.Path,
			ValueName: value.ValueName, ValueType: value.ValueType, Data: value.Data,
		}))
	}
	return result
}

func manifestVerifyKeys(values []manifest.VerifyEntry) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, semanticKey(value))
	}
	return result
}

func semanticKey(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func firstUnexpectedMultisetCoordinate(expected, actual []string, prefix string) string {
	counts := make(map[string]int, len(expected))
	for _, value := range expected {
		counts[value]++
	}
	for index, value := range actual {
		if counts[value] == 0 {
			return fmt.Sprintf("%s[%d]", prefix, index)
		}
		counts[value]--
	}
	for _, count := range counts {
		if count != 0 {
			return prefix
		}
	}
	return ""
}

func walkValidationModuleDeclarations(context *validationmode.Context, session *ValidationModeSession, mod *modules.Module, mf *manifest.Manifest, portableRoot string, instancePolicies []validationmode.HostPathPolicy) error {
	for index, authored := range mod.Matches.PathExists {
		if err := preflightHostPath(context, session, fmt.Sprintf("matches.pathExists[%d]", index), authored, validationmode.HostPathPolicy{}); err != nil {
			return err
		}
	}
	if mod.Capture != nil {
		if err := walkValidationCapture(context, session, mod.Capture, "capture", portableRoot, validationmode.HostPathPolicy{}); err != nil {
			return err
		}
	}
	if mod.Secrets != nil {
		for index, authored := range mod.Secrets.Files {
			if err := preflightHostPath(context, session, fmt.Sprintf("secrets.files[%d]", index), authored, validationmode.HostPathPolicy{}); err != nil {
				return err
			}
		}
	}
	for index, value := range mod.Restore {
		if err := walkValidationRestore(context, session, value, fmt.Sprintf("restore[%d]", index), portableRoot, validationmode.HostPathPolicy{}); err != nil {
			return err
		}
	}
	for index, value := range mod.Verify {
		if value.Path == "" || value.Type == "command-exists" {
			continue
		}
		if value.Type == "registry-key-exists" || value.Type == "registry-value-equals" {
			if err := preflightRegistry(context, session, fmt.Sprintf("verify[%d].path", index), value.Path, value.ValueName); err != nil {
				return err
			}
			continue
		}
		if err := preflightHostPath(context, session, fmt.Sprintf("verify[%d].path", index), value.Path, validationmode.HostPathPolicy{}); err != nil {
			return err
		}
	}
	if mod.Config != nil {
		for detectorIndex, detector := range mod.Config.InstanceDetectors {
			if detector.Type != "path" {
				continue
			}
			if err := preflightHostPath(context, session, fmt.Sprintf("config.instanceDetectors[%d].glob", detectorIndex), detector.Glob, validationmode.HostPathPolicy{}); err != nil {
				return err
			}
		}
		for setIndex := range mod.Config.Sets {
			set := &mod.Config.Sets[setIndex]
			setCoordinate := fmt.Sprintf("config.sets[%d]", setIndex)
			for generationIndex := range set.Generations {
				generation := &set.Generations[generationIndex]
				generationCoordinate := fmt.Sprintf("%s.generations[%d]", setCoordinate, generationIndex)
				policies := instancePolicies
				if len(policies) == 0 {
					policies = []validationmode.HostPathPolicy{{}}
				}
				for _, policy := range policies {
					if generation.Capture != nil {
						if err := walkValidationCapture(context, session, generation.Capture, generationCoordinate+".capture", portableRoot, policy); err != nil {
							return err
						}
					}
					for restoreIndex, restore := range generation.Restore {
						if err := walkValidationRestore(context, session, restore, fmt.Sprintf("%s.restore[%d]", generationCoordinate, restoreIndex), portableRoot, policy); err != nil {
							return err
						}
					}
				}
				for validationIndex, validation := range generation.Validate {
					if err := preflightPortablePath(session, portableRoot, fmt.Sprintf("%s.validate[%d].path", generationCoordinate, validationIndex), validation.Path); err != nil {
						return err
					}
				}
			}
			for migrationIndex, migration := range set.Migrations {
				migrationCoordinate := fmt.Sprintf("%s.migrations[%d]", setCoordinate, migrationIndex)
				for operationIndex, operation := range migration.Operations {
					operationCoordinate := fmt.Sprintf("%s.operations[%d]", migrationCoordinate, operationIndex)
					switch operation.Type {
					case "file-copy", "file-move":
						if err := preflightPortablePath(session, portableRoot, operationCoordinate+".source", operation.Source); err != nil {
							return err
						}
						if err := preflightPortablePath(session, portableRoot, operationCoordinate+".target", operation.Target); err != nil {
							return err
						}
					case "file-delete", "json-set", "json-delete", "json-move", "ini-set", "ini-delete", "ini-move":
						if err := preflightPortablePath(session, portableRoot, operationCoordinate+".path", operation.Path); err != nil {
							return err
						}
					}
				}
				for validationIndex, validation := range migration.Validate {
					if err := preflightPortablePath(session, portableRoot, fmt.Sprintf("%s.validate[%d].path", migrationCoordinate, validationIndex), validation.Path); err != nil {
						return err
					}
				}
			}
		}
	}
	for appIndex, app := range mf.Apps {
		if app.Manual == nil || app.Manual.VerifyPath == "" {
			continue
		}
		if err := preflightHostPath(context, session, fmt.Sprintf("apps[%d].manual.verifyPath", appIndex), app.Manual.VerifyPath, validationmode.HostPathPolicy{}); err != nil {
			return err
		}
	}
	return nil
}

func walkValidationCapture(context *validationmode.Context, session *ValidationModeSession, capture *modules.CaptureDef, coordinate, portableRoot string, policy validationmode.HostPathPolicy) error {
	for index, value := range capture.Files {
		if err := preflightHostPath(context, session, fmt.Sprintf("%s.files[%d].source", coordinate, index), value.Source, policy); err != nil {
			return err
		}
		if err := preflightPortablePath(session, portableRoot, fmt.Sprintf("%s.files[%d].dest", coordinate, index), value.Dest); err != nil {
			return err
		}
	}
	for index, value := range capture.RegistryKeys {
		if err := preflightRegistry(context, session, fmt.Sprintf("%s.registryKeys[%d].key", coordinate, index), value.Key, ""); err != nil {
			return err
		}
		if err := preflightPortablePath(session, portableRoot, fmt.Sprintf("%s.registryKeys[%d].dest", coordinate, index), value.Dest); err != nil {
			return err
		}
	}
	for index, value := range capture.RegistryValues {
		if err := preflightRegistry(context, session, fmt.Sprintf("%s.registryValues[%d].key", coordinate, index), value.Key, value.ValueName); err != nil {
			return err
		}
	}
	return nil
}

func walkValidationRestore(context *validationmode.Context, session *ValidationModeSession, restore modules.RestoreDef, coordinate, portableRoot string, policy validationmode.HostPathPolicy) error {
	if restore.Source != "" {
		if err := preflightPortablePath(session, portableRoot, coordinate+".source", restore.Source); err != nil {
			return err
		}
	}
	if restore.Key != "" {
		return preflightRegistry(context, session, coordinate+".key", restore.Key, restore.ValueName)
	}
	if restore.Target != "" {
		return preflightHostPath(context, session, coordinate+".target", restore.Target, policy)
	}
	return nil
}

func preflightHostPath(context *validationmode.Context, session *ValidationModeSession, coordinate, authored string, policy validationmode.HostPathPolicy) error {
	if strings.EqualFold(authored, "${instance.root}") && policy.InstanceRoot != "" {
		policy.AllowRoot = true
	}
	if _, err := context.ResolveHostPath(validationHostProbe(authored), policy); err != nil {
		return validationPreflightFailure(session, coordinate, tokenizedValidationTarget("path", authored), isolationReasonUnsafePath)
	}
	original, err := context.OriginalHostPath(authored, policy)
	if err != nil {
		return validationPreflightFailure(session, coordinate, tokenizedValidationTarget("path", authored), isolationReasonUnsafePath)
	}
	if original == "" {
		return nil
	}
	return session.registerOriginalFilesystemPath(coordinate, tokenizedValidationTarget("path", authored), original)
}

func validationHostProbe(authored string) string {
	probe := strings.NewReplacer("*", "x", "?", "x").Replace(authored)
	for {
		opening := strings.IndexByte(probe, '[')
		if opening < 0 {
			return probe
		}
		closing := strings.IndexByte(probe[opening+1:], ']')
		if closing < 0 {
			return probe
		}
		closing += opening + 1
		probe = probe[:opening] + "x" + probe[closing+1:]
	}
}

func preflightPortablePath(session *ValidationModeSession, root, coordinate, authored string) error {
	portable := strings.ReplaceAll(authored, `\`, "/")
	portable = strings.TrimPrefix(portable, "./")
	if _, err := validationmode.ResolvePortablePath(root, portable); err != nil {
		return validationPreflightFailure(session, coordinate, tokenizedValidationTarget("portable", authored), isolationReasonUnsafePath)
	}
	return nil
}

func preflightRegistry(context *validationmode.Context, session *ValidationModeSession, coordinate, key, valueName string) error {
	semantic, err := validationmode.NormalizeHKCU(key)
	if err != nil {
		return validationPreflightFailure(session, coordinate, tokenizedValidationTarget("registry", key), isolationReasonUnsafeRegistry)
	}
	if _, err := context.MapHKCU(semantic); err != nil {
		return validationPreflightFailure(session, coordinate, tokenizedValidationTarget("registry", key), isolationReasonUnsafeRegistry)
	}
	protection := validationmode.ProtectedRegistry{Key: semantic, ValueName: valueName, WholeKey: valueName == ""}
	identity := semantic + "\x00" + valueName
	return session.registerOriginalRegistryProtection(coordinate, tokenizedValidationTarget("registry", identity), protection)
}

func validationPreflightFailure(session *ValidationModeSession, coordinate, target string, reason isolationReason) error {
	if session == nil {
		return fmt.Errorf("%w: validation preflight session is inactive", validationmode.ErrUnsafePath)
	}
	return session.recordIsolationFinding(coordinate, target, reason)
}

func tokenizedValidationTarget(kind, authored string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(authored)))
	return kind + "-" + hex.EncodeToString(digest[:6])
}
