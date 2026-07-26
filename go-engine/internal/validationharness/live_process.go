// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
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
	if err == nil {
		return nil
	}
	return err.cause
}

func liveExecutionError(code LiveExecutionFailureCode, cause error) *LiveExecutionError {
	return &LiveExecutionError{Code: code, cause: cause}
}

// trustedLiveMutationPermit is intentionally unexported. A future trusted
// workflow may construct it only after validating its external authority; a
// catalog row or caller outside this package cannot turn candidate data into a
// mutation permit.
type trustedLiveMutationPermit struct{ granted bool }

func newTrustedLiveMutationPermit() trustedLiveMutationPermit {
	return trustedLiveMutationPermit{granted: true}
}

// LiveProcessRequest is an internal execution request. Its zero class is a
// read-only probe. Mutating classes require a trusted in-memory permit.
type LiveProcessRequest struct {
	Name        string
	Args        []string
	Dir         string
	Environment map[string]string
	OutputLimit int
	Class       LiveExecutionClass

	permit trustedLiveMutationPermit
}

// liveProcessOutput is deliberately private and has no serialization tags:
// callers must decode it before creating any public evidence object.
type liveProcessOutput struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}

func (request LiveProcessRequest) executionClass() LiveExecutionClass {
	if request.Class == "" {
		return LiveExecutionProbe
	}
	return request.Class
}

func (request LiveProcessRequest) outputLimit() int {
	if request.OutputLimit == 0 {
		return maxLiveProcessOutputBytes
	}
	return request.OutputLimit
}

func (request LiveProcessRequest) mutates() bool {
	switch request.executionClass() {
	case LiveExecutionEngine, LiveExecutionSeed, LiveExecutionWinget, LiveExecutionUninstall:
		return true
	default:
		return false
	}
}

func validateLiveProcessRequest(request LiveProcessRequest) error {
	if !validLiveProcessValue(request.Name) || !filepath.IsAbs(request.Name) || filepath.Clean(request.Name) != request.Name || !validLiveProcessValue(request.Dir) && request.Dir != "" {
		return liveExecutionError(LiveExecutionInvalidRequest, nil)
	}
	switch request.executionClass() {
	case LiveExecutionProbe, LiveExecutionEngine, LiveExecutionSeed, LiveExecutionWinget, LiveExecutionUninstall:
	default:
		return liveExecutionError(LiveExecutionInvalidRequest, nil)
	}
	if len(request.Args) > 64 || request.outputLimit() < 1 || request.outputLimit() > maxLiveProcessOutputBytes {
		return liveExecutionError(LiveExecutionInvalidRequest, nil)
	}
	for _, arg := range request.Args {
		if !validLiveProcessValue(arg) {
			return liveExecutionError(LiveExecutionInvalidRequest, nil)
		}
	}
	if _, err := liveProcessEnvironment(request.Environment); err != nil {
		return err
	}
	if request.mutates() && !request.permit.granted {
		return liveExecutionError(LiveExecutionMutationDenied, nil)
	}
	return nil
}

func runLiveProcess(ctx context.Context, request LiveProcessRequest) (liveProcessOutput, error) {
	if err := validateLiveProcessRequest(request); err != nil {
		return liveProcessOutput{}, err
	}
	if err := ctx.Err(); err != nil {
		return liveProcessOutput{}, liveProcessContextError(err)
	}
	return runLiveProcessPlatform(ctx, request)
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
	"APPDATA": {}, "COMSPEC": {}, "LOCALAPPDATA": {}, "PATH": {}, "PATHEXT": {},
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
