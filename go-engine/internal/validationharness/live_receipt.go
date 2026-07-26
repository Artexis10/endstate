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
)

func (operation liveOperation) valid() bool {
	switch operation {
	case liveOperationWingetExactList, liveOperationEngineApply, liveOperationEngineVerify, liveOperationEngineCapture, liveOperationEngineRebuild, liveOperationEngineRevert, liveOperationHashBoundSeed, liveOperationWingetExactInstall, liveOperationWingetExactUninstall:
		return true
	default:
		return false
	}
}

func (operation liveOperation) mutation() bool { return operation != liveOperationWingetExactList }

type liveReceiptIssuer struct {
	id        uint64
	admitFn   func(liveOperation, uint64, [32]byte) (liveReceiptAdmission, error)
	sealFn    func(*liveExecutionReceipt) error
	consumeFn func(*liveExecutionReceipt, liveOperation, uint64, [32]byte) bool
	releaseFn func(liveReceiptAdmission)
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

func newLiveReceiptIssuer() *liveReceiptIssuer {
	issuer := &liveReceiptIssuer{id: liveReceiptSerial.Add(1)}
	liveReceiptIssuers.Store(issuer.id, issuer)
	key := make([]byte, sha256.Size)
	_, _ = rand.Read(key)
	var mu sync.Mutex
	var next uint64
	var active liveReceiptAdmission
	nonces := make(map[[32]byte]struct{})
	consumed := make(map[[32]byte]struct{})
	issuer.admitFn = func(operation liveOperation, sequence uint64, nonce [32]byte) (liveReceiptAdmission, error) {
		mu.Lock()
		defer mu.Unlock()
		if !operation.valid() || sequence == 0 || nonce == ([32]byte{}) || active.issuer != nil || sequence != next+1 {
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
		nonces[nonce], next, active = struct{}{}, sequence, admission
		return admission, nil
	}
	issuer.releaseFn = func(admission liveReceiptAdmission) {
		mu.Lock()
		defer mu.Unlock()
		if active.issuer == issuer && active.sequence == admission.sequence && active.nonce == admission.nonce && active.token == admission.token {
			active = liveReceiptAdmission{}
		}
	}
	issuer.sealFn = func(receipt *liveExecutionReceipt) error {
		mu.Lock()
		defer mu.Unlock()
		if receipt == nil || active.issuer != issuer || receipt.issuerID != issuer.id || receipt.sequence != active.sequence || receipt.nonce != active.nonce || receipt.admissionToken != active.token {
			return errors.New("live receipt seal rejected")
		}
		receipt.tag = liveReceiptMAC(key, receipt)
		receipt.sealed = true
		return nil
	}
	issuer.consumeFn = func(receipt *liveExecutionReceipt, operation liveOperation, sequence uint64, nonce [32]byte) bool {
		mu.Lock()
		defer mu.Unlock()
		if receipt == nil || receipt.issuerID != issuer.id || receipt.operation != operation || receipt.sequence != sequence || receipt.nonce != nonce || receipt.admissionToken == ([32]byte{}) || receipt.failure != "" || receipt.validate() != nil {
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
}

func (identity liveReceiptExpectedIdentity) valid(operation liveOperation) bool {
	if identity.packageRef == ([32]byte{}) {
		return false
	}
	if operation == liveOperationWingetExactList {
		return true
	}
	if identity.definition == ([32]byte{}) || identity.engine == ([32]byte{}) {
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
	if receipt == nil || receipt.issuerID == 0 || !receipt.operation.valid() || receipt.sequence == 0 || receipt.nonce == ([32]byte{}) || receipt.admissionToken == ([32]byte{}) || !receipt.sealed || receipt.requestSHA256 != receipt.requestDigest() || receipt.stdoutSHA256 != sha256.Sum256(receipt.stdout) || receipt.stderrSHA256 != sha256.Sum256(receipt.stderr) || receipt.resultSHA256 != receipt.resultDigest() {
		return errors.New("invalid live execution receipt")
	}
	if receipt.image.canonical == "" || receipt.image.volume == 0 || receipt.image.indexHigh == 0 && receipt.image.indexLow == 0 || receipt.image.sha256 == ([32]byte{}) || receipt.pid == 0 || receipt.created.IsZero() || receipt.started.IsZero() || receipt.finished.IsZero() || receipt.finished.Before(receipt.started) {
		return errors.New("incomplete live execution receipt")
	}
	return nil
}

// liveReceiptDecoderHandoff is the only output handoff permitted to package
// internal evidence decoders. It validates the sealed causal receipt before
// returning defensive output copies; failures are diagnostic only.
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
