// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestWindowsHostedLiveInitialAbsentSnapshotSkipsPreflightAndAdmitsApply(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	runner, issuer := newWindowsHostedLiveAdapterPhaseRunner(t, definition, hostedLiveAdapterBoundary{observation: LiveObservation{Status: LiveObservationAbsent, Ref: definition.WingetRef}})
	if err := runner.initial(context.Background()); err != nil {
		t.Fatalf("initial() error = %v", err)
	}
	if err := runner.initialBoundary.RequireAbsent(); err != nil {
		t.Fatalf("initial boundary = %+v: %v", runner.initialBoundary, err)
	}
	apply, err := issuer.admit(liveOperationEngineApply, 3, runner.session.NonceFor(liveOperationEngineApply, 3))
	if err != nil {
		t.Fatalf("initial() did not atomically skip optional preflight: %v", err)
	}
	apply.complete()
}

func TestWindowsHostedLiveSequencePlanDerivesBothApprovedShapes(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		operations map[uint64]LiveCampaignOperation
		apply      uint64
		preflight  bool
	}{
		{"fifteen", liveWindowsCleanupAuthoritySession(t, definition).definition.operations, 3, true},
		{"thirteen", liveHostedLiveThirteenOperationPlan(t, definition), 1, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := &LiveAuthoritySession{definition: liveAuthorityDefinition{operations: test.operations}}
			plan, err := deriveHostedLiveSequencePlan(session)
			if err != nil || plan.apply != test.apply || (plan.preflightUninstall != 0) != test.preflight {
				t.Fatalf("deriveHostedLiveSequencePlan() = %+v, %v", plan, err)
			}
		})
	}
}

func liveHostedLiveThirteenOperationPlan(t *testing.T, definition LiveDefinition) map[uint64]LiveCampaignOperation {
	t.Helper()
	campaign := liveTestCampaign()
	campaign.Operations = append([]LiveCampaignOperation(nil), campaign.Operations[2:]...)
	operations := make(map[uint64]LiveCampaignOperation, len(campaign.Operations))
	for index := range campaign.Operations {
		operation := campaign.Operations[index]
		operation.Sequence = uint64(index + 1)
		operations[operation.Sequence] = operation
	}
	return operations
}

func TestWindowsHostedLiveInitialRejectsNonAbsentBoundaryWithoutAdvancement(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		boundary hostedLiveAdapterBoundary
	}{
		{"declared target present", hostedLiveAdapterBoundary{observation: LiveObservation{Status: LiveObservationAbsent, Ref: definition.WingetRef}, presentTarget: definition.DeclaredTargets[0].Identity}},
		{"mixed package state", hostedLiveAdapterBoundary{observation: LiveObservation{Status: LiveObservationMixed, Ref: definition.WingetRef}}},
		{"ambiguous package state", hostedLiveAdapterBoundary{observation: LiveObservation{Status: LiveObservationAmbiguous, Ref: definition.WingetRef}}},
		{"version mismatch", hostedLiveAdapterBoundary{observation: LiveObservation{Status: LiveObservationVersionMismatch, Ref: definition.WingetRef}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner, issuer := newWindowsHostedLiveAdapterPhaseRunner(t, definition, test.boundary)
			if err := runner.initial(context.Background()); err == nil {
				t.Fatal("initial() accepted a non-absent boundary")
			}
			if _, err := issuer.admit(liveOperationEngineApply, 3, runner.session.NonceFor(liveOperationEngineApply, 3)); err == nil {
				t.Fatal("rejected initial boundary advanced to engine apply")
			}
		})
	}
}

func TestWindowsHostedLiveObservePresentRejectsRefStatusAndVersionDisagreement(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	refMismatch := definition
	refMismatch.WingetRef = "Other.Ref"
	for _, test := range []struct {
		name       string
		definition LiveDefinition
		observer   LiveObserver
		wantErr    bool
	}{
		{"present", definition, hostedLiveAdapterPresentObserver(definition, "1.2"), false},
		{"empty package version", definition, hostedLiveAdapterPresentObserverVersion(definition, "", "1.2"), true},
		{"mixed", definition, hostedLiveAdapterMixedObserver(definition), true},
		{"ambiguous", definition, hostedLiveAdapterAmbiguousObserver(definition), true},
		{"version mismatch", definition, hostedLiveAdapterPresentObserver(definition, "1.3"), true},
		{"ref mismatch", refMismatch, hostedLiveAdapterPresentObserver(definition, "1.2"), true},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &windowsHostedLiveRunner{definition: test.definition, bindings: hostedLiveWindowsBindings{observer: test.observer}}
			err := runner.observePresent(context.Background())
			if (err != nil) != test.wantErr {
				t.Fatalf("observePresent() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestWindowsHostedLiveEngineApplyAndVerifyUsePermitDerivedRequestsAndRetainSealedReceipts(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	runner, _ := newWindowsHostedLiveAdapterPhaseRunner(t, definition, hostedLiveAdapterBoundary{observation: LiveObservation{Status: LiveObservationAbsent, Ref: definition.WingetRef}})
	var requests []LiveProcessRequest
	runner.bindings.runProcess = hostedLiveAdapterSealingRunProcess(t, &requests)
	if err := runner.initial(context.Background()); err != nil {
		t.Fatalf("initial() error = %v", err)
	}
	if err := runner.engineApply(context.Background()); err != nil {
		t.Fatalf("engineApply() error = %v", err)
	}
	if err := runner.engineVerify(context.Background()); err != nil {
		t.Fatalf("engineVerify() error = %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("process requests = %d, want 2", len(requests))
	}
	for index, want := range []struct {
		operation liveOperation
		sequence  uint64
		receipt   *liveExecutionReceipt
	}{
		{liveOperationEngineApply, 3, runner.applyReceipt},
		{liveOperationEngineVerify, 4, runner.verifyReceipt},
	} {
		request := requests[index]
		if request.operation != want.operation || request.admission.operation != want.operation || request.admission.sequence != want.sequence || request.permit.capability == nil || request.permit.capability.admissionToken != request.admission.token || request.permit.capability.operation != want.operation || request.permit.capability.sequence != want.sequence || request.executable != request.permit.capability.executable || !sameLiveArguments(request.args, request.permit.capability.arguments) || request.expected.engine != request.permit.capability.engine {
			t.Fatalf("request %d is not permit-derived: %+v", index, request)
		}
		if want.receipt == nil || !want.receipt.sealed || want.receipt.operation != want.operation || want.receipt.sequence != want.sequence || want.receipt.admissionToken != request.admission.token {
			t.Fatalf("receipt %d = %+v", index, want.receipt)
		}
	}
}

func TestWindowsHostedLiveSeedAndCaptureUseMappedPermitRequests(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	runner, _ := newWindowsHostedLiveAdapterPhaseRunner(t, definition, hostedLiveAdapterBoundary{observation: LiveObservation{Status: LiveObservationAbsent, Ref: definition.WingetRef}})
	var requests []LiveProcessRequest
	runner.bindings.runProcess = hostedLiveAdapterSealingRunProcess(t, &requests)
	if err := runner.initial(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runner.engineApply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runner.engineVerify(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runner.seed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runner.engineCapture(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(requests) != 4 {
		t.Fatalf("requests = %d", len(requests))
	}
	for index, want := range []struct {
		operation liveOperation
		sequence  uint64
		receipt   *liveExecutionReceipt
	}{{liveOperationEngineApply, runner.plan.apply, runner.applyReceipt}, {liveOperationEngineVerify, runner.plan.verify, runner.verifyReceipt}, {liveOperationHashBoundSeed, runner.plan.seed, runner.seedReceipt}, {liveOperationEngineCapture, runner.plan.capture, runner.captureReceipt}} {
		request := requests[index]
		if request.operation != want.operation || request.admission.sequence != want.sequence || request.permit.capability == nil || want.receipt == nil || !want.receipt.sealed {
			t.Fatalf("request %d = %+v, receipt = %+v", index, request, want.receipt)
		}
	}
}

func TestWindowsHostedLiveProductionBindingsProvideEveryPrivateSeam(t *testing.T) {
	bindings := newWindowsHostedLiveProductionBindings("engine", LiveObserver{}, liveTrustedAppXBinding{}, newLiveReceiptIssuer(), `C:\AppData`)
	if bindings.snapshotTargets == nil || bindings.snapshotStorage == nil || bindings.wipeTargets == nil || bindings.cleanupAttempt == nil || bindings.hostname == nil || bindings.captureClaims == nil || bindings.inspectArtifact == nil || bindings.declaredTargetPaths == nil {
		t.Fatalf("production bindings left a hosted live seam unset: %+v", bindings)
	}
}

func TestHostedLiveGitHubRunnerImageRequiresBothValidatedValues(t *testing.T) {
	t.Setenv("ImageOS", "windows-2025")
	t.Setenv("ImageVersion", "2026.07.01")
	if image, err := hostedLiveGitHubRunnerImage(); err != nil || image != "windows-2025-2026.07.01" {
		t.Fatalf("hostedLiveGitHubRunnerImage() = %q, %v", image, err)
	}
	t.Setenv("ImageVersion", "")
	if _, err := hostedLiveGitHubRunnerImage(); err == nil {
		t.Fatal("hostedLiveGitHubRunnerImage() accepted a missing image version")
	}
}

func TestWindowsHostedLiveEvidenceBaseOmitsUnreachedObservations(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	runner, _ := newWindowsHostedLiveAdapterPhaseRunner(t, definition, hostedLiveAdapterBoundary{})
	runner.bindings.runnerImage = "windows-2025-2026.07.01"
	evidence, err := runner.hostedLiveEvidenceBase(hostedLiveRunResult{err: fmt.Errorf("failed")})
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Engine.Version != "unknown" || evidence.Capture != (hostedLiveEvidenceCapture{}) || evidence.Package.Version != "" || evidence.Runner.Image != runner.bindings.runnerImage || evidence.Runner.OS != "windows" {
		t.Fatalf("early evidence leaked or invented observations: %+v", evidence)
	}
}

func TestWindowsHostedLiveEvidenceBaseRejectsPassedResultWithoutJourneyProof(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	runner, _ := newWindowsHostedLiveAdapterPhaseRunner(t, definition, hostedLiveAdapterBoundary{})
	runner.bindings.runnerImage = "windows-2025-2026.07.01"
	if _, err := runner.hostedLiveEvidenceBase(hostedLiveRunResult{}); err == nil {
		t.Fatal("hostedLiveEvidenceBase() accepted a passed result without full journey proof")
	}
}

func TestWindowsHostedLiveRunsHermeticFullJourney(t *testing.T) {
	definition, outputs, targets := liveConcreteRestoreJourneyForTest(t)
	appData, temp := t.TempDir(), t.TempDir()
	withWindowsLiveTestAppData(t, appData)
	withWindowsLiveTestRunnerTemp(t, temp)
	attempt, err := newWindowsLiveAttemptRoot(temp)
	if err != nil {
		t.Fatal(err)
	}
	storage, err := bindHostedLiveStorageRoot(attempt)
	if err != nil {
		t.Fatal(err)
	}
	outputs.Revert.Stdout = []byte(strings.ReplaceAll(string(outputs.Revert.Stdout), liveJSONPath(`C:\trusted\restore\journal.json`), liveJSONPath(filepath.Join(storage.path, "logs", "journal.json"))))
	engine := filepath.Join(t.TempDir(), "engine.exe")
	if err := os.WriteFile(engine, []byte("engine"), 0600); err != nil {
		t.Fatal(err)
	}
	session := liveWindowsCleanupAuthoritySession(t, definition)
	issuer := session.NewReceiptIssuer()
	plan, err := deriveHostedLiveSequencePlan(session)
	if err != nil {
		t.Fatal(err)
	}
	state := &hostedLiveJourneyObserverState{definition: definition, present: []bool{false, true, false, true, true, true, true, false}}
	paths := make([]string, 0, len(definition.DeclaredTargets))
	for _, target := range definition.DeclaredTargets {
		paths = append(paths, targets[target.Identity])
	}
	seeded, absent := hostedLiveJourneyTargets(t), hostedLiveJourneyAbsentTargets()
	before, rebuilt, reverted, recovered := hostedLiveJourneyStorage(t, storage.path, definition, targets)
	storageStates := []hostedLiveStorageSnapshot{before, rebuilt, reverted, recovered, recovered, recovered}
	targetStates := []hostedLiveTargets{seeded, absent, seeded, absent, seeded, seeded}
	var targetIndex, storageIndex int
	runner := &windowsHostedLiveRunner{session: session, definition: definition, appData: appData, attemptRoot: attempt, checkoutRoot: productionLiveRepoRoot(t), storageRoot: storage, plan: plan,
		bindings: hostedLiveWindowsBindings{enginePath: engine, observer: state.observer(), winget: liveTrustedAppXBinding{metadata: liveAppXPackageMetadata{packageRoot: `C:\reviewed`, executableName: "winget.exe", receipt: liveAppXPackageReceiptForTest()}}, issuer: issuer, boundary: hostedLiveJourneyBoundary{observer: state.observer()}, runnerImage: "windows-2025-2026.07.01",
			runProcess: hostedLiveJourneyRunProcess(t, &outputs), snapshotTargets: func(LiveDefinition, string) (hostedLiveTargets, error) {
				value := targetStates[targetIndex]
				targetIndex++
				return value, nil
			}, snapshotStorage: func(hostedLiveStorageRoot) (hostedLiveStorageSnapshot, error) {
				value := storageStates[storageIndex]
				storageIndex++
				return value, nil
			},
			wipeTargets: hostedLiveJourneyWipe, cleanupAttempt: hostedLiveJourneyCleanup,
			hostname: func() (string, error) { return "runner", nil }, captureClaims: hostedLiveJourneyCaptureClaims, inspectArtifact: hostedLiveJourneyInspectArtifact, declaredTargetPaths: func(LiveDefinition, string) ([]string, error) { return append([]string(nil), paths...), nil }, declaredTargetBindings: func(LiveDefinition, string) (map[string]string, error) { return targets, nil },
		}}
	if boundary, err := snapshotLiveBoundary(context.Background(), definition, runner.bindings.boundary); err != nil || boundary.RequireAbsent() != nil {
		t.Fatalf("initial fixture boundary = %+v, %v", boundary, err)
	}
	state.index = 0
	result := runHostedLive(context.Background(), runner)
	if result.err != nil || !result.eligible || !runner.journeyProven || targetIndex != len(targetStates) || storageIndex != len(storageStates) {
		t.Fatalf("runHostedLive() = %+v, proof=%t targets=%d storage=%d", result, runner.journeyProven, targetIndex, storageIndex)
	}
	base, err := runner.hostedLiveEvidenceBase(result)
	if err != nil {
		t.Fatalf("hostedLiveEvidenceBase() error = %v", err)
	}
	evidence, err := hostedLiveEvidenceFromRun(base, result)
	if err != nil {
		t.Fatalf("hostedLiveEvidenceFromRun() error = %v", err)
	}
	if _, err := encodeHostedLiveEvidence(evidence); err != nil || evidence.Status != "passed" || evidence.Package.Version != "1.2" || evidence.Capture.Size != 1 {
		t.Fatalf("full journey evidence = %+v, error = %v", evidence, err)
	}
}

func TestHostedLiveNoInstalledProcessExitAcceptsOnlySealedProcessExit(t *testing.T) {
	for _, test := range []struct {
		name      string
		failure   LiveExecutionFailureCode
		sealed    bool
		wantError bool
	}{
		{name: "exact process exit", failure: LiveExecutionProcessExit, sealed: true},
		{name: "containment failure", failure: LiveExecutionContainment, sealed: true, wantError: true},
		{name: "timeout", failure: LiveExecutionTimeout, sealed: true, wantError: true},
		{name: "unsealed receipt", failure: LiveExecutionProcessExit, sealed: false, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			receipt := &liveExecutionReceipt{sealed: test.sealed, exitCode: int(liveWingetNoInstalledHRESULT)}
			if got := hostedLiveNoInstalledProcessExit(receipt, liveExecutionError(test.failure, nil)); got == test.wantError {
				t.Fatalf("hostedLiveNoInstalledProcessExit() = %v, want %v", got, !test.wantError)
			}
		})
	}
}

func newWindowsHostedLiveAdapterPhaseRunner(t *testing.T, definition LiveDefinition, boundary hostedLiveAdapterBoundary) (*windowsHostedLiveRunner, *liveReceiptIssuer) {
	t.Helper()
	session := liveWindowsCleanupAuthoritySession(t, definition)
	issuer := session.NewReceiptIssuer()
	if issuer == nil {
		t.Fatal("NewReceiptIssuer() = nil")
	}
	plan, err := deriveHostedLiveSequencePlan(session)
	if err != nil {
		t.Fatal(err)
	}
	return &windowsHostedLiveRunner{session: session, definition: definition, plan: plan, bindings: hostedLiveWindowsBindings{issuer: issuer, boundary: boundary}}, issuer
}

type hostedLiveAdapterBoundary struct {
	observation   LiveObservation
	presentTarget string
}

func (reader hostedLiveAdapterBoundary) Observe(context.Context, LiveObserverDefinition) LiveObservation {
	return reader.observation
}

func (reader hostedLiveAdapterBoundary) Target(_ context.Context, target LiveDeclaredTarget) (liveBoundaryTargetState, error) {
	return liveBoundaryTargetState{present: target.Identity == reader.presentTarget, kind: target.Kind}, nil
}

func (hostedLiveAdapterBoundary) Services(context.Context) ([]string, error)      { return nil, nil }
func (hostedLiveAdapterBoundary) Drivers(context.Context) ([]string, error)       { return nil, nil }
func (hostedLiveAdapterBoundary) Tasks(context.Context) ([]string, error)         { return nil, nil }
func (hostedLiveAdapterBoundary) PendingReboot(context.Context) ([]string, error) { return nil, nil }

func hostedLiveAdapterPresentObserver(definition LiveDefinition, executableVersion string) LiveObserver {
	return hostedLiveAdapterPresentObserverVersion(definition, "1.2", executableVersion)
}

func hostedLiveAdapterPresentObserverVersion(definition LiveDefinition, packageVersion, executableVersion string) LiveObserver {
	path := `C:\Program Files\Notepad++\` + definition.Observer.ExecutableNames[0]
	return LiveObserver{
		Process:  &fakeLiveProcess{result: LiveProcessResult{ExitCode: 0, Stdout: []byte(wingetTable + wingetRow(definition.Observer.WingetRef, packageVersion, "winget"))}},
		Registry: fakeLiveRegistry{records: []LiveUninstallRecord{{View: LiveRegistryHKLM64, DisplayName: "Notepad++", DisplayVersion: packageVersion, InstallLocation: `C:\Program Files\Notepad++`, DisplayIcon: `"C:\Program Files\Notepad++\notepad++.exe"`}}},
		Path:     fakeLivePath{},
		Files:    fakeLiveFiles{files: map[string]LiveFileInfo{path: {Regular: true}}, versions: map[string]string{path: executableVersion}},
	}
}

func hostedLiveAdapterMixedObserver(definition LiveDefinition) LiveObserver {
	return LiveObserver{Process: &fakeLiveProcess{result: LiveProcessResult{ExitCode: 0, Stdout: []byte(wingetTable + wingetRow(definition.Observer.WingetRef, "1.2", "winget"))}}, Registry: fakeLiveRegistry{}, Path: fakeLivePath{}, Files: fakeLiveFiles{}}
}

func hostedLiveAdapterAmbiguousObserver(definition LiveDefinition) LiveObserver {
	return LiveObserver{
		Process: &fakeLiveProcess{result: LiveProcessResult{ExitCode: 0, Stdout: []byte(wingetTable + wingetRow(definition.Observer.WingetRef, "1.2", "winget"))}},
		Registry: fakeLiveRegistry{records: []LiveUninstallRecord{
			{View: LiveRegistryHKLM64, DisplayName: "Notepad++", DisplayVersion: "1.2", InstallLocation: `C:\Program Files\Notepad++`},
			{View: LiveRegistryHKLM64, DisplayName: "Notepad++", DisplayVersion: "1.3", InstallLocation: `C:\Program Files\Notepad++`},
		}},
		Path:  fakeLivePath{},
		Files: fakeLiveFiles{},
	}
}

func hostedLiveAdapterSealingRunProcess(t *testing.T, requests *[]LiveProcessRequest) func(context.Context, LiveProcessRequest) (*liveExecutionReceipt, error) {
	t.Helper()
	return func(_ context.Context, request LiveProcessRequest) (*liveExecutionReceipt, error) {
		defer request.admission.complete()
		*requests = append(*requests, request)
		if request.permit.capability == nil || !request.permit.capability.finalize(request, request.permit.capability.executableSHA256, time.Now().UTC()) {
			return nil, fmt.Errorf("permit-derived request could not finalize")
		}
		receipt := liveUnsealedReceiptForTest(t, request.admission, nil, nil, "")
		receipt.executable = request.executable
		receipt.args = append([]string(nil), request.args...)
		receipt.directory = request.dir
		receipt.environment = cloneLiveEnvironment(request.environment)
		receipt.expected = request.expected
		receipt.image.sha256 = request.permit.capability.executableSHA256
		receipt.requestSHA256 = receipt.requestDigest()
		receipt.resultSHA256 = receipt.resultDigest()
		if err := request.admission.issuer.sealFn(receipt); err != nil {
			return nil, err
		}
		return receipt, nil
	}
}

type hostedLiveJourneyObserverState struct {
	definition LiveDefinition
	present    []bool
	index      int
	current    bool
}
type hostedLiveJourneyBoundary struct{ observer LiveObserver }

func (boundary hostedLiveJourneyBoundary) Observe(ctx context.Context, definition LiveObserverDefinition) LiveObservation {
	return boundary.observer.Observe(ctx, definition)
}
func (hostedLiveJourneyBoundary) Target(_ context.Context, target LiveDeclaredTarget) (liveBoundaryTargetState, error) {
	return liveBoundaryTargetState{kind: target.Kind}, nil
}
func (hostedLiveJourneyBoundary) Services(context.Context) ([]string, error)      { return nil, nil }
func (hostedLiveJourneyBoundary) Drivers(context.Context) ([]string, error)       { return nil, nil }
func (hostedLiveJourneyBoundary) Tasks(context.Context) ([]string, error)         { return nil, nil }
func (hostedLiveJourneyBoundary) PendingReboot(context.Context) ([]string, error) { return nil, nil }

func (state *hostedLiveJourneyObserverState) observer() LiveObserver {
	return LiveObserver{Process: state, Registry: state, Path: fakeLivePath{}, Files: state}
}
func (state *hostedLiveJourneyObserverState) Run(context.Context, string, ...string) (LiveProcessResult, error) {
	state.current = state.present[state.index]
	state.index++
	if !state.current {
		return LiveProcessResult{Classification: LiveProcessNoInstalled}, nil
	}
	return LiveProcessResult{ExitCode: 0, Stdout: []byte(wingetTable + wingetRow(state.definition.Observer.WingetRef, "1.2", "winget"))}, nil
}
func (state *hostedLiveJourneyObserverState) UninstallRecords(context.Context) ([]LiveUninstallRecord, error) {
	if !state.current {
		return nil, nil
	}
	return []LiveUninstallRecord{{View: LiveRegistryHKLM64, DisplayName: "Notepad++", DisplayVersion: "1.2", InstallLocation: `C:\Program Files\Notepad++`, DisplayIcon: `"C:\Program Files\Notepad++\notepad++.exe"`}}, nil
}
func (state *hostedLiveJourneyObserverState) Stat(string) (LiveFileInfo, error) {
	if !state.current {
		return LiveFileInfo{}, os.ErrNotExist
	}
	return LiveFileInfo{Regular: true}, nil
}
func (state *hostedLiveJourneyObserverState) FileVersion(string) (string, error) {
	if !state.current {
		return "", os.ErrNotExist
	}
	return "1.2", nil
}

func hostedLiveJourneyTargets(t *testing.T) hostedLiveTargets {
	t.Helper()
	value := hostedLiveJourneyAbsentTargets()
	for index := range value.files {
		if value.files[index].identity == "apps/notepad-plus-plus/config.xml" || value.files[index].identity == "apps/notepad-plus-plus/shortcuts.xml" {
			data := []byte(value.files[index].identity)
			digest := sha256.Sum256(data)
			value.files[index] = hostedLiveTargetFile{identity: value.files[index].identity, mode: 0600, size: int64(len(data)), sha256: fmt.Sprintf("%x", digest), bytes: data}
		}
	}
	return value
}
func hostedLiveJourneyAbsentTargets() hostedLiveTargets {
	return hostedLiveTargets{files: []hostedLiveTargetFile{{identity: "apps/notepad-plus-plus/config.xml", absent: true}, {identity: "apps/notepad-plus-plus/contextMenu.xml", absent: true}, {identity: "apps/notepad-plus-plus/langs.xml", absent: true}, {identity: "apps/notepad-plus-plus/shortcuts.xml", absent: true}, {identity: "apps/notepad-plus-plus/stylers.xml", absent: true}}, directory: hostedLiveTargetDirectory{identity: "apps/notepad-plus-plus/userDefineLangs"}}
}
func liveAppXPackageReceiptForTest() liveTrustedAppXReceipt {
	return liveTrustedAppXReceipt{valid: true}
}

func hostedLiveJourneyStorage(t *testing.T, root string, definition LiveDefinition, targets map[string]string) (hostedLiveStorageSnapshot, hostedLiveStorageSnapshot, hostedLiveStorageSnapshot, hostedLiveStorageSnapshot) {
	t.Helper()
	identity, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	actions := make([]configrestore.StoreActionInspection, 0, len(definition.production.Restore))
	for index, item := range definition.production.Restore {
		identity, err := liveRestoreIdentity(item.Source)
		if err != nil || targets[identity] == "" {
			t.Fatalf("journey restore identity %q: %v", identity, err)
		}
		status := configrestore.StoreActionStatusSkippedMissingSource
		if identity == "apps/notepad-plus-plus/config.xml" || identity == "apps/notepad-plus-plus/shortcuts.xml" {
			status = configrestore.StoreActionStatusRestored
		}
		source := `C:\trusted\extract\configs\notepad-plus-plus\` + strings.TrimPrefix(identity, "apps/notepad-plus-plus/")
		actions = append(actions, configrestore.StoreActionInspection{Index: index, Status: status, SourceIdentity: configrestore.InspectionIdentity(source), TargetIdentity: configrestore.InspectionIdentity(targets[identity])})
	}
	member := func(id, runID, journal, digest string, ordinal uint64, reverted bool) hostedLiveStoreMember {
		return hostedLiveStoreMember{runID: runID, member: configrestore.StoreMemberInspection{ID: id, Kind: configrestore.StoreMemberLegacy, Ordinal: ordinal, MemberDigest: id + "-digest", Reverted: reverted, LegacyJournalIdentity: journal, LegacyJournalDigest: digest}, actions: append([]configrestore.StoreActionInspection(nil), actions...)}
	}
	journal := filepath.Join(root, "logs", "journal.json")
	before := hostedLiveStorageSnapshot{root: root, storeExists: true, store: boundaryTree{".": {Kind: "directory", Identity: identity}, "v1": {Kind: "directory", Identity: identity}, "v1/legacy-reverts": {Kind: "directory", Identity: identity}}, members: []hostedLiveStoreMember{member("existing", "old", filepath.Join(root, "logs", "old.json"), "old", 0, false)}}
	rebuilt := cloneHostedLiveStorageSnapshot(before)
	rebuilt.members = append(rebuilt.members, member("first", "apply-rebuild-restore", journal, "journal", 1, false))
	reverted := cloneHostedLiveStorageSnapshot(rebuilt)
	reverted.members[1].member.Reverted = true
	reverted.members[1].member.RevertDigest = "revert"
	reverted.store["v1/legacy-reverts/first.json"] = boundaryEntry{Kind: "file", Size: 1, Identity: identity}
	recovered := cloneHostedLiveStorageSnapshot(reverted)
	recovered.members = append(recovered.members, member("recovery", "apply-rebuild-recovery", filepath.Join(root, "logs", "recovery.json"), "recovery", 2, false))
	return before, rebuilt, reverted, recovered
}
func hostedLiveJourneyRunProcess(t *testing.T, outputs *liveJourneyOutputs) func(context.Context, LiveProcessRequest) (*liveExecutionReceipt, error) {
	return func(_ context.Context, request LiveProcessRequest) (*liveExecutionReceipt, error) {
		defer request.admission.complete()
		output := liveCommandOutput{}
		switch request.admission.sequence {
		case 3:
			output = outputs.InitialApply
		case 4:
			output = outputs.Verify
		case 6:
			output = outputs.Capture
		case 9:
			output = outputs.RestoreRebuild
		case 10:
			output = outputs.Revert
		case 11:
			output = outputs.RecoveryRebuild
		case 12:
			output = outputs.ConvergenceRebuild
		}
		receipt := liveUnsealedReceiptForTest(t, request.admission, output.Stdout, output.Stderr, "")
		receipt.executable = request.executable
		receipt.args = append([]string(nil), request.args...)
		receipt.directory = request.dir
		receipt.environment = cloneLiveEnvironment(request.environment)
		receipt.expected = request.expected
		receipt.image.sha256 = request.expected.engine
		if request.operation == liveOperationHashBoundSeed || request.operation == liveOperationWingetExactUninstall {
			receipt.image.sha256 = request.expected.runner
		}
		receipt.requestSHA256 = receipt.requestDigest()
		receipt.resultSHA256 = receipt.resultDigest()
		if request.permit.capability == nil || !request.permit.capability.finalize(request, request.permit.capability.executableSHA256, time.Now().UTC()) {
			return nil, fmt.Errorf("finalize sequence %d", request.admission.sequence)
		}
		if err := request.admission.issuer.sealFn(receipt); err != nil {
			return nil, err
		}
		return receipt, nil
	}
}
func hostedLiveJourneyWipe(admission liveReceiptAdmission, permit trustedLiveHostMutationPermit, definition LiveDefinition, appData string) (*liveHostMutationReceipt, error) {
	defer admission.complete()
	binding, err := windowsLiveHostMutationBinding(definition, appData, windowsLiveAttemptRoot{})
	if err != nil || !admission.issuer.finalizeHostMutationFn(admission, permit.capability, binding, time.Now().UTC()) {
		return nil, fmt.Errorf("wipe finalize")
	}
	receipt := &liveHostMutationReceipt{issuerID: admission.issuer.id, operation: admission.operation, sequence: admission.sequence, nonce: admission.nonce, admissionToken: admission.token, binding: binding, succeeded: true}
	return receipt, admission.issuer.sealHostMutationFn(receipt)
}
func hostedLiveJourneyCleanup(admission liveReceiptAdmission, permit trustedLiveHostMutationPermit, definition LiveDefinition, appData string, attempt windowsLiveAttemptRoot) (*liveHostMutationReceipt, error) {
	defer admission.complete()
	binding, err := windowsLiveHostMutationBinding(definition, appData, attempt)
	if err != nil || !admission.issuer.finalizeHostMutationFn(admission, permit.capability, binding, time.Now().UTC()) {
		return nil, fmt.Errorf("cleanup finalize")
	}
	if err := os.RemoveAll(attempt.path); err != nil {
		return nil, err
	}
	receipt := &liveHostMutationReceipt{issuerID: admission.issuer.id, operation: admission.operation, sequence: admission.sequence, nonce: admission.nonce, admissionToken: admission.token, binding: binding, succeeded: true}
	return receipt, admission.issuer.sealHostMutationFn(receipt)
}
func hostedLiveJourneyCaptureClaims(_ *liveReceiptIssuer, definition LiveDefinition, _ *modules.Module, _ *liveExecutionReceipt, _ uint64, _ [32]byte, _ string, _ string) (liveCaptureArtifactClaims, *Failure) {
	return liveCaptureArtifactClaims{ModuleRevision: definition.ModuleRevision, MachineName: "runner", ReceiptCreated: time.Now().UTC(), ReceiptFinished: time.Now().UTC(), EndstateVersion: "1.0.0", OS: "windows"}, nil
}
func hostedLiveJourneyInspectArtifact(LiveDefinition, []liveTargetSnapshot, liveCaptureArtifactClaims, string) (liveArtifactEvidence, *Failure) {
	return liveArtifactEvidence{SHA256: fmt.Sprintf("%064x", 1), Size: 1}, nil
}
