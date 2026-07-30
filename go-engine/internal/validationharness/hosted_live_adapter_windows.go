// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// hostedLiveWindowsBindings is intentionally private: callers cannot supply
// arbitrary commands or host mutation authority to the hosted runner.
type hostedLiveWindowsBindings struct {
	enginePath             string
	observer               LiveObserver
	winget                 liveTrustedAppXBinding
	issuer                 *liveReceiptIssuer
	boundary               liveBoundaryReader
	runnerImage            string
	runProcess             func(context.Context, LiveProcessRequest) (*liveExecutionReceipt, error)
	snapshotTargets        func(LiveDefinition, string) (hostedLiveTargets, error)
	snapshotStorage        func(hostedLiveStorageRoot) (hostedLiveStorageSnapshot, error)
	wipeTargets            func(liveReceiptAdmission, trustedLiveHostMutationPermit, LiveDefinition, string) (*liveHostMutationReceipt, error)
	cleanupAttempt         func(liveReceiptAdmission, trustedLiveHostMutationPermit, LiveDefinition, string, windowsLiveAttemptRoot) (*liveHostMutationReceipt, error)
	hostname               func() (string, error)
	captureClaims          func(*liveReceiptIssuer, LiveDefinition, *modules.Module, *liveExecutionReceipt, uint64, [32]byte, string, string) (liveCaptureArtifactClaims, *Failure)
	inspectArtifact        func(LiveDefinition, []liveTargetSnapshot, liveCaptureArtifactClaims, string) (liveArtifactEvidence, *Failure)
	declaredTargetPaths    func(LiveDefinition, string) ([]string, error)
	declaredTargetBindings func(LiveDefinition, string) (map[string]string, error)
}

type hostedLiveSequencePlan struct {
	preflightUninstall, preflightWipe uint64
	apply, verify                     uint64
	seed, capture                     uint64
	uninstall, wipe                   uint64
	rebuild, revert, recovery         uint64
	convergence                       uint64
	finalUninstall, finalWipe         uint64
	attemptCleanup                    uint64
}

func deriveHostedLiveSequencePlan(session *LiveAuthoritySession) (hostedLiveSequencePlan, error) {
	if session == nil {
		return hostedLiveSequencePlan{}, fmt.Errorf("hosted live authority is unavailable")
	}
	operations := session.definition.operations
	offset := uint64(0)
	plan := hostedLiveSequencePlan{}
	if len(operations) == 15 {
		if operations[1].Operation != string(liveOperationWingetExactUninstall) || operations[2].Operation != string(liveOperationDeclaredTargetWipe) {
			return plan, fmt.Errorf("hosted live preflight plan is invalid")
		}
		plan.preflightUninstall, plan.preflightWipe, offset = 1, 2, 2
	} else if len(operations) != 13 {
		return plan, fmt.Errorf("hosted live operation plan is invalid")
	}
	expected := []liveOperation{liveOperationEngineApply, liveOperationEngineVerify, liveOperationHashBoundSeed, liveOperationEngineCapture, liveOperationWingetExactUninstall, liveOperationDeclaredTargetWipe, liveOperationEngineRebuild, liveOperationEngineRevert, liveOperationEngineRebuild, liveOperationEngineRebuild, liveOperationWingetExactUninstall, liveOperationDeclaredTargetWipe, liveOperationAttemptRootCleanup}
	for index, operation := range expected {
		sequence := uint64(index) + 1 + offset
		if operations[sequence].Operation != string(operation) {
			return hostedLiveSequencePlan{}, fmt.Errorf("hosted live operation plan is invalid")
		}
		switch index {
		case 0:
			plan.apply = sequence
		case 1:
			plan.verify = sequence
		case 2:
			plan.seed = sequence
		case 3:
			plan.capture = sequence
		case 4:
			plan.uninstall = sequence
		case 5:
			plan.wipe = sequence
		case 6:
			plan.rebuild = sequence
		case 7:
			plan.revert = sequence
		case 8:
			plan.recovery = sequence
		case 9:
			plan.convergence = sequence
		case 10:
			plan.finalUninstall = sequence
		case 11:
			plan.finalWipe = sequence
		case 12:
			plan.attemptCleanup = sequence
		}
	}
	return plan, nil
}

type windowsHostedLiveRunner struct {
	session                                                                                                   *LiveAuthoritySession
	definition                                                                                                LiveDefinition
	appData                                                                                                   string
	attemptRoot                                                                                               windowsLiveAttemptRoot
	checkoutRoot                                                                                              string
	storageRoot                                                                                               hostedLiveStorageRoot
	plan                                                                                                      hostedLiveSequencePlan
	bindings                                                                                                  hostedLiveWindowsBindings
	initialBoundary                                                                                           liveBoundarySnapshot
	repositoryBoundary                                                                                        boundaryTree
	engineBoundary                                                                                            boundaryEntry
	applyReceipt, verifyReceipt                                                                               *liveExecutionReceipt
	seedReceipt, captureReceipt                                                                               *liveExecutionReceipt
	rebuildReceipt, revertReceipt, recoveryReceipt, convergenceReceipt                                        *liveExecutionReceipt
	seedTargets, wipedTargets, rebuiltTargets, revertedTargets, recoveredTargets, convergedTargets            hostedLiveTargets
	preRestoreStorage, rebuiltStorage, revertedStorage, recoveredStorage, convergenceStorage                  hostedLiveStorageSnapshot
	captureArtifact                                                                                           liveArtifactEvidence
	captureVersion                                                                                            string
	packageAfterApply, packageAfterRebuild, packageAfterRevert, packageAfterRecovery, packageAfterConvergence PackageObservation
	cleanupEntered                                                                                            bool
	journeyProven                                                                                             bool
}

func (runner *windowsHostedLiveRunner) validate(context.Context) error {
	if runner == nil || runner.session == nil || runner.bindings.issuer == nil {
		return fmt.Errorf("hosted live authority is unavailable")
	}
	return nil
}
func (runner *windowsHostedLiveRunner) compile(context.Context) error {
	if runner == nil || validateLiveDefinition(runner.definition) != nil || runner.bindings.enginePath == "" || runner.bindings.observer.Process == nil || runner.bindings.observer.Registry == nil || runner.bindings.observer.Path == nil || runner.bindings.observer.Files == nil || !runner.bindings.winget.metadata.receipt.valid || !validHostedLiveEvidenceValue(runner.bindings.runnerImage) || runner.bindings.boundary == nil || runner.bindings.runProcess == nil || runner.bindings.snapshotTargets == nil || runner.bindings.snapshotStorage == nil || runner.bindings.wipeTargets == nil || runner.bindings.cleanupAttempt == nil || runner.bindings.hostname == nil || runner.bindings.captureClaims == nil || runner.bindings.inspectArtifact == nil || runner.bindings.declaredTargetPaths == nil || runner.bindings.declaredTargetBindings == nil || runner.checkoutRoot == "" || runner.storageRoot.valid() != nil {
		return fmt.Errorf("hosted live definition or bindings are invalid")
	}
	compiled, err := CompileLiveDefinition(runner.checkoutRoot, "apps.notepad-plus-plus")
	if err != nil {
		return err
	}
	left, err := CanonicalLiveDefinitionSHA256(runner.definition)
	if err != nil {
		return err
	}
	right, err := CanonicalLiveDefinitionSHA256(compiled)
	if err != nil || left != right {
		return fmt.Errorf("hosted live definition changed")
	}
	if runner.repositoryBoundary, err = snapshotBoundaryTree(runner.checkoutRoot); err != nil {
		return err
	}
	if runner.engineBoundary, err = snapshotBoundaryFile(runner.bindings.enginePath); err != nil {
		return err
	}
	return nil
}
func (runner *windowsHostedLiveRunner) initial(ctx context.Context) error {
	snapshot, err := snapshotLiveBoundary(ctx, runner.definition, runner.bindings.boundary)
	if err != nil {
		return err
	}
	executedPreflight := false
	if snapshot.RequireAbsent() != nil {
		if runner.plan.preflightUninstall == 0 || !hostedLiveFullyPresent(snapshot) {
			return fmt.Errorf("hosted live initial boundary is mixed or ambiguous")
		}
		if err := runner.wingetUninstall(ctx, runner.plan.preflightUninstall); err != nil {
			return err
		}
		if err := runner.targetWipe(runner.plan.preflightWipe); err != nil {
			return err
		}
		executedPreflight = true
		snapshot, err = snapshotLiveBoundary(ctx, runner.definition, runner.bindings.boundary)
		if err != nil || snapshot.RequireAbsent() != nil {
			return fmt.Errorf("hosted live preflight did not establish absence")
		}
	}
	if !executedPreflight && runner.plan.preflightUninstall != 0 && runner.plan.preflightWipe != 0 && runner.bindings.issuer.skipDeclaredPreflight() == nil {
		runner.initialBoundary = snapshot
		return nil
	} else if !executedPreflight && runner.plan.preflightUninstall != 0 {
		return fmt.Errorf("hosted live preflight skip is unavailable")
	}
	if runner.plan.apply == 0 {
		return fmt.Errorf("hosted live operation plan is invalid")
	}
	runner.initialBoundary = snapshot
	return nil
}
func hostedLiveFullyPresent(snapshot liveBoundarySnapshot) bool {
	if snapshot.observation.Status != LiveObservationPresent {
		return false
	}
	for _, target := range snapshot.targets {
		if !target.present {
			return false
		}
	}
	return true
}
func (runner *windowsHostedLiveRunner) engineApply(ctx context.Context) error {
	admission, err := runner.bindings.issuer.admit(liveOperationEngineApply, runner.plan.apply, runner.session.NonceFor(liveOperationEngineApply, runner.plan.apply))
	if err != nil {
		return err
	}
	permit, err := runner.session.MintMutationPermit(admission)
	if err != nil {
		admission.complete()
		return err
	}
	receipt, err := runner.bindings.runProcess(ctx, newLiveTrustedEngineMutation(admission, permit, liveOperationEngineApply, maxLiveProcessOutputBytes))
	if err != nil {
		return err
	}
	runner.applyReceipt = receipt
	return nil
}
func (runner *windowsHostedLiveRunner) observePresent(ctx context.Context) error {
	observation, err := runner.observeExactPackage(ctx)
	if err != nil {
		return err
	}
	runner.packageAfterApply = observation
	return nil
}

func (runner *windowsHostedLiveRunner) observeExactPackage(ctx context.Context) (PackageObservation, error) {
	observation := runner.bindings.observer.Observe(ctx, runner.definition.Observer)
	if observation.Status != LiveObservationPresent || observation.Ref != runner.definition.WingetRef || observation.WingetVersion == "" {
		return PackageObservation{}, fmt.Errorf("hosted live package state is not exact")
	}
	return PackageObservation{Ref: observation.Ref, Version: observation.WingetVersion, Status: string(observation.Status)}, nil
}
func (runner *windowsHostedLiveRunner) engineVerify(ctx context.Context) error {
	admission, err := runner.bindings.issuer.admit(liveOperationEngineVerify, runner.plan.verify, runner.session.NonceFor(liveOperationEngineVerify, runner.plan.verify))
	if err != nil {
		return err
	}
	permit, err := runner.session.MintMutationPermit(admission)
	if err != nil {
		admission.complete()
		return err
	}
	receipt, err := runner.bindings.runProcess(ctx, newLiveTrustedEngineMutation(admission, permit, liveOperationEngineVerify, maxLiveProcessOutputBytes))
	if err != nil {
		return err
	}
	runner.verifyReceipt = receipt
	return nil
}
func (runner *windowsHostedLiveRunner) engine(ctx context.Context, operation liveOperation, sequence uint64) (*liveExecutionReceipt, error) {
	admission, err := runner.bindings.issuer.admit(operation, sequence, runner.session.NonceFor(operation, sequence))
	if err != nil {
		return nil, err
	}
	permit, err := runner.session.MintMutationPermit(admission)
	if err != nil {
		admission.complete()
		return nil, err
	}
	return runner.bindings.runProcess(ctx, newLiveTrustedEngineMutation(admission, permit, operation, maxLiveProcessOutputBytes))
}
func (runner *windowsHostedLiveRunner) seed(ctx context.Context) error {
	receipt, err := runner.engine(ctx, liveOperationHashBoundSeed, runner.plan.seed)
	if err == nil {
		runner.seedReceipt = receipt
	}
	return err
}
func (runner *windowsHostedLiveRunner) snapshotSeed(context.Context) error {
	targets, err := runner.bindings.snapshotTargets(runner.definition, runner.appData)
	if err != nil || targets.RequireSeeded() != nil {
		return fmt.Errorf("hosted live seed targets are invalid")
	}
	runner.seedTargets = targets
	return nil
}
func (runner *windowsHostedLiveRunner) engineCapture(ctx context.Context) error {
	receipt, err := runner.engine(ctx, liveOperationEngineCapture, runner.plan.capture)
	if err == nil {
		runner.captureReceipt = receipt
	}
	return err
}
func (runner *windowsHostedLiveRunner) inspectCapture(context.Context) error {
	module, ok := runner.definition.productionModule()
	if !ok {
		return fmt.Errorf("hosted live production module is unavailable")
	}
	hostname, err := runner.bindings.hostname()
	if err != nil || hostname == "" {
		return fmt.Errorf("hosted live hostname is unavailable")
	}
	claims, failure := runner.bindings.captureClaims(runner.bindings.issuer, runner.definition, module, runner.captureReceipt, runner.plan.capture, runner.session.NonceFor(liveOperationEngineCapture, runner.plan.capture), hostname, runtime.GOOS)
	if failure != nil {
		return fmt.Errorf("hosted live capture claims are invalid")
	}
	snapshots, err := runner.seedTargets.captureSnapshots()
	if err != nil {
		return err
	}
	evidence, failure := runner.bindings.inspectArtifact(runner.definition, snapshots, claims, claims.OutputPath)
	if failure != nil {
		return fmt.Errorf("hosted live capture artifact is invalid")
	}
	runner.captureArtifact, runner.captureVersion = evidence, claims.EndstateVersion
	return nil
}
func (runner *windowsHostedLiveRunner) uninstall(ctx context.Context) error {
	return runner.wingetUninstall(ctx, runner.plan.uninstall)
}
func (runner *windowsHostedLiveRunner) wingetUninstall(ctx context.Context, sequence uint64) error {
	admission, err := runner.bindings.issuer.admit(liveOperationWingetExactUninstall, sequence, runner.session.NonceFor(liveOperationWingetExactUninstall, sequence))
	if err != nil {
		return err
	}
	permit, err := runner.session.MintMutationPermit(admission)
	if err != nil {
		admission.complete()
		return err
	}
	_, err = runner.bindings.runProcess(ctx, newLiveTrustedAppXWingetExactUninstall(admission, permit, runner.bindings.winget, maxLiveProcessOutputBytes))
	return err
}
func (runner *windowsHostedLiveRunner) wipe(context.Context) error {
	return runner.targetWipe(runner.plan.wipe)
}
func (runner *windowsHostedLiveRunner) targetWipe(sequence uint64) error {
	admission, err := runner.bindings.issuer.admit(liveOperationDeclaredTargetWipe, sequence, runner.session.NonceFor(liveOperationDeclaredTargetWipe, sequence))
	if err != nil {
		return err
	}
	binding, err := windowsLiveHostMutationBinding(runner.definition, runner.appData, windowsLiveAttemptRoot{})
	if err != nil {
		admission.complete()
		return err
	}
	permit, err := runner.session.MintHostMutationPermit(admission, binding)
	if err != nil {
		admission.complete()
		return err
	}
	_, err = runner.bindings.wipeTargets(admission, permit, runner.definition, runner.appData)
	return err
}
func (runner *windowsHostedLiveRunner) observeAbsent(ctx context.Context) error {
	if runner.bindings.observer.Observe(ctx, runner.definition.Observer).Status != LiveObservationAbsent {
		return fmt.Errorf("hosted live package remains present")
	}
	targets, err := runner.bindings.snapshotTargets(runner.definition, runner.appData)
	if err != nil || targets.RequireAbsent() != nil {
		return fmt.Errorf("hosted live targets remain present")
	}
	storage, err := runner.bindings.snapshotStorage(runner.storageRoot)
	if err != nil {
		return err
	}
	runner.wipedTargets, runner.preRestoreStorage = targets, storage
	return nil
}
func (runner *windowsHostedLiveRunner) rebuild(ctx context.Context) error {
	receipt, err := runner.engine(ctx, liveOperationEngineRebuild, runner.plan.rebuild)
	if err == nil {
		runner.rebuildReceipt = receipt
	}
	return err
}
func (runner *windowsHostedLiveRunner) inspectRebuild(ctx context.Context) error {
	observation, err := runner.observeExactPackage(ctx)
	if err != nil {
		return err
	}
	targets, err := runner.bindings.snapshotTargets(runner.definition, runner.appData)
	if err != nil || targets.Equal(runner.seedTargets) != nil {
		return fmt.Errorf("hosted live rebuild targets differ")
	}
	storage, err := runner.bindings.snapshotStorage(runner.storageRoot)
	if err != nil {
		return err
	}
	runner.rebuiltTargets, runner.rebuiltStorage, runner.packageAfterRebuild = targets, storage, observation
	if runner.packageAfterApply != runner.packageAfterRebuild {
		return fmt.Errorf("hosted live package version changed during rebuild")
	}
	return nil
}
func (runner *windowsHostedLiveRunner) revert(ctx context.Context) error {
	receipt, err := runner.engine(ctx, liveOperationEngineRevert, runner.plan.revert)
	if err == nil {
		runner.revertReceipt = receipt
	}
	return err
}
func (runner *windowsHostedLiveRunner) observeRetained(ctx context.Context) error {
	observation, err := runner.observeExactPackage(ctx)
	if err != nil {
		return err
	}
	targets, err := runner.bindings.snapshotTargets(runner.definition, runner.appData)
	if err != nil || targets.RequireAbsent() != nil {
		return fmt.Errorf("hosted live revert targets remain")
	}
	storage, err := runner.bindings.snapshotStorage(runner.storageRoot)
	if err != nil {
		return err
	}
	runner.revertedTargets, runner.revertedStorage, runner.packageAfterRevert = targets, storage, observation
	if runner.packageAfterRebuild != runner.packageAfterRevert {
		return fmt.Errorf("hosted live package version changed during revert")
	}
	return nil
}
func (runner *windowsHostedLiveRunner) recovery(ctx context.Context) error {
	receipt, err := runner.engine(ctx, liveOperationEngineRebuild, runner.plan.recovery)
	if err == nil {
		runner.recoveryReceipt = receipt
	}
	return err
}
func (runner *windowsHostedLiveRunner) inspectRecovery(ctx context.Context) error {
	observation, err := runner.observeExactPackage(ctx)
	if err != nil {
		return err
	}
	targets, err := runner.bindings.snapshotTargets(runner.definition, runner.appData)
	if err != nil || targets.Equal(runner.seedTargets) != nil {
		return fmt.Errorf("hosted live recovery targets differ")
	}
	storage, err := runner.bindings.snapshotStorage(runner.storageRoot)
	if err != nil {
		return err
	}
	runner.recoveredTargets, runner.recoveredStorage, runner.packageAfterRecovery = targets, storage, observation
	if runner.packageAfterRebuild != runner.packageAfterRecovery {
		return fmt.Errorf("hosted live package version changed during recovery")
	}
	return nil
}
func (runner *windowsHostedLiveRunner) convergence(ctx context.Context) error {
	storage, err := runner.bindings.snapshotStorage(runner.storageRoot)
	if err != nil {
		return err
	}
	runner.convergenceStorage = storage
	receipt, err := runner.engine(ctx, liveOperationEngineRebuild, runner.plan.convergence)
	if err == nil {
		runner.convergenceReceipt = receipt
	}
	return err
}
func (runner *windowsHostedLiveRunner) inspectConvergence(ctx context.Context) error {
	observation, err := runner.observeExactPackage(ctx)
	if err != nil {
		return err
	}
	targets, err := runner.bindings.snapshotTargets(runner.definition, runner.appData)
	if err != nil || targets.Equal(runner.seedTargets) != nil {
		return fmt.Errorf("hosted live convergence targets differ")
	}
	after, err := runner.bindings.snapshotStorage(runner.storageRoot)
	if err != nil {
		return err
	}
	if err := requireHostedLiveConvergence(runner.convergenceStorage, after); err != nil {
		return err
	}
	paths, err := runner.bindings.declaredTargetPaths(runner.definition, runner.appData)
	if err != nil {
		return err
	}
	runtimeRestoreTargets, err := runner.bindings.declaredTargetBindings(runner.definition, runner.appData)
	if err != nil {
		return err
	}
	set := liveJourneyReceiptSet{ScenarioID: liveConfigRoundtripScenarioID,
		InitialApply:       liveReceiptExpectation{receipt: runner.applyReceipt, operation: liveOperationEngineApply, sequence: runner.plan.apply, nonce: runner.session.NonceFor(liveOperationEngineApply, runner.plan.apply)},
		Verify:             liveReceiptExpectation{receipt: runner.verifyReceipt, operation: liveOperationEngineVerify, sequence: runner.plan.verify, nonce: runner.session.NonceFor(liveOperationEngineVerify, runner.plan.verify)},
		Capture:            liveReceiptExpectation{receipt: runner.captureReceipt, operation: liveOperationEngineCapture, sequence: runner.plan.capture, nonce: runner.session.NonceFor(liveOperationEngineCapture, runner.plan.capture)},
		RestoreRebuild:     liveReceiptExpectation{receipt: runner.rebuildReceipt, operation: liveOperationEngineRebuild, sequence: runner.plan.rebuild, nonce: runner.session.NonceFor(liveOperationEngineRebuild, runner.plan.rebuild)},
		Revert:             liveReceiptExpectation{receipt: runner.revertReceipt, operation: liveOperationEngineRevert, sequence: runner.plan.revert, nonce: runner.session.NonceFor(liveOperationEngineRevert, runner.plan.revert)},
		RecoveryRebuild:    liveReceiptExpectation{receipt: runner.recoveryReceipt, operation: liveOperationEngineRebuild, sequence: runner.plan.recovery, nonce: runner.session.NonceFor(liveOperationEngineRebuild, runner.plan.recovery)},
		ConvergenceRebuild: liveReceiptExpectation{receipt: runner.convergenceReceipt, operation: liveOperationEngineRebuild, sequence: runner.plan.convergence, nonce: runner.session.NonceFor(liveOperationEngineRebuild, runner.plan.convergence)},
		PackageAfterRevert: runner.packageAfterRevert, runtimeRestoreTargets: runtimeRestoreTargets}
	projection, proof, failure := decodeLiveJourneyReceiptProof(runner.bindings.issuer, runner.definition, set)
	if failure != nil || projection.CapturedMappings != 2 || projection.RestoredMappings != 2 || !projection.PackagePresentAfterRevert {
		return fmt.Errorf("hosted live journey proof is invalid")
	}
	journalDigest := ""
	for _, member := range runner.rebuiltStorage.members {
		if member.member.LegacyJournalIdentity == proof.revert.JournalUsed {
			journalDigest = member.member.LegacyJournalDigest
			break
		}
	}
	first, err := bindHostedLiveFirstRestore(runner.preRestoreStorage, runner.rebuiltStorage, hostedLiveRestoreProof{nestedApplyRunID: proof.restoreRebuild.applyRunID, journalIdentity: proof.revert.JournalUsed, journalDigest: journalDigest, declaredTargets: paths, restoreResults: proof.restoreRebuild.restoreItems})
	if err != nil {
		return err
	}
	if err := bindHostedLiveRevert(runner.rebuiltStorage, runner.revertedStorage, first, proof.revert); err != nil {
		return err
	}
	if _, err := bindHostedLiveRecovery(runner.revertedStorage, runner.recoveredStorage, hostedLiveRestoreProof{nestedApplyRunID: proof.recoveryRebuild.applyRunID, declaredTargets: paths, restoreResults: proof.recoveryRebuild.restoreItems}); err != nil {
		return err
	}
	runner.convergedTargets, runner.convergenceStorage, runner.packageAfterConvergence = targets, after, observation
	if runner.packageAfterRebuild != runner.packageAfterConvergence {
		return fmt.Errorf("hosted live package version changed during convergence")
	}
	runner.journeyProven = true
	return nil
}

func (runner *windowsHostedLiveRunner) hostedLiveEvidenceBase(result hostedLiveRunResult) (hostedLiveEvidence, error) {
	if runner == nil || runner.session == nil || validateLiveDefinition(runner.definition) != nil || !validHostedLiveEvidenceValue(runner.bindings.runnerImage) {
		return hostedLiveEvidence{}, fmt.Errorf("hosted live evidence authority is unavailable")
	}
	campaign := runner.session.campaign
	campaignID, err := CanonicalLiveCampaignIdentity(campaign)
	if err != nil {
		return hostedLiveEvidence{}, err
	}
	definitionSHA256, err := CanonicalLiveDefinitionSHA256(runner.definition)
	if err != nil {
		return hostedLiveEvidence{}, err
	}
	version := runner.captureVersion
	if version == "" {
		version = "unknown"
	}
	packageVersion := runner.packageAfterApply.Version
	if runner.journeyProven {
		packageVersion = runner.packageAfterConvergence.Version
	}
	if result.err == nil && (!runner.journeyProven || runner.captureArtifact.SHA256 == "" || runner.captureArtifact.Size < 1 || runner.captureVersion == "" || runner.packageAfterConvergence.Ref != runner.definition.WingetRef || runner.packageAfterConvergence.Version == "") {
		return hostedLiveEvidence{}, fmt.Errorf("hosted live success lacks journey proof")
	}
	return hostedLiveEvidence{
		Campaign:  campaignID,
		Run:       hostedLiveEvidenceRun{ID: campaign.RunID, Attempt: campaign.RunAttempt, Event: campaign.Event, Ref: campaign.Ref, TrustedCommit: campaign.ControllerCommit},
		Engine:    hostedLiveEvidenceEngine{Commit: campaign.TestedCheckoutCommit, Version: version, SHA256: campaign.EngineSHA256, ValidatorSHA256: campaign.ValidatorSHA256},
		Inputs:    hostedLiveEvidenceInputs{DefinitionSHA256: definitionSHA256, ModuleSHA256: campaign.ModuleRevision, ValidationSourceSHA256: campaign.ValidationSourceSHA256, SeedSHA256: campaign.SeedSHA256, ComparatorSHA256: campaign.ComparatorSHA256, TargetsSHA256: campaign.TargetsSHA256, ObserverSHA256: campaign.ObserverSHA256, WorkflowSHA256: campaign.WorkflowPolicySHA256},
		Capture:   hostedLiveEvidenceCapture{SHA256: runner.captureArtifact.SHA256, Size: runner.captureArtifact.Size},
		Runner:    hostedLiveEvidenceRunner{OS: "windows", Image: runner.bindings.runnerImage},
		Package:   hostedLiveEvidencePackage{Driver: "winget", Source: "winget", Ref: campaign.PackageRef, Version: packageVersion},
		Candidate: true,
	}, nil
}
func (runner *windowsHostedLiveRunner) finalUninstall(ctx context.Context) error {
	if runner == nil || runner.session == nil || runner.bindings.issuer == nil {
		return fmt.Errorf("hosted live cleanup is unavailable")
	}
	if !runner.cleanupEntered {
		if err := runner.session.EnterCleanup(runner.bindings.issuer); err != nil {
			return err
		}
		runner.cleanupEntered = true
	}
	admission, err := runner.bindings.issuer.admit(liveOperationWingetExactUninstall, runner.plan.finalUninstall, runner.session.NonceFor(liveOperationWingetExactUninstall, runner.plan.finalUninstall))
	if err != nil {
		return err
	}
	permit, err := runner.session.MintMutationPermit(admission)
	if err != nil {
		admission.complete()
		return err
	}
	if runner.bindings.issuer.markCleanupPrelaunchFailureFn == nil || !runner.bindings.issuer.markCleanupPrelaunchFailureFn(admission) {
		admission.complete()
		return fmt.Errorf("hosted live cleanup marker is unavailable")
	}
	receipt, processErr := runner.bindings.runProcess(ctx, newLiveTrustedAppXWingetExactUninstall(admission, permit, runner.bindings.winget, maxLiveProcessOutputBytes))
	if hostedLiveNoInstalledProcessExit(receipt, processErr) {
		return nil
	}
	return processErr
}

func hostedLiveNoInstalledProcessExit(receipt *liveExecutionReceipt, processErr error) bool {
	var execution *LiveExecutionError
	return errors.As(processErr, &execution) && execution.Code == LiveExecutionProcessExit && receipt != nil && receipt.sealed && uint32(receipt.exitCode) == liveWingetNoInstalledHRESULT
}
func (runner *windowsHostedLiveRunner) finalWipe(context.Context) error {
	admission, err := runner.bindings.issuer.admit(liveOperationDeclaredTargetWipe, runner.plan.finalWipe, runner.session.NonceFor(liveOperationDeclaredTargetWipe, runner.plan.finalWipe))
	if err != nil {
		return err
	}
	binding, err := windowsLiveHostMutationBinding(runner.definition, runner.appData, windowsLiveAttemptRoot{})
	if err != nil {
		admission.complete()
		return err
	}
	permit, err := runner.session.MintHostMutationPermit(admission, binding)
	if err != nil {
		admission.complete()
		return err
	}
	if runner.bindings.issuer.markCleanupPrelaunchFailureFn == nil || !runner.bindings.issuer.markCleanupPrelaunchFailureFn(admission) {
		admission.complete()
		return fmt.Errorf("hosted live cleanup marker is unavailable")
	}
	_, err = runner.bindings.wipeTargets(admission, permit, runner.definition, runner.appData)
	return err
}
func (runner *windowsHostedLiveRunner) attemptRootCleanup(context.Context) error {
	admission, err := runner.bindings.issuer.admit(liveOperationAttemptRootCleanup, runner.plan.attemptCleanup, runner.session.NonceFor(liveOperationAttemptRootCleanup, runner.plan.attemptCleanup))
	if err != nil {
		return err
	}
	binding, err := windowsLiveHostMutationBinding(runner.definition, runner.appData, runner.attemptRoot)
	if err != nil {
		admission.complete()
		return err
	}
	permit, err := runner.session.MintHostMutationPermit(admission, binding)
	if err != nil {
		admission.complete()
		return err
	}
	if runner.bindings.issuer.markCleanupPrelaunchFailureFn == nil || !runner.bindings.issuer.markCleanupPrelaunchFailureFn(admission) {
		admission.complete()
		return fmt.Errorf("hosted live cleanup marker is unavailable")
	}
	_, err = runner.bindings.cleanupAttempt(admission, permit, runner.definition, runner.appData, runner.attemptRoot)
	return err
}
func (runner *windowsHostedLiveRunner) finalBoundary(ctx context.Context) error {
	if runner.attemptRoot.path != "" {
		if _, err := os.Lstat(runner.attemptRoot.path); !os.IsNotExist(err) {
			return fmt.Errorf("hosted live attempt root remains")
		}
	}
	boundary, err := snapshotLiveBoundary(ctx, runner.definition, runner.bindings.boundary)
	if err != nil || boundary.RequireAbsent() != nil || boundary.Equal(runner.initialBoundary) != nil {
		return fmt.Errorf("hosted live final boundary differs")
	}
	if repository, err := snapshotBoundaryTree(runner.checkoutRoot); err != nil || firstBoundaryDifference(runner.repositoryBoundary, repository) != "" {
		return fmt.Errorf("hosted live repository boundary differs")
	}
	if engine, err := snapshotBoundaryFile(runner.bindings.enginePath); err != nil || boundaryEntryDifference(runner.engineBoundary, engine) != "" {
		return fmt.Errorf("hosted live engine boundary differs")
	}
	return nil
}

func newHostedLiveRunner(session *LiveAuthoritySession, definition LiveDefinition, appData, attemptRoot string) (hostedLiveRunner, error) {
	return nil, fmt.Errorf("hosted live runner construction is factory-only")
}
