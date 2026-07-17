// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"unicode/utf16"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

type registrySnapshot struct {
	Key       string `json:"key"`
	ValueName string `json:"valueName"`
	Exists    bool   `json:"exists"`
	ValueType uint32 `json:"valueType"`
	Data      []byte `json:"data"`
}

func readRegistrySnapshot(
	ctx context.Context,
	reader RegistryReader,
	value *RegistryValue,
) (registrySnapshot, error) {
	if reader == nil {
		return registrySnapshot{}, fmt.Errorf("registry action requires a read-only registry reader")
	}
	if value == nil {
		return registrySnapshot{}, fmt.Errorf("registry action has no desired value")
	}
	if err := checkSnapshotContext(ctx); err != nil {
		return registrySnapshot{}, err
	}
	result, err := reader.ReadValue(ctx, value.Key, value.ValueName)
	if err != nil {
		return registrySnapshot{}, err
	}
	if !result.Exists && (result.ValueType != 0 || len(result.Data) != 0) {
		return registrySnapshot{}, fmt.Errorf("absent registry value returned type or data")
	}
	return registrySnapshot{
		Key: value.Key, ValueName: value.ValueName, Exists: result.Exists,
		ValueType: result.ValueType, Data: append([]byte(nil), result.Data...),
	}, nil
}

func persistRegistrySnapshot(path string, snapshot registrySnapshot) error {
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	if err := safepath.AtomicWriteFile(path, append(data, '\n'), 0o600); err != nil {
		return err
	}
	verified, err := loadRegistrySnapshot(path)
	if err != nil {
		return err
	}
	if !registrySnapshotsEqual(snapshot, verified) {
		return fmt.Errorf("registry snapshot verification mismatch")
	}
	return nil
}

func loadRegistrySnapshot(path string) (registrySnapshot, error) {
	written, mode, err := safepath.ReadRegularFile(path)
	if err != nil {
		return registrySnapshot{}, err
	}
	if runtime.GOOS != "windows" && mode.Perm() != 0o600 {
		return registrySnapshot{}, fmt.Errorf("registry snapshot mode is %#o, want 0600", mode.Perm())
	}
	var snapshot registrySnapshot
	if err := json.Unmarshal(written, &snapshot); err != nil {
		return registrySnapshot{}, err
	}
	return snapshot, nil
}

func registrySnapshotsEqual(left, right registrySnapshot) bool {
	return left.Key == right.Key && left.ValueName == right.ValueName && left.Exists == right.Exists &&
		left.ValueType == right.ValueType && bytes.Equal(left.Data, right.Data)
}

func digestRegistrySnapshot(snapshot registrySnapshot) string {
	hasher := sha256.New()
	writeDigestString(hasher, "endstate-registry-state-v1")
	writeDigestString(hasher, strings.ToLower(snapshot.Key))
	writeDigestString(hasher, strings.ToLower(snapshot.ValueName))
	if !snapshot.Exists {
		writeDigestString(hasher, "absent")
		return hex.EncodeToString(hasher.Sum(nil))
	}
	writeDigestString(hasher, "value")
	writeDigestUint64(hasher, uint64(snapshot.ValueType))
	writeDigestUint64(hasher, uint64(len(snapshot.Data)))
	_, _ = hasher.Write(snapshot.Data)
	return hex.EncodeToString(hasher.Sum(nil))
}

func desiredRegistrySnapshot(value *RegistryValue) (registrySnapshot, error) {
	if value == nil {
		return registrySnapshot{}, fmt.Errorf("registry action has no desired value")
	}
	valueType := strings.ToUpper(strings.TrimSpace(value.ValueType))
	var raw []byte
	var numericType uint32
	switch valueType {
	case "REG_DWORD":
		parsed, err := parseDWORD(value.Data)
		if err != nil {
			return registrySnapshot{}, err
		}
		raw = make([]byte, 4)
		binary.LittleEndian.PutUint32(raw, uint32(parsed))
		numericType = RegistryTypeDWORD
	case "REG_SZ", "REG_EXPAND_SZ":
		encoded := utf16.Encode([]rune(value.Data + "\x00"))
		raw = make([]byte, len(encoded)*2)
		for index, character := range encoded {
			binary.LittleEndian.PutUint16(raw[index*2:], character)
		}
		if valueType == "REG_SZ" {
			numericType = RegistryTypeSZ
		} else {
			numericType = RegistryTypeExpandSZ
		}
	default:
		return registrySnapshot{}, fmt.Errorf("unsupported desired registry type %q", value.ValueType)
	}
	return registrySnapshot{
		Key: value.Key, ValueName: value.ValueName, Exists: true, ValueType: numericType, Data: raw,
	}, nil
}

func registryStateRecord(snapshot registrySnapshot, backupPath string) StateRecord {
	kind := StateRegistryValue
	if !snapshot.Exists {
		kind = StateAbsent
	}
	return StateRecord{Kind: kind, Digest: digestRegistrySnapshot(snapshot), BackupPath: backupPath}
}
