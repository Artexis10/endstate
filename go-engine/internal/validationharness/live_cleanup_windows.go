// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

func windowsLiveHostMutationBinding(definition LiveDefinition, appData string, attempt windowsLiveAttemptRoot) (liveHostMutationBinding, error) {
	root, err := validatedWindowsLiveAppData(appData)
	if err != nil || validateLiveDefinition(definition) != nil {
		return liveHostMutationBinding{}, fmt.Errorf("live host mutation binding is unsafe")
	}
	binding := liveHostMutationBinding{appData: sha256.Sum256([]byte(filepath.Clean(root)))}
	if attempt.path != "" {
		if !attempt.valid() {
			return liveHostMutationBinding{}, fmt.Errorf("live attempt root is unsafe")
		}
		binding.attemptRoot = sha256.Sum256([]byte(attempt.path))
	}
	return binding, nil
}

func validatedWindowsLiveAppData(supplied string) (string, error) {
	root, err := windowsLiveRoamingAppData()
	if err != nil || safepath.ValidateRoot(root) != nil || supplied == "" || !strings.EqualFold(filepath.Clean(root), filepath.Clean(supplied)) {
		return "", fmt.Errorf("live APPDATA root is unsafe")
	}
	if environment := os.Getenv("APPDATA"); environment != "" && !strings.EqualFold(filepath.Clean(root), filepath.Clean(environment)) {
		return "", fmt.Errorf("live APPDATA environment differs from known folder")
	}
	return filepath.Clean(root), nil
}

func runWindowsLiveDeclaredTargetWipe(admission liveReceiptAdmission, permit trustedLiveHostMutationPermit, definition LiveDefinition, appData string) (*liveHostMutationReceipt, error) {
	appData, err := validatedWindowsLiveAppData(appData)
	if err != nil {
		return nil, err
	}
	binding, err := windowsLiveHostMutationBinding(definition, appData, windowsLiveAttemptRoot{})
	if err != nil {
		return nil, err
	}
	return runWindowsLiveHostMutation(admission, permit, definition, binding, func() error {
		if err := wipeWindowsLiveDeclaredTargets(definition, appData); err != nil {
			return err
		}
		for _, target := range definition.DeclaredTargets {
			state, err := (windowsLiveBoundaryReader{appData: appData}).Target(nil, target)
			if err != nil || state.present {
				return fmt.Errorf("declared target remains after wipe")
			}
		}
		return nil
	})
}

func runWindowsLiveAttemptRootCleanup(admission liveReceiptAdmission, permit trustedLiveHostMutationPermit, definition LiveDefinition, appData string, attempt windowsLiveAttemptRoot) (*liveHostMutationReceipt, error) {
	appData, err := validatedWindowsLiveAppData(appData)
	if err != nil {
		return nil, err
	}
	binding, err := windowsLiveHostMutationBinding(definition, appData, attempt)
	if err != nil {
		return nil, err
	}
	return runWindowsLiveHostMutation(admission, permit, definition, binding, attempt.Cleanup)
}

func runWindowsLiveHostMutation(admission liveReceiptAdmission, permit trustedLiveHostMutationPermit, definition LiveDefinition, binding liveHostMutationBinding, mutate func() error) (*liveHostMutationReceipt, error) {
	defer admission.complete()
	capability := permit.capability
	if capability == nil || !windowsLiveHostPermitMatchesDefinition(capability, definition) || !capability.validFor(admission, binding, time.Now().UTC()) || admission.issuer == nil || admission.issuer.finalizeHostMutationFn == nil || !admission.issuer.finalizeHostMutationFn(admission, capability, binding, time.Now().UTC()) {
		return nil, fmt.Errorf("live host mutation is not authorized")
	}
	err := mutate()
	receipt := &liveHostMutationReceipt{issuerID: admission.issuer.id, operation: admission.operation, sequence: admission.sequence, nonce: admission.nonce, admissionToken: admission.token, binding: binding, succeeded: err == nil}
	if admission.issuer.sealHostMutationFn == nil || admission.issuer.sealHostMutationFn(receipt) != nil {
		return nil, fmt.Errorf("live host mutation receipt could not seal")
	}
	return receipt, err
}

func windowsLiveHostPermitMatchesDefinition(capability *liveHostMutationCapability, definition LiveDefinition) bool {
	digest, err := CanonicalLiveDefinitionSHA256(definition)
	return err == nil && capability.definition == liveSHA256Bytes(digest) && capability.targets == liveSHA256Bytes(liveSHA256Hex(definition.DeclaredTargets)) && capability.observer == liveSHA256Bytes(liveSHA256Hex(definition.Observer)) && capability.workflow != ([32]byte{})
}

type liveResolvedDeclaredTarget struct {
	identity string
	path     string
	kind     LiveDeclaredTargetKind
}

type windowsLiveAttemptRoot struct {
	parent string
	path   string
	nonce  [32]byte
	object windowsLiveObjectIdentity
}

type windowsLiveAttemptRootCleanupReservation struct{ root windowsLiveAttemptRoot }

var windowsLiveAttemptRoots sync.Map

func newWindowsLiveAttemptRoot(parent string) (windowsLiveAttemptRoot, error) {
	trustedParent, err := windowsLiveRunnerTemp()
	if err != nil || parent == "" || !strings.EqualFold(filepath.Clean(parent), filepath.Clean(trustedParent)) || safepath.ValidateRoot(parent) != nil {
		return windowsLiveAttemptRoot{}, fmt.Errorf("live attempt parent is unsafe")
	}
	path, err := os.MkdirTemp(trustedParent, "endstate-hosted-live-")
	if err != nil {
		return windowsLiveAttemptRoot{}, err
	}
	root := windowsLiveAttemptRoot{parent: filepath.Clean(parent), path: filepath.Clean(path)}
	if _, err := rand.Read(root.nonce[:]); err != nil || root.nonce == ([32]byte{}) {
		_ = os.Remove(path)
		return windowsLiveAttemptRoot{}, fmt.Errorf("live attempt ownership receipt is unavailable")
	}
	object, err := windowsLiveObjectIdentityForPath(root.path, true)
	if err != nil {
		_ = os.Remove(path)
		return windowsLiveAttemptRoot{}, fmt.Errorf("live attempt root identity is unavailable")
	}
	root.object = object
	windowsLiveAttemptRoots.Store(root.nonce, root)
	if !root.valid() {
		windowsLiveAttemptRoots.Delete(root.nonce)
		_ = os.Remove(path)
		return windowsLiveAttemptRoot{}, fmt.Errorf("live attempt root is unsafe")
	}
	return root, nil
}

func (root windowsLiveAttemptRoot) valid() bool {
	if root.parent == "" || root.path == "" || root.nonce == ([32]byte{}) || filepath.Dir(root.path) != root.parent || !strings.HasPrefix(filepath.Base(root.path), "endstate-hosted-live-") {
		return false
	}
	value, exists := windowsLiveAttemptRoots.Load(root.nonce)
	owned, ok := value.(windowsLiveAttemptRoot)
	if !exists || !ok || owned.parent != root.parent || owned.path != root.path || owned.nonce != root.nonce || owned.object != root.object {
		return false
	}
	if err := safepath.ValidateRoot(root.parent); err != nil {
		return false
	}
	object, err := windowsLiveObjectIdentityForPath(root.path, true)
	return err == nil && object == root.object
}

func (root windowsLiveAttemptRoot) Cleanup() error {
	reservation := windowsLiveAttemptRootCleanupReservation{root: root}
	if !windowsLiveAttemptRoots.CompareAndSwap(root.nonce, root, reservation) {
		return fmt.Errorf("live attempt root is unsafe")
	}
	reserved := true
	defer func() {
		if reserved {
			windowsLiveAttemptRoots.CompareAndSwap(root.nonce, reservation, root)
		}
	}()
	if windowsLiveCleanupBeforeAttemptRootOpen != nil {
		windowsLiveCleanupBeforeAttemptRootOpen(root.path)
	}
	handle, err := openWindowsLiveCleanupHandle(root.path, true)
	if err != nil {
		return fmt.Errorf("cleanup live attempt root: %w", err)
	}
	ctx, cancel := defaultWindowsLiveCleanupContext()
	defer cancel()
	if err := removeWindowsLiveDirectoryHandleWithBudget(ctx, handle, root.path, windowsLiveCleanupBudget{maxDepth: maxWindowsLiveCleanupDepth, maxEntries: maxWindowsLiveCleanupEntries}, root.object); err != nil {
		return fmt.Errorf("cleanup live attempt root: %w", err)
	}
	windowsLiveAttemptRoots.Delete(root.nonce)
	reserved = false
	return nil
}

func resolveWindowsLiveDeclaredTargets(definition LiveDefinition, appData string) ([]liveResolvedDeclaredTarget, error) {
	if err := validateLiveDefinition(definition); err != nil {
		return nil, fmt.Errorf("live declared target definition is invalid")
	}
	if err := safepath.ValidateRoot(appData); err != nil {
		return nil, fmt.Errorf("live APPDATA root is unsafe")
	}
	resolved := make([]liveResolvedDeclaredTarget, 0, len(definition.DeclaredTargets))
	seen := make(map[string]struct{}, len(definition.DeclaredTargets))
	for _, target := range definition.DeclaredTargets {
		portable, ok := liveAppDataTargetRelative(target.Template)
		if !ok {
			return nil, fmt.Errorf("live declared target is outside APPDATA")
		}
		path, err := safepath.Resolve(appData, portable)
		if err != nil {
			return nil, fmt.Errorf("resolve live declared target: %w", err)
		}
		key := strings.ToLower(path)
		if _, duplicate := seen[key]; duplicate {
			return nil, fmt.Errorf("live declared target resolves more than once")
		}
		for _, existing := range resolved {
			if liveWindowsPathsOverlap(path, existing.path) {
				return nil, fmt.Errorf("live declared targets overlap")
			}
		}
		seen[key] = struct{}{}
		resolved = append(resolved, liveResolvedDeclaredTarget{identity: target.Identity, path: path, kind: target.Kind})
	}
	return resolved, nil
}

func liveAppDataTargetRelative(template string) (string, bool) {
	const prefix = `%APPDATA%\`
	if !strings.HasPrefix(strings.ToUpper(template), prefix) {
		return "", false
	}
	relative := template[len(prefix):]
	if relative == "" || strings.ContainsAny(relative, `:*?[]{}$%`) || strings.Contains(relative, `..`) {
		return "", false
	}
	return strings.ReplaceAll(relative, `\`, "/"), true
}

func liveWindowsPathsOverlap(left, right string) bool {
	left, right = strings.TrimRight(strings.ToLower(left), `\`), strings.TrimRight(strings.ToLower(right), `\`)
	return left == right || strings.HasPrefix(left, right+`\`) || strings.HasPrefix(right, left+`\`)
}

func wipeWindowsLiveDeclaredTargets(definition LiveDefinition, appData string) error {
	targets, err := resolveWindowsLiveDeclaredTargets(definition, appData)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if err := wipeWindowsLiveDeclaredTarget(target); err != nil {
			return fmt.Errorf("wipe declared target %q: %w", target.identity, err)
		}
	}
	return nil
}

func wipeWindowsLiveDeclaredTarget(target liveResolvedDeclaredTarget) error {
	info, err := os.Lstat(target.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || safepath.IsLinkOrReparse(info) {
		return fmt.Errorf("declared target is unsafe")
	}
	switch target.kind {
	case LiveDeclaredTargetFile:
		if !info.Mode().IsRegular() {
			return fmt.Errorf("declared file target is not regular")
		}
		ctx, cancel := defaultWindowsLiveCleanupContext()
		defer cancel()
		return removeWindowsLiveExactLeaf(ctx, target.path, false)
	case LiveDeclaredTargetDirectory:
		if !info.IsDir() {
			return fmt.Errorf("declared directory target is not a directory")
		}
		return removeWindowsLiveDirectory(target.path)
	default:
		return fmt.Errorf("declared target kind is invalid")
	}
}

func removeWindowsLiveDirectory(path string) error {
	ctx, cancel := defaultWindowsLiveCleanupContext()
	defer cancel()
	return removeWindowsLiveDirectoryWithBudget(ctx, path, windowsLiveCleanupBudget{maxDepth: maxWindowsLiveCleanupDepth, maxEntries: maxWindowsLiveCleanupEntries})
}
