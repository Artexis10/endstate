// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package restore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"golang.org/x/sys/windows/registry"
)

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
		if entry.BackupCreated && entry.BackupPath != "" {
			if err := deleteDurableLegacyRegistryKey(entry.TargetPath); err != nil {
				return err
			}
			if err := durableRevertCheckpoint("after_registry_key_deleted", index); err != nil {
				return err
			}
			if err := exec.Command("reg", "import", entry.BackupPath).Run(); err != nil {
				return fmt.Errorf("cannot revert registry import from %s: %w", entry.BackupPath, err)
			}
			return nil
		}
		return deleteDurableLegacyRegistryKey(entry.TargetPath)
	default:
		return fmt.Errorf("unsupported durable registry revert type %q", entry.RestoreType)
	}
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
	if err := ValidateRegistryTarget(target); err != nil {
		return durableLegacyRevertState{}, err
	}
	hive, subkey, err := splitHKCUKey(target)
	if err != nil {
		return durableLegacyRevertState{}, err
	}
	key, err := registry.OpenKey(hive, subkey, registry.QUERY_VALUE|registry.ENUMERATE_SUB_KEYS)
	if err == registry.ErrNotExist {
		return absentDurableRegistryState("registry-key"), nil
	}
	if err != nil {
		return durableLegacyRevertState{}, fmt.Errorf("inspect registry key %s: %w", target, err)
	}
	_ = key.Close()
	temporary, err := os.CreateTemp(workRoot, ".registry-state-*.reg")
	if err != nil {
		return durableLegacyRevertState{}, err
	}
	path := temporary.Name()
	if err := temporary.Close(); err != nil {
		_ = os.Remove(path)
		return durableLegacyRevertState{}, err
	}
	defer os.Remove(path)
	if err := exec.Command("reg", "export", target, path, "/y").Run(); err != nil {
		return durableLegacyRevertState{}, fmt.Errorf("capture registry key %s: %w", target, err)
	}
	data, _, err := safepath.ReadRegularFile(filepath.Clean(path))
	if err != nil {
		return durableLegacyRevertState{}, err
	}
	return digestDurableRegistryExport(data), nil
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
