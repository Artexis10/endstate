// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

type validationFilesystemGuard interface {
	Protect([]validationmode.ProtectedPath) error
	Seal()
	Check() ([]validationmode.Change, error)
	Label(string) string
}

type validationRegistryGuard interface {
	Protect([]validationmode.ProtectedRegistry) error
	Seal()
	Check() ([]validationmode.RegistryChange, error)
}

func newValidationModeSession(context *validationmode.Context, recorder *validationIsolationRecorder) *ValidationModeSession {
	return &ValidationModeSession{
		context:              context,
		recorder:             recorder,
		registryGuard:        validationmode.NewRegistryGuard(context),
		filesystemCoordinate: make(map[string]string),
		registryCoordinate:   make(map[string]string),
	}
}

func (session *ValidationModeSession) captureAuthorityPaths() {
	workingDirectory, err := os.Getwd()
	if err != nil {
		_ = session.recordIsolationFinding("authority.checkout", "checkout", isolationReasonUnsafePath)
	} else {
		_ = session.registerOriginalFilesystemPath("authority.checkout", "checkout", workingDirectory)
	}
	for _, authority := range []struct {
		environment string
		coordinate  string
		label       string
	}{
		{environment: "GITHUB_WORKSPACE", coordinate: "authority.github-workspace", label: "github-workspace"},
		{environment: "RUNNER_WORKSPACE", coordinate: "authority.runner-workspace", label: "runner-workspace"},
	} {
		if value, set := os.LookupEnv(authority.environment); set {
			_ = session.registerOriginalFilesystemPath(authority.coordinate, authority.label, value)
		}
	}
}

func (session *ValidationModeSession) registerOriginalFilesystemPath(coordinate, safeLabel, path string) error {
	coordinate = safeIsolationField(coordinate, "invalid-coordinate")
	safeLabel = safeIsolationField(safeLabel, "invalid-target")
	if session == nil || session.context == nil {
		return fmt.Errorf("%w: validation session is inactive", validationmode.ErrUnsafePath)
	}
	session.lifecycleMu.Lock()
	if session.sealed {
		session.lifecycleMu.Unlock()
		return session.recordIsolationFinding(coordinate, safeLabel, isolationReasonUnsafePath)
	}
	var err error
	if session.filesystemGuard == nil {
		var guard *validationmode.WriteGuard
		guard, err = validationmode.NewWriteGuard(session.context.Root(), []string{path})
		if err == nil {
			session.filesystemGuard = guard
			err = guard.Protect([]validationmode.ProtectedPath{{Path: path, Label: safeLabel}})
		}
	} else {
		err = session.filesystemGuard.Protect([]validationmode.ProtectedPath{{Path: path, Label: safeLabel}})
	}
	if err == nil {
		session.filesystemCoordinate[safeLabel] = coordinate
	}
	session.lifecycleMu.Unlock()
	if err != nil {
		return session.recordIsolationFinding(coordinate, safeLabel, isolationReasonForGuardError(err, isolationReasonUnsafePath))
	}
	return nil
}

func (session *ValidationModeSession) registerOriginalRegistryProtection(coordinate, safeLabel string, protection validationmode.ProtectedRegistry) error {
	coordinate = safeIsolationField(coordinate, "invalid-coordinate")
	safeLabel = safeIsolationField(safeLabel, "invalid-target")
	if session == nil || session.context == nil || session.registryGuard == nil {
		return fmt.Errorf("%w: validation registry session is inactive", validationmode.ErrUnsafeRegistry)
	}
	session.lifecycleMu.Lock()
	if session.sealed {
		session.lifecycleMu.Unlock()
		return session.recordIsolationFinding(coordinate, safeLabel, isolationReasonUnsafeRegistry)
	}
	protection.Label = safeLabel
	err := session.registryGuard.Protect([]validationmode.ProtectedRegistry{protection})
	if err == nil {
		session.registryCoordinate[safeLabel] = coordinate
	}
	session.lifecycleMu.Unlock()
	if err != nil {
		return session.recordIsolationFinding(coordinate, safeLabel, isolationReasonForGuardError(err, isolationReasonUnsafeRegistry))
	}
	return nil
}

func (session *ValidationModeSession) sealIsolation() {
	if session == nil {
		return
	}
	session.lifecycleMu.Lock()
	defer session.lifecycleMu.Unlock()
	if session.sealed {
		return
	}
	if session.filesystemGuard != nil {
		session.filesystemGuard.Seal()
	}
	if session.registryGuard != nil {
		session.registryGuard.Seal()
	}
	session.sealed = true
}

func (session *ValidationModeSession) checkIsolationGuards() {
	if session.filesystemGuard != nil {
		changes, err := session.filesystemGuard.Check()
		if err != nil {
			_ = session.recordIsolationFinding("guard.filesystem", "filesystem", isolationReasonForGuardError(err, isolationReasonUnsafePath))
		} else {
			for _, change := range changes {
				label := safeIsolationField(session.filesystemGuard.Label(change.Path), "protected")
				coordinate := session.filesystemCoordinate[label]
				if coordinate == "" {
					coordinate = "guard.filesystem"
				}
				_ = session.recordIsolationFinding(coordinate, label, isolationReasonGuardChanged)
			}
		}
	}
	if session.registryGuard != nil {
		changes, err := session.registryGuard.Check()
		if err != nil {
			_ = session.recordIsolationFinding("guard.registry", "registry", isolationReasonForGuardError(err, isolationReasonUnsafeRegistry))
		} else {
			for _, change := range changes {
				label := safeIsolationField(change.Label, "registry-target")
				coordinate := session.registryCoordinate[label]
				if coordinate == "" {
					coordinate = "guard.registry"
				}
				_ = session.recordIsolationFinding(coordinate, label, isolationReasonGuardChanged)
			}
		}
	}
}

func isolationReasonForGuardError(err error, fallback isolationReason) isolationReason {
	if errors.Is(err, validationmode.ErrGuardBudget) {
		return isolationReasonGuardBudget
	}
	return fallback
}

type isolationReason string

const (
	isolationReasonUnsafePath     isolationReason = "unsafe_path"
	isolationReasonUnsafeRegistry isolationReason = "unsafe_registry"
	isolationReasonGuardChanged   isolationReason = "guard_changed"
	isolationReasonGuardBudget    isolationReason = "guard_budget"
)

var safeIsolationFieldPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+/\[\]-]{0,159}$`)

type isolationFinding struct {
	scenarioID string
	moduleID   string
	coordinate string
	target     string
	reason     isolationReason
}

func (finding isolationFinding) key() string {
	return strings.Join([]string{finding.scenarioID, finding.moduleID, finding.coordinate, finding.target, string(finding.reason)}, "\x00")
}

func (finding isolationFinding) err() error {
	return fmt.Errorf(
		"%w: scenario=%s module=%s coordinate=%s target=%s reason=%s",
		finding.sentinel(), finding.scenarioID, finding.moduleID,
		finding.coordinate, finding.target, finding.reason,
	)
}

func (finding isolationFinding) sentinel() error {
	switch finding.reason {
	case isolationReasonUnsafeRegistry:
		return validationmode.ErrUnsafeRegistry
	case isolationReasonGuardChanged:
		return validationmode.ErrGuardChanged
	case isolationReasonGuardBudget:
		return validationmode.ErrGuardBudget
	default:
		return validationmode.ErrUnsafePath
	}
}

type validationIsolationRecorder struct {
	mu          sync.Mutex
	descriptor  validationmode.Descriptor
	findings    map[string]isolationFinding
	packageFail bool
}

func newValidationIsolationRecorder(descriptor validationmode.Descriptor) *validationIsolationRecorder {
	return &validationIsolationRecorder{
		descriptor: descriptor,
		findings:   make(map[string]isolationFinding),
	}
}

func (recorder *validationIsolationRecorder) record(coordinate, target string, reason isolationReason) error {
	if recorder == nil {
		return fmt.Errorf("%w: validation isolation recorder is inactive", validationmode.ErrUnsafePath)
	}
	finding := isolationFinding{
		scenarioID: safeIsolationField(recorder.descriptor.ScenarioID, "scenario"),
		moduleID:   safeIsolationField(recorder.descriptor.ModuleID, "module"),
		coordinate: safeIsolationField(coordinate, "invalid-coordinate"),
		target:     safeIsolationField(target, "invalid-target"),
		reason:     normalizeIsolationReason(reason),
	}
	recorder.mu.Lock()
	recorder.findings[finding.key()] = finding
	recorder.mu.Unlock()
	return finding.err()
}

func (recorder *validationIsolationRecorder) recordPackageFailure() {
	if recorder == nil {
		return
	}
	recorder.mu.Lock()
	recorder.packageFail = true
	recorder.mu.Unlock()
}

func (recorder *validationIsolationRecorder) poisoned() bool {
	if recorder == nil {
		return false
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return recorder.packageFail || len(recorder.findings) > 0
}

func (recorder *validationIsolationRecorder) isolationError() error {
	if recorder == nil {
		return nil
	}
	recorder.mu.Lock()
	packageFail := recorder.packageFail
	findings := make([]isolationFinding, 0, len(recorder.findings))
	for _, finding := range recorder.findings {
		findings = append(findings, finding)
	}
	recorder.mu.Unlock()
	sort.Slice(findings, func(left, right int) bool { return findings[left].key() < findings[right].key() })
	errorsToJoin := make([]error, 0, len(findings)+1)
	if packageFail {
		errorsToJoin = append(errorsToJoin, fmt.Errorf("%w: descriptor-bound package operation rejected", validationmode.ErrPackageIdentity))
	}
	for _, finding := range findings {
		errorsToJoin = append(errorsToJoin, finding.err())
	}
	return errors.Join(errorsToJoin...)
}

func safeIsolationField(value, fallback string) string {
	value = strings.TrimSpace(value)
	if !safeIsolationFieldPattern.MatchString(value) {
		return fallback
	}
	return value
}

func normalizeIsolationReason(reason isolationReason) isolationReason {
	switch reason {
	case isolationReasonUnsafePath, isolationReasonUnsafeRegistry, isolationReasonGuardChanged, isolationReasonGuardBudget:
		return reason
	default:
		return isolationReasonUnsafePath
	}
}
