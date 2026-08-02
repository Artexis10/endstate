// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/packagesource"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

var (
	contextEnvironmentMu  = &sync.Mutex{}
	resultLeafPattern     = regexp.MustCompile(`^endstate-validation-(?:(?:guard|cwd)-)?[a-f0-9]{32}$`)
	authorityLeafPattern  = regexp.MustCompile(`^endstate-validation-task-[a-f0-9]{32}$`)
	stableScenarioPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// Run validates one exact production module/scenario pair and executes its
// declared config roundtrip through the caller-built engine. Operational failures
// are returned in Result; errors are reserved for harness-owned I/O failures.
func Run(ctx context.Context, request Request) (Result, error) {
	return runRequestWithRegistryFixtureFactory(ctx, request, func(context *validationmode.Context) (scenarioRegistryFixture, error) {
		return validationmode.NewRegistryFixture(context)
	})
}

func runRequestWithRegistryFixtureFactory(ctx context.Context, request Request, factory registryFixtureFactory) (Result, error) {
	selected, failure := compileSelection(request, time.Now().UTC())
	if failure != nil {
		result := failedRequestResult(request, failure)
		if selected != nil {
			result = failedSelectionResult(selected, failure)
		}
		if validateResultPath(request.ResultPath) == nil {
			if err := persistResult(request.ResultPath, result); err != nil {
				return result, err
			}
		}
		return result, nil
	}
	if !stableScenarioPattern.MatchString(selected.scenario.ID) {
		result := failedSelectionResult(selected, fail(CodeScenarioSelection, "selection", "scenario", "scenario ID cannot bind validation-mode authority"))
		if err := persistResult(request.ResultPath, result); err != nil {
			return result, err
		}
		return result, nil
	}

	return runSelectedScenarioWithRegistryFixtureFactory(ctx, selected, factory, executeSelectedScenario)
}

type scenarioExecution func(context.Context, *selection, *scenarioRuntime) Result

func runSelectedScenarioWithRegistryFixtureFactory(ctx context.Context, selected *selection, factory registryFixtureFactory, execute scenarioExecution) (Result, error) {
	runtime, cleanup, runtimeFailure, err := prepareScenarioRuntimeWithRegistryFixtureFactory(selected, factory)
	if runtimeFailure != nil {
		result := failedSelectionResult(selected, runtimeFailure)
		if persistErr := persistResult(selected.request.ResultPath, result); persistErr != nil {
			return result, persistErr
		}
		return result, nil
	}
	if err != nil {
		return failedSelectionResult(selected, fail(CodeIsolationFailure, "setup", "runtime", "prepare disposable runtime")), err
	}
	result := execute(ctx, selected, runtime)
	result = applyCleanupFailure(result, cleanup())
	if err := persistResult(selected.request.ResultPath, result); err != nil {
		return result, err
	}
	return result, nil
}

func executeSelectedScenario(ctx context.Context, selected *selection, runtime *scenarioRuntime) Result {
	var result Result
	switch selected.scenario.Mode {
	case validationmatrix.ScenarioConfigGenerationV2, validationmatrix.ScenarioConfigMigrationV2:
		result = executeV2Journey(ctx, runtime, newCLIJourneyExecutor(selected, runtime))
	case validationmatrix.ScenarioInstallContract:
		result = executeInstallJourney(ctx, runtime, newCLIJourneyExecutor(selected, runtime))
	case validationmatrix.ScenarioCaptureContract:
		result = executeCaptureContractJourney(ctx, runtime, newCLIJourneyExecutor(selected, runtime))
	case validationmatrix.ScenarioRestoreContract:
		result = executeRestoreContractJourney(ctx, runtime, newCLIJourneyExecutor(selected, runtime))
	case validationmatrix.ScenarioConfigRoundtripV1:
		result = executeJourney(ctx, runtime, newCLIJourneyExecutor(selected, runtime))
	default:
		result = failedSelectionResult(selected, fail(CodeUnsupportedFixture, "execution", "scenario.mode", "scenario mode is not implemented by this validation runtime"))
	}
	return result
}

type registryFixtureFactory func(*validationmode.Context) (scenarioRegistryFixture, error)

func failedRequestResult(request Request, failure *Failure) Result {
	return Result{
		SchemaVersion: ResultSchemaVersion, ModuleID: request.ModuleID, ScenarioID: request.ScenarioID,
		Status: ResultStatusFailed, ProofLevels: []validationmatrix.ProofLevel{}, AssertionCounts: map[string]int{},
		Failure: failure, PhaseTimings: map[string]time.Duration{},
	}
}

func failedSelectionResult(selected *selection, failure *Failure) Result {
	result := failedRequestResult(selected.request, failure)
	result.ModuleRevision = selected.module.Revision
	result.Kind = selected.scenario.Mode
	return result
}

func prepareScenarioRuntime(selected *selection) (*scenarioRuntime, func() error, *Failure, error) {
	return prepareScenarioRuntimeWithRegistryFixtureFactory(selected, func(context *validationmode.Context) (scenarioRegistryFixture, error) {
		return validationmode.NewRegistryFixture(context)
	})
}

func prepareScenarioRuntimeWithRegistryFixtureFactory(selected *selection, factory registryFixtureFactory) (*scenarioRuntime, func() error, *Failure, error) {
	nonce, err := randomNonce()
	if err != nil {
		return nil, func() error { return nil }, nil, err
	}
	temporaryRoot, err := safepath.CanonicalizePlatformRootAlias(filepath.Clean(os.TempDir()))
	if err != nil {
		return nil, func() error { return nil }, nil, err
	}
	authorityRoot := filepath.Join(temporaryRoot, "endstate-validation-task-"+nonce)
	root := filepath.Join(authorityRoot, "endstate-validation-"+nonce)
	guardRoot := filepath.Join(authorityRoot, "endstate-validation-guard-"+nonce)
	childWorkingDir := filepath.Join(authorityRoot, "endstate-validation-cwd-"+nonce)
	if err := os.Mkdir(authorityRoot, 0o700); err != nil {
		return nil, func() error { return nil }, nil, err
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		_ = cleanupAuthorityRoot(authorityRoot)
		return nil, func() error { return nil }, nil, err
	}
	if err := os.Mkdir(guardRoot, 0o700); err != nil {
		_ = cleanupGeneratedRoot(root)
		_ = cleanupAuthorityRoot(authorityRoot)
		return nil, func() error { return nil }, nil, err
	}
	if err := os.Mkdir(childWorkingDir, 0o700); err != nil {
		_ = cleanupGeneratedRoot(guardRoot)
		_ = cleanupGeneratedRoot(root)
		_ = cleanupAuthorityRoot(authorityRoot)
		return nil, func() error { return nil }, nil, err
	}
	var registryFixture scenarioRegistryFixture
	var cleanupOnce sync.Once
	var cleanupErr error
	cleanup := func() error {
		cleanupOnce.Do(func() {
			var cleanupErrors []error
			if registryFixture != nil {
				cleanupErrors = append(cleanupErrors, registryFixture.Cleanup())
			}
			cleanupErrors = append(cleanupErrors, cleanupGeneratedRoot(childWorkingDir))
			cleanupErrors = append(cleanupErrors, cleanupGeneratedRoot(guardRoot))
			cleanupErrors = append(cleanupErrors, cleanupGeneratedRoot(root))
			cleanupErrors = append(cleanupErrors, cleanupAuthorityRoot(authorityRoot))
			cleanupErr = errors.Join(cleanupErrors...)
		})
		return cleanupErr
	}

	inventory := validationInventory(selected.module)
	if selected.scenario.Mode == validationmatrix.ScenarioConfigGenerationV2 || selected.scenario.Mode == validationmatrix.ScenarioConfigMigrationV2 {
		inventory.Version = selected.v2Fixture.Definition.SourceVersion
	}
	descriptor := validationmode.Descriptor{
		SchemaVersion: 1, ScenarioID: selected.scenario.ID, Nonce: nonce,
		ModuleID: selected.module.ID, Inventory: inventory,
	}
	data, err := json.Marshal(descriptor)
	if err != nil {
		failure, setupErr := abandonScenarioRuntime(cleanup, nil, err)
		return nil, func() error { return nil }, failure, setupErr
	}
	if err := safepath.MkdirParent(root, ".endstate/validation-mode.json", 0o700); err != nil {
		failure, setupErr := abandonScenarioRuntime(cleanup, nil, err)
		return nil, func() error { return nil }, failure, setupErr
	}
	if err := safepath.AtomicWriteFile(filepath.Join(root, ".endstate", "validation-mode.json"), data, 0o600); err != nil {
		failure, setupErr := abandonScenarioRuntime(cleanup, nil, err)
		return nil, func() error { return nil }, failure, setupErr
	}
	if err := copyProductionModule(selected.module, root); err != nil {
		failure, setupErr := abandonScenarioRuntime(cleanup, nil, err)
		return nil, func() error { return nil }, failure, setupErr
	}
	validationContext, err := loadAndMaterializeContext(root)
	if err != nil {
		failure, setupErr := abandonScenarioRuntime(cleanup, nil, err)
		return nil, func() error { return nil }, failure, setupErr
	}
	if scenarioNeedsRegistryFixture(selected) {
		if factory == nil {
			failure, setupErr := abandonScenarioRuntime(cleanup, fail(CodeIsolationFailure, "setup", "runtime", "registry fixture factory is unavailable"), nil)
			return nil, func() error { return nil }, failure, setupErr
		}
		registryFixture, err = factory(validationContext)
		if err != nil || registryFixture == nil {
			failure, setupErr := abandonScenarioRuntime(cleanup, fail(CodeIsolationFailure, "setup", "runtime", "registry fixture could not be created"), nil)
			return nil, func() error { return nil }, failure, setupErr
		}
		if err := registryFixture.Cleanup(); err != nil {
			failure, setupErr := abandonScenarioRuntime(cleanup, fail(CodeIsolationFailure, "cleanup", "runtime", "registry fixture namespace could not be established"), nil)
			return nil, func() error { return nil }, failure, setupErr
		}
	}
	var plan *FixturePlan
	var v2Plan *V2FixturePlan
	var installPlan *InstallContractPlan
	var capturePlan *CaptureContractPlan
	var restorePlan *RestoreContractPlan
	var fixtureFailure *Failure
	switch selected.scenario.Mode {
	case validationmatrix.ScenarioConfigRoundtripV1:
		if moduleHasRegistryFixtureContract(selected.module) {
			if len(selected.registries.Entries) == 0 {
				fixtureFailure = fail(CodeUnsupportedFixture, "fixture", "capture.registry", "selected registry fixture contract is absent")
				break
			}
			plan, fixtureFailure = compileCompositeFixturePlanWithRegistryDefinitions(validationContext, selected.module, selected.scenario, selected.fixture, selected.registries, registryFixture)
		} else {
			plan, fixtureFailure = compileFixturePlan(validationContext, selected.module, selected.scenario, selected.fixture)
		}
	case validationmatrix.ScenarioConfigGenerationV2, validationmatrix.ScenarioConfigMigrationV2:
		v2Plan, fixtureFailure = compileV2FixturePlan(validationContext, selected.module, selected.scenario, selected.v2Fixture, inventory)
	case validationmatrix.ScenarioInstallContract:
		if selected.installPlan == nil || selected.installPlan.Inventory != inventory {
			fixtureFailure = fail(CodeUnsupportedFixture, "fixture", "inventory", "install plan inventory differs from runtime authority")
			break
		}
		compiled := *selected.installPlan
		compiled.Verifiers = append([]modules.VerifyDef(nil), selected.installPlan.Verifiers...)
		compiled.context = validationContext
		installPlan = &compiled
		fixtureFailure = installPlan.materializeManifest(root)
	case validationmatrix.ScenarioCaptureContract:
		if selected.capturePlan == nil || selected.capturePlan.Inventory != inventory {
			fixtureFailure = fail(CodeUnsupportedFixture, "fixture", "inventory", "capture plan inventory differs from runtime authority")
			break
		}
		compiled := *selected.capturePlan
		compiled.Targets = append([]CaptureContractTarget(nil), selected.capturePlan.Targets...)
		for index := range compiled.Targets {
			compiled.Targets[index].Content = append([]byte(nil), selected.capturePlan.Targets[index].Content...)
			resolved, resolveErr := validationContext.ResolveHostPath(compiled.Targets[index].AuthoredSource, validationmode.HostPathPolicy{})
			if resolveErr != nil || validationContext.ValidateSandboxPath(resolved) != nil || !fixtureContained(root, resolved) {
				fixtureFailure = fail(CodeUnsupportedFixture, "fixture", compiled.Targets[index].Coordinate, "capture source cannot be resolved through validation mode")
				break
			}
			compiled.Targets[index].Resolved = resolved
		}
		if fixtureFailure != nil {
			break
		}
		compiled.Verifiers = append([]modules.VerifyDef(nil), selected.capturePlan.Verifiers...)
		compiled.context = validationContext
		compiled.root = root
		capturePlan = &compiled
	case validationmatrix.ScenarioRestoreContract:
		if selected.restorePlan == nil || selected.restorePlan.Inventory != inventory {
			fixtureFailure = fail(CodeUnsupportedFixture, "fixture", "inventory", "restore plan inventory differs from runtime authority")
			break
		}
		compiled := *selected.restorePlan
		compiled.Verifiers = append([]modules.VerifyDef(nil), selected.restorePlan.Verifiers...)
		compiled.Restored = append([]byte(nil), selected.restorePlan.Restored...)
		compiled.Original = append([]byte(nil), selected.restorePlan.Original...)
		compiled.PayloadPath = ""
		compiled.context = validationContext
		compiled.root = root
		resolved, resolveErr := validationContext.ResolveHostPath(compiled.Restore.Target, validationmode.HostPathPolicy{})
		if resolveErr != nil || validationContext.ValidateSandboxPath(resolved) != nil || !fixtureContained(root, resolved) {
			fixtureFailure = fail(CodeUnsupportedFixture, "fixture", "restore[0].target", "restore target cannot be resolved through validation mode")
			break
		}
		plan = &FixturePlan{context: validationContext, Targets: []FixtureTarget{{
			Coordinate: "restore[0]", Authored: compiled.Restore.Target,
			Destination: strings.TrimPrefix(filepath.ToSlash(compiled.Restore.Source), "./payload/"),
			Resolved:    resolved, PayloadPath: resolved, Captured: string(compiled.Restored), Mutated: string(compiled.Original),
		}}}
		if fixtureFailure = compiled.materializeArtifact(root); fixtureFailure != nil {
			break
		}
		restorePlan = &compiled
	default:
		fixtureFailure = fail(CodeUnsupportedFixture, "fixture", "scenario.mode", "scenario mode is not implemented by this validation runtime")
	}
	if fixtureFailure != nil {
		failure, setupErr := abandonScenarioRuntime(cleanup, fixtureFailure, nil)
		return nil, func() error { return nil }, failure, setupErr
	}
	var transition *v2VersionTransition
	if selected.scenario.Mode == validationmatrix.ScenarioConfigMigrationV2 {
		transition, fixtureFailure = compileV2VersionTransition(root, selected.scenario, selected.v2Fixture, selected.module, inventory, nonce, descriptor, data)
		if fixtureFailure != nil {
			failure, setupErr := abandonScenarioRuntime(cleanup, fixtureFailure, nil)
			return nil, func() error { return nil }, failure, setupErr
		}
	}
	runtime := &scenarioRuntime{
		Module: selected.module, Scenario: selected.scenario, Plan: plan, V2Plan: v2Plan, InstallPlan: installPlan, CapturePlan: capturePlan, RestorePlan: restorePlan,
		V2Transition:  transition,
		AuthorityRoot: authorityRoot, Root: root, GuardRoot: guardRoot, ChildWorkingDir: childWorkingDir,
		Nonce: nonce, Inventory: inventory, RegistryFixture: registryFixture,
	}
	if err := runtime.prepareGuardsAndTools(); err != nil {
		failure, setupErr := abandonGuardPreparation(cleanup, err)
		return nil, func() error { return nil }, failure, setupErr
	}
	if err := runtime.captureIndependentBoundaries(selected.request.RepoRoot, selected.request.EnginePath); err != nil {
		failure, setupErr := abandonScenarioRuntime(cleanup, nil, err)
		return nil, func() error { return nil }, failure, setupErr
	}
	if err := validateSelectedSidecarBoundary(selected.request.RepoRoot, selected.record.FilePath, selected.record.SourceSnapshot(), runtime.repositoryBoundary); err != nil {
		failure, setupErr := abandonScenarioRuntime(cleanup, nil, err)
		return nil, func() error { return nil }, failure, setupErr
	}
	return runtime, cleanup, nil, nil
}

func scenarioNeedsRegistryFixture(selected *selection) bool {
	if selected == nil || selected.module == nil {
		return false
	}
	if selected.scenario.Mode == validationmatrix.ScenarioConfigRoundtripV1 && moduleHasRegistryFixtureContract(selected.module) {
		return true
	}
	if selected.installPlan != nil {
		for _, verifier := range selected.installPlan.Verifiers {
			if verifier.Type == "registry-key-exists" {
				return true
			}
		}
	}
	for _, verifier := range selected.module.Verify {
		if verifier.Type == "registry-key-exists" {
			return true
		}
	}
	return false
}

func abandonScenarioRuntime(cleanup func() error, failure *Failure, setupErr error) (*Failure, error) {
	if cleanupErr := cleanup(); cleanupErr != nil {
		return fail(CodeIsolationFailure, "cleanup", "runtime", "validation-owned cleanup did not complete safely"), nil
	}
	return failure, setupErr
}

func abandonGuardPreparation(cleanup func() error, setupErr error) (*Failure, error) {
	if failure := registryVerifierFixtureSetupFailure(setupErr); failure != nil {
		return abandonScenarioRuntime(cleanup, failure, nil)
	}
	return abandonScenarioRuntime(cleanup, nil, setupErr)
}

func randomNonce() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func validationInventory(mod *modules.Module) validationmode.Inventory {
	driver := "validation"
	ref := strings.TrimPrefix(mod.ID, "apps.")
	source := ""
	if len(mod.Matches.Winget) > 0 {
		driver, ref = "winget", mod.Matches.Winget[0]
		source = packagesource.ResolveWinget(ref, "")
	} else if len(mod.Matches.Chocolatey) > 0 {
		driver, ref = "chocolatey", mod.Matches.Chocolatey[0]
	}
	return validationmode.Inventory{
		AppID: strings.ToLower(strings.ReplaceAll(ref, ".", "-")), Driver: driver, Ref: ref,
		DisplayName: mod.DisplayName, Source: source, InitialState: "present",
	}
}

func copyProductionModule(mod *modules.Module, root string) error {
	raw, _, err := safepath.ReadRegularFile(mod.FilePath)
	if err != nil {
		return err
	}
	directory := filepath.Base(filepath.Dir(mod.FilePath))
	relative := filepath.ToSlash(filepath.Join("modules", "apps", directory, "module.jsonc"))
	if err := safepath.MkdirParent(root, relative, 0o700); err != nil {
		return err
	}
	destination, err := safepath.Resolve(root, relative)
	if err != nil {
		return err
	}
	if err := safepath.AtomicWriteFile(destination, raw, 0o600); err != nil {
		return err
	}
	copied, _, err := safepath.ReadRegularFile(destination)
	if err != nil || string(copied) != string(raw) {
		return fmt.Errorf("production module bytes changed during copy")
	}
	catalog, err := modules.LoadCatalog(filepath.Join(root, "modules", "apps"))
	if err != nil {
		return err
	}
	loaded := catalog[mod.ID]
	if loaded == nil || loaded.Revision != mod.Revision || string(loaded.CanonicalSnapshot()) != string(mod.CanonicalSnapshot()) {
		return fmt.Errorf("production module canonical identity changed during copy")
	}
	return nil
}

func loadAndMaterializeContext(root string) (*validationmode.Context, error) {
	contextEnvironmentMu.Lock()
	defer contextEnvironmentMu.Unlock()
	originalMode, modeSet := os.LookupEnv(validationmode.TestModeEnvironment)
	originalRoot, rootSet := os.LookupEnv(validationmode.RootEnvironment)
	restoreEnvironment := func() {
		if modeSet {
			_ = os.Setenv(validationmode.TestModeEnvironment, originalMode)
		} else {
			_ = os.Unsetenv(validationmode.TestModeEnvironment)
		}
		if rootSet {
			_ = os.Setenv(validationmode.RootEnvironment, originalRoot)
		} else {
			_ = os.Unsetenv(validationmode.RootEnvironment)
		}
	}
	if err := os.Setenv(validationmode.TestModeEnvironment, "1"); err != nil {
		return nil, err
	}
	if err := os.Setenv(validationmode.RootEnvironment, root); err != nil {
		restoreEnvironment()
		return nil, err
	}
	validationContext, err := validationmode.LoadFromEnvironment()
	if err != nil {
		restoreEnvironment()
		return nil, err
	}
	restoreAliases, err := validationContext.Activate()
	if err != nil {
		restoreEnvironment()
		return nil, err
	}
	if err := restoreAliases(); err != nil {
		restoreEnvironment()
		return nil, err
	}
	restoreEnvironment()
	return validationContext, nil
}

func persistResult(path string, result Result) error {
	if failure := validateResultPath(path); failure != nil {
		return fmt.Errorf("result path failed immediate revalidation: %s", failure.Detail)
	}
	parent := filepath.Dir(path)
	parentInfo, err := os.Lstat(parent)
	if err != nil || safepath.IsLinkOrReparse(parentInfo) || !parentInfo.IsDir() {
		return fmt.Errorf("result parent changed type before persistence")
	}
	if existing, err := os.Lstat(path); err == nil && (safepath.IsLinkOrReparse(existing) || !existing.Mode().IsRegular()) {
		return fmt.Errorf("result leaf changed type before persistence")
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("result leaf cannot be inspected before persistence")
	}
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return safepath.AtomicWriteFile(path, data, 0o600)
}

func applyCleanupFailure(result Result, cleanupErr error) Result {
	if cleanupErr == nil {
		return result
	}
	result.Status = ResultStatusFailed
	result.ProofLevels = []validationmatrix.ProofLevel{}
	result.Failure = fail(CodeIsolationFailure, "cleanup", "runtime", "validation-owned cleanup did not complete safely")
	return result
}

func cleanupGeneratedRoot(root string) error {
	if root == "" || !filepath.IsAbs(root) || !resultLeafPattern.MatchString(filepath.Base(root)) {
		return fmt.Errorf("refuse cleanup of unowned root")
	}
	temporaryRoot, err := safepath.CanonicalizePlatformRootAlias(filepath.Clean(os.TempDir()))
	if err != nil {
		return fmt.Errorf("canonicalize temporary root: %w", err)
	}
	relative, err := filepath.Rel(temporaryRoot, root)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refuse cleanup outside temp")
	}
	entries, err := fixtureTreePostorder(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		info, err := os.Lstat(entry.Path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || safepath.IsLinkOrReparse(info) || info.IsDir() != entry.Directory || !info.Mode().IsRegular() && !info.IsDir() {
			return fmt.Errorf("owned cleanup member changed type")
		}
		if err := os.Remove(entry.Path); err != nil {
			return err
		}
	}
	return nil
}

func cleanupAuthorityRoot(root string) error {
	if root == "" || !filepath.IsAbs(root) || !authorityLeafPattern.MatchString(filepath.Base(root)) {
		return fmt.Errorf("refuse cleanup of unowned task authority")
	}
	temporaryRoot, err := safepath.CanonicalizePlatformRootAlias(filepath.Clean(os.TempDir()))
	if err != nil {
		return fmt.Errorf("canonicalize temporary root: %w", err)
	}
	relative, err := filepath.Rel(temporaryRoot, root)
	if err != nil || filepath.Dir(relative) != "." || filepath.IsAbs(relative) {
		return fmt.Errorf("refuse cleanup of nested task authority")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return fmt.Errorf("refuse cleanup of nonempty task authority")
	}
	return os.Remove(root)
}
