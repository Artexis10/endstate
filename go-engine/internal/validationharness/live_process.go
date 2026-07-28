// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"crypto/sha256"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

const (
	maxLiveProcessOutputBytes = 1 << 20
	maxLiveProcessValueBytes  = 4 * 1024
)

type LiveExecutionClass string

const (
	LiveExecutionProbe     LiveExecutionClass = "probe"
	LiveExecutionEngine    LiveExecutionClass = "engine"
	LiveExecutionSeed      LiveExecutionClass = "seed"
	LiveExecutionWinget    LiveExecutionClass = "winget"
	LiveExecutionUninstall LiveExecutionClass = "uninstaller"
)

type LiveExecutionFailureCode string

const (
	LiveExecutionInvalidRequest LiveExecutionFailureCode = "invalid-request"
	LiveExecutionMutationDenied LiveExecutionFailureCode = "mutation-denied"
	LiveExecutionUnsupported    LiveExecutionFailureCode = "unsupported"
	LiveExecutionStartFailed    LiveExecutionFailureCode = "start-failed"
	LiveExecutionContainment    LiveExecutionFailureCode = "containment-failed"
	LiveExecutionTimeout        LiveExecutionFailureCode = "timeout"
	LiveExecutionCanceled       LiveExecutionFailureCode = "canceled"
	LiveExecutionOutputLimit    LiveExecutionFailureCode = "output-limit"
	LiveExecutionProcessExit    LiveExecutionFailureCode = "process-exit"
)

// LiveExecutionError has stable failure categories. Its text deliberately
// omits commands, arguments, paths, environment values, and child output.
type LiveExecutionError struct {
	Code  LiveExecutionFailureCode
	cause error
}

func (err *LiveExecutionError) Error() string {
	if err == nil {
		return ""
	}
	return "live process " + string(err.Code)
}

func (err *LiveExecutionError) Unwrap() error {
	return nil
}

func liveExecutionError(code LiveExecutionFailureCode, cause error) *LiveExecutionError {
	return &LiveExecutionError{Code: code, cause: cause}
}

// trustedLiveMutationPermit is intentionally unexported. A future trusted
// workflow may construct it only after validating its external authority; a
// catalog row or caller outside this package cannot turn candidate data into a
// mutation permit.
type trustedLiveMutationPermit struct{ capability *liveMutationCapability }

// trustedLiveHostMutationPermit is the internal authority capability for the
// two non-process campaign mutations. Its binding is opaque digests, never a
// caller-selected cleanup path.
type trustedLiveHostMutationPermit struct{ capability *liveHostMutationCapability }

type liveHostMutationBinding struct {
	appData, attemptRoot [32]byte
}

type liveHostMutationCapability struct {
	campaign                      [32]byte
	operation                     liveOperation
	sequence                      uint64
	nonce                         [32]byte
	issuedAt, expiresAt           time.Time
	definition, targets, observer [32]byte
	workflow                      [32]byte
	issuerID                      uint64
	admissionToken                [32]byte
	binding                       liveHostMutationBinding
	consumed                      atomic.Bool
}

func (capability *liveHostMutationCapability) validFor(admission liveReceiptAdmission, binding liveHostMutationBinding, now time.Time) bool {
	return capability.matches(admission, binding, now) && admission.issuer != nil && admission.issuer.activeFn != nil && admission.issuer.activeFn(admission)
}

func (capability *liveHostMutationCapability) matches(admission liveReceiptAdmission, binding liveHostMutationBinding, now time.Time) bool {
	return capability != nil && admission.issuer != nil && !capability.consumed.Load() && (capability.operation == liveOperationDeclaredTargetWipe || capability.operation == liveOperationAttemptRootCleanup) && capability.operation == admission.operation && (capability.operation != liveOperationDeclaredTargetWipe || binding.attemptRoot == ([32]byte{})) && (capability.operation != liveOperationAttemptRootCleanup || binding.attemptRoot != ([32]byte{})) && capability.campaign != ([32]byte{}) && capability.sequence == admission.sequence && capability.nonce == admission.nonce && capability.issuerID == admission.issuer.id && capability.admissionToken == admission.token && capability.binding == binding && capability.issuedAt.Before(now.Add(time.Nanosecond)) && capability.expiresAt.After(now) && capability.definition != ([32]byte{}) && capability.targets != ([32]byte{}) && capability.observer != ([32]byte{}) && capability.workflow != ([32]byte{}) && admission.issuer.authorityCampaign == capability.campaign
}

type liveMutationCapability struct {
	serial uint64

	campaign                      [32]byte
	operation                     liveOperation
	sequence                      uint64
	nonce                         [32]byte
	issuedAt, expiresAt           time.Time
	definition, engine, seed      [32]byte
	packageRef                    [32]byte
	comparator, targets, observer [32]byte
	workflow                      [32]byte
	issuerID                      uint64
	admissionToken                [32]byte
	packageArguments              []string
	executable                    string
	executableSHA256              [32]byte
	arguments                     []string
	directory                     string
	environment                   map[string]string
	consumed                      atomic.Bool
}

func (capability *liveMutationCapability) validFor(request LiveProcessRequest, now time.Time) bool {
	if capability == nil || capability.consumed.Load() || request.admission.issuer == nil || request.admission.issuer.activeFn == nil || !request.admission.issuer.activeFn(request.admission) {
		return false
	}
	return capability.matches(request, capability.executableSHA256, now)
}

func (capability *liveMutationCapability) matches(request LiveProcessRequest, image [32]byte, now time.Time) bool {
	if capability == nil || capability.campaign == ([32]byte{}) || !capability.operation.mutation() || capability.operation != request.operation || capability.sequence != request.admission.sequence || capability.nonce != request.admission.nonce || capability.issuedAt.IsZero() || capability.expiresAt.IsZero() || capability.issuedAt.After(now) || !capability.expiresAt.After(now) || capability.definition == ([32]byte{}) || capability.engine == ([32]byte{}) || capability.seed == ([32]byte{}) || capability.packageRef == ([32]byte{}) || capability.comparator == ([32]byte{}) || capability.targets == ([32]byte{}) || capability.observer == ([32]byte{}) || capability.workflow == ([32]byte{}) || capability.executableSHA256 != image {
		return false
	}
	if request.admission.issuer == nil || request.admission.issuer.id != capability.issuerID || request.admission.issuer.authorityCampaign != capability.campaign || request.admission.token != capability.admissionToken || request.expected.definition != capability.definition || request.expected.engine != capability.engine || request.expected.seed != capability.seed || request.expected.packageRef != capability.packageRef || request.expected.comparator != capability.comparator || request.expected.targets != capability.targets || request.expected.observer != capability.observer || request.expected.workflow != capability.workflow || request.executable != capability.executable || request.dir != capability.directory || !sameLiveArguments(request.args, capability.arguments) || !sameLiveEnvironment(request.environment, capability.environment) {
		return false
	}
	if request.executionClass() == LiveExecutionEngine && capability.executableSHA256 != request.expected.engine {
		return false
	}
	if request.executionClass() != LiveExecutionEngine && capability.executableSHA256 != request.expected.runner {
		return false
	}
	if request.operation == liveOperationWingetExactInstall {
		return liveExactWingetInstallArguments(request.args) && sameLiveArguments(request.args, capability.arguments)
	}
	if request.operation != liveOperationWingetExactUninstall {
		return true
	}
	return len(request.args) == 8 && len(capability.packageArguments) == 8 && liveExactWingetUninstallArguments(request.args, request.args[2]) && request.args[2] != "" && strings.Join(request.args, "\x00") == strings.Join(capability.packageArguments, "\x00")
}

func sameLiveArguments(left, right []string) bool {
	return strings.Join(left, "\x00") == strings.Join(right, "\x00")
}
func sameLiveEnvironment(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		rightValue, exists := right[key]
		if !exists || rightValue != value {
			return false
		}
	}
	return true
}

func (capability *liveMutationCapability) finalize(request LiveProcessRequest, image [32]byte, now time.Time) bool {
	if capability == nil || request.admission.issuer == nil || request.admission.issuer.finalizeMutationFn == nil {
		return false
	}
	return request.admission.issuer.finalizeMutationFn(request.admission, capability, request, image, now)
}

// LiveProcessRequest is an internal execution request. It has no zero-value
// behavior: probes are created only by a reviewed typed builder, and mutations
// only by a permit-bearing typed builder.
type LiveProcessRequest struct {
	executable  string
	args        []string
	dir         string
	environment map[string]string
	outputLimit int
	operation   liveOperation
	admission   liveReceiptAdmission
	expected    liveReceiptExpectedIdentity
	permit      trustedLiveMutationPermit
	appx        *liveTrustedAppXBinding
}

type liveAppXPackageMetadata struct {
	familyName, fullName, packageRoot, executableName string
	receipt                                           liveTrustedAppXReceipt
}

// liveTrustedAppXReceipt is populated only after AppModel selection, protected
// binding, and Authenticode verification. The runner rechecks it immediately
// before launch rather than treating a package-root spelling as authority.
type liveTrustedAppXReceipt struct {
	volume, indexHigh, indexLow uint32
	sha256                      [32]byte
	valid                       bool
}

// liveTrustedAppXBinding can only be constructed by the Windows AppModel
// resolver after it has selected exact package metadata. It is not a generic
// path exemption.
type liveTrustedAppXBinding struct{ metadata liveAppXPackageMetadata }

// liveProcessOutput is deliberately private and has no serialization tags:
// callers must decode it before creating any public evidence object.
type liveProcessOutput struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
	launched bool
	image    liveReceiptImageIdentity
	pid      uint32
	created  time.Time
	started  time.Time
	finished time.Time
}

func newLiveWingetListProbe(admission liveReceiptAdmission, executable, ref string, environment map[string]string, outputLimit int) LiveProcessRequest {
	return LiveProcessRequest{
		executable: executable, environment: cloneLiveEnvironment(environment), outputLimit: outputLimit, operation: liveOperationWingetExactList, admission: admission,
		args: []string{"list", "--id", ref, "--exact", "--source", "winget", "--accept-source-agreements", "--disable-interactivity"},
	}
}

func newLiveTrustedAppXWingetListProbe(admission liveReceiptAdmission, binding liveTrustedAppXBinding, ref string, environment map[string]string, outputLimit int) LiveProcessRequest {
	request := newLiveWingetListProbe(admission, filepath.Join(binding.metadata.packageRoot, binding.metadata.executableName), ref, environment, outputLimit)
	request.appx = &binding
	request.expected.packageRef = binding.metadata.receipt.sha256
	return request
}

// newLiveTrustedAppXWingetExactUninstall has no package, executable, argument,
// directory, or environment parameters. The authority-bound permit supplies
// the complete reviewed invocation; the resolver supplies only the held AppX
// executable binding.
func newLiveTrustedAppXWingetExactUninstall(admission liveReceiptAdmission, permit trustedLiveMutationPermit, binding liveTrustedAppXBinding, outputLimit int) LiveProcessRequest {
	return newLiveTrustedAppXWingetMutation(admission, permit, binding, liveOperationWingetExactUninstall, outputLimit)
}

// newLiveTrustedAppXWingetExactInstall is intentionally unreachable from the
// current 13/15-slot campaign plans. It exists only as the equally constrained
// typed primitive should a later reviewed plan admit this exact operation.
func newLiveTrustedAppXWingetExactInstall(admission liveReceiptAdmission, permit trustedLiveMutationPermit, binding liveTrustedAppXBinding, outputLimit int) LiveProcessRequest {
	return newLiveTrustedAppXWingetMutation(admission, permit, binding, liveOperationWingetExactInstall, outputLimit)
}

func newLiveTrustedAppXWingetMutation(admission liveReceiptAdmission, permit trustedLiveMutationPermit, binding liveTrustedAppXBinding, operation liveOperation, outputLimit int) LiveProcessRequest {
	capability := permit.capability
	if capability == nil {
		return LiveProcessRequest{operation: operation, admission: admission, permit: permit, outputLimit: outputLimit}
	}
	expected := liveReceiptExpectedIdentity{
		definition: capability.definition, engine: capability.engine, seed: capability.seed, packageRef: capability.packageRef,
		comparator: capability.comparator, targets: capability.targets, observer: capability.observer, workflow: capability.workflow, runner: capability.executableSHA256,
	}
	return LiveProcessRequest{
		executable: filepath.Join(binding.metadata.packageRoot, binding.metadata.executableName), args: append([]string(nil), capability.arguments...), dir: capability.directory,
		environment: cloneLiveEnvironment(capability.environment), outputLimit: outputLimit, operation: operation, admission: admission, expected: expected, permit: permit, appx: &binding,
	}
}

// newLiveTrustedEngineMutation copies the reviewed invocation exclusively from
// the authority capability; hosted adapters never accept engine command fields.
func newLiveTrustedEngineMutation(admission liveReceiptAdmission, permit trustedLiveMutationPermit, operation liveOperation, outputLimit int) LiveProcessRequest {
	capability := permit.capability
	if capability == nil {
		return LiveProcessRequest{operation: operation, admission: admission, permit: permit, outputLimit: outputLimit}
	}
	expected := liveReceiptExpectedIdentity{definition: capability.definition, engine: capability.engine, seed: capability.seed, packageRef: capability.packageRef, comparator: capability.comparator, targets: capability.targets, observer: capability.observer, workflow: capability.workflow, runner: capability.executableSHA256}
	return LiveProcessRequest{executable: capability.executable, args: append([]string(nil), capability.arguments...), dir: capability.directory, environment: cloneLiveEnvironment(capability.environment), outputLimit: outputLimit, operation: operation, admission: admission, expected: expected, permit: permit}
}

func newLiveEngineApply(admission liveReceiptAdmission, permit trustedLiveMutationPermit, executable string, args []string, dir string, environment map[string]string, expected liveReceiptExpectedIdentity, outputLimit int) LiveProcessRequest {
	return newLiveTypedMutation(admission, permit, liveOperationEngineApply, executable, args, dir, environment, expected, outputLimit)
}

func newLiveEngineVerify(admission liveReceiptAdmission, permit trustedLiveMutationPermit, executable string, args []string, dir string, environment map[string]string, expected liveReceiptExpectedIdentity, outputLimit int) LiveProcessRequest {
	return newLiveTypedMutation(admission, permit, liveOperationEngineVerify, executable, args, dir, environment, expected, outputLimit)
}

func newLiveEngineCapture(admission liveReceiptAdmission, permit trustedLiveMutationPermit, executable string, args []string, dir string, environment map[string]string, expected liveReceiptExpectedIdentity, outputLimit int) LiveProcessRequest {
	return newLiveTypedMutation(admission, permit, liveOperationEngineCapture, executable, args, dir, environment, expected, outputLimit)
}

func newLiveEngineRebuild(admission liveReceiptAdmission, permit trustedLiveMutationPermit, executable string, args []string, dir string, environment map[string]string, expected liveReceiptExpectedIdentity, outputLimit int) LiveProcessRequest {
	return newLiveTypedMutation(admission, permit, liveOperationEngineRebuild, executable, args, dir, environment, expected, outputLimit)
}

func newLiveEngineRevert(admission liveReceiptAdmission, permit trustedLiveMutationPermit, executable string, args []string, dir string, environment map[string]string, expected liveReceiptExpectedIdentity, outputLimit int) LiveProcessRequest {
	return newLiveTypedMutation(admission, permit, liveOperationEngineRevert, executable, args, dir, environment, expected, outputLimit)
}

func newLiveHashBoundSeed(admission liveReceiptAdmission, permit trustedLiveMutationPermit, executable string, args []string, dir string, environment map[string]string, expected liveReceiptExpectedIdentity, outputLimit int) LiveProcessRequest {
	return newLiveTypedMutation(admission, permit, liveOperationHashBoundSeed, executable, args, dir, environment, expected, outputLimit)
}

func newLiveTypedMutation(admission liveReceiptAdmission, permit trustedLiveMutationPermit, operation liveOperation, executable string, args []string, dir string, environment map[string]string, expected liveReceiptExpectedIdentity, outputLimit int) LiveProcessRequest {
	return LiveProcessRequest{executable: executable, args: append([]string(nil), args...), dir: dir, environment: cloneLiveEnvironment(environment), outputLimit: outputLimit, operation: operation, admission: admission, expected: expected, permit: permit}
}

func (request LiveProcessRequest) executionClass() LiveExecutionClass {
	switch request.operation {
	case liveOperationWingetExactList:
		return LiveExecutionProbe
	case liveOperationHashBoundSeed:
		return LiveExecutionSeed
	case liveOperationWingetExactInstall:
		return LiveExecutionWinget
	case liveOperationWingetExactUninstall:
		return LiveExecutionUninstall
	default:
		return LiveExecutionEngine
	}
}

func (request LiveProcessRequest) outputByteLimit() int {
	if request.outputLimit == 0 {
		return maxLiveProcessOutputBytes
	}
	return request.outputLimit
}

func (request LiveProcessRequest) mutates() bool {
	return request.operation.mutation()
}

func validateLiveProcessRequest(request LiveProcessRequest) error {
	if !validLiveProcessValue(request.executable) || !filepath.IsAbs(request.executable) || filepath.Clean(request.executable) != request.executable || !validLiveProcessValue(request.dir) && request.dir != "" {
		return liveExecutionError(LiveExecutionInvalidRequest, nil)
	}
	if !request.operation.valid() || !request.admission.valid() || request.admission.operation != request.operation {
		return liveExecutionError(LiveExecutionInvalidRequest, nil)
	}
	if len(request.args) > 64 || request.outputByteLimit() < 1 || request.outputByteLimit() > maxLiveProcessOutputBytes {
		return liveExecutionError(LiveExecutionInvalidRequest, nil)
	}
	for _, arg := range request.args {
		if !validLiveProcessValue(arg) {
			return liveExecutionError(LiveExecutionInvalidRequest, nil)
		}
	}
	if _, err := liveProcessEnvironment(request.environment); err != nil {
		return err
	}
	if request.operation == liveOperationWingetExactList && !validLiveProbe(request) {
		return liveExecutionError(LiveExecutionInvalidRequest, nil)
	}
	if !request.expected.valid(request.operation) {
		return liveExecutionError(LiveExecutionInvalidRequest, nil)
	}
	if request.mutates() && !request.permit.capability.validFor(request, time.Now().UTC()) {
		return liveExecutionError(LiveExecutionMutationDenied, nil)
	}
	if (request.operation == liveOperationWingetExactInstall || request.operation == liveOperationWingetExactUninstall) && (request.appx == nil || !request.appx.metadata.receipt.valid || request.executable != filepath.Join(request.appx.metadata.packageRoot, request.appx.metadata.executableName)) {
		return liveExecutionError(LiveExecutionInvalidRequest, nil)
	}
	if request.operation == liveOperationWingetExactInstall && !liveExactWingetInstallArguments(request.args) {
		return liveExecutionError(LiveExecutionInvalidRequest, nil)
	}
	return nil
}

func validLiveProbe(request LiveProcessRequest) bool {
	_, ok := liveWingetListProbeReference(request.args)
	return request.operation == liveOperationWingetExactList && ok
}

func liveWingetListProbeReference(args []string) (string, bool) {
	if len(args) != 8 || args[0] != "list" || args[1] != "--id" || !validLiveProcessValue(args[2]) {
		return "", false
	}
	return args[2], args[3] == "--exact" && args[4] == "--source" && args[5] == "winget" && args[6] == "--accept-source-agreements" && args[7] == "--disable-interactivity"
}

func liveExactWingetInstallArguments(args []string) bool {
	return len(args) == 8 && args[0] == "install" && args[1] == "Notepad++.Notepad++" && args[2] == "--exact" && args[3] == "--source" && args[4] == "winget" && args[5] == "--accept-package-agreements" && args[6] == "--accept-source-agreements" && args[7] == "--disable-interactivity"
}

func runLiveProcess(ctx context.Context, request LiveProcessRequest) (*liveExecutionReceipt, error) {
	defer request.admission.complete()
	if err := validateLiveProcessRequest(request); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, liveProcessContextError(err)
	}
	output, err := runLiveProcessPlatform(ctx, request, func(image liveReceiptImageIdentity) error {
		if request.mutates() {
			if !request.permit.capability.finalize(request, image.sha256, time.Now().UTC()) {
				return liveExecutionError(LiveExecutionMutationDenied, nil)
			}
			return nil
		}
		if request.admission.issuer.commitLaunchFn == nil || !request.admission.issuer.commitLaunchFn(request.admission) {
			return liveExecutionError(LiveExecutionContainment, nil)
		}
		return nil
	})
	if !output.launched {
		request.admission.issuer.abortLaunchFn(request.admission)
		return nil, err
	}
	if request.mutates() && request.permit.capability.executableSHA256 != output.image.sha256 {
		err = liveExecutionError(LiveExecutionContainment, nil)
	}
	if request.executionClass() == LiveExecutionEngine && request.expected.engine != output.image.sha256 {
		err = liveExecutionError(LiveExecutionContainment, nil)
	}
	if request.appx != nil && request.appx.metadata.receipt.sha256 != output.image.sha256 {
		err = liveExecutionError(LiveExecutionContainment, nil)
	}
	receipt := &liveExecutionReceipt{
		issuerID: request.admission.issuer.id, operation: request.operation, sequence: request.admission.sequence, nonce: request.admission.nonce, admissionToken: request.admission.token,
		executable: request.executable, args: append([]string(nil), request.args...), directory: request.dir, environment: cloneLiveEnvironment(request.environment), expected: request.expected,
		image: output.image, pid: output.pid, created: output.created.UTC(), started: output.started.UTC(), finished: output.finished.UTC(), exitCode: output.ExitCode,
		stdout: append([]byte(nil), output.Stdout...), stderr: append([]byte(nil), output.Stderr...),
	}
	if execution, ok := err.(*LiveExecutionError); ok {
		receipt.failure = execution.Code
	}
	receipt.requestSHA256 = receipt.requestDigest()
	receipt.stdoutSHA256 = sha256.Sum256(receipt.stdout)
	receipt.stderrSHA256 = sha256.Sum256(receipt.stderr)
	receipt.resultSHA256 = receipt.resultDigest()
	if err := request.admission.issuer.sealFn(receipt); err != nil {
		return nil, liveExecutionError(LiveExecutionContainment, err)
	}
	return receipt, err
}

func liveProcessContextError(err error) error {
	if err == context.DeadlineExceeded {
		return liveExecutionError(LiveExecutionTimeout, err)
	}
	return liveExecutionError(LiveExecutionCanceled, err)
}

func liveProcessEnvironment(overrides map[string]string) ([]string, error) {
	if len(overrides) > len(liveProcessEnvironmentAllowlist) {
		return nil, liveExecutionError(LiveExecutionInvalidRequest, nil)
	}
	values := make([]string, 0, len(overrides))
	for name, value := range overrides {
		if _, allowed := liveProcessEnvironmentAllowlist[strings.ToUpper(name)]; !allowed || name != strings.ToUpper(name) || !validLiveProcessEnvironmentValue(value) {
			return nil, liveExecutionError(LiveExecutionInvalidRequest, nil)
		}
		values = append(values, name+"="+value)
	}
	sort.Strings(values)
	return values, nil
}

var liveProcessEnvironmentAllowlist = map[string]struct{}{
	"APPDATA": {}, "COMSPEC": {}, "ENDSTATE_ROOT": {}, "LOCALAPPDATA": {}, "PATH": {}, "PATHEXT": {},
	"SYSTEMROOT": {}, "TEMP": {}, "TMP": {}, "USERPROFILE": {}, "WINDIR": {},
}

func validLiveProcessValue(value string) bool {
	return value != "" && len(value) <= maxLiveProcessValueBytes && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\x00\r\n")
}

func validLiveProcessEnvironmentValue(value string) bool {
	return len(value) <= maxLiveProcessOutputBytes && !strings.ContainsAny(value, "\x00\r\n")
}

func containsLiveEnvironment(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
