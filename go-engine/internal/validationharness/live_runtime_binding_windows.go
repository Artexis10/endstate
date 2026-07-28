// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

type liveTrustedPowerShellBinding struct {
	path   string
	sha256 [32]byte
}

// bindWindowsLiveOperationTemplate is deliberately a closed resolver. Public
// campaign text carries semantic placeholders; it never carries runner paths.
func bindWindowsLiveOperationTemplate(template LiveCampaignOperation, attemptRoot, checkoutRoot string, winget liveTrustedAppXBinding, powershell liveTrustedPowerShellBinding, environment map[string]string) (LiveCampaignOperation, error) {
	if filepath.IsAbs(attemptRoot) == false || filepath.IsAbs(checkoutRoot) == false || !validLiveCampaignOperationTemplate(template, "Notepad++.Notepad++", stringsFromSHA256(template.ExecutableSHA256), stringsFromSHA256(template.ExecutableSHA256)) {
		return LiveCampaignOperation{}, fmt.Errorf("live operation template is invalid")
	}
	return bindWindowsLiveOperationTemplateUnchecked(template, filepath.Clean(attemptRoot), filepath.Clean(checkoutRoot), winget, powershell, environment)
}

func bindWindowsLiveOperationTemplateUnchecked(template LiveCampaignOperation, attemptRoot, checkoutRoot string, winget liveTrustedAppXBinding, powershell liveTrustedPowerShellBinding, environment map[string]string) (LiveCampaignOperation, error) {
	bound := LiveCampaignOperation{Sequence: template.Sequence, Operation: template.Operation, Environment: cloneLiveEnvironment(environment)}
	kind := liveOperation(template.Operation)
	if kind == liveOperationDeclaredTargetWipe || kind == liveOperationAttemptRootCleanup {
		return bound, nil
	}
	if kind == liveOperationWingetExactUninstall {
		if !winget.metadata.receipt.valid {
			return LiveCampaignOperation{}, fmt.Errorf("trusted winget binding is invalid")
		}
		bound.Executable = filepath.Join(winget.metadata.packageRoot, winget.metadata.executableName)
		bound.ExecutableSHA256 = fmt.Sprintf("%x", winget.metadata.receipt.sha256)
		bound.Arguments = append([]string(nil), template.Arguments...)
		return bound, nil
	}
	if kind == liveOperationHashBoundSeed {
		if powershell.path == "" || powershell.sha256 == ([32]byte{}) {
			return LiveCampaignOperation{}, fmt.Errorf("trusted powershell binding is invalid")
		}
		bound.Executable = powershell.path
		bound.ExecutableSHA256 = hex.EncodeToString(powershell.sha256[:])
		bound.Directory = filepath.Join(attemptRoot, "seed")
		bound.Arguments = []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(attemptRoot, "seed", "seed.ps1")}
		bound.Environment = liveSeedRuntimeEnvironment(environment)
		return bound, nil
	}
	bound.Directory = filepath.Join(checkoutRoot, "go-engine")
	if template.Environment["ENDSTATE_ROOT"] != liveTemplateEndstateRoot || len(template.Environment) != 1 {
		return LiveCampaignOperation{}, fmt.Errorf("live operation root authority is invalid")
	}
	bound.Environment["ENDSTATE_ROOT"] = attemptRoot
	bound.ExecutableSHA256 = template.ExecutableSHA256
	switch kind {
	case liveOperationEngineApply:
		bound.Executable, bound.Arguments = filepath.Join(checkoutRoot, "go-engine", "endstate.exe"), []string{"apply", "--manifest", filepath.Join(attemptRoot, "manifests", "install.jsonc"), "--events", "jsonl", "--json"}
	case liveOperationEngineVerify:
		bound.Executable, bound.Arguments = filepath.Join(checkoutRoot, "go-engine", "endstate.exe"), []string{"verify", "--manifest", filepath.Join(attemptRoot, "manifests", "install.jsonc"), "--events", "jsonl", "--json"}
	case liveOperationEngineCapture:
		bound.Executable, bound.Arguments = filepath.Join(checkoutRoot, "go-engine", "endstate.exe"), []string{"capture", "--only", "notepad++-notepad++,apps.notepad-plus-plus", "--out", filepath.Join(attemptRoot, "capture.zip"), "--events", "jsonl", "--json"}
	case liveOperationEngineRebuild:
		bound.Executable, bound.Arguments = filepath.Join(checkoutRoot, "go-engine", "endstate.exe"), []string{"rebuild", "--from", filepath.Join(attemptRoot, "capture.zip"), "--confirm", "--events", "jsonl", "--json"}
	case liveOperationEngineRevert:
		bound.Executable, bound.Arguments = filepath.Join(checkoutRoot, "go-engine", "endstate.exe"), []string{"revert", "--events", "jsonl", "--json"}
	case liveOperationHashBoundSeed:
		bound.Executable, bound.Arguments = filepath.Join(checkoutRoot, "go-engine", "internal", "validationharness", "testdata", "hosted-live-seed.exe"), []string{"seed"}
	default:
		return LiveCampaignOperation{}, fmt.Errorf("live operation template is invalid")
	}
	return bound, nil
}

// bindWindowsLiveRuntime makes a session usable exactly once. The typed root
// and AppX binding are retained by the caller; permits receive only a frozen
// concrete invocation copied from this map.
var windowsLiveRuntimeEnvironment = trustedWindowsLiveRuntimeEnvironment

func bindWindowsLiveRuntime(session *LiveAuthoritySession, definition LiveDefinition, attempt windowsLiveAttemptRoot, checkoutRoot string, winget liveTrustedWingetTarget) error {
	definitionDigest, err := CanonicalLiveDefinitionSHA256(definition)
	if session == nil || definitionDigest == "" || session.definition.definition != liveSHA256Bytes(definitionDigest) || !attempt.valid() || checkoutRoot == "" || !winget.binding.metadata.receipt.valid {
		return fmt.Errorf("live runtime binding is invalid")
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if !session.runtimeBindingRequired || session.runtimeBound || session.issuerID != 0 || len(session.minted) != 0 || session.cleanup || filepath.IsAbs(checkoutRoot) == false {
		return fmt.Errorf("live runtime binding is unavailable")
	}
	environment, err := windowsLiveRuntimeEnvironment(attempt.path)
	if err != nil {
		return err
	}
	enginePath := filepath.Join(checkoutRoot, "go-engine", "endstate.exe")
	engineDigest, err := liveWindowsFileSHA256(enginePath)
	if err != nil || fmt.Sprintf("%x", engineDigest) != session.campaign.EngineSHA256 {
		return fmt.Errorf("live engine runtime binding is invalid")
	}
	powershell, err := trustedWindowsLivePowerShell()
	if err != nil {
		return err
	}
	if err := stageWindowsLiveDefinitionInputs(definition, attempt); err != nil {
		return err
	}
	if err := stageWindowsLiveSeed(checkoutRoot, attempt, session.definition.seed); err != nil {
		return err
	}
	concrete := make(map[uint64]LiveCampaignOperation, len(session.definition.templates))
	for sequence, template := range session.definition.templates {
		bound, err := bindWindowsLiveOperationTemplateUnchecked(template, attempt.path, filepath.Clean(checkoutRoot), winget.binding, powershell, environment)
		if err != nil {
			return err
		}
		concrete[sequence] = bound
	}
	session.definition.operations = concrete
	session.runtimeBound = true
	return nil
}

func stageWindowsLiveDefinitionInputs(definition LiveDefinition, attempt windowsLiveAttemptRoot) error {
	module, ok := definition.productionModule()
	if !ok || definition.ModuleID != "apps.notepad-plus-plus" || definition.WingetRef != "Notepad++.Notepad++" || !attempt.valid() {
		return fmt.Errorf("stage live definition inputs: definition is invalid")
	}
	modulePath := filepath.Join(attempt.path, "modules", "apps", "notepad-plus-plus")
	if err := os.MkdirAll(modulePath, 0o700); err != nil {
		return fmt.Errorf("stage live definition inputs: %w", err)
	}
	moduleRaw := module.CanonicalSnapshot()
	parsed, err := modules.ParseModuleJSON(moduleRaw)
	if err != nil || parsed.ID != definition.ModuleID || parsed.Revision != definition.ModuleRevision {
		return fmt.Errorf("stage live definition inputs: module projection changed")
	}
	if err := safepath.AtomicWriteFile(filepath.Join(modulePath, "module.jsonc"), moduleRaw, 0o600); err != nil {
		return fmt.Errorf("stage live definition inputs: %w", err)
	}
	staged, err := os.ReadFile(filepath.Join(modulePath, "module.jsonc"))
	if err != nil || string(staged) != string(moduleRaw) {
		return fmt.Errorf("stage live definition inputs: module write changed")
	}
	verify := make([]manifest.VerifyEntry, len(module.Verify))
	for index, entry := range module.Verify {
		verify[index] = manifest.VerifyEntry{Type: entry.Type, Command: entry.Command, Path: entry.Path, ValueName: entry.ValueName, ValueType: entry.ValueType, Data: entry.Data}
	}
	restore := make([]manifest.RestoreEntry, len(module.Restore))
	for index, entry := range module.Restore {
		restore[index] = manifest.RestoreEntry{Type: entry.Type, Source: entry.Source, Target: entry.Target, Pattern: entry.Pattern, Reason: entry.Reason, Backup: entry.Backup, Optional: entry.Optional, Exclude: append([]string(nil), entry.Exclude...), FromModule: definition.ModuleID, Key: entry.Key, ValueName: entry.ValueName, ValueType: entry.ValueType, Data: entry.Data}
	}
	manifestValue := manifest.Manifest{Version: 1, Name: "hosted-live-notepad", Apps: []manifest.App{{ID: "notepad++-notepad++", Refs: map[string]string{"windows": definition.WingetRef}, Driver: "winget", Source: "winget", DisplayName: module.DisplayName}}, ConfigModules: []string{definition.ModuleID}, Restore: restore, Verify: verify}
	raw, err := json.Marshal(manifestValue)
	if err != nil {
		return fmt.Errorf("stage live definition inputs: manifest is invalid")
	}
	manifestPath := filepath.Join(attempt.path, "manifests", "install.jsonc")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o700); err != nil {
		return fmt.Errorf("stage live definition inputs: %w", err)
	}
	if err := safepath.AtomicWriteFile(manifestPath, raw, 0o600); err != nil {
		return fmt.Errorf("stage live definition inputs: %w", err)
	}
	stagedManifest, err := manifest.LoadManifest(manifestPath)
	if err != nil || len(stagedManifest.Apps) != 1 || stagedManifest.Apps[0].ID != "notepad++-notepad++" || stagedManifest.Apps[0].Refs["windows"] != definition.WingetRef || len(stagedManifest.ConfigModules) != 1 || stagedManifest.ConfigModules[0] != definition.ModuleID || len(stagedManifest.Restore) != len(restore) || len(stagedManifest.Verify) != len(verify) {
		return fmt.Errorf("stage live definition inputs: manifest projection changed")
	}
	return nil
}

func trustedWindowsLivePowerShell() (liveTrustedPowerShellBinding, error) {
	root := os.Getenv("SYSTEMROOT")
	if root == "" {
		root = os.Getenv("WINDIR")
	}
	path := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	digest, err := liveWindowsFileSHA256(path)
	if root == "" || err != nil {
		return liveTrustedPowerShellBinding{}, fmt.Errorf("trusted powershell binding is unavailable")
	}
	return liveTrustedPowerShellBinding{path: path, sha256: digest}, nil
}

func liveSeedRuntimeEnvironment(environment map[string]string) map[string]string {
	seed := make(map[string]string)
	for _, key := range []string{"APPDATA", "SYSTEMROOT", "WINDIR", "TEMP", "TMP"} {
		if value := environment[key]; value != "" {
			seed[key] = value
		}
	}
	return seed
}

func stageWindowsLiveSeed(checkoutRoot string, attempt windowsLiveAttemptRoot, want [32]byte) error {
	catalog, err := validationmatrix.LoadCatalog(checkoutRoot, sessionNowUTC())
	if err != nil {
		return fmt.Errorf("live seed source is unavailable")
	}
	record, ok := catalog.Records["apps.notepad-plus-plus"]
	if !ok {
		return fmt.Errorf("live seed source is unavailable")
	}
	seed, err := validationmatrix.ReadHashBoundSeed(record)
	if err != nil || sha256.Sum256(seed) != want {
		return fmt.Errorf("live seed source is invalid")
	}
	directory := filepath.Join(attempt.path, "seed")
	if err := os.Mkdir(directory, 0o700); err != nil {
		return fmt.Errorf("stage live seed: %w", err)
	}
	path, err := safepath.Resolve(directory, "seed.ps1")
	if err != nil {
		return fmt.Errorf("stage live seed: %w", err)
	}
	if err := safepath.AtomicWriteFile(path, seed, 0o600); err != nil {
		return fmt.Errorf("stage live seed: %w", err)
	}
	staged, _, err := safepath.ReadRegularFileBounded(path, int64(len(seed)+1))
	if err != nil || sha256.Sum256(staged) != want {
		return fmt.Errorf("stage live seed: staged bytes changed")
	}
	return nil
}

func sessionNowUTC() time.Time { return time.Now().UTC() }

func trustedWindowsLiveRuntimeEnvironment(attemptRoot string) (map[string]string, error) {
	appData, err := windowsLiveRoamingAppData()
	if err != nil {
		return nil, fmt.Errorf("live runtime environment is unavailable")
	}
	environment := make(map[string]string)
	for key := range liveProcessEnvironmentAllowlist {
		if value := os.Getenv(key); value != "" {
			if !validLiveProcessEnvironmentValue(value) {
				return nil, fmt.Errorf("live runtime environment is invalid")
			}
			environment[key] = value
		}
	}
	if !validLiveProcessEnvironmentValue(appData) || !validLiveProcessEnvironmentValue(attemptRoot) {
		return nil, fmt.Errorf("live runtime environment is invalid")
	}
	environment["APPDATA"] = appData
	environment["ENDSTATE_ROOT"] = attemptRoot
	return environment, nil
}

// stringsFromSHA256 retains the template validator's exact SHA shape without
// broadening the public authority surface; the unchecked resolver is used by
// a session only after campaign validation.
func stringsFromSHA256(value string) string { return value }
