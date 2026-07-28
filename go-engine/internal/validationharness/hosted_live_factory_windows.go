// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// prepareWindowsHostedLiveRunner owns every mutable runtime path. It is the
// only production construction route: callers provide neither app-data nor an
// attempt/result directory, command, argument, working directory, or env.
func prepareWindowsHostedLiveRunner(ctx context.Context, session *LiveAuthoritySession, definition LiveDefinition, checkoutRoot string) (*windowsHostedLiveRunner, error) {
	if session == nil || ctx == nil || filepath.IsAbs(checkoutRoot) == false {
		return nil, fmt.Errorf("hosted live runtime inputs are invalid")
	}
	appData, err := windowsLiveRoamingAppData()
	if err != nil {
		return nil, fmt.Errorf("hosted live APPDATA is unavailable")
	}
	appData, err = validatedWindowsLiveAppData(appData)
	if err != nil {
		return nil, err
	}
	parent, err := windowsLiveRunnerTemp()
	if err != nil {
		return nil, fmt.Errorf("hosted live attempt parent is unavailable")
	}
	attempt, err := newWindowsLiveAttemptRoot(parent)
	if err != nil {
		return nil, err
	}
	keepAttempt := false
	defer func() {
		if !keepAttempt {
			_ = attempt.Cleanup()
		}
	}()
	resolver, err := newLiveWingetResolver()
	if err != nil {
		return nil, fmt.Errorf("hosted live winget resolver is unavailable")
	}
	winget, err := resolver.ResolveLiveWinget(ctx)
	if err != nil {
		return nil, fmt.Errorf("hosted live winget binding is unavailable")
	}
	if err := bindWindowsLiveRuntime(session, definition, attempt, filepath.Clean(checkoutRoot), winget); err != nil {
		return nil, err
	}
	storage, err := bindHostedLiveStorageRoot(attempt)
	if err != nil {
		return nil, err
	}
	plan, err := deriveHostedLiveSequencePlan(session)
	if err != nil {
		return nil, err
	}
	issuer := session.NewReceiptIssuer()
	if issuer == nil {
		return nil, fmt.Errorf("hosted live receipt issuer is unavailable")
	}
	observer, err := NewWindowsLiveObserver(nil)
	if err != nil {
		return nil, err
	}
	apply := session.definition.operations[plan.apply]
	if apply.Executable == "" {
		return nil, fmt.Errorf("hosted live engine binding is unavailable")
	}
	runnerImage, err := hostedLiveGitHubRunnerImage()
	if err != nil {
		return nil, err
	}
	keepAttempt = true
	bindings := newWindowsHostedLiveProductionBindings(apply.Executable, observer, winget.binding, issuer, appData)
	bindings.runnerImage = runnerImage
	return &windowsHostedLiveRunner{session: session, definition: definition, appData: appData, attemptRoot: attempt, checkoutRoot: filepath.Clean(checkoutRoot), storageRoot: storage, plan: plan, bindings: bindings}, nil
}

var hostedLiveGitHubRunnerImage = func() (string, error) {
	osName, version := strings.TrimSpace(os.Getenv("ImageOS")), strings.TrimSpace(os.Getenv("ImageVersion"))
	image := osName + "-" + version
	if osName == "" || version == "" || !validHostedLiveEvidenceValue(image) {
		return "", fmt.Errorf("hosted live GitHub runner identity is unavailable")
	}
	return image, nil
}

func newWindowsHostedLiveProductionBindings(enginePath string, observer LiveObserver, winget liveTrustedAppXBinding, issuer *liveReceiptIssuer, appData string) hostedLiveWindowsBindings {
	return hostedLiveWindowsBindings{
		enginePath: enginePath, observer: observer, winget: winget, issuer: issuer, boundary: windowsLiveBoundaryReader{observer: observer, appData: appData}, runProcess: runLiveProcess,
		snapshotTargets: snapshotHostedLiveTargets, snapshotStorage: snapshotHostedLiveStorage,
		wipeTargets: runWindowsLiveDeclaredTargetWipe, cleanupAttempt: runWindowsLiveAttemptRootCleanup,
		hostname: os.Hostname, captureClaims: projectLiveCaptureClaims, inspectArtifact: inspectLiveCaptureArtifact,
		declaredTargetPaths: hostedLiveDeclaredTargetPaths, declaredTargetBindings: hostedLiveDeclaredTargetBindings,
	}
}
