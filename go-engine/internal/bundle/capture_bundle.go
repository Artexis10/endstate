// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/configvalidate"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
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

// CreateCaptureBundle creates either a v1 compatibility bundle or a
// structurally isolated v2 bundle. Only successful, nonempty generation
// captures enable v2.
func CreateCaptureBundle(request CaptureBundleRequest) (*CaptureBundleResult, error) {
	if strings.TrimSpace(request.ManifestPath) == "" || strings.TrimSpace(request.OutputPath) == "" {
		return nil, fmt.Errorf("capture bundle: manifestPath and outputPath are required")
	}
	baseManifest, err := manifest.LoadManifest(request.ManifestPath)
	if err != nil {
		return nil, fmt.Errorf("capture bundle: load source manifest: %w", err)
	}

	stagingRoot, err := os.MkdirTemp("", "endstate-capture-bundle-")
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
	diagnostics := append([]CaptureBundleDiagnostic(nil), request.PreplanningDiagnostics...)
	var payloadValidationWarnings []string
	sensitiveExcluded := 0
	for _, plan := range plans {
		capture, excluded, diagnostic := collectGenerationCapture(plan, stagingRoot)
		sensitiveExcluded += excluded
		if diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		if warning := validateCapturedPayload(plan, *capture, stagingRoot); warning != "" {
			payloadValidationWarnings = append(payloadValidationWarnings, warning)
		}
		configCaptures = append(configCaptures, *capture)
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
	legacy, err := collectLegacyCaptureLanes(captureModules, stagingRoot, manifestVersion == 2)
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
			return nil, fmt.Errorf("capture bundle: redact share payloads: %w", redactErr)
		}
		// Decided at capture time and encoded in the restore types, so an older
		// engine applying this bundle still merges. Runs after the manifest is
		// assembled so it sees the final rewritten ./configs/ sources it needs to
		// sniff.
		baseManifest.Restore = preferMergeForShare(baseManifest.Restore, stagingRoot)
	}
	stagedManifest := filepath.Join(stagingRoot, "manifest.jsonc")
	manifestBytes, err := json.MarshalIndent(baseManifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("capture bundle: marshal manifest: %w", err)
	}
	if err := os.WriteFile(stagedManifest, manifestBytes, 0o644); err != nil {
		return nil, fmt.Errorf("capture bundle: write manifest: %w", err)
	}
	if _, err := manifest.LoadManifest(stagedManifest); err != nil {
		return nil, fmt.Errorf("capture bundle: strict final manifest validation: %w", err)
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
	if err := os.WriteFile(filepath.Join(stagingRoot, "metadata.json"), metadataBytes, 0o644); err != nil {
		return nil, fmt.Errorf("capture bundle: write metadata: %w", err)
	}
	if request.OnStage != nil {
		request.OnStage(StagePackaging)
	}
	if err := writeCaptureZipAtomically(stagingRoot, request.OutputPath); err != nil {
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

func collectGenerationCapture(plan ConfigSetCapturePlan, stagingRoot string) (*manifest.ConfigCapture, int, *CaptureBundleDiagnostic) {
	diagnostic := capturePlanDiagnostic(plan)
	collection, err := CollectConfigSet(plan, stagingRoot)
	if err != nil {
		diagnostic.Status = CaptureBundleStatusFailed
		diagnostic.Code = captureBundleErrorCode(err)
		diagnostic.Detail = err.Error()
		return nil, 0, &diagnostic
	}
	if collection.FilesCollected == 0 {
		removeCapturePayload(stagingRoot, collection.PayloadRoot)
		diagnostic.Status = CaptureBundleStatusSkipped
		diagnostic.Code = CaptureBundleDiagnosticEmpty
		diagnostic.Detail = "generation capture produced no regular files"
		return nil, collection.SecretsExcluded, &diagnostic
	}
	payloadHost, err := containedHostPath(stagingRoot, collection.PayloadRoot)
	if err != nil {
		removeCapturePayload(stagingRoot, collection.PayloadRoot)
		diagnostic.Status = CaptureBundleStatusFailed
		diagnostic.Code = ConfigCaptureUnsafePath
		diagnostic.Detail = err.Error()
		return nil, collection.SecretsExcluded, &diagnostic
	}
	payloadManifest, err := BuildPayloadManifest(payloadHost)
	if err == nil {
		err = VerifyPayloadManifest(payloadHost, payloadManifest)
	}
	if err != nil {
		removeCapturePayload(stagingRoot, collection.PayloadRoot)
		diagnostic.Status = CaptureBundleStatusFailed
		diagnostic.Code = captureBundleErrorCode(err)
		diagnostic.Detail = err.Error()
		return nil, collection.SecretsExcluded, &diagnostic
	}
	snapshot, err := WriteModuleSnapshot(stagingRoot, plan.Module)
	if err != nil {
		removeCapturePayload(stagingRoot, collection.PayloadRoot)
		diagnostic.Status = CaptureBundleStatusFailed
		diagnostic.Code = captureBundleErrorCode(err)
		diagnostic.Detail = err.Error()
		return nil, collection.SecretsExcluded, &diagnostic
	}

	evidence := plan.Instance.Evidence
	return &manifest.ConfigCapture{
		CaptureID:   collection.CaptureID,
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
		PayloadRoot:     collection.PayloadRoot,
		PayloadManifest: payloadManifest,
	}, collection.SecretsExcluded, nil
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
func validateCapturedPayload(plan ConfigSetCapturePlan, capture manifest.ConfigCapture, stagingRoot string) string {
	if plan.Generation == nil || len(plan.Generation.Validate) == 0 {
		return ""
	}
	payloadHost, err := containedHostPath(stagingRoot, capture.PayloadRoot)
	if err != nil {
		return ""
	}
	if err := configvalidate.ValidateStaging(payloadHost, plan.Generation.Validate); err != nil {
		return capturePayloadValidationWarning(plan.Module)
	}
	return ""
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
	lanes     []manifest.LegacyConfigLane
	restores  []manifest.RestoreEntry
	moduleIDs []string
	included  []string
	skipped   []string
	warnings  []string
	modules   []LegacyModuleCaptureResult
}

func collectLegacyCaptureLanes(candidates []*modules.Module, stagingRoot string, mixedV2 bool) (*legacyCaptureCollection, error) {
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
		workRoot, err := os.MkdirTemp("", "endstate-legacy-capture-")
		if err != nil {
			return nil, fmt.Errorf("capture bundle: create legacy staging for %s: %w", mod.ID, err)
		}
		fileCollected, secretsExcluded, fileErr := CollectConfigFiles(mod, workRoot)
		if fileErr != nil {
			_ = os.RemoveAll(workRoot)
			legacy.skipped = append(legacy.skipped, shortID)
			legacy.warnings = append(legacy.warnings, fmt.Sprintf("module %s: %v", mod.ID, fileErr))
			legacy.modules = append(legacy.modules, LegacyModuleCaptureResult{
				ModuleID: mod.ID, Paths: []string{}, SecretsExcluded: secretsExcluded, Status: LegacyCaptureStatusFailed,
			})
			continue
		}
		registryCollected, registryErr := CollectRegistryKeys(mod, workRoot)
		hadCollectionError := false
		if registryErr != nil {
			hadCollectionError = true
			legacy.warnings = append(legacy.warnings, fmt.Sprintf("module %s registry: %v", mod.ID, registryErr))
		}
		registryValuesCollected, registryValuesErr := CollectRegistryValues(mod, workRoot)
		if registryValuesErr != nil {
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
			layoutID = legacyCaptureID
		}
		sourceRoot := filepath.Join(workRoot, "configs", shortID)
		destinationRoot, err := containedHostPath(stagingRoot, path.Join("configs", layoutID))
		if err != nil {
			_ = os.RemoveAll(workRoot)
			return nil, fmt.Errorf("capture bundle: legacy root for %s: %w", mod.ID, err)
		}
		if err := os.MkdirAll(filepath.Dir(destinationRoot), 0o755); err != nil {
			_ = os.RemoveAll(workRoot)
			return nil, fmt.Errorf("capture bundle: create legacy parent for %s: %w", mod.ID, err)
		}
		if err := os.Rename(sourceRoot, destinationRoot); err != nil {
			_ = os.RemoveAll(workRoot)
			return nil, fmt.Errorf("capture bundle: stage legacy payload for %s: %w", mod.ID, err)
		}
		_ = os.RemoveAll(workRoot)

		legacy.included = append(legacy.included, shortID)
		legacy.moduleIDs = append(legacy.moduleIDs, mod.ID)
		legacy.modules = append(legacy.modules, LegacyModuleCaptureResult{
			ModuleID:        mod.ID,
			Paths:           rewriteLegacyCollectionPaths(collected, shortID, layoutID),
			FilesCaptured:   len(collected),
			SecretsExcluded: secretsExcluded,
			Status:          LegacyCaptureStatusCaptured,
		})
		if mixedV2 {
			legacy.lanes = append(legacy.lanes, manifest.LegacyConfigLane{
				CaptureID: legacyCaptureID, ModuleID: mod.ID, ModuleSchemaVersion: 1, PayloadRoot: path.Join("configs", legacyCaptureID),
			})
		}
		for _, restore := range mod.Restore {
			entry := rewriteLegacyRestore(restore, layoutID)
			// Module provenance travels with every bundle, not just mixed-v2 ones.
			// Restore input building routes an entry with an empty FromModule into
			// ordinaryRestores, which is converted with an empty filter and is never
			// reached by --only scoping — so a plain v1 bundle's entries were
			// unfilterable, and a recipient running `apply --only <app>
			// --enable-restore` got every module's config instead of the selection.
			entry.FromModule = mod.ID
			// LegacyCaptureID stays v2-only: the v1 input validator rejects a
			// manifest that carries explicit v2 legacy identity.
			if mixedV2 {
				entry.LegacyCaptureID = legacyCaptureID
			}
			legacy.restores = append(legacy.restores, entry)
		}
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
