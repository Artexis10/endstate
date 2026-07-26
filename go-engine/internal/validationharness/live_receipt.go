// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
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

type liveReceiptCapability struct{ serial uint64 }

type liveReceiptIssuer struct {
	capability *liveReceiptCapability
	mu         sync.Mutex
	next       uint64
	active     bool
	nonces     map[[32]byte]struct{}
}

type liveReceiptAdmission struct {
	issuer    *liveReceiptIssuer
	operation liveOperation
	sequence  uint64
	nonce     [32]byte
}

var liveReceiptSerial atomic.Uint64

func newLiveReceiptIssuer() *liveReceiptIssuer {
	return &liveReceiptIssuer{capability: &liveReceiptCapability{serial: liveReceiptSerial.Add(1)}, nonces: make(map[[32]byte]struct{})}
}

func newLiveReceiptNonce() ([32]byte, error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nonce, err
	}
	return nonce, nil
}

func (issuer *liveReceiptIssuer) admit(operation liveOperation, sequence uint64, nonce [32]byte) (liveReceiptAdmission, error) {
	if issuer == nil || issuer.capability == nil || issuer.capability.serial == 0 || !operation.valid() || sequence == 0 || nonce == ([32]byte{}) {
		return liveReceiptAdmission{}, errors.New("invalid live receipt admission")
	}
	issuer.mu.Lock()
	defer issuer.mu.Unlock()
	if issuer.active || sequence != issuer.next+1 {
		return liveReceiptAdmission{}, errors.New("live receipt admission order rejected")
	}
	if _, exists := issuer.nonces[nonce]; exists {
		return liveReceiptAdmission{}, errors.New("live receipt nonce replay")
	}
	issuer.nonces[nonce] = struct{}{}
	issuer.next = sequence
	issuer.active = true
	return liveReceiptAdmission{issuer: issuer, operation: operation, sequence: sequence, nonce: nonce}, nil
}

func (admission liveReceiptAdmission) complete() {
	if admission.issuer == nil {
		return
	}
	admission.issuer.mu.Lock()
	admission.issuer.active = false
	admission.issuer.mu.Unlock()
}

func (admission liveReceiptAdmission) valid() bool {
	if admission.issuer == nil || admission.issuer.capability == nil || admission.issuer.capability.serial == 0 || !admission.operation.valid() || admission.sequence == 0 || admission.nonce == ([32]byte{}) {
		return false
	}
	admission.issuer.mu.Lock()
	defer admission.issuer.mu.Unlock()
	_, used := admission.issuer.nonces[admission.nonce]
	return used && admission.issuer.active && admission.sequence <= admission.issuer.next
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
	capability *liveReceiptCapability
	issuer     *liveReceiptIssuer
	operation  liveOperation
	sequence   uint64
	nonce      [32]byte

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
	if receipt == nil || receipt.issuer == nil || receipt.capability == nil || receipt.capability.serial == 0 || receipt.issuer.capability != receipt.capability || !receipt.operation.valid() || receipt.sequence == 0 || receipt.nonce == ([32]byte{}) || !receipt.sealed || receipt.requestSHA256 != receipt.requestDigest() || receipt.stdoutSHA256 != sha256.Sum256(receipt.stdout) || receipt.stderrSHA256 != sha256.Sum256(receipt.stderr) || receipt.resultSHA256 != receipt.resultDigest() {
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
func liveReceiptDecoderHandoff(receipt *liveExecutionReceipt, operation liveOperation, sequence uint64, nonce [32]byte) ([]byte, []byte, error) {
	if receipt == nil || receipt.operation != operation || receipt.sequence != sequence || receipt.nonce != nonce || receipt.failure != "" || receipt.validate() != nil {
		return nil, nil, errors.New("live receipt handoff rejected")
	}
	return append([]byte(nil), receipt.stdout...), append([]byte(nil), receipt.stderr...), nil
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
