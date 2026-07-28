// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// liveOperation is deliberately closed: a receipt can only attest one of the
// exact proof operations below, never a caller-selected subprocess shape.
type liveOperation string

const (
	liveOperationWingetExactList      liveOperation = "winget-exact-list"
	liveOperationEngineApply          liveOperation = "engine-apply"
	liveOperationEngineVerify         liveOperation = "engine-verify"
	liveOperationEngineCapture        liveOperation = "engine-capture"
	liveOperationEngineRebuild        liveOperation = "engine-rebuild"
	liveOperationEngineRevert         liveOperation = "engine-revert"
	liveOperationHashBoundSeed        liveOperation = "hash-bound-seed"
	liveOperationWingetExactInstall   liveOperation = "winget-exact-install"
	liveOperationWingetExactUninstall liveOperation = "winget-exact-uninstall"
	liveOperationDeclaredTargetWipe   liveOperation = "declared-target-wipe"
	liveOperationAttemptRootCleanup   liveOperation = "attempt-root-cleanup"
)

func (operation liveOperation) valid() bool {
	switch operation {
	case liveOperationWingetExactList, liveOperationEngineApply, liveOperationEngineVerify, liveOperationEngineCapture, liveOperationEngineRebuild, liveOperationEngineRevert, liveOperationHashBoundSeed, liveOperationWingetExactInstall, liveOperationWingetExactUninstall, liveOperationDeclaredTargetWipe, liveOperationAttemptRootCleanup:
		return true
	default:
		return false
	}
}

func (operation liveOperation) mutation() bool { return operation != liveOperationWingetExactList }

type liveReceiptIssuer struct {
	id                     uint64
	authorityCampaign      [32]byte
	admitFn                func(liveOperation, uint64, [32]byte) (liveReceiptAdmission, error)
	sealFn                 func(*liveExecutionReceipt) error
	consumeFn              func(*liveExecutionReceipt, liveOperation, uint64, [32]byte) bool
	consumeBatchFn         func([]liveReceiptExpectation) bool
	releaseFn              func(liveReceiptAdmission)
	activeFn               func(liveReceiptAdmission) bool
	commitLaunchFn         func(liveReceiptAdmission) bool
	abortLaunchFn          func(liveReceiptAdmission)
	finalizeMutationFn     func(liveReceiptAdmission, *liveMutationCapability, LiveProcessRequest, [32]byte, time.Time) bool
	finalizeHostMutationFn func(liveReceiptAdmission, *liveHostMutationCapability, liveHostMutationBinding, time.Time) bool
	sealHostMutationFn     func(*liveHostMutationReceipt) error
	skipPreflightFn        func() error
	enterCleanupFn         func(uint64, []liveOperation) error
}

type liveAdmissionState uint8

const (
	liveAdmissionReserved liveAdmissionState = iota + 1
	liveAdmissionLaunchCommitted
	liveAdmissionSealed
)

type liveDeclaredPreflight struct {
	firstOperation, secondOperation liveOperation
	firstSequence, secondSequence   uint64
	firstNonce, secondNonce         [32]byte
}

type liveReceiptAdmission struct {
	issuer    *liveReceiptIssuer
	operation liveOperation
	sequence  uint64
	nonce     [32]byte
	token     [32]byte
}

var liveReceiptSerial atomic.Uint64
var liveReceiptIssuers sync.Map

func newLiveReceiptIssuer(optional ...liveDeclaredPreflight) *liveReceiptIssuer {
	issuer := &liveReceiptIssuer{id: liveReceiptSerial.Add(1)}
	liveReceiptIssuers.Store(issuer.id, issuer)
	key := make([]byte, sha256.Size)
	_, _ = rand.Read(key)
	var mu sync.Mutex
	var next uint64
	var active liveReceiptAdmission
	var state liveAdmissionState
	var hostBinding liveHostMutationBinding
	var cleanup bool
	var cleanupStart uint64
	var cleanupOperations []liveOperation
	nonces := make(map[[32]byte]struct{})
	consumed := make(map[[32]byte]struct{})
	issuer.admitFn = func(operation liveOperation, sequence uint64, nonce [32]byte) (liveReceiptAdmission, error) {
		mu.Lock()
		defer mu.Unlock()
		if !operation.valid() || sequence == 0 || nonce == ([32]byte{}) || active.issuer != nil || sequence != next+1 || cleanup && (sequence < cleanupStart || sequence-cleanupStart >= uint64(len(cleanupOperations)) || operation != cleanupOperations[sequence-cleanupStart]) {
			return liveReceiptAdmission{}, errors.New("live receipt admission rejected")
		}
		if _, exists := nonces[nonce]; exists {
			return liveReceiptAdmission{}, errors.New("live receipt nonce replay")
		}
		var token [32]byte
		if _, err := rand.Read(token[:]); err != nil || token == ([32]byte{}) {
			return liveReceiptAdmission{}, errors.New("live receipt token unavailable")
		}
		admission := liveReceiptAdmission{issuer: issuer, operation: operation, sequence: sequence, nonce: nonce, token: token}
		nonces[nonce], active, state = struct{}{}, admission, liveAdmissionReserved
		return admission, nil
	}
	issuer.releaseFn = func(admission liveReceiptAdmission) {
		mu.Lock()
		defer mu.Unlock()
		if (state == liveAdmissionReserved || state == liveAdmissionSealed) && active.issuer == issuer && active.sequence == admission.sequence && active.nonce == admission.nonce && active.token == admission.token {
			active = liveReceiptAdmission{}
			state = 0
		}
	}
	issuer.activeFn = func(admission liveReceiptAdmission) bool {
		mu.Lock()
		defer mu.Unlock()
		return state == liveAdmissionReserved && active.issuer == issuer && active.operation == admission.operation && active.sequence == admission.sequence && active.nonce == admission.nonce && active.token == admission.token
	}
	issuer.commitLaunchFn = func(admission liveReceiptAdmission) bool {
		mu.Lock()
		defer mu.Unlock()
		if state != liveAdmissionReserved || admission.operation != liveOperationWingetExactList || active.issuer != issuer || active.operation != admission.operation || active.sequence != admission.sequence || active.nonce != admission.nonce || active.token != admission.token {
			return false
		}
		state = liveAdmissionLaunchCommitted
		return true
	}
	issuer.abortLaunchFn = func(admission liveReceiptAdmission) {
		mu.Lock()
		defer mu.Unlock()
		if state == liveAdmissionLaunchCommitted && active.issuer == issuer && active.operation == admission.operation && active.sequence == admission.sequence && active.nonce == admission.nonce && active.token == admission.token {
			active = liveReceiptAdmission{}
			state = 0
		}
	}
	issuer.enterCleanupFn = func(start uint64, operations []liveOperation) error {
		mu.Lock()
		defer mu.Unlock()
		if cleanup || state == liveAdmissionLaunchCommitted || start == 0 || next >= start || len(operations) != 3 || operations[0] != liveOperationWingetExactUninstall || operations[1] != liveOperationDeclaredTargetWipe || operations[2] != liveOperationAttemptRootCleanup {
			return errors.New("live cleanup transition rejected")
		}
		if state == liveAdmissionReserved {
			active = liveReceiptAdmission{}
			state = 0
		} else if active.issuer != nil {
			return errors.New("live cleanup transition rejected")
		}
		cleanup, cleanupStart, cleanupOperations, next = true, start, append([]liveOperation(nil), operations...), start-1
		return nil
	}
	issuer.finalizeMutationFn = func(admission liveReceiptAdmission, capability *liveMutationCapability, request LiveProcessRequest, image [32]byte, now time.Time) bool {
		mu.Lock()
		defer mu.Unlock()
		if state != liveAdmissionReserved || active.issuer != issuer || active.operation != admission.operation || active.sequence != admission.sequence || active.nonce != admission.nonce || active.token != admission.token || capability == nil || capability.consumed.Load() || !capability.matches(request, image, now) {
			return false
		}
		if !capability.consumed.CompareAndSwap(false, true) {
			return false
		}
		state = liveAdmissionLaunchCommitted
		return true
	}
	issuer.finalizeHostMutationFn = func(admission liveReceiptAdmission, capability *liveHostMutationCapability, binding liveHostMutationBinding, now time.Time) bool {
		mu.Lock()
		defer mu.Unlock()
		if state != liveAdmissionReserved || active.issuer != issuer || active.operation != admission.operation || active.sequence != admission.sequence || active.nonce != admission.nonce || active.token != admission.token || capability == nil || !capability.matches(admission, binding, now) || !capability.consumed.CompareAndSwap(false, true) {
			return false
		}
		hostBinding = binding
		state = liveAdmissionLaunchCommitted
		return true
	}
	if len(optional) == 1 {
		preflight := optional[0]
		issuer.skipPreflightFn = func() error {
			mu.Lock()
			defer mu.Unlock()
			if active.issuer != nil || next != 0 || preflight.firstOperation != liveOperationWingetExactUninstall || preflight.firstSequence != 1 || preflight.secondOperation != liveOperationDeclaredTargetWipe || preflight.secondSequence != 2 || preflight.firstNonce == ([32]byte{}) || preflight.secondNonce == ([32]byte{}) {
				return errors.New("live receipt optional pair rejected")
			}
			if _, exists := nonces[preflight.firstNonce]; exists {
				return errors.New("live receipt nonce replay")
			}
			if _, exists := nonces[preflight.secondNonce]; exists {
				return errors.New("live receipt nonce replay")
			}
			nonces[preflight.firstNonce], nonces[preflight.secondNonce], next = struct{}{}, struct{}{}, 2
			return nil
		}
	}
	issuer.sealFn = func(receipt *liveExecutionReceipt) error {
		mu.Lock()
		defer mu.Unlock()
		if receipt == nil || state != liveAdmissionLaunchCommitted || active.issuer != issuer || receipt.issuerID != issuer.id || receipt.operation != active.operation || receipt.sequence != active.sequence || receipt.nonce != active.nonce || receipt.admissionToken != active.token || receipt.sealed || receipt.validateUnsealed() != nil {
			return errors.New("live receipt seal rejected")
		}
		receipt.tag = liveReceiptMAC(key, receipt)
		receipt.sealed = true
		next = receipt.sequence
		state = liveAdmissionSealed
		return nil
	}
	issuer.consumeFn = func(receipt *liveExecutionReceipt, operation liveOperation, sequence uint64, nonce [32]byte) bool {
		mu.Lock()
		defer mu.Unlock()
		if cleanup || receipt == nil || receipt.issuerID != issuer.id || receipt.operation != operation || receipt.sequence != sequence || receipt.nonce != nonce || receipt.admissionToken == ([32]byte{}) || receipt.failure != "" || receipt.validate() != nil {
			return false
		}
		want := liveReceiptMAC(key, receipt)
		if !hmac.Equal(receipt.tag[:], want[:]) {
			return false
		}
		if _, exists := consumed[receipt.admissionToken]; exists {
			return false
		}
		consumed[receipt.admissionToken] = struct{}{}
		return true
	}
	issuer.consumeBatchFn = func(expectations []liveReceiptExpectation) bool {
		mu.Lock()
		defer mu.Unlock()
		if cleanup || len(expectations) == 0 {
			return false
		}
		for _, expected := range expectations {
			receipt := expected.receipt
			if receipt == nil || receipt.issuerID != issuer.id || receipt.operation != expected.operation || receipt.sequence != expected.sequence || receipt.nonce != expected.nonce || receipt.failure != "" || receipt.validate() != nil {
				return false
			}
			want := liveReceiptMAC(key, receipt)
			if !hmac.Equal(receipt.tag[:], want[:]) {
				return false
			}
			if _, exists := consumed[receipt.admissionToken]; exists {
				return false
			}
		}
		for _, expected := range expectations {
			consumed[expected.receipt.admissionToken] = struct{}{}
		}
		return true
	}
	issuer.sealHostMutationFn = func(receipt *liveHostMutationReceipt) error {
		mu.Lock()
		defer mu.Unlock()
		if receipt == nil || state != liveAdmissionLaunchCommitted || active.issuer != issuer || receipt.issuerID != issuer.id || receipt.operation != active.operation || receipt.sequence != active.sequence || receipt.nonce != active.nonce || receipt.admissionToken != active.token || receipt.binding != hostBinding || receipt.sealed || receipt.validateUnsealed() != nil {
			return errors.New("live host receipt seal rejected")
		}
		receipt.tag = liveHostReceiptMAC(key, receipt)
		receipt.sealed = true
		next = receipt.sequence
		state = liveAdmissionSealed
		return nil
	}
	return issuer
}

func newLiveReceiptNonce() ([32]byte, error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nonce, err
	}
	return nonce, nil
}

func (issuer *liveReceiptIssuer) admit(operation liveOperation, sequence uint64, nonce [32]byte) (liveReceiptAdmission, error) {
	if issuer == nil || issuer.admitFn == nil {
		return liveReceiptAdmission{}, errors.New("invalid live receipt issuer")
	}
	return issuer.admitFn(operation, sequence, nonce)
}

func (issuer *liveReceiptIssuer) skipDeclaredPreflight() error {
	if issuer == nil || issuer.skipPreflightFn == nil {
		return errors.New("invalid live receipt issuer")
	}
	return issuer.skipPreflightFn()
}

func (admission liveReceiptAdmission) complete() {
	if admission.issuer == nil {
		return
	}
	admission.issuer.releaseFn(admission)
}

func (admission liveReceiptAdmission) valid() bool {
	return admission.issuer != nil && admission.issuer.admitFn != nil && admission.operation.valid() && admission.sequence != 0 && admission.nonce != ([32]byte{}) && admission.token != ([32]byte{})
}

type liveReceiptExpectedIdentity struct {
	definition [32]byte
	engine     [32]byte
	seed       [32]byte
	packageRef [32]byte
	comparator [32]byte
	targets    [32]byte
	observer   [32]byte
	workflow   [32]byte
	runner     [32]byte
}

type liveReceiptExpectation struct {
	receipt   *liveExecutionReceipt
	operation liveOperation
	sequence  uint64
	nonce     [32]byte
}

type liveJourneyReceiptSet struct {
	ScenarioID                                                                                 string
	InitialApply, Verify, Capture, RestoreRebuild, Revert, RecoveryRebuild, ConvergenceRebuild liveReceiptExpectation
	RestoreJournalID                                                                           string
	PackageAfterRevert                                                                         PackageObservation
}

// decodeLiveJourneyReceipts is the proof-only handoff for engine evidence. It
// consumes every authenticated phase atomically, decodes inside this package,
// and exposes only the projection returned by the official decoder.
func decodeLiveJourneyReceipts(issuer *liveReceiptIssuer, definition LiveDefinition, set liveJourneyReceiptSet) (liveJourneyProjection, *Failure) {
	if issuer == nil || issuer.consumeBatchFn == nil {
		return liveJourneyProjection{}, fail(CodeEnvelopeContract, "live", "receipt", "live receipt issuer is unavailable")
	}
	expected := []liveReceiptExpectation{set.InitialApply, set.Verify, set.Capture, set.RestoreRebuild, set.Revert, set.RecoveryRebuild, set.ConvergenceRebuild}
	for _, expectation := range expected {
		if expectation.operation != liveOperationEngineApply && expectation.operation != liveOperationEngineVerify && expectation.operation != liveOperationEngineCapture && expectation.operation != liveOperationEngineRebuild && expectation.operation != liveOperationEngineRevert {
			return liveJourneyProjection{}, fail(CodeEnvelopeContract, "live", "receipt", "live receipt operation is invalid")
		}
	}
	if !issuer.consumeBatchFn(expected) {
		return liveJourneyProjection{}, fail(CodeEnvelopeContract, "live", "receipt", "live receipt handoff rejected")
	}
	inputs := liveJourneyOutputs{ScenarioID: set.ScenarioID, RestoreJournalID: set.RestoreJournalID, PackageAfterRevert: set.PackageAfterRevert,
		InitialApply: liveCommandOutput{Stdout: set.InitialApply.receipt.stdout, Stderr: set.InitialApply.receipt.stderr}, Verify: liveCommandOutput{Stdout: set.Verify.receipt.stdout, Stderr: set.Verify.receipt.stderr}, Capture: liveCommandOutput{Stdout: set.Capture.receipt.stdout, Stderr: set.Capture.receipt.stderr}, RestoreRebuild: liveCommandOutput{Stdout: set.RestoreRebuild.receipt.stdout, Stderr: set.RestoreRebuild.receipt.stderr}, Revert: liveCommandOutput{Stdout: set.Revert.receipt.stdout, Stderr: set.Revert.receipt.stderr}, RecoveryRebuild: liveCommandOutput{Stdout: set.RecoveryRebuild.receipt.stdout, Stderr: set.RecoveryRebuild.receipt.stderr}, ConvergenceRebuild: liveCommandOutput{Stdout: set.ConvergenceRebuild.receipt.stdout, Stderr: set.ConvergenceRebuild.receipt.stderr}}
	return decodeLiveJourney(definition, inputs)
}

func (identity liveReceiptExpectedIdentity) valid(operation liveOperation) bool {
	if identity.packageRef == ([32]byte{}) {
		return false
	}
	if operation == liveOperationWingetExactList {
		return true
	}
	if identity.definition == ([32]byte{}) || identity.engine == ([32]byte{}) || identity.comparator == ([32]byte{}) || identity.targets == ([32]byte{}) || identity.observer == ([32]byte{}) || identity.workflow == ([32]byte{}) || identity.runner == ([32]byte{}) {
		return false
	}
	return operation != liveOperationHashBoundSeed || identity.seed != ([32]byte{})
}

type liveReceiptImageIdentity struct {
	canonical string
	volume    uint32
	indexHigh uint32
	indexLow  uint32
	sha256    [32]byte
}

type liveExecutionReceipt struct {
	issuerID       uint64
	operation      liveOperation
	sequence       uint64
	nonce          [32]byte
	admissionToken [32]byte

	executable    string
	args          []string
	directory     string
	environment   map[string]string
	expected      liveReceiptExpectedIdentity
	requestSHA256 [32]byte

	image    liveReceiptImageIdentity
	pid      uint32
	created  time.Time
	started  time.Time
	finished time.Time
	exitCode int
	failure  LiveExecutionFailureCode

	stdout       []byte
	stderr       []byte
	stdoutSHA256 [32]byte
	stderrSHA256 [32]byte
	resultSHA256 [32]byte
	tag          [32]byte
	sealed       bool
}

type liveHostMutationReceipt struct {
	issuerID, sequence uint64
	operation          liveOperation
	nonce              [32]byte
	admissionToken     [32]byte
	binding            liveHostMutationBinding
	succeeded          bool
	sealed             bool
	tag                [32]byte
}

func (receipt *liveHostMutationReceipt) validateUnsealed() error {
	if receipt == nil || (receipt.operation != liveOperationDeclaredTargetWipe && receipt.operation != liveOperationAttemptRootCleanup) || receipt.issuerID == 0 || receipt.sequence == 0 || receipt.nonce == ([32]byte{}) || receipt.admissionToken == ([32]byte{}) || receipt.binding.appData == ([32]byte{}) {
		return errors.New("invalid live host mutation receipt")
	}
	if receipt.operation == liveOperationDeclaredTargetWipe && receipt.binding.attemptRoot != ([32]byte{}) || receipt.operation == liveOperationAttemptRootCleanup && receipt.binding.attemptRoot == ([32]byte{}) {
		return errors.New("invalid live host mutation receipt")
	}
	return nil
}

func liveHostReceiptMAC(key []byte, receipt *liveHostMutationReceipt) [32]byte {
	mac := hmac.New(sha256.New, key)
	var header bytes.Buffer
	liveReceiptWriteString(&header, string(receipt.operation))
	var word [8]byte
	binary.BigEndian.PutUint64(word[:], receipt.issuerID)
	header.Write(word[:])
	binary.BigEndian.PutUint64(word[:], receipt.sequence)
	header.Write(word[:])
	mac.Write(header.Bytes())
	mac.Write(receipt.nonce[:])
	mac.Write(receipt.admissionToken[:])
	mac.Write(receipt.binding.appData[:])
	mac.Write(receipt.binding.attemptRoot[:])
	if receipt.succeeded {
		mac.Write([]byte{1})
	}
	var tag [32]byte
	copy(tag[:], mac.Sum(nil))
	return tag
}

func (receipt *liveExecutionReceipt) requestDigest() [32]byte {
	var canonical bytes.Buffer
	liveReceiptWriteString(&canonical, string(receipt.operation))
	liveReceiptWriteString(&canonical, receipt.executable)
	liveReceiptWriteString(&canonical, receipt.directory)
	liveReceiptWriteValues(&canonical, receipt.args)
	names := make([]string, 0, len(receipt.environment))
	for name := range receipt.environment {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		liveReceiptWriteString(&canonical, name)
		liveReceiptWriteString(&canonical, receipt.environment[name])
	}
	canonical.Write(receipt.expected.definition[:])
	canonical.Write(receipt.expected.engine[:])
	canonical.Write(receipt.expected.seed[:])
	canonical.Write(receipt.expected.packageRef[:])
	canonical.Write(receipt.expected.comparator[:])
	canonical.Write(receipt.expected.targets[:])
	canonical.Write(receipt.expected.observer[:])
	canonical.Write(receipt.expected.workflow[:])
	canonical.Write(receipt.expected.runner[:])
	return sha256.Sum256(canonical.Bytes())
}

func (receipt *liveExecutionReceipt) resultDigest() [32]byte {
	var canonical bytes.Buffer
	liveReceiptWriteString(&canonical, receipt.image.canonical)
	var word [8]byte
	binary.BigEndian.PutUint64(word[:], uint64(receipt.image.volume))
	canonical.Write(word[:])
	binary.BigEndian.PutUint64(word[:], uint64(receipt.image.indexHigh))
	canonical.Write(word[:])
	binary.BigEndian.PutUint64(word[:], uint64(receipt.image.indexLow))
	canonical.Write(word[:])
	canonical.Write(receipt.image.sha256[:])
	binary.BigEndian.PutUint64(word[:], uint64(receipt.pid))
	canonical.Write(word[:])
	binary.BigEndian.PutUint64(word[:], uint64(receipt.created.UTC().UnixNano()))
	canonical.Write(word[:])
	binary.BigEndian.PutUint64(word[:], uint64(receipt.started.UTC().UnixNano()))
	canonical.Write(word[:])
	binary.BigEndian.PutUint64(word[:], uint64(receipt.finished.UTC().UnixNano()))
	canonical.Write(word[:])
	binary.BigEndian.PutUint64(word[:], uint64(int64(receipt.exitCode)))
	canonical.Write(word[:])
	liveReceiptWriteString(&canonical, string(receipt.failure))
	canonical.Write(receipt.stdoutSHA256[:])
	canonical.Write(receipt.stderrSHA256[:])
	return sha256.Sum256(canonical.Bytes())
}

func liveReceiptWriteString(buffer *bytes.Buffer, value string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	buffer.Write(size[:])
	buffer.WriteString(value)
}

func liveReceiptWriteValues(buffer *bytes.Buffer, values []string) {
	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(values)))
	buffer.Write(size[:])
	for _, value := range values {
		liveReceiptWriteString(buffer, value)
	}
}

func (receipt *liveExecutionReceipt) validate() error {
	if receipt == nil || !receipt.sealed || receipt.validateUnsealed() != nil {
		return errors.New("invalid live execution receipt")
	}
	return nil
}

func (receipt *liveExecutionReceipt) validateUnsealed() error {
	if receipt == nil || receipt.issuerID == 0 || !receipt.operation.valid() || receipt.sequence == 0 || receipt.nonce == ([32]byte{}) || receipt.admissionToken == ([32]byte{}) || !receipt.expected.valid(receipt.operation) || receipt.requestSHA256 != receipt.requestDigest() || receipt.stdoutSHA256 != sha256.Sum256(receipt.stdout) || receipt.stderrSHA256 != sha256.Sum256(receipt.stderr) || receipt.resultSHA256 != receipt.resultDigest() {
		return errors.New("invalid live execution receipt")
	}
	if receipt.image.canonical == "" || receipt.image.volume == 0 || receipt.image.indexHigh == 0 && receipt.image.indexLow == 0 || receipt.image.sha256 == ([32]byte{}) || receipt.pid == 0 || receipt.created.IsZero() || receipt.started.IsZero() || receipt.finished.IsZero() || receipt.finished.Before(receipt.started) {
		return errors.New("incomplete live execution receipt")
	}
	return nil
}

func liveReceiptMAC(key []byte, receipt *liveExecutionReceipt) [32]byte {
	mac := hmac.New(sha256.New, key)
	var header bytes.Buffer
	liveReceiptWriteString(&header, string(receipt.operation))
	var word [8]byte
	binary.BigEndian.PutUint64(word[:], receipt.issuerID)
	header.Write(word[:])
	binary.BigEndian.PutUint64(word[:], receipt.sequence)
	header.Write(word[:])
	mac.Write(header.Bytes())
	mac.Write(receipt.nonce[:])
	mac.Write(receipt.requestSHA256[:])
	mac.Write(receipt.resultSHA256[:])
	mac.Write(receipt.admissionToken[:])
	mac.Write(receipt.stdoutSHA256[:])
	mac.Write(receipt.stderrSHA256[:])
	var tag [32]byte
	copy(tag[:], mac.Sum(nil))
	return tag
}

func classifyWingetListReceipt(receipt *liveExecutionReceipt, ref string, sequence uint64, nonce [32]byte) (LiveProcessResult, error) {
	if receipt == nil || !validLiveObserverValue(ref) {
		return LiveProcessResult{}, errors.New("live receipt handoff rejected")
	}
	value, ok := liveReceiptIssuers.Load(receipt.issuerID)
	issuer, ok := value.(*liveReceiptIssuer)
	if !ok || !issuer.consumeFn(receipt, liveOperationWingetExactList, sequence, nonce) {
		return LiveProcessResult{}, errors.New("live receipt handoff rejected")
	}
	version, err := ParseLiveWingetTable(receipt.stdout, ref)
	if err != nil {
		return LiveProcessResult{}, errors.New("live receipt output rejected")
	}
	return LiveProcessResult{ExitCode: receipt.exitCode, Version: version, Classification: LiveProcessCompleted}, nil
}

func cloneLiveEnvironment(environment map[string]string) map[string]string {
	if environment == nil {
		return nil
	}
	copy := make(map[string]string, len(environment))
	for name, value := range environment {
		copy[name] = value
	}
	return copy
}
