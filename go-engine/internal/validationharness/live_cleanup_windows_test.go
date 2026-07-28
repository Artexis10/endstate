// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"context"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveWindowsLiveDeclaredTargetsAndWipeExactLeaves(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	appData := t.TempDir()
	withWindowsLiveTestAppData(t, appData)
	config := filepath.Join(appData, "Notepad++", "config.xml")
	directory := filepath.Join(appData, "Notepad++", "userDefineLangs")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "language.xml"), []byte("seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	targets, err := resolveWindowsLiveDeclaredTargets(definition, appData)
	if err != nil || len(targets) != 6 {
		t.Fatalf("resolveWindowsLiveDeclaredTargets() = %+v, %v", targets, err)
	}
	if err := wipeWindowsLiveDeclaredTargets(definition, appData); err != nil {
		t.Fatalf("wipeWindowsLiveDeclaredTargets() error = %v", err)
	}
	for _, path := range []string{config, directory} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("target %q remains after wipe: %v", path, err)
		}
	}
}

func TestResolveWindowsLiveDeclaredTargetsRejectsReparseParent(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	appData, target := t.TempDir(), t.TempDir()
	junction := filepath.Join(appData, "Notepad++")
	if output, err := exec.Command("cmd", "/d", "/c", "mklink", "/J", junction, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink junction: %v: %s", err, output)
	}
	if _, err := resolveWindowsLiveDeclaredTargets(definition, appData); err == nil {
		t.Fatal("resolveWindowsLiveDeclaredTargets() accepted a reparse-backed target")
	}
}

func TestWindowsLiveAttemptRootCleanupIsBoundedAndPropagatesFailure(t *testing.T) {
	parent := t.TempDir()
	attempt, err := newWindowsLiveAttemptRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attempt.path, "receipt.json"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := attempt.Cleanup(); err != nil {
		t.Fatalf("Cleanup() error = %v", err)
	}
	if _, err := os.Lstat(attempt.path); !os.IsNotExist(err) {
		t.Fatalf("attempt root remains after cleanup: %v", err)
	}
	if err := (windowsLiveAttemptRoot{parent: parent, path: filepath.Join(parent, "foreign")}).Cleanup(); err == nil {
		t.Fatal("Cleanup() accepted a foreign attempt root")
	}
}

func TestWindowsLiveDeclaredTargetWipeRequiresBoundHostPermitAndSealsReceipt(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	appData := t.TempDir()
	withWindowsLiveTestAppData(t, appData)
	path := filepath.Join(appData, "Notepad++", "config.xml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	session := liveWindowsHostMutationSession(t, definition, 1, liveOperationDeclaredTargetWipe)
	session.definition.operations[2] = LiveCampaignOperation{Sequence: 2, Operation: string(liveOperationDeclaredTargetWipe)}
	issuer := session.NewReceiptIssuer()
	admission, err := issuer.admit(liveOperationDeclaredTargetWipe, 1, session.NonceFor(liveOperationDeclaredTargetWipe, 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runWindowsLiveDeclaredTargetWipe(admission, trustedLiveHostMutationPermit{}, definition, appData); err == nil {
		t.Fatal("wipe accepted a missing host permit")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("missing permit mutated target: %v", err)
	}
	session = liveWindowsHostMutationSession(t, definition, 1, liveOperationDeclaredTargetWipe)
	issuer = session.NewReceiptIssuer()
	admission, err = issuer.admit(liveOperationDeclaredTargetWipe, 1, session.NonceFor(liveOperationDeclaredTargetWipe, 1))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := windowsLiveHostMutationBinding(definition, appData, windowsLiveAttemptRoot{})
	if err != nil {
		t.Fatal(err)
	}
	permit, err := session.MintHostMutationPermit(admission, binding)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := runWindowsLiveDeclaredTargetWipe(admission, permit, definition, appData)
	if err != nil || receipt == nil || !receipt.sealed || !receipt.succeeded {
		t.Fatalf("bound wipe = %+v, %v", receipt, err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("bound wipe did not remove target: %v", err)
	}
}

func TestWindowsLiveDeclaredTargetWipeRejectsWrongRootAndReplayWithoutMutation(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	appData, foreignAppData := t.TempDir(), t.TempDir()
	withWindowsLiveTestAppData(t, appData)
	path := filepath.Join(appData, "Notepad++", "config.xml")
	foreignPath := filepath.Join(foreignAppData, "Notepad++", "config.xml")
	for _, candidate := range []string{path, foreignPath} {
		if err := os.MkdirAll(filepath.Dir(candidate), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(candidate, []byte("seed"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	session := liveWindowsHostMutationSession(t, definition, 1, liveOperationDeclaredTargetWipe)
	issuer := session.NewReceiptIssuer()
	admission, err := issuer.admit(liveOperationDeclaredTargetWipe, 1, session.NonceFor(liveOperationDeclaredTargetWipe, 1))
	if err != nil {
		t.Fatal(err)
	}
	binding, err := windowsLiveHostMutationBinding(definition, appData, windowsLiveAttemptRoot{})
	if err != nil {
		t.Fatal(err)
	}
	permit, err := session.MintHostMutationPermit(admission, binding)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runWindowsLiveDeclaredTargetWipe(admission, permit, definition, foreignAppData); err == nil {
		t.Fatal("wipe accepted a permit bound to a different APPDATA root")
	}
	if _, err := os.Lstat(foreignPath); err != nil {
		t.Fatalf("wrong-root permit mutated target: %v", err)
	}
	if _, err := issuer.admit(liveOperationDeclaredTargetWipe, 2, session.NonceFor(liveOperationDeclaredTargetWipe, 2)); err == nil {
		t.Fatal("wrong-root permit advanced receipt sequence")
	}

	session = liveWindowsHostMutationSession(t, definition, 1, liveOperationDeclaredTargetWipe)
	issuer = session.NewReceiptIssuer()
	admission, err = issuer.admit(liveOperationDeclaredTargetWipe, 1, session.NonceFor(liveOperationDeclaredTargetWipe, 1))
	if err != nil {
		t.Fatal(err)
	}
	binding, err = windowsLiveHostMutationBinding(definition, appData, windowsLiveAttemptRoot{})
	if err != nil {
		t.Fatal(err)
	}
	permit, err = session.MintHostMutationPermit(admission, binding)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runWindowsLiveDeclaredTargetWipe(admission, permit, definition, appData); err != nil {
		t.Fatalf("initial wipe failed: %v", err)
	}
	if err := os.WriteFile(path, []byte("recreated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runWindowsLiveDeclaredTargetWipe(admission, permit, definition, appData); err == nil {
		t.Fatal("wipe accepted a replayed permit")
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("replayed permit mutated target: %v", err)
	}
	if _, err := issuer.admit(liveOperationDeclaredTargetWipe, 3, liveReceiptTestNonce(72)); err == nil {
		t.Fatal("replayed permit advanced receipt sequence")
	}
}

func TestWindowsLiveHandleCleanupRejectsBudgetExhaustionWithoutTraversal(t *testing.T) {
	root := t.TempDir()
	deep := root
	for range 4 {
		deep = filepath.Join(deep, "nested")
		if err := os.Mkdir(deep, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeWindowsLiveDirectoryWithBudget(context.Background(), root, windowsLiveCleanupBudget{maxDepth: 2, maxEntries: 32}); err == nil {
		t.Fatal("handle cleanup accepted a depth-exhausting tree")
	}
	if _, err := os.Lstat(root); err != nil {
		t.Fatalf("budget failure removed root: %v", err)
	}
	deadline, cancel := context.WithCancel(context.Background())
	cancel()
	if err := removeWindowsLiveDirectoryWithBudget(deadline, root, windowsLiveCleanupBudget{maxDepth: 32, maxEntries: 32}); err == nil {
		t.Fatal("handle cleanup accepted an expired context")
	}
}

func liveWindowsHostMutationSession(t *testing.T, definition LiveDefinition, sequence uint64, operation liveOperation) *LiveAuthoritySession {
	t.Helper()
	digest, err := CanonicalLiveDefinitionSHA256(definition)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	campaign := sha256.Sum256([]byte("windows-host-mutation"))
	return &LiveAuthoritySession{campaignID: campaign, campaign: LiveCampaign{PhaseNonce: "windows-host-mutation", ExpiresAt: now.Add(time.Hour)}, now: now, minted: make(map[liveAuthorityPermitKey]struct{}), definition: liveAuthorityDefinition{
		definition: liveSHA256Bytes(digest), targets: liveSHA256Bytes(liveSHA256Hex(definition.DeclaredTargets)), observer: liveSHA256Bytes(liveSHA256Hex(definition.Observer)), workflow: sha256.Sum256([]byte("workflow")), operations: map[uint64]LiveCampaignOperation{sequence: {Sequence: sequence, Operation: string(operation)}},
	}}
}

func withWindowsLiveTestAppData(t *testing.T, root string) {
	t.Helper()
	original := windowsLiveRoamingAppData
	windowsLiveRoamingAppData = func() (string, error) { return root, nil }
	t.Cleanup(func() { windowsLiveRoamingAppData = original })
	t.Setenv("APPDATA", root)
}

func TestResolveWindowsLiveDeclaredTargetsRejectsForeignAndOverlappingTemplates(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*LiveDefinition){
		func(value *LiveDefinition) { value.DeclaredTargets[0].Template = `%LOCALAPPDATA%\Notepad++\config.xml` },
		func(value *LiveDefinition) {
			value.DeclaredTargets[0].Template = `%APPDATA%\Notepad++\userDefineLangs\config.xml`
		},
	} {
		candidate := definition
		candidate.DeclaredTargets = append([]LiveDeclaredTarget(nil), definition.DeclaredTargets...)
		mutate(&candidate)
		if _, err := resolveWindowsLiveDeclaredTargets(candidate, t.TempDir()); err == nil {
			t.Fatal("resolveWindowsLiveDeclaredTargets() accepted an unsafe template")
		}
	}
}
