// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package restore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var durableRegistryImportFile = func(path string) error {
	if err := exec.Command("reg", "import", path).Run(); err != nil {
		return fmt.Errorf("cannot import registry file %s: %w", path, err)
	}
	return nil
}

var regRenameKeyProc = windows.NewLazySystemDLL("advapi32.dll").NewProc("RegRenameKey")

func durableLegacyRegistryStates(entry JournalEntry, workRoot string) (durableLegacyRevertState, durableLegacyRevertState, error) {
	switch entry.RestoreType {
	case "registry-set":
		backup, err := readRegistrySetBackup(entry.BackupPath)
		if err != nil {
			return durableLegacyRevertState{}, durableLegacyRevertState{}, err
		}
		hive, subkey, err := splitHKCUKey(backup.Key)
		if err != nil {
			return durableLegacyRevertState{}, durableLegacyRevertState{}, err
		}
		existed, valueType, data, _ := readRegistryValue(hive, subkey, backup.ValueName)
		before := digestDurableRegistryValue(backup.Key, backup.ValueName, existed, valueType, data)
		desired := digestDurableRegistryValue(
			backup.Key, backup.ValueName, backup.Existed, backup.PriorType, backup.PriorData,
		)
		return before, desired, nil
	case "registry-import":
		before, err := captureDurableRegistryKey(entry.TargetPath, workRoot)
		if err != nil {
			return durableLegacyRevertState{}, durableLegacyRevertState{}, err
		}
		if entry.BackupCreated && entry.BackupPath != "" {
			data, _, err := safepath.ReadRegularFile(entry.BackupPath)
			if err != nil {
				return durableLegacyRevertState{}, durableLegacyRevertState{}, err
			}
			return before, digestDurableRegistryExport(data), nil
		}
		return before, absentDurableRegistryState("registry-key"), nil
	default:
		return durableLegacyRevertState{}, durableLegacyRevertState{}, fmt.Errorf("unsupported durable registry revert type %q", entry.RestoreType)
	}
}

func applyDurableLegacyRegistryRevert(entry JournalEntry, index int) error {
	switch entry.RestoreType {
	case "registry-set":
		backup, err := readRegistrySetBackup(entry.BackupPath)
		if err != nil {
			return err
		}
		return revertRegistrySet(backup)
	case "registry-import":
		return deleteDurableLegacyRegistryKey(entry.TargetPath)
	default:
		return fmt.Errorf("unsupported durable registry revert type %q", entry.RestoreType)
	}
}

func durableLegacyRegistryScratchTargets(entry JournalEntry, entryDigest string) (string, string, error) {
	if entry.RestoreType != "registry-import" || !entry.BackupCreated || entry.BackupPath == "" {
		return "", "", nil
	}
	_, subkey, err := splitHKCUKey(entry.TargetPath)
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

func validateDurableLegacyRegistryScratchAvailable(stage, held, workRoot string) error {
	_ = workRoot
	for _, target := range []string{stage, held} {
		if target == "" {
			continue
		}
		state, err := captureDurableRegistryKey(target, workRoot)
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
	entry JournalEntry, prepared durableLegacyRevertPrepared, index int, workRoot string,
) error {
	targetState, err := captureDurableRegistryKey(entry.TargetPath, workRoot)
	if err != nil {
		return err
	}
	stageState, err := captureDurableRegistryKeyAs(prepared.StagePath, entry.TargetPath, workRoot)
	if err != nil {
		return err
	}
	heldState, err := captureDurableRegistryKeyAs(prepared.HeldPath, entry.TargetPath, workRoot)
	if err != nil {
		return err
	}

	if targetState == prepared.Desired {
		if stageState.Kind != "absent" {
			if stageState != prepared.Desired {
				return fmt.Errorf("legacy registry revert stage changed after target replacement")
			}
			if err := deleteDurableLegacyRegistryKey(prepared.StagePath); err != nil {
				return err
			}
		}
		if heldState.Kind != "absent" {
			if heldState != prepared.Before {
				return fmt.Errorf("legacy registry revert held key changed after target replacement")
			}
			if err := deleteDurableLegacyRegistryKey(prepared.HeldPath); err != nil {
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
		if err := deleteDurableLegacyRegistryKey(prepared.StagePath); err != nil {
			return err
		}
		stageState = absentDurableRegistryState("registry-key")
	}
	if stageState.Kind == "absent" {
		if err := stageDurableLegacyRegistryImport(entry, prepared, workRoot); err != nil {
			return err
		}
		stageState, err = captureDurableRegistryKeyAs(prepared.StagePath, entry.TargetPath, workRoot)
		if err != nil {
			return err
		}
	}
	if stageState != prepared.Desired {
		return fmt.Errorf("legacy registry revert stage differs from recorded desired state")
	}
	if !heldExists && targetState.Kind != "absent" {
		if err := renameDurableRegistryKey(entry.TargetPath, prepared.HeldPath); err != nil {
			return err
		}
		if err := durableRevertCheckpoint("after_registry_target_held", index); err != nil {
			return err
		}
		heldExists = true
	}
	targetState, err = captureDurableRegistryKey(entry.TargetPath, workRoot)
	if err != nil {
		return err
	}
	if targetState.Kind == "absent" {
		if err := renameDurableRegistryKey(prepared.StagePath, entry.TargetPath); err != nil {
			return err
		}
	}
	actual, err := captureDurableRegistryKey(entry.TargetPath, workRoot)
	if err != nil {
		return err
	}
	if actual != prepared.Desired {
		return fmt.Errorf("legacy registry revert target does not match staged desired state")
	}
	if heldExists {
		if err := deleteDurableLegacyRegistryKey(prepared.HeldPath); err != nil {
			return err
		}
	}
	return nil
}

func stageDurableLegacyRegistryImport(entry JournalEntry, prepared durableLegacyRevertPrepared, workRoot string) error {
	data, _, err := safepath.ReadRegularFile(entry.BackupPath)
	if err != nil {
		return err
	}
	rewritten, err := rewriteDurableRegistryExport(data, entry.TargetPath, prepared.StagePath)
	if err != nil {
		return err
	}
	path := filepath.Join(workRoot, fmt.Sprintf("entry-%06d-registry-stage.reg", prepared.EntryIndex))
	if err := safepath.AtomicWriteFile(path, rewritten, 0o600); err != nil {
		return err
	}
	defer os.Remove(path)
	if err := durableRegistryImportFile(path); err != nil {
		return errors.Join(err, deleteDurableLegacyRegistryKey(prepared.StagePath))
	}
	state, err := captureDurableRegistryKeyAs(prepared.StagePath, entry.TargetPath, workRoot)
	if err != nil {
		return err
	}
	if state != prepared.Desired {
		_ = deleteDurableLegacyRegistryKey(prepared.StagePath)
		return fmt.Errorf("staged registry import does not match recorded desired state")
	}
	return nil
}

func captureDurableRegistryKeyAs(actual, semantic, workRoot string) (durableLegacyRevertState, error) {
	state, data, err := exportDurableRegistryKey(actual, workRoot)
	if err != nil || state.Kind == "absent" {
		return state, err
	}
	rewritten, err := rewriteDurableRegistryExport(data, actual, semantic)
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
	return []byte(strings.Join(lines, "\r\n")), nil
}

func canonicalDurableRegistryKey(target string) (string, error) {
	_, subkey, err := splitHKCUKey(target)
	if err != nil {
		return "", err
	}
	return `HKEY_CURRENT_USER\` + subkey, nil
}

func renameDurableRegistryKey(source, destination string) error {
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

func deleteDurableLegacyRegistryKey(target string) error {
	if err := exec.Command("reg", "delete", target, "/f").Run(); err != nil {
		hive, subkey, splitErr := splitHKCUKey(target)
		if splitErr != nil {
			return splitErr
		}
		key, queryErr := registry.OpenKey(hive, subkey, registry.QUERY_VALUE)
		if queryErr == nil {
			_ = key.Close()
			return fmt.Errorf("cannot delete registry key %s: %w", target, err)
		}
		if queryErr != registry.ErrNotExist {
			return fmt.Errorf("verify registry key deletion %s: %w", target, queryErr)
		}
	}
	return nil
}

func captureDurableRegistryKey(target, workRoot string) (durableLegacyRevertState, error) {
	state, data, err := exportDurableRegistryKey(target, workRoot)
	if err != nil || state.Kind == "absent" {
		return state, err
	}
	return digestDurableRegistryExport(data), nil
}

func exportDurableRegistryKey(target, workRoot string) (durableLegacyRevertState, []byte, error) {
	if err := ValidateRegistryTarget(target); err != nil {
		return durableLegacyRevertState{}, nil, err
	}
	hive, subkey, err := splitHKCUKey(target)
	if err != nil {
		return durableLegacyRevertState{}, nil, err
	}
	key, err := registry.OpenKey(hive, subkey, registry.QUERY_VALUE|registry.ENUMERATE_SUB_KEYS)
	if err == registry.ErrNotExist {
		return absentDurableRegistryState("registry-key"), nil, nil
	}
	if err != nil {
		return durableLegacyRevertState{}, nil, fmt.Errorf("inspect registry key %s: %w", target, err)
	}
	_ = key.Close()
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
	if err := exec.Command("reg", "export", target, path, "/y").Run(); err != nil {
		return durableLegacyRevertState{}, nil, fmt.Errorf("capture registry key %s: %w", target, err)
	}
	data, _, err := safepath.ReadRegularFile(filepath.Clean(path))
	if err != nil {
		return durableLegacyRevertState{}, nil, err
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
