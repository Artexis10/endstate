// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package restore

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/Artexis10/endstate/go-engine/internal/registryfile"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	durableRegistryImportFile = func(path string) error {
		if err := exec.Command("reg", "import", path).Run(); err != nil {
			return fmt.Errorf("cannot import registry file %s: %w", path, err)
		}
		return nil
	}
	durableRegistryKeyExistsNative = func(target string) (bool, error) {
		hive, subkey, err := splitHKCUKey(target)
		if err != nil {
			return false, err
		}
		key, err := registry.OpenKey(hive, subkey, registry.QUERY_VALUE|registry.ENUMERATE_SUB_KEYS)
		if err == registry.ErrNotExist {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		_ = key.Close()
		return true, nil
	}
	durableRegistryExportNative = func(target, path string) error {
		return exec.Command("reg", "export", target, path, "/y").Run()
	}
	durableRegistryDeleteNative = func(target string) error {
		if err := exec.Command("reg", "delete", target, "/f").Run(); err != nil {
			exists, queryErr := durableRegistryKeyExistsNative(target)
			if queryErr != nil {
				return fmt.Errorf("verify registry key deletion %s: %w", target, queryErr)
			}
			if exists {
				return fmt.Errorf("cannot delete registry key %s: %w", target, err)
			}
		}
		return nil
	}
	durableRegistryRenameNative = renameDurableRegistryKeyNative
)

func durableRegistryNativeKey(semantic string, boundary legacyValidationBoundary) (string, error) {
	if boundary.context == nil {
		if err := ValidateRegistryTarget(semantic); err != nil {
			return "", err
		}
		return semantic, nil
	}
	canonical, err := validationmode.NormalizeHKCU(semantic)
	if err != nil {
		return "", err
	}
	return boundary.context.MapHKCU(canonical)
}

var regRenameKeyProc = windows.NewLazySystemDLL("advapi32.dll").NewProc("RegRenameKey")

func durableLegacyRegistryStates(entry JournalEntry, workRoot string, boundary legacyValidationBoundary) (durableLegacyRevertState, durableLegacyRevertState, error) {
	switch entry.RestoreType {
	case "registry-set":
		backupPath, err := boundary.resolveBackup(entry.BackupPath)
		if err != nil {
			return durableLegacyRevertState{}, durableLegacyRevertState{}, err
		}
		backup, err := readRegistrySetBackup(backupPath)
		if err != nil {
			return durableLegacyRevertState{}, durableLegacyRevertState{}, err
		}
		nativeKey, err := durableRegistryNativeKey(backup.Key, boundary)
		if err != nil {
			return durableLegacyRevertState{}, durableLegacyRevertState{}, err
		}
		if boundary.context != nil && !strings.EqualFold(entry.TargetPath, registrySetTarget(RestoreAction{Key: backup.Key, ValueName: backup.ValueName})) {
			return durableLegacyRevertState{}, durableLegacyRevertState{}, fmt.Errorf("registry-set journal target differs from semantic backup identity")
		}
		existed, valueType, data, _ := registrySetReadNative(nativeKey, backup.ValueName)
		before := digestDurableRegistryValue(backup.Key, backup.ValueName, existed, valueType, data)
		desired := digestDurableRegistryValue(
			backup.Key, backup.ValueName, backup.Existed, backup.PriorType, backup.PriorData,
		)
		return before, desired, nil
	case "registry-import":
		before, err := captureDurableRegistryKey(entry.TargetPath, workRoot, boundary)
		if err != nil {
			return durableLegacyRevertState{}, durableLegacyRevertState{}, err
		}
		if entry.BackupCreated && entry.BackupPath != "" {
			backupPath, resolveErr := boundary.resolveBackup(entry.BackupPath)
			if resolveErr != nil {
				return durableLegacyRevertState{}, durableLegacyRevertState{}, resolveErr
			}
			data, _, err := safepath.ReadRegularFile(backupPath)
			if err != nil {
				return durableLegacyRevertState{}, durableLegacyRevertState{}, err
			}
			if boundary.context != nil {
				semanticKey, normalizeErr := validationmode.NormalizeHKCU(entry.TargetPath)
				if normalizeErr != nil {
					return durableLegacyRevertState{}, durableLegacyRevertState{}, normalizeErr
				}
				data, err = registryfile.RewriteSubtree(data, semanticKey, semanticKey)
				if err != nil {
					return durableLegacyRevertState{}, durableLegacyRevertState{}, err
				}
			}
			return before, digestDurableRegistryExport(data), nil
		}
		return before, absentDurableRegistryState("registry-key"), nil
	default:
		return durableLegacyRevertState{}, durableLegacyRevertState{}, fmt.Errorf("unsupported durable registry revert type %q", entry.RestoreType)
	}
}

func applyDurableLegacyRegistryRevert(entry JournalEntry, index int, boundary legacyValidationBoundary) error {
	_ = index
	switch entry.RestoreType {
	case "registry-set":
		backupPath, err := boundary.resolveBackup(entry.BackupPath)
		if err != nil {
			return err
		}
		backup, err := readRegistrySetBackup(backupPath)
		if err != nil {
			return err
		}
		if boundary.context == nil {
			return revertRegistrySet(backup)
		}
		if !strings.EqualFold(entry.TargetPath, registrySetTarget(RestoreAction{Key: backup.Key, ValueName: backup.ValueName})) {
			return fmt.Errorf("registry-set journal target differs from semantic backup identity")
		}
		nativeKey, err := durableRegistryNativeKey(backup.Key, boundary)
		if err != nil {
			return err
		}
		if !backup.Existed {
			return registrySetDeleteNative(nativeKey, backup.ValueName)
		}
		if backup.PriorType == "" {
			return fmt.Errorf("registry-set revert prior value had an unsupported type; left in place")
		}
		return registrySetWriteNative(nativeKey, backup.ValueName, backup.PriorType, backup.PriorData)
	case "registry-import":
		return deleteDurableLegacyRegistryKey(entry.TargetPath, boundary)
	default:
		return fmt.Errorf("unsupported durable registry revert type %q", entry.RestoreType)
	}
}

func durableLegacyRegistryScratchTargets(entry JournalEntry, entryDigest string, boundary legacyValidationBoundary) (string, string, error) {
	if entry.RestoreType != "registry-import" || !entry.BackupCreated || entry.BackupPath == "" {
		return "", "", nil
	}
	semanticTarget := entry.TargetPath
	if boundary.context != nil {
		canonical, err := validationmode.NormalizeHKCU(entry.TargetPath)
		if err != nil {
			return "", "", err
		}
		semanticTarget = canonical
	}
	_, subkey, err := splitHKCUKey(semanticTarget)
	if err != nil {
		return "", "", err
	}
	separator := strings.LastIndex(subkey, `\`)
	parent, name := "", subkey
	if separator >= 0 {
		parent, name = subkey[:separator], subkey[separator+1:]
	}
	if name == "" {
		return "", "", fmt.Errorf("registry import target must name a subkey")
	}
	prefix := `HKCU\`
	if parent != "" {
		prefix += parent + `\`
	}
	suffix := entryDigest[:16]
	return prefix + "." + name + ".endstate-revert-" + suffix + "-stage",
		prefix + "." + name + ".endstate-revert-" + suffix + "-held", nil
}

func validateDurableLegacyRegistryScratchAvailable(stage, held, workRoot string, boundary legacyValidationBoundary) error {
	for _, target := range []string{stage, held} {
		if target == "" {
			continue
		}
		state, err := captureDurableRegistryKey(target, workRoot, boundary)
		if err != nil {
			return err
		}
		if state.Kind != "absent" {
			return fmt.Errorf("legacy registry revert scratch key %q already exists", target)
		}
	}
	return nil
}

func applyDurableLegacyRegistryImportSwap(
	entry JournalEntry, prepared durableLegacyRevertPrepared, index int, workRoot string, boundary legacyValidationBoundary,
) error {
	targetState, err := captureDurableRegistryKey(entry.TargetPath, workRoot, boundary)
	if err != nil {
		return err
	}
	stageState, err := captureDurableRegistryKeyAs(prepared.StagePath, entry.TargetPath, workRoot, boundary)
	if err != nil {
		return err
	}
	heldState, err := captureDurableRegistryKeyAs(prepared.HeldPath, entry.TargetPath, workRoot, boundary)
	if err != nil {
		return err
	}

	if targetState == prepared.Desired {
		if stageState.Kind != "absent" {
			if stageState != prepared.Desired {
				return fmt.Errorf("legacy registry revert stage changed after target replacement")
			}
			if err := deleteDurableLegacyRegistryKey(prepared.StagePath, boundary); err != nil {
				return err
			}
		}
		if heldState.Kind != "absent" {
			if heldState != prepared.Before {
				return fmt.Errorf("legacy registry revert held key changed after target replacement")
			}
			if err := deleteDurableLegacyRegistryKey(prepared.HeldPath, boundary); err != nil {
				return err
			}
		}
		return nil
	}

	heldExists := heldState.Kind != "absent"
	if targetState != prepared.Before {
		if targetState.Kind != "absent" || !heldExists || heldState != prepared.Before {
			return fmt.Errorf("legacy registry revert target %q changed after its durable before-state was recorded", entry.TargetPath)
		}
	}
	if heldExists && heldState != prepared.Before {
		return fmt.Errorf("legacy registry revert held key differs from recorded before-state")
	}
	if stageState.Kind != "absent" && stageState != prepared.Desired {
		if targetState != prepared.Before || heldExists {
			return fmt.Errorf("legacy registry revert stage differs from recorded desired state")
		}
		if err := deleteDurableLegacyRegistryKey(prepared.StagePath, boundary); err != nil {
			return err
		}
		stageState = absentDurableRegistryState("registry-key")
	}
	if stageState.Kind == "absent" {
		if err := stageDurableLegacyRegistryImport(entry, prepared, workRoot, boundary); err != nil {
			return err
		}
		stageState, err = captureDurableRegistryKeyAs(prepared.StagePath, entry.TargetPath, workRoot, boundary)
		if err != nil {
			return err
		}
	}
	if stageState != prepared.Desired {
		return fmt.Errorf("legacy registry revert stage differs from recorded desired state")
	}
	if !heldExists && targetState.Kind != "absent" {
		if err := renameDurableRegistryKey(entry.TargetPath, prepared.HeldPath, boundary); err != nil {
			return err
		}
		if err := durableRevertCheckpoint("after_registry_target_held", index); err != nil {
			return err
		}
		heldExists = true
	}
	targetState, err = captureDurableRegistryKey(entry.TargetPath, workRoot, boundary)
	if err != nil {
		return err
	}
	if targetState.Kind == "absent" {
		if err := renameDurableRegistryKey(prepared.StagePath, entry.TargetPath, boundary); err != nil {
			return err
		}
	}
	actual, err := captureDurableRegistryKey(entry.TargetPath, workRoot, boundary)
	if err != nil {
		return err
	}
	if actual != prepared.Desired {
		return fmt.Errorf("legacy registry revert target does not match staged desired state")
	}
	if heldExists {
		if err := deleteDurableLegacyRegistryKey(prepared.HeldPath, boundary); err != nil {
			return err
		}
	}
	return nil
}

func stageDurableLegacyRegistryImport(entry JournalEntry, prepared durableLegacyRevertPrepared, workRoot string, boundary legacyValidationBoundary) error {
	backupPath, err := boundary.resolveBackup(entry.BackupPath)
	if err != nil {
		return err
	}
	data, _, err := safepath.ReadRegularFile(backupPath)
	if err != nil {
		return err
	}
	var rewritten []byte
	stageIdentity := prepared.StagePath
	if boundary.context != nil {
		stageIdentity, err = durableRegistryNativeKey(prepared.StagePath, boundary)
		if err != nil {
			return err
		}
		rewritten, err = registryfile.RewriteSubtree(data, entry.TargetPath, stageIdentity)
	} else {
		rewritten, err = rewriteDurableRegistryExport(data, entry.TargetPath, prepared.StagePath)
	}
	if err != nil {
		return err
	}
	path := filepath.Join(workRoot, fmt.Sprintf("entry-%06d-registry-stage.reg", prepared.EntryIndex))
	if err := safepath.AtomicWriteFile(path, rewritten, 0o600); err != nil {
		return err
	}
	defer os.Remove(path)
	if err := durableRegistryImportFile(path); err != nil {
		return errors.Join(err, deleteDurableLegacyRegistryKey(prepared.StagePath, boundary))
	}
	state, err := captureDurableRegistryKeyAs(prepared.StagePath, entry.TargetPath, workRoot, boundary)
	if err != nil {
		return err
	}
	if state != prepared.Desired {
		_ = deleteDurableLegacyRegistryKey(prepared.StagePath, boundary)
		return fmt.Errorf("staged registry import does not match recorded desired state")
	}
	return nil
}

func captureDurableRegistryKeyAs(actual, semantic, workRoot string, boundary legacyValidationBoundary) (durableLegacyRevertState, error) {
	state, data, err := exportDurableRegistryKey(actual, workRoot, boundary)
	if err != nil || state.Kind == "absent" {
		return state, err
	}
	var rewritten []byte
	if boundary.context != nil {
		rewritten, err = registryfile.RewriteSubtree(data, actual, semantic)
	} else {
		rewritten, err = rewriteDurableRegistryExport(data, actual, semantic)
	}
	if err != nil {
		return durableLegacyRevertState{}, err
	}
	return digestDurableRegistryExport(rewritten), nil
}

func rewriteDurableRegistryExport(data []byte, from, to string) ([]byte, error) {
	content, err := decodeRegistryImport(data)
	if err != nil {
		return nil, err
	}
	canonicalFrom, err := canonicalDurableRegistryKey(from)
	if err != nil {
		return nil, err
	}
	canonicalTo, err := canonicalDurableRegistryKey(to)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "[") || !strings.HasSuffix(trimmed, "]") {
			continue
		}
		key := strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")
		deleting := strings.HasPrefix(key, "-")
		key = strings.TrimPrefix(key, "-")
		if !strings.EqualFold(key, canonicalFrom) && !strings.HasPrefix(strings.ToLower(key), strings.ToLower(canonicalFrom)+`\`) {
			return nil, fmt.Errorf("registry export key %q is outside declared target %q", key, from)
		}
		replaced := canonicalTo + key[len(canonicalFrom):]
		if deleting {
			replaced = "-" + replaced
		}
		lines[index] = "[" + replaced + "]"
	}
	runes := utf16.Encode([]rune(strings.Join(lines, "\r\n")))
	encoded := make([]byte, 2+len(runes)*2)
	encoded[0], encoded[1] = 0xff, 0xfe
	for index, value := range runes {
		binary.LittleEndian.PutUint16(encoded[2+index*2:], value)
	}
	return encoded, nil
}

func canonicalDurableRegistryKey(target string) (string, error) {
	_, subkey, err := splitHKCUKey(target)
	if err != nil {
		return "", err
	}
	return `HKEY_CURRENT_USER\` + subkey, nil
}

func renameDurableRegistryKey(source, destination string, boundary legacyValidationBoundary) error {
	nativeSource, err := durableRegistryNativeKey(source, boundary)
	if err != nil {
		return err
	}
	nativeDestination, err := durableRegistryNativeKey(destination, boundary)
	if err != nil {
		return err
	}
	return durableRegistryRenameNative(nativeSource, nativeDestination)
}

func renameDurableRegistryKeyNative(source, destination string) error {
	_, sourceSubkey, err := splitHKCUKey(source)
	if err != nil {
		return err
	}
	_, destinationSubkey, err := splitHKCUKey(destination)
	if err != nil {
		return err
	}
	sourceSeparator := strings.LastIndex(sourceSubkey, `\`)
	destinationSeparator := strings.LastIndex(destinationSubkey, `\`)
	if sourceSeparator < 0 || destinationSeparator < 0 ||
		!strings.EqualFold(sourceSubkey[:sourceSeparator], destinationSubkey[:destinationSeparator]) {
		return fmt.Errorf("registry scratch rename must remain beneath one parent key")
	}
	parent, err := registry.OpenKey(registry.CURRENT_USER, sourceSubkey[:sourceSeparator], registry.ALL_ACCESS)
	if err != nil {
		return err
	}
	defer parent.Close()
	from, err := windows.UTF16PtrFromString(sourceSubkey[sourceSeparator+1:])
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destinationSubkey[destinationSeparator+1:])
	if err != nil {
		return err
	}
	result, _, _ := regRenameKeyProc.Call(
		uintptr(parent), uintptr(unsafe.Pointer(from)), uintptr(unsafe.Pointer(to)),
	)
	if result != 0 {
		return fmt.Errorf("rename registry key %s to %s: %w", source, destination, syscall.Errno(result))
	}
	return nil
}

func deleteDurableLegacyRegistryKey(target string, boundary legacyValidationBoundary) error {
	nativeTarget, err := durableRegistryNativeKey(target, boundary)
	if err != nil {
		return err
	}
	return durableRegistryDeleteNative(nativeTarget)
}

func captureDurableRegistryKey(target, workRoot string, boundary legacyValidationBoundary) (durableLegacyRevertState, error) {
	state, data, err := exportDurableRegistryKey(target, workRoot, boundary)
	if err != nil || state.Kind == "absent" {
		return state, err
	}
	return digestDurableRegistryExport(data), nil
}

func exportDurableRegistryKey(target, workRoot string, boundary legacyValidationBoundary) (durableLegacyRevertState, []byte, error) {
	nativeTarget, err := durableRegistryNativeKey(target, boundary)
	if err != nil {
		return durableLegacyRevertState{}, nil, err
	}
	exists, err := durableRegistryKeyExistsNative(nativeTarget)
	if err != nil {
		return durableLegacyRevertState{}, nil, fmt.Errorf("inspect registry key %s: %w", target, err)
	}
	if !exists {
		return absentDurableRegistryState("registry-key"), nil, nil
	}
	temporary, err := os.CreateTemp(workRoot, ".registry-state-*.reg")
	if err != nil {
		return durableLegacyRevertState{}, nil, err
	}
	path := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(path)
		return durableLegacyRevertState{}, nil, err
	}
	defer os.Remove(path)
	if boundary.context != nil {
		if err := boundary.context.ValidateSandboxPath(filepath.Clean(path)); err != nil {
			return durableLegacyRevertState{}, nil, err
		}
	}
	if err := durableRegistryExportNative(nativeTarget, path); err != nil {
		return durableLegacyRevertState{}, nil, fmt.Errorf("capture registry key %s: %w", target, err)
	}
	data, _, err := safepath.ReadRegularFile(filepath.Clean(path))
	if err != nil {
		return durableLegacyRevertState{}, nil, err
	}
	if boundary.context != nil {
		semanticTarget, normalizeErr := validationmode.NormalizeHKCU(target)
		if normalizeErr != nil {
			return durableLegacyRevertState{}, nil, normalizeErr
		}
		data, err = registryfile.RewriteSubtree(data, nativeTarget, semanticTarget)
		if err != nil {
			return durableLegacyRevertState{}, nil, err
		}
	}
	return durableLegacyRevertState{Kind: "registry-key"}, data, nil
}

func digestDurableRegistryValue(key, name string, existed bool, valueType, data string) durableLegacyRevertState {
	if !existed {
		return absentDurableRegistryState("registry-value")
	}
	payload := strings.Join([]string{
		"endstate-legacy-revert-registry-value-v1", strings.ToLower(key), strings.ToLower(name),
		strings.ToUpper(valueType), data,
	}, "\x00")
	sum := sha256.Sum256([]byte(payload))
	return durableLegacyRevertState{Kind: "registry-value", Digest: hex.EncodeToString(sum[:])}
}

func digestDurableRegistryExport(data []byte) durableLegacyRevertState {
	content, err := decodeRegistryImport(data)
	if err != nil {
		sum := sha256.Sum256(data)
		return durableLegacyRevertState{Kind: "registry-key", Digest: hex.EncodeToString(sum[:])}
	}
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	records := make([]string, 0, len(lines))
	section := ""
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(strings.ToLower(line), "windows registry editor") {
			continue
		}
		line = strings.ReplaceAll(line, "HKEY_CURRENT_USER", "HKCU")
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(line)
			records = append(records, "key\x00"+section)
			continue
		}
		records = append(records, "value\x00"+section+"\x00"+line)
	}
	sort.Strings(records)
	sum := sha256.Sum256([]byte(strings.Join(records, "\n")))
	return durableLegacyRevertState{Kind: "registry-key", Digest: hex.EncodeToString(sum[:])}
}

func absentDurableRegistryState(kind string) durableLegacyRevertState {
	sum := sha256.Sum256([]byte("endstate-legacy-revert-" + kind + "-v1:absent"))
	return durableLegacyRevertState{Kind: "absent", Digest: hex.EncodeToString(sum[:])}
}
