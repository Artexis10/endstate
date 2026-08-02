// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/configtarget"
	"github.com/Artexis10/endstate/go-engine/internal/configvalidate"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

const (
	CaptureBundleStatusSkipped    = "skipped"
	CaptureBundleStatusFailed     = "failed"
	CaptureBundleDiagnosticEmpty  = "CONFIG_CAPTURE_EMPTY"
	CaptureBundleDiagnosticFailed = "CONFIG_CAPTURE_FAILED"
	LegacyCaptureStatusCaptured   = "captured"
	LegacyCaptureStatusSkipped    = "skipped"
	LegacyCaptureStatusFailed     = "failed"
)

// captureTargetCollisionWarning is the friendly, jargon-free notice surfaced
// when two captured modules claim the same restore target and the capture-time
// collision guard keeps a single deterministic winner. See
// collectLegacyCaptureLanes.
const captureTargetCollisionWarning = "Some settings were captured by more than one app. Endstate kept a single clean copy so your restore won't conflict."

// CaptureBundleRequest is the typed input for generation-aware bundle
// creation. Modules contains the matched catalog modules; generation plans are
// already resolved against one pinned catalog snapshot.
type CaptureBundleRequest struct {
	ManifestPath    string
	OutputPath      string
	EndstateVersion string
	Modules         []*modules.Module
	GenerationPlans []ConfigSetCapturePlan
	OnStage         func(Stage)
	// PreplanningDiagnostics carries deterministic catalog/discovery/generation
	// refusals that produced no executable collection plan. They are reported
	// and persisted exactly like collection-time diagnostics.
	PreplanningDiagnostics []CaptureBundleDiagnostic
	// Share marks this bundle as produced for sharing rather than self-rebuild.
	// It makes restore entries merge-preferring and blanks the machine name.
	Share bool
	// Name is the human label for the bundle (--name), recorded in metadata.
	Name string
	// ValidationContext virtualizes capture-side host I/O only. Nil preserves
	// production collection and publication behavior.
	ValidationContext *validationmode.Context
}

// CaptureBundleDiagnostic records a non-fatal per-config-set capture outcome.
// A failed or empty set never changes the version decision or falls back to a
// flat legacy restore lane.
type CaptureBundleDiagnostic struct {
	CaptureID   string `json:"captureId"`
	ModuleID    string `json:"moduleId"`
	ConfigSetID string `json:"configSetId"`
	InstanceID  string `json:"instanceId"`
	Status      string `json:"status"`
	Code        string `json:"code"`
	Detail      string `json:"detail,omitempty"`
}

// CaptureBundleResult describes the artifact that was actually produced.
type CaptureBundleResult struct {
	BundleSchemaVersion    string
	ManifestVersion        int
	ConfigCaptures         []manifest.ConfigCapture
	LegacyConfigLanes      []manifest.LegacyConfigLane
	ConfigCapturesIncluded []string
	ConfigModulesIncluded  []string
	ConfigModulesSkipped   []string
	Diagnostics            []CaptureBundleDiagnostic
	CaptureWarnings        []string
	LegacyModules          []LegacyModuleCaptureResult
	SensitiveExcluded      int
}

// LegacyModuleCaptureResult exposes facts from the single schema-v1
// collection pass that actually populated the artifact. Paths are the final
// portable zip paths, including mixed-v2 lane rewriting.
type LegacyModuleCaptureResult struct {
	ModuleID        string
	Paths           []string
	FilesCaptured   int
	SecretsExcluded int
	Status          string
}

// LegacyCaptureID returns a deterministic, opaque module-scoped identity in a
// domain distinct from generation CaptureID values.
func LegacyCaptureID(moduleID string) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte("endstate\x00legacy-config-capture\x00v1\x00"))
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(moduleID)))
	_, _ = hash.Write(length[:])
	_, _ = hash.Write([]byte(moduleID))
	return "legacy-" + hex.EncodeToString(hash.Sum(nil))
}

// ProjectCapturePlanningManifest produces the declaration/provenance view used
// to validate an already-selected capture plan before collection starts. It is
// deliberately a planning projection, not a claim that collection succeeded;
// the final artifact is still assembled only from collected results below.
// Keeping this projection in bundle makes legacy rewriting and generation
// provenance share the production source of truth.
func ProjectCapturePlanningManifest(apps []manifest.App, legacyModules []*modules.Module, plans []ConfigSetCapturePlan) (*manifest.Manifest, error) {
	projected := &manifest.Manifest{Version: 1, Apps: append([]manifest.App(nil), apps...)}
	if len(plans) > 0 {
		projected.Version = 2
	}
	verifiedModules := map[string]struct{}{}
	appendVerifies := func(mod *modules.Module) {
		if mod == nil {
			return
		}
		if _, exists := verifiedModules[mod.ID]; exists {
			return
		}
		verifiedModules[mod.ID] = struct{}{}
		projected.Verify = append(projected.Verify, projectModuleVerifies(mod)...)
	}

	legacy := append([]*modules.Module(nil), legacyModules...)
	sort.SliceStable(legacy, func(left, right int) bool {
		if legacy[left] == nil {
			return false
		}
		if legacy[right] == nil {
			return true
		}
		return legacy[left].ID < legacy[right].ID
	})
	seenLegacy := map[string]struct{}{}
	for _, mod := range legacy {
		if mod == nil || mod.EffectiveSchemaVersion() != 1 {
			continue
		}
		if _, duplicate := seenLegacy[mod.ID]; duplicate {
			continue
		}
		seenLegacy[mod.ID] = struct{}{}
		projected.ConfigModules = append(projected.ConfigModules, mod.ID)
		layoutID := legacyModuleDirName(mod.ID)
		legacyCaptureID := ""
		payloadRoot := ""
		if projected.Version == 2 {
			legacyCaptureID = LegacyCaptureID(mod.ID)
			payloadRoot = ConfigPayloadRoot(mod.ID, legacyCaptureID)
			layoutID = strings.TrimPrefix(payloadRoot, "configs/")
			projected.LegacyConfigLanes = append(projected.LegacyConfigLanes, manifest.LegacyConfigLane{
				CaptureID: legacyCaptureID, ModuleID: mod.ID, ModuleSchemaVersion: 1,
				PayloadRoot: payloadRoot,
			})
		}
		for _, restore := range mod.Restore {
			entry := rewriteLegacyRestore(restore, layoutID)
			entry.FromModule = mod.ID
			entry.LegacyCaptureID = legacyCaptureID
			projected.Restore = append(projected.Restore, entry)
		}
		appendVerifies(mod)
	}

	planning := append([]ConfigSetCapturePlan(nil), plans...)
	sort.SliceStable(planning, func(left, right int) bool {
		return capturePlanIdentity(planning[left]) < capturePlanIdentity(planning[right])
	})
	for _, plan := range planning {
		snapshot, err := moduleSnapshotIdentity(plan.Module)
		if err != nil {
			return nil, err
		}
		captureID := CaptureID(plan.Module.ID, plan.Set.ID, plan.Instance.ID)
		projected.ConfigCaptures = append(projected.ConfigCaptures, projectConfigCapture(
			plan, ConfigPayloadRoot(plan.Module.ID, captureID), []manifest.PayloadManifestEntry{}, snapshot,
		))
		appendVerifies(plan.Module)
	}
	return projected, nil
}

func validateCaptureModuleIdentities(candidates []*modules.Module, plans []ConfigSetCapturePlan) error {
	all := append([]*modules.Module(nil), candidates...)
	for _, plan := range plans {
		all = append(all, plan.Module)
	}
	seen := make(map[string]string, len(all))
	for _, mod := range all {
		if mod == nil {
			continue
		}
		identity, err := captureModuleObjectIdentity(mod)
		if err != nil {
			return fmt.Errorf("capture bundle: module identity for %s: %w", mod.ID, err)
		}
		if previous, exists := seen[mod.ID]; exists && previous != identity {
			return fmt.Errorf("capture bundle: ambiguous capture module identity for %s", mod.ID)
		}
		seen[mod.ID] = identity
	}
	return nil
}

// captureModuleObjectIdentity binds decisions to the exact semantic module
// object used by collection. Revision alone is insufficient for hand-built v1
// modules, while ID alone lets a foreign duplicate donate verifier authority.
func captureModuleObjectIdentity(mod *modules.Module) (string, error) {
	if mod == nil {
		return "", fmt.Errorf("module is nil")
	}
	canonical, err := json.Marshal(mod)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte("endstate\x00capture-module-object\x00v1\x00"))
	_, _ = hash.Write([]byte(mod.Revision))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(canonical)
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// CreateCaptureBundle creates either a v1 compatibility bundle or a
// structurally isolated v2 bundle. Only successful, nonempty generation
// captures enable v2.
func CreateCaptureBundle(request CaptureBundleRequest) (*CaptureBundleResult, error) {
	if strings.TrimSpace(request.ManifestPath) == "" || strings.TrimSpace(request.OutputPath) == "" {
		return nil, fmt.Errorf("capture bundle: manifestPath and outputPath are required")
	}
	selected := append([]*modules.Module(nil), request.Modules...)
	for _, plan := range request.GenerationPlans {
		selected = append(selected, plan.Module)
	}
	if err := preflightRegistryCaptureBoundaries(selected, request.ValidationContext); err != nil {
		return nil, err
	}
	if err := validateCaptureModuleIdentities(request.Modules, request.GenerationPlans); err != nil {
		return nil, err
	}
	if request.ValidationContext != nil {
		moduleID := request.ValidationContext.Descriptor().ModuleID
		for _, target := range []struct {
			coordinate string
			path       string
		}{{"manifestPath", request.ManifestPath}, {"outputPath", request.OutputPath}} {
			if err := request.ValidationContext.ValidateSandboxPath(filepath.Clean(target.path)); err != nil {
				return nil, captureIsolation(moduleID, target.coordinate, "path", target.coordinate, err)
			}
		}
	}
	var baseManifest *manifest.Manifest
	var err error
	var validationCaptureApp *manifest.App
	if request.ValidationContext != nil && strings.EqualFold(request.ValidationContext.Descriptor().Inventory.Driver, "validation") {
		inventory := request.ValidationContext.Descriptor().Inventory
		expected := manifest.App{ID: inventory.AppID, Refs: map[string]string{"windows": inventory.Ref}, Driver: inventory.Driver, Source: inventory.Source, DisplayName: inventory.DisplayName}
		validationCaptureApp = &expected
		baseManifest, err = manifest.LoadManifestForValidationCapture(request.ManifestPath, expected)
	} else {
		baseManifest, err = manifest.LoadManifest(request.ManifestPath)
	}
	if err != nil {
		if request.ValidationContext != nil {
			return nil, captureIsolation(captureRequestModuleID(request), "manifestPath", "path", "manifestPath", validationmode.ErrUnsafePath)
		}
		return nil, fmt.Errorf("capture bundle: load source manifest: %w", err)
	}

	stagingRoot, err := createCaptureWorkRoot(request.ValidationContext, "endstate-capture-bundle-")
	if err != nil {
		return nil, fmt.Errorf("capture bundle: create staging root: %w", err)
	}
	defer os.RemoveAll(stagingRoot)

	if request.OnStage != nil && (len(request.Modules) > 0 || len(request.GenerationPlans) > 0) {
		request.OnStage(StageSettings)
	}

	plans := append([]ConfigSetCapturePlan(nil), request.GenerationPlans...)
	sort.SliceStable(plans, func(left, right int) bool {
		return capturePlanIdentity(plans[left]) < capturePlanIdentity(plans[right])
	})
	configCaptures := make([]manifest.ConfigCapture, 0, len(plans))
	successfulGenerationModules := make([]*modules.Module, 0, len(plans))
	diagnostics := append([]CaptureBundleDiagnostic(nil), request.PreplanningDiagnostics...)
	var payloadValidationWarnings []string
	sensitiveExcluded := 0
	for _, plan := range plans {
		capture, excluded, diagnostic, isolationErr := collectGenerationCapture(plan, stagingRoot, request.ValidationContext)
		if isolationErr != nil {
			return nil, isolationErr
		}
		sensitiveExcluded += excluded
		if diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		warning, validationErr := validateCapturedPayload(plan, *capture, stagingRoot, request.ValidationContext)
		if validationErr != nil {
			return nil, validationErr
		}
		if warning != "" {
			payloadValidationWarnings = append(payloadValidationWarnings, warning)
		}
		configCaptures = append(configCaptures, *capture)
		successfulGenerationModules = append(successfulGenerationModules, plan.Module)
	}
	sort.Slice(configCaptures, func(left, right int) bool { return configCaptures[left].CaptureID < configCaptures[right].CaptureID })
	sortCaptureDiagnostics(diagnostics)

	manifestVersion := 1
	bundleSchemaVersion := "1.0"
	if len(configCaptures) > 0 {
		manifestVersion = 2
		bundleSchemaVersion = "2.0"
	}
	captureModules := request.Modules
	var deniedModules []string
	if request.Share {
		// Drop account- and device-bound modules whole rather than scrubbing them.
		// Their value to a recipient is near zero and partially redacting a
		// credential-shaped store is a bad trade.
		captureModules, deniedModules = partitionShareDeniedModules(captureModules)
	}
	legacy, err := collectLegacyCaptureLanes(captureModules, stagingRoot, manifestVersion == 2, request.ValidationContext)
	if err != nil {
		return nil, err
	}

	verifiedModules := append([]*modules.Module(nil), legacy.capturedModules...)
	verifiedModules = append(verifiedModules, successfulGenerationModules...)
	baseManifest.Verify, err = capturedModuleVerifies(verifiedModules)
	if err != nil {
		return nil, err
	}
	prepareCaptureManifest(baseManifest, manifestVersion, configCaptures, legacy)
	var redaction RedactionReport
	if request.Share {
		// Redact before the merge sniff, because redaction rewrites payload bytes
		// and the sniff decides on those bytes. Doing it the other way round could
		// classify a file that redaction then changes.
		var redactErr error
		redaction, redactErr = redactShareTree(stagingRoot, captureHostname())
		if redactErr != nil {
			if request.ValidationContext != nil {
				return nil, captureIsolation(captureRequestModuleID(request), "share.redaction", "portable", "configs", validationmode.ErrUnsafePath)
			}
			return nil, fmt.Errorf("capture bundle: redact share payloads: %w", redactErr)
		}
		// Decided at capture time and encoded in the restore types, so an older
		// engine applying this bundle still merges. Runs after the manifest is
		// assembled so it sees the final rewritten ./configs/ sources it needs to
		// sniff.
		baseManifest.Restore = preferMergeForShare(baseManifest.Restore, stagingRoot)
	}
	stagedManifest, err := resolveCapturePortable(request.ValidationContext, captureRequestModuleID(request), "manifest.publish", stagingRoot, "manifest.jsonc")
	if err != nil {
		return nil, err
	}
	manifestBytes, err := json.MarshalIndent(baseManifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("capture bundle: marshal manifest: %w", err)
	}
	if err := os.WriteFile(stagedManifest, manifestBytes, 0o644); err != nil {
		if request.ValidationContext != nil {
			return nil, captureIsolation(captureRequestModuleID(request), "manifest.publish", "portable", "manifest.jsonc", validationmode.ErrUnsafePath)
		}
		return nil, fmt.Errorf("capture bundle: write manifest: %w", err)
	}
	var strictErr error
	if validationCaptureApp != nil {
		_, strictErr = manifest.LoadProjectedManifestForValidationCapture(stagedManifest, *validationCaptureApp)
	} else {
		_, strictErr = manifest.LoadManifest(stagedManifest)
	}
	if strictErr != nil {
		if request.ValidationContext != nil {
			return nil, captureIsolation(captureRequestModuleID(request), "manifest.publish", "portable", "manifest.jsonc", validationmode.ErrUnsafePath)
		}
		return nil, fmt.Errorf("capture bundle: strict final manifest validation: %w", strictErr)
	}

	captureIDs := make([]string, 0, len(configCaptures))
	for _, capture := range configCaptures {
		captureIDs = append(captureIDs, capture.CaptureID)
	}
	captureWarnings := append([]string(nil), legacy.warnings...)
	captureWarnings = append(captureWarnings, payloadValidationWarnings...)
	for _, denied := range deniedModules {
		captureWarnings = append(captureWarnings,
			"share mode omitted "+denied+": its configuration is account- or device-bound and is not portable to another person")
	}
	for _, diagnostic := range diagnostics {
		captureWarnings = append(captureWarnings, captureBundleDiagnosticWarning(diagnostic))
	}
	sort.Strings(captureWarnings)
	machineName := captureHostname()
	if request.Share {
		// The hostname is an identifier of the sender, and a share bundle is
		// handed to someone else.
		machineName = ""
	}
	metadata := BundleMetadata{
		SchemaVersion:         bundleSchemaVersion,
		CapturedAt:            time.Now().UTC().Format(time.RFC3339),
		MachineName:           machineName,
		EndstateVersion:       request.EndstateVersion,
		ConfigModulesIncluded: nonNilStrings(legacy.included),
		ConfigModulesSkipped:  nonNilStrings(legacy.skipped),
		CaptureWarnings:       nonNilStrings(captureWarnings),
		OS:                    runtime.GOOS,
		Share:                 request.Share,
		Name:                  request.Name,
	}
	if request.Share {
		metadata.Redaction = &redaction
	}
	if manifestVersion == 2 {
		metadata.ManifestVersion = manifestVersion
		metadata.ConfigCapturesIncluded = nonNilStrings(captureIDs)
	}
	metadataBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("capture bundle: marshal metadata: %w", err)
	}
	metadataPath, err := resolveCapturePortable(request.ValidationContext, captureRequestModuleID(request), "metadata.publish", stagingRoot, "metadata.json")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(metadataPath, metadataBytes, 0o644); err != nil {
		if request.ValidationContext != nil {
			return nil, captureIsolation(captureRequestModuleID(request), "metadata.publish", "portable", "metadata.json", validationmode.ErrUnsafePath)
		}
		return nil, fmt.Errorf("capture bundle: write metadata: %w", err)
	}
	if request.OnStage != nil {
		request.OnStage(StagePackaging)
	}
	if err := writeCaptureZipAtomically(stagingRoot, request.OutputPath); err != nil {
		if request.ValidationContext != nil {
			return nil, captureIsolation(captureRequestModuleID(request), "outputPath", "path", "outputPath", validationmode.ErrUnsafePath)
		}
		return nil, err
	}

	return &CaptureBundleResult{
		BundleSchemaVersion:    bundleSchemaVersion,
		ManifestVersion:        manifestVersion,
		ConfigCaptures:         append([]manifest.ConfigCapture(nil), configCaptures...),
		LegacyConfigLanes:      append([]manifest.LegacyConfigLane(nil), legacy.lanes...),
		ConfigCapturesIncluded: nonNilStrings(captureIDs),
		ConfigModulesIncluded:  nonNilStrings(legacy.included),
		ConfigModulesSkipped:   nonNilStrings(legacy.skipped),
		Diagnostics:            append([]CaptureBundleDiagnostic(nil), diagnostics...),
		CaptureWarnings:        nonNilStrings(captureWarnings),
		LegacyModules:          cloneLegacyModuleResults(legacy.modules),
		SensitiveExcluded:      sensitiveExcluded,
	}, nil
}

func collectGenerationCapture(plan ConfigSetCapturePlan, stagingRoot string, context *validationmode.Context) (*manifest.ConfigCapture, int, *CaptureBundleDiagnostic, error) {
	diagnostic := capturePlanDiagnostic(plan)
	collection, err := CollectConfigSetWithValidation(plan, stagingRoot, context)
	if err != nil {
		var isolation *CaptureIsolationError
		if errors.As(err, &isolation) {
			return nil, 0, nil, err
		}
		var boundary *registrySecretBoundaryError
		if errors.As(err, &boundary) {
			return nil, 0, nil, err
		}
		diagnostic.Status = CaptureBundleStatusFailed
		diagnostic.Code = captureBundleErrorCode(err)
		diagnostic.Detail = err.Error()
		return nil, 0, &diagnostic, nil
	}
	if collection.FilesCollected == 0 {
		removeCapturePayload(stagingRoot, collection.PayloadRoot)
		diagnostic.Status = CaptureBundleStatusSkipped
		diagnostic.Code = CaptureBundleDiagnosticEmpty
		diagnostic.Detail = "generation capture produced no regular files"
		return nil, collection.SecretsExcluded, &diagnostic, nil
	}
	payloadHost, err := resolveCapturePortable(context, plan.Module.ID, "payloadRoot", stagingRoot, collection.PayloadRoot)
	if err != nil {
		removeCapturePayload(stagingRoot, collection.PayloadRoot)
		if context != nil {
			return nil, collection.SecretsExcluded, nil, captureIsolation(plan.Module.ID, "payloadRoot", "portable", collection.PayloadRoot, validationmode.ErrUnsafePath)
		}
		diagnostic.Status = CaptureBundleStatusFailed
		diagnostic.Code = ConfigCaptureUnsafePath
		diagnostic.Detail = err.Error()
		return nil, collection.SecretsExcluded, &diagnostic, nil
	}
	payloadManifest, err := BuildPayloadManifest(payloadHost)
	if err == nil {
		err = VerifyPayloadManifest(payloadHost, payloadManifest)
	}
	if err != nil {
		removeCapturePayload(stagingRoot, collection.PayloadRoot)
		if context != nil {
			return nil, collection.SecretsExcluded, nil, captureIsolation(plan.Module.ID, "payloadRoot", "portable", collection.PayloadRoot, validationmode.ErrUnsafePath)
		}
		diagnostic.Status = CaptureBundleStatusFailed
		diagnostic.Code = captureBundleErrorCode(err)
		diagnostic.Detail = err.Error()
		return nil, collection.SecretsExcluded, &diagnostic, nil
	}
	snapshot, err := WriteModuleSnapshot(stagingRoot, plan.Module)
	if err != nil {
		removeCapturePayload(stagingRoot, collection.PayloadRoot)
		if context != nil {
			return nil, collection.SecretsExcluded, nil, captureIsolation(plan.Module.ID, "captureModule.snapshotPath", "portable", "provenance/modules", validationmode.ErrUnsafePath)
		}
		diagnostic.Status = CaptureBundleStatusFailed
		diagnostic.Code = captureBundleErrorCode(err)
		diagnostic.Detail = err.Error()
		return nil, collection.SecretsExcluded, &diagnostic, nil
	}

	capture := projectConfigCapture(plan, collection.PayloadRoot, payloadManifest, snapshot)
	return &capture, collection.SecretsExcluded, nil, nil
}

func projectConfigCapture(plan ConfigSetCapturePlan, payloadRoot string, payloadManifest []manifest.PayloadManifestEntry, snapshot ModuleSnapshot) manifest.ConfigCapture {
	evidence := plan.Instance.Evidence
	return manifest.ConfigCapture{
		CaptureID:   CaptureID(plan.Module.ID, plan.Set.ID, plan.Instance.ID),
		ModuleID:    plan.Module.ID,
		ConfigSetID: plan.Set.ID,
		SourceInstance: manifest.ConfigSourceInstance{
			ID:                plan.Instance.ID,
			DetectorID:        plan.Instance.DetectorID,
			RawVersion:        plan.Instance.Version.Raw,
			NormalizedVersion: plan.Instance.Version.Normalized,
			Evidence: &manifest.ConfigSourceInstanceEvidence{
				Type: evidence.Type, AppID: evidence.AppID, Backend: evidence.Backend,
				Platform: evidence.Platform, Ref: evidence.Ref, Driver: evidence.Driver,
			},
		},
		SourceGeneration:            plan.Generation.ID,
		SourceGenerationFingerprint: plan.Generation.Fingerprint,
		CaptureModule: manifest.CaptureModuleProvenance{
			SchemaVersion: plan.Module.EffectiveSchemaVersion(), ContentHash: snapshot.ContentHash, SnapshotPath: snapshot.Path,
		},
		PayloadRoot:     payloadRoot,
		PayloadManifest: payloadManifest,
	}
}

// validateCapturedPayload runs the target generation's own declarative
// validations against the freshly staged payload — the identical rules restore
// staging will later enforce (migration.Stage -> configvalidate.ValidateStaging,
// configrestore -> ValidateResolved). Before this ran at capture time a payload
// that could never pass those rules was still shipped, and the user only found
// out when a restore refused it on another machine.
//
// A payload that fails is KEPT, not dropped: restore staging will refuse it
// anyway, so dropping here would silently strip the user's settings with no
// record, contrary to the invariant that losing settings must never be an
// invisible downgrade. Instead the caller surfaces a friendly warning marking
// the set as possibly-unrestorable, and returns "" when the payload is clean.
func validateCapturedPayload(plan ConfigSetCapturePlan, capture manifest.ConfigCapture, stagingRoot string, context *validationmode.Context) (string, error) {
	if plan.Generation == nil || len(plan.Generation.Validate) == 0 {
		return "", nil
	}
	payloadHost, err := resolveCapturePortable(context, plan.Module.ID, "payloadRoot", stagingRoot, capture.PayloadRoot)
	if err != nil {
		if context != nil {
			return "", err
		}
		return "", nil
	}
	if err := configvalidate.ValidateStaging(payloadHost, plan.Generation.Validate); err != nil {
		return capturePayloadValidationWarning(plan.Module), nil
	}
	return "", nil
}

// capturePayloadValidationWarning renders the user-facing, jargon-free warning
// for a captured set that cannot pass its module's restore-staging validations.
func capturePayloadValidationWarning(mod *modules.Module) string {
	name := ""
	if mod != nil {
		name = strings.TrimSpace(mod.DisplayName)
		if name == "" {
			name = legacyModuleDirName(mod.ID)
		}
	}
	if name == "" {
		name = "an app"
	}
	return fmt.Sprintf("Settings for %s were saved but may not restore cleanly on another machine.", name)
}

type legacyCaptureCollection struct {
	lanes           []manifest.LegacyConfigLane
	restores        []manifest.RestoreEntry
	moduleIDs       []string
	capturedModules []*modules.Module
	included        []string
	skipped         []string
	warnings        []string
	modules         []LegacyModuleCaptureResult
}

func collectLegacyCaptureLanes(candidates []*modules.Module, stagingRoot string, mixedV2 bool, context *validationmode.Context) (*legacyCaptureCollection, error) {
	legacy := &legacyCaptureCollection{}
	mods := append([]*modules.Module(nil), candidates...)
	sort.SliceStable(mods, func(left, right int) bool {
		if mods[left] == nil {
			return false
		}
		if mods[right] == nil {
			return true
		}
		return mods[left].ID < mods[right].ID
	})
	seen := make(map[string]struct{}, len(mods))
	staged := make([]stagedLegacyRestores, 0, len(mods))
	for _, mod := range mods {
		if mod == nil || mod.EffectiveSchemaVersion() != 1 {
			continue
		}
		if _, duplicate := seen[mod.ID]; duplicate {
			continue
		}
		seen[mod.ID] = struct{}{}
		shortID := legacyModuleDirName(mod.ID)
		if mixedV2 && len(mod.Restore) == 0 {
			legacy.skipped = append(legacy.skipped, shortID)
			legacy.warnings = append(legacy.warnings, fmt.Sprintf("module %s: captured legacy payload has no flat restore actions", mod.ID))
			legacy.modules = append(legacy.modules, LegacyModuleCaptureResult{ModuleID: mod.ID, Paths: []string{}, Status: LegacyCaptureStatusSkipped})
			continue
		}
		workRoot, err := createCaptureWorkRoot(context, "endstate-legacy-capture-")
		if err != nil {
			return nil, fmt.Errorf("capture bundle: create legacy staging for %s: %w", mod.ID, err)
		}
		fileCollected, secretsExcluded, fileErr := CollectConfigFilesWithValidation(mod, workRoot, context)
		if fileErr != nil {
			_ = os.RemoveAll(workRoot)
			var isolation *CaptureIsolationError
			if errors.As(fileErr, &isolation) {
				return nil, fileErr
			}
			if isRegistryCaptureBoundaryFailure(fileErr) {
				return nil, fileErr
			}
			legacy.skipped = append(legacy.skipped, shortID)
			legacy.warnings = append(legacy.warnings, fmt.Sprintf("module %s: %v", mod.ID, fileErr))
			legacy.modules = append(legacy.modules, LegacyModuleCaptureResult{
				ModuleID: mod.ID, Paths: []string{}, SecretsExcluded: secretsExcluded, Status: LegacyCaptureStatusFailed,
			})
			continue
		}
		registryCollected, registryErr := CollectRegistryKeysWithValidation(mod, workRoot, context)
		hadCollectionError := false
		if registryErr != nil {
			var isolation *CaptureIsolationError
			if errors.As(registryErr, &isolation) {
				_ = os.RemoveAll(workRoot)
				return nil, registryErr
			}
			if isRegistryCaptureBoundaryFailure(registryErr) {
				_ = os.RemoveAll(workRoot)
				return nil, registryErr
			}
			hadCollectionError = true
			legacy.warnings = append(legacy.warnings, fmt.Sprintf("module %s registry: %v", mod.ID, registryErr))
		}
		registryValuesCollected, registryValuesErr := CollectRegistryValuesWithValidation(mod, workRoot, context)
		if registryValuesErr != nil {
			var isolation *CaptureIsolationError
			if errors.As(registryValuesErr, &isolation) {
				_ = os.RemoveAll(workRoot)
				return nil, registryValuesErr
			}
			if isRegistryCaptureBoundaryFailure(registryValuesErr) {
				_ = os.RemoveAll(workRoot)
				return nil, registryValuesErr
			}
			hadCollectionError = true
			legacy.warnings = append(legacy.warnings, fmt.Sprintf("module %s registry values: %v", mod.ID, registryValuesErr))
		}
		collected := append(fileCollected, registryCollected...)
		collected = append(collected, registryValuesCollected...)
		if len(collected) == 0 {
			_ = os.RemoveAll(workRoot)
			legacy.skipped = append(legacy.skipped, shortID)
			status := LegacyCaptureStatusSkipped
			if hadCollectionError {
				status = LegacyCaptureStatusFailed
			}
			legacy.modules = append(legacy.modules, LegacyModuleCaptureResult{
				ModuleID: mod.ID, Paths: []string{}, SecretsExcluded: secretsExcluded, Status: status,
			})
			continue
		}

		layoutID := shortID
		legacyCaptureID := ""
		if mixedV2 {
			legacyCaptureID = LegacyCaptureID(mod.ID)
			// The full LegacyCaptureID stays the lane's identity; the on-disk
			// folder gets a human-readable name so mixed-v2 bundles read like
			// plain v1 ones (configs/powertoys-135f78ef/) instead of an opaque
			// configs/legacy-<64hex>/.
			layoutID = strings.TrimPrefix(ConfigPayloadRoot(mod.ID, legacyCaptureID), "configs/")
		}
		sourceRoot := filepath.Join(workRoot, "configs", shortID)
		if context != nil {
			sourceRoot, err = resolveCapturePortable(context, mod.ID, "legacy.sourceRoot", workRoot, path.Join("configs", shortID))
			if err != nil {
				_ = os.RemoveAll(workRoot)
				return nil, err
			}
		}
		destinationRoot, err := resolveCapturePortable(context, mod.ID, "legacy.payloadRoot", stagingRoot, path.Join("configs", layoutID))
		if err != nil {
			_ = os.RemoveAll(workRoot)
			return nil, fmt.Errorf("capture bundle: legacy root for %s: %w", mod.ID, err)
		}
		if err := os.MkdirAll(filepath.Dir(destinationRoot), 0o755); err != nil {
			_ = os.RemoveAll(workRoot)
			if context != nil {
				return nil, captureIsolation(mod.ID, "legacy.payloadRoot", "portable", path.Join("configs", layoutID), validationmode.ErrUnsafePath)
			}
			return nil, fmt.Errorf("capture bundle: create legacy parent for %s: %w", mod.ID, err)
		}
		if err := os.Rename(sourceRoot, destinationRoot); err != nil {
			_ = os.RemoveAll(workRoot)
			if context != nil {
				return nil, captureIsolation(mod.ID, "legacy.payloadRoot", "portable", path.Join("configs", layoutID), validationmode.ErrUnsafePath)
			}
			return nil, fmt.Errorf("capture bundle: stage legacy payload for %s: %w", mod.ID, err)
		}
		_ = os.RemoveAll(workRoot)

		legacy.included = append(legacy.included, shortID)
		legacy.moduleIDs = append(legacy.moduleIDs, mod.ID)
		legacy.capturedModules = append(legacy.capturedModules, mod)
		legacy.modules = append(legacy.modules, LegacyModuleCaptureResult{
			ModuleID:        mod.ID,
			Paths:           rewriteLegacyCollectionPaths(collected, shortID, layoutID),
			FilesCaptured:   len(collected),
			SecretsExcluded: secretsExcluded,
			Status:          LegacyCaptureStatusCaptured,
		})
		if mixedV2 {
			legacy.lanes = append(legacy.lanes, manifest.LegacyConfigLane{
				CaptureID: legacyCaptureID, ModuleID: mod.ID, ModuleSchemaVersion: 1, PayloadRoot: path.Join("configs", layoutID),
			})
		}
		staged = append(staged, stagedLegacyRestores{mod: mod, shortID: shortID, layoutID: layoutID, legacyCaptureID: legacyCaptureID})
	}

	// Capture-time target-collision guard (defense-in-depth behind the
	// catalog-integrity invariant): only successfully captured modules are staged
	// above, so resolving collisions here — after collection, before emitting
	// restore entries — keeps exactly one deterministic winner per overlapping
	// target. Without this, two modules claiming the same target both ship a
	// restore entry and the restore planner then fails BOTH sets
	// (internal/planner/config_collision.go), leaving the colliding settings
	// silently unrestorable.
	dropped, collided := resolveLegacyTargetCollisions(staged)
	if collided {
		legacy.warnings = append(legacy.warnings, captureTargetCollisionWarning)
	}
	for _, stage := range staged {
		dropSet := dropped[stage.mod.ID]
		entries := make([]manifest.RestoreEntry, 0, len(stage.mod.Restore))
		for restoreIndex, restore := range stage.mod.Restore {
			if _, drop := dropSet[restoreIndex]; drop {
				continue
			}
			entry := rewriteLegacyRestore(restore, stage.layoutID)
			// Module provenance travels with every bundle, not just mixed-v2 ones.
			// Restore input building routes an entry with an empty FromModule into
			// ordinaryRestores, which is converted with an empty filter and is never
			// reached by --only scoping — so a plain v1 bundle's entries were
			// unfilterable, and a recipient running `apply --only <app>
			// --enable-restore` got every module's config instead of the selection.
			entry.FromModule = stage.mod.ID
			// LegacyCaptureID stays v2-only: the v1 input validator rejects a
			// manifest that carries explicit v2 legacy identity.
			if mixedV2 {
				entry.LegacyCaptureID = stage.legacyCaptureID
			}
			entries = append(entries, entry)
		}
		// Every restore entry this module declared lost a target collision, so it
		// contributes nothing to the flat restore list. Keeping its lane would
		// leave a lane no restore entry references — which strict manifest
		// validation rejects, failing the whole capture — over a payload the
		// winner already restores. Drop the lane with the entries instead of
		// shipping half a module.
		if len(entries) == 0 && len(stage.mod.Restore) > 0 {
			legacy.dropLegacyLane(stage, stagingRoot)
			continue
		}
		legacy.restores = append(legacy.restores, entries...)
	}
	sort.Strings(legacy.included)
	sort.Strings(legacy.skipped)
	sort.Strings(legacy.warnings)
	sort.Strings(legacy.moduleIDs)
	sort.Slice(legacy.lanes, func(left, right int) bool { return legacy.lanes[left].CaptureID < legacy.lanes[right].CaptureID })
	sort.SliceStable(legacy.restores, func(left, right int) bool {
		return restoreSortKey(legacy.restores[left]) < restoreSortKey(legacy.restores[right])
	})
	sort.Slice(legacy.modules, func(left, right int) bool { return legacy.modules[left].ModuleID < legacy.modules[right].ModuleID })
	return legacy, nil
}

func createCaptureWorkRoot(context *validationmode.Context, pattern string) (string, error) {
	if context == nil {
		return os.MkdirTemp("", pattern)
	}
	parent := filepath.Join(context.Root(), "state", "capture-work")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", captureIsolation(context.Descriptor().ModuleID, "capture.workRoot", "portable", "capture-work", validationmode.ErrUnsafePath)
	}
	if err := context.ValidateSandboxPath(parent); err != nil {
		return "", captureIsolation(context.Descriptor().ModuleID, "capture.workRoot", "portable", "capture-work", err)
	}
	root, err := os.MkdirTemp(parent, pattern)
	if err != nil {
		return "", captureIsolation(context.Descriptor().ModuleID, "capture.workRoot", "portable", "capture-work", validationmode.ErrUnsafePath)
	}
	return root, nil
}

func captureRequestModuleID(request CaptureBundleRequest) string {
	if request.ValidationContext != nil {
		return request.ValidationContext.Descriptor().ModuleID
	}
	return "capture"
}

// stagedLegacyRestores holds a successfully captured schema-v1 module whose
// restore entries are emitted only after the capture-time collision guard has
// picked a winner for every overlapping target.
type stagedLegacyRestores struct {
	mod             *modules.Module
	shortID         string
	layoutID        string
	legacyCaptureID string
}

// dropLegacyLane removes every trace of a staged module that lost all of its
// restore entries to target collisions: the lane, the manifest module listing,
// and the staged payload nothing would restore. The module is reported as
// skipped rather than captured so the capture summary matches what the bundle
// actually carries.
func (legacy *legacyCaptureCollection) dropLegacyLane(stage stagedLegacyRestores, stagingRoot string) {
	lanes := legacy.lanes[:0]
	for _, lane := range legacy.lanes {
		if lane.ModuleID != stage.mod.ID {
			lanes = append(lanes, lane)
		}
	}
	legacy.lanes = lanes

	moduleIDs := legacy.moduleIDs[:0]
	for _, moduleID := range legacy.moduleIDs {
		if moduleID != stage.mod.ID {
			moduleIDs = append(moduleIDs, moduleID)
		}
	}
	legacy.moduleIDs = moduleIDs

	included := legacy.included[:0]
	for _, shortID := range legacy.included {
		if shortID != stage.shortID {
			included = append(included, shortID)
		}
	}
	legacy.included = included
	legacy.skipped = append(legacy.skipped, stage.shortID)

	for index, module := range legacy.modules {
		if module.ModuleID != stage.mod.ID {
			continue
		}
		legacy.modules[index].Status = LegacyCaptureStatusSkipped
		legacy.modules[index].Paths = []string{}
		legacy.modules[index].FilesCaptured = 0
	}

	if payloadRoot, err := containedHostPath(stagingRoot, path.Join("configs", stage.layoutID)); err == nil {
		_ = os.RemoveAll(payloadRoot)
	}
}

// resolveLegacyTargetCollisions expands every staged module's restore targets to
// canonical claims (the same host transform the restore planner applies) and,
// for each cross-module overlap, drops the losing module's colliding restore
// entry. It returns the per-module set of restore indices to drop and whether
// any collision was resolved. Two entries within one module may legitimately
// re-target the same path, so only cross-module overlaps are collisions.
func resolveLegacyTargetCollisions(staged []stagedLegacyRestores) (map[string]map[int]struct{}, bool) {
	type indexedClaim struct {
		stageIndex   int
		restoreIndex int
		claim        configtarget.Claim
	}
	claims := make([]indexedClaim, 0)
	for stageIndex, stage := range staged {
		for restoreIndex, restore := range stage.mod.Restore {
			claim, ok := legacyRestoreClaim(restore)
			if !ok {
				continue
			}
			claims = append(claims, indexedClaim{stageIndex: stageIndex, restoreIndex: restoreIndex, claim: claim})
		}
	}

	dropped := make(map[string]map[int]struct{})
	markDrop := func(moduleID string, restoreIndex int) {
		set := dropped[moduleID]
		if set == nil {
			set = make(map[int]struct{})
			dropped[moduleID] = set
		}
		set[restoreIndex] = struct{}{}
	}

	collided := false
	for left := 0; left < len(claims); left++ {
		for right := left + 1; right < len(claims); right++ {
			leftClaim, rightClaim := claims[left], claims[right]
			if leftClaim.stageIndex == rightClaim.stageIndex {
				continue
			}
			if !configtarget.ClaimsOverlap(leftClaim.claim, rightClaim.claim) {
				continue
			}
			leftMod := staged[leftClaim.stageIndex].mod
			rightMod := staged[rightClaim.stageIndex].mod
			if legacyModuleBeats(leftMod, rightMod) {
				markDrop(rightMod.ID, rightClaim.restoreIndex)
			} else {
				markDrop(leftMod.ID, leftClaim.restoreIndex)
			}
			collided = true
		}
	}
	return dropped, collided
}

// legacyRestoreClaim expands a schema-v1 restore target to its canonical claim
// using the exact host transform resolveRestoreTarget applies in the planner.
// Legacy modules carry no config instance, so an empty instance is used and only
// environment variables in the module-authored target are expanded. Restore
// types that declare no comparable target, or that fail to expand, are skipped
// so the guard only ever acts on a positively detected overlap.
func legacyRestoreClaim(restore modules.RestoreDef) (configtarget.Claim, bool) {
	var instance modules.ConfigInstance
	switch restore.Type {
	case "copy", "merge-json", "merge-ini", "append", "delete-glob":
		target, err := modules.ExpandInstancePath(restore.Target, instance, modules.HostPath)
		if err != nil {
			return configtarget.Claim{}, false
		}
		return configtarget.Claim{Kind: configtarget.Filesystem, Canonical: configtarget.CanonicalFilesystem(target)}, true
	case "registry-set":
		key, err := modules.ExpandInstanceTemplate(restore.Key, instance)
		if err != nil {
			return configtarget.Claim{}, false
		}
		valueName, err := modules.ExpandInstanceTemplate(restore.ValueName, instance)
		if err != nil {
			return configtarget.Claim{}, false
		}
		return configtarget.Claim{Kind: configtarget.Registry, Canonical: configtarget.CanonicalRegistry(key, valueName)}, true
	default:
		return configtarget.Claim{}, false
	}
}

// legacyModulePrecedence scores a module's matcher identity for collision
// tie-breaking. Higher wins: a package-identified module (winget/chocolatey)
// beats an installed-application match (exe/uninstall display name), which beats
// a pathExists-only module. This is the "more-specific matcher" ladder the
// collision precedence rule refers to.
func legacyModulePrecedence(mod *modules.Module) int {
	switch {
	case len(mod.Matches.Winget) > 0 || len(mod.Matches.Chocolatey) > 0:
		return 3
	case len(mod.Matches.Exe) > 0 || len(mod.Matches.UninstallDisplayName) > 0:
		return 2
	default:
		return 1
	}
}

// legacyModuleBeats reports whether left deterministically wins a target
// collision over right: higher matcher precedence first, then the
// lexicographically smaller module ID. Module IDs are unique, so this is a total
// order and always yields a single winner.
func legacyModuleBeats(left, right *modules.Module) bool {
	leftScore, rightScore := legacyModulePrecedence(left), legacyModulePrecedence(right)
	if leftScore != rightScore {
		return leftScore > rightScore
	}
	return left.ID < right.ID
}

func rewriteLegacyCollectionPaths(values []string, oldLayoutID, newLayoutID string) []string {
	paths := make([]string, 0, len(values))
	oldPrefix := "configs/" + oldLayoutID
	newPrefix := "configs/" + newLayoutID
	for _, value := range values {
		normalized := filepath.ToSlash(value)
		if normalized == oldPrefix || strings.HasPrefix(normalized, oldPrefix+"/") {
			normalized = newPrefix + strings.TrimPrefix(normalized, oldPrefix)
		}
		paths = append(paths, normalized)
	}
	sort.Strings(paths)
	return paths
}

func cloneLegacyModuleResults(values []LegacyModuleCaptureResult) []LegacyModuleCaptureResult {
	if values == nil {
		return []LegacyModuleCaptureResult{}
	}
	cloned := make([]LegacyModuleCaptureResult, len(values))
	for index, value := range values {
		cloned[index] = value
		cloned[index].Paths = nonNilStrings(value.Paths)
	}
	return cloned
}

func rewriteLegacyRestore(restore modules.RestoreDef, layoutID string) manifest.RestoreEntry {
	return manifest.RestoreEntry{
		Type: restore.Type, Source: rewriteSourcePath(restore.Source, layoutID), Target: restore.Target,
		Pattern: restore.Pattern, Reason: restore.Reason, Backup: restore.Backup, Optional: restore.Optional,
		Exclude: append([]string(nil), restore.Exclude...), Key: restore.Key, ValueName: restore.ValueName,
		ValueType: restore.ValueType, Data: restore.Data,
	}
}

func projectModuleVerifies(mod *modules.Module) []manifest.VerifyEntry {
	if mod == nil || len(mod.Verify) == 0 {
		return nil
	}
	projected := make([]manifest.VerifyEntry, 0, len(mod.Verify))
	for _, verify := range mod.Verify {
		projected = append(projected, manifest.VerifyEntry{
			Type: verify.Type, Command: verify.Command, Path: verify.Path,
			ValueName: verify.ValueName, ValueType: verify.ValueType, Data: verify.Data,
		})
	}
	return projected
}

// capturedModuleVerifies accepts only the exact module objects whose payloads
// made it into the artifact. It never looks a successful ID back up in the
// broader candidate set, where a foreign duplicate could donate authority.
func capturedModuleVerifies(capturedModules []*modules.Module) ([]manifest.VerifyEntry, error) {
	ordered := append([]*modules.Module(nil), capturedModules...)
	sort.SliceStable(ordered, func(left, right int) bool {
		if ordered[left] == nil {
			return false
		}
		if ordered[right] == nil {
			return true
		}
		return ordered[left].ID < ordered[right].ID
	})
	seen := make(map[string]string, len(ordered))
	var projected []manifest.VerifyEntry
	for _, mod := range ordered {
		if mod == nil {
			continue
		}
		identity, err := captureModuleObjectIdentity(mod)
		if err != nil {
			return nil, err
		}
		if previous, duplicate := seen[mod.ID]; duplicate {
			if previous != identity {
				return nil, fmt.Errorf("capture bundle: ambiguous captured module identity for %s", mod.ID)
			}
			continue
		}
		seen[mod.ID] = identity
		projected = append(projected, projectModuleVerifies(mod)...)
	}
	return projected, nil
}

func prepareCaptureManifest(base *manifest.Manifest, version int, captures []manifest.ConfigCapture, legacy *legacyCaptureCollection) {
	base.Version = version
	base.ConfigCaptures = nil
	base.LegacyConfigLanes = nil
	if version == 2 {
		base.ConfigCaptures = nonNilConfigCaptures(captures)
		base.LegacyConfigLanes = append([]manifest.LegacyConfigLane(nil), legacy.lanes...)
		base.ConfigModules = append([]string(nil), legacy.moduleIDs...)
		base.Restore = append([]manifest.RestoreEntry(nil), legacy.restores...)
		return
	}
	if len(legacy.included) > 0 {
		base.ConfigModules = append([]string(nil), legacy.moduleIDs...)
		if len(legacy.restores) > 0 {
			base.Restore = append([]manifest.RestoreEntry(nil), legacy.restores...)
		}
	}
}

func writeCaptureZipAtomically(stagingRoot, outputPath string) error {
	return writeCaptureZipAtomicallyUsing(stagingRoot, outputPath, replaceFileAtomically)
}

func writeCaptureZipAtomicallyUsing(stagingRoot, outputPath string, replace func(temporary, destination string) error) error {
	outputAbs, err := filepath.Abs(outputPath)
	if err != nil {
		return fmt.Errorf("capture bundle: resolve output path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(outputAbs), 0o755); err != nil {
		return fmt.Errorf("capture bundle: create output directory: %w", err)
	}
	temporaryFile, err := os.CreateTemp(filepath.Dir(outputAbs), "."+filepath.Base(outputAbs)+".tmp-*")
	if err != nil {
		return fmt.Errorf("capture bundle: create temporary zip: %w", err)
	}
	temporary := temporaryFile.Name()
	defer os.Remove(temporary)
	if err := createZipFromDirFile(stagingRoot, temporaryFile); err != nil {
		return fmt.Errorf("capture bundle: create zip: %w", err)
	}
	if err := replace(temporary, outputAbs); err != nil {
		return fmt.Errorf("capture bundle: publish zip: %w", err)
	}
	return nil
}

func capturePlanIdentity(plan ConfigSetCapturePlan) string {
	if plan.Module == nil || plan.Set == nil {
		return ""
	}
	return CaptureID(plan.Module.ID, plan.Set.ID, plan.Instance.ID)
}

func capturePlanDiagnostic(plan ConfigSetCapturePlan) CaptureBundleDiagnostic {
	diagnostic := CaptureBundleDiagnostic{CaptureID: capturePlanIdentity(plan), InstanceID: plan.Instance.ID}
	if plan.Module != nil {
		diagnostic.ModuleID = plan.Module.ID
	}
	if plan.Set != nil {
		diagnostic.ConfigSetID = plan.Set.ID
	}
	return diagnostic
}

func captureBundleErrorCode(err error) string {
	if code := ConfigCaptureDiagnosticCode(err); code != "" {
		return code
	}
	if code := IntegrityDiagnosticCode(err); code != "" {
		return code
	}
	return CaptureBundleDiagnosticFailed
}

func captureBundleDiagnosticWarning(diagnostic CaptureBundleDiagnostic) string {
	return fmt.Sprintf(
		"config capture: captureId=%q moduleId=%q configSetId=%q status=%q code=%q detail=%q",
		diagnostic.CaptureID,
		diagnostic.ModuleID,
		diagnostic.ConfigSetID,
		diagnostic.Status,
		diagnostic.Code,
		diagnostic.Detail,
	)
}

func removeCapturePayload(stagingRoot, portableRoot string) {
	if hostPath, err := containedHostPath(stagingRoot, portableRoot); err == nil {
		_ = os.RemoveAll(hostPath)
	}
}

func sortCaptureDiagnostics(diagnostics []CaptureBundleDiagnostic) {
	sort.SliceStable(diagnostics, func(left, right int) bool {
		leftKey := diagnostics[left].CaptureID + "\x00" + diagnostics[left].Code
		rightKey := diagnostics[right].CaptureID + "\x00" + diagnostics[right].Code
		return leftKey < rightKey
	})
}

func restoreSortKey(entry manifest.RestoreEntry) string {
	return strings.Join([]string{entry.LegacyCaptureID, entry.Type, entry.Source, entry.Target, entry.Key, entry.ValueName}, "\x00")
}

func legacyModuleDirName(moduleID string) string {
	return strings.TrimPrefix(moduleID, "apps.")
}

func captureHostname() string {
	hostname, _ := os.Hostname()
	return hostname
}

func nonNilStrings(values []string) []string {
	result := make([]string, len(values))
	copy(result, values)
	return result
}

func nonNilConfigCaptures(values []manifest.ConfigCapture) []manifest.ConfigCapture {
	if values == nil {
		return []manifest.ConfigCapture{}
	}
	return append([]manifest.ConfigCapture(nil), values...)
}

// partitionShareDeniedModules splits modules into those a share bundle may
// carry and those it must not, preserving order.
func partitionShareDeniedModules(mods []*modules.Module) (kept []*modules.Module, denied []string) {
	for _, mod := range mods {
		if mod != nil && ShareModuleDenied(mod.ID) {
			denied = append(denied, mod.ID)
			continue
		}
		kept = append(kept, mod)
	}
	sort.Strings(denied)
	return kept, denied
}
