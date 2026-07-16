// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	transactionDescriptorFormat = "endstate.config-restore-transaction"
	transactionStoreVersion     = 1
	legacyMemberFormat          = "endstate.config-restore-legacy-member"
	memberRevertFormat          = "endstate.config-restore-member-revert"
)

type transactionDescriptorIdentity struct {
	Format          string `json:"format"`
	Version         int    `json:"version"`
	TransactionID   string `json:"transactionId"`
	RestoreRunID    string `json:"restoreRunId"`
	RunID           string `json:"runId"`
	RunStartedAtUTC string `json:"runStartedAtUtc"`
	MutationOrdinal uint64 `json:"mutationOrdinal"`
	CaptureID       string `json:"captureId"`
}

type transactionDescriptorDisk struct {
	Format           string `json:"format"`
	Version          int    `json:"version"`
	TransactionID    string `json:"transactionId"`
	RestoreRunID     string `json:"restoreRunId"`
	RunID            string `json:"runId"`
	RunStartedAtUTC  string `json:"runStartedAtUtc"`
	MutationOrdinal  uint64 `json:"mutationOrdinal"`
	CaptureID        string `json:"captureId"`
	DescriptorDigest string `json:"descriptorDigest"`
}

func newTransactionDescriptor(
	transactionID string,
	restoreRunID string,
	runID string,
	started time.Time,
	ordinal uint64,
	captureID string,
) (transactionDescriptorDisk, []byte, error) {
	identity := transactionDescriptorIdentity{
		Format: transactionDescriptorFormat, Version: transactionStoreVersion,
		TransactionID: transactionID, RestoreRunID: restoreRunID, RunID: runID,
		RunStartedAtUTC: started.UTC().Format(time.RFC3339Nano), MutationOrdinal: ordinal, CaptureID: captureID,
	}
	identityBytes, err := json.Marshal(identity)
	if err != nil {
		return transactionDescriptorDisk{}, nil, err
	}
	digest := sha256.Sum256(identityBytes)
	disk := transactionDescriptorDisk{
		Format: identity.Format, Version: identity.Version, TransactionID: identity.TransactionID,
		RestoreRunID: identity.RestoreRunID, RunID: identity.RunID, RunStartedAtUTC: identity.RunStartedAtUTC,
		MutationOrdinal: identity.MutationOrdinal, CaptureID: identity.CaptureID,
		DescriptorDigest: hex.EncodeToString(digest[:]),
	}
	encoded, err := json.Marshal(disk)
	if err != nil {
		return transactionDescriptorDisk{}, nil, err
	}
	return disk, append(encoded, '\n'), nil
}

func decodeTransactionDescriptor(data []byte) (transactionDescriptorDisk, []byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var disk transactionDescriptorDisk
	if err := decoder.Decode(&disk); err != nil {
		return transactionDescriptorDisk{}, nil, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return transactionDescriptorDisk{}, nil, err
	}
	started, err := time.Parse(time.RFC3339Nano, disk.RunStartedAtUTC)
	if err != nil || started.Location() != time.UTC || started.Format(time.RFC3339Nano) != disk.RunStartedAtUTC {
		return transactionDescriptorDisk{}, nil, fmt.Errorf("transaction descriptor start time is not canonical UTC")
	}
	if disk.Format != transactionDescriptorFormat || disk.Version != transactionStoreVersion ||
		!isOpaqueStoreID(disk.TransactionID) || !isOpaqueStoreID(disk.RestoreRunID) ||
		disk.RunID == "" || disk.RunID != strings.TrimSpace(disk.RunID) || containsControl(disk.RunID) ||
		disk.CaptureID == "" || disk.CaptureID != strings.TrimSpace(disk.CaptureID) || containsControl(disk.CaptureID) {
		return transactionDescriptorDisk{}, nil, fmt.Errorf("transaction descriptor identity is invalid")
	}
	expected, canonical, err := newTransactionDescriptor(
		disk.TransactionID, disk.RestoreRunID, disk.RunID, started, disk.MutationOrdinal, disk.CaptureID,
	)
	if err != nil {
		return transactionDescriptorDisk{}, nil, err
	}
	if disk.DescriptorDigest != expected.DescriptorDigest || !bytes.Equal(data, canonical) {
		return transactionDescriptorDisk{}, nil, fmt.Errorf("transaction descriptor digest or canonical bytes differ")
	}
	return disk, canonical, nil
}

func isOpaqueStoreID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9') && !(character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

type legacyMemberIdentity struct {
	Format          string `json:"format"`
	Version         int    `json:"version"`
	MemberID        string `json:"memberId"`
	RestoreRunID    string `json:"restoreRunId"`
	RunID           string `json:"runId"`
	RunStartedAtUTC string `json:"runStartedAtUtc"`
	MutationOrdinal uint64 `json:"mutationOrdinal"`
	JournalPath     string `json:"journalPath"`
	JournalDigest   string `json:"journalDigest"`
}

type legacyMemberDisk struct {
	Format          string `json:"format"`
	Version         int    `json:"version"`
	MemberID        string `json:"memberId"`
	RestoreRunID    string `json:"restoreRunId"`
	RunID           string `json:"runId"`
	RunStartedAtUTC string `json:"runStartedAtUtc"`
	MutationOrdinal uint64 `json:"mutationOrdinal"`
	JournalPath     string `json:"journalPath"`
	JournalDigest   string `json:"journalDigest"`
	MemberDigest    string `json:"memberDigest"`
}

func newLegacyMember(
	memberID, restoreRunID, runID string,
	started time.Time,
	ordinal uint64,
	journalPath, journalDigest string,
) (legacyMemberDisk, []byte, error) {
	identity := legacyMemberIdentity{
		Format: legacyMemberFormat, Version: transactionStoreVersion,
		MemberID: memberID, RestoreRunID: restoreRunID, RunID: runID,
		RunStartedAtUTC: started.UTC().Format(time.RFC3339Nano), MutationOrdinal: ordinal,
		JournalPath: journalPath, JournalDigest: journalDigest,
	}
	identityBytes, err := json.Marshal(identity)
	if err != nil {
		return legacyMemberDisk{}, nil, err
	}
	digest := sha256.Sum256(identityBytes)
	disk := legacyMemberDisk{
		Format: identity.Format, Version: identity.Version, MemberID: identity.MemberID,
		RestoreRunID: identity.RestoreRunID, RunID: identity.RunID, RunStartedAtUTC: identity.RunStartedAtUTC,
		MutationOrdinal: identity.MutationOrdinal, JournalPath: identity.JournalPath,
		JournalDigest: identity.JournalDigest, MemberDigest: hex.EncodeToString(digest[:]),
	}
	encoded, err := json.Marshal(disk)
	return disk, append(encoded, '\n'), err
}

func decodeLegacyMember(data []byte) (legacyMemberDisk, time.Time, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var disk legacyMemberDisk
	if err := decoder.Decode(&disk); err != nil {
		return legacyMemberDisk{}, time.Time{}, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return legacyMemberDisk{}, time.Time{}, err
	}
	started, err := time.Parse(time.RFC3339Nano, disk.RunStartedAtUTC)
	if err != nil || started.Location() != time.UTC || started.Format(time.RFC3339Nano) != disk.RunStartedAtUTC {
		return legacyMemberDisk{}, time.Time{}, fmt.Errorf("legacy member start time is not canonical UTC")
	}
	if disk.Format != legacyMemberFormat || disk.Version != transactionStoreVersion ||
		!isOpaqueStoreID(disk.MemberID) || !isOpaqueStoreID(disk.RestoreRunID) ||
		disk.RunID == "" || disk.RunID != strings.TrimSpace(disk.RunID) || containsControl(disk.RunID) ||
		disk.JournalPath == "" || !filepath.IsAbs(disk.JournalPath) || filepath.Clean(disk.JournalPath) != disk.JournalPath ||
		!isLowerHexDigest(disk.JournalDigest) {
		return legacyMemberDisk{}, time.Time{}, fmt.Errorf("legacy member identity is invalid")
	}
	expected, canonical, err := newLegacyMember(
		disk.MemberID, disk.RestoreRunID, disk.RunID, started, disk.MutationOrdinal, disk.JournalPath, disk.JournalDigest,
	)
	if err != nil {
		return legacyMemberDisk{}, time.Time{}, err
	}
	if disk.MemberDigest != expected.MemberDigest || !bytes.Equal(data, canonical) {
		return legacyMemberDisk{}, time.Time{}, fmt.Errorf("legacy member digest or canonical bytes differ")
	}
	return disk, started, nil
}

type memberRevertIdentity struct {
	Format       string          `json:"format"`
	Version      int             `json:"version"`
	Kind         StoreMemberKind `json:"kind"`
	MemberID     string          `json:"memberId"`
	SourceDigest string          `json:"sourceDigest"`
}

type memberRevertDisk struct {
	Format       string          `json:"format"`
	Version      int             `json:"version"`
	Kind         StoreMemberKind `json:"kind"`
	MemberID     string          `json:"memberId"`
	SourceDigest string          `json:"sourceDigest"`
	RevertDigest string          `json:"revertDigest"`
}

func newMemberRevert(kind StoreMemberKind, memberID, sourceDigest string) (memberRevertDisk, []byte, error) {
	identity := memberRevertIdentity{
		Format: memberRevertFormat, Version: transactionStoreVersion,
		Kind: kind, MemberID: memberID, SourceDigest: sourceDigest,
	}
	identityBytes, err := json.Marshal(identity)
	if err != nil {
		return memberRevertDisk{}, nil, err
	}
	digest := sha256.Sum256(identityBytes)
	disk := memberRevertDisk{
		Format: identity.Format, Version: identity.Version, Kind: identity.Kind,
		MemberID: identity.MemberID, SourceDigest: identity.SourceDigest, RevertDigest: hex.EncodeToString(digest[:]),
	}
	encoded, err := json.Marshal(disk)
	return disk, append(encoded, '\n'), err
}

func decodeMemberRevert(data []byte) (memberRevertDisk, []byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var disk memberRevertDisk
	if err := decoder.Decode(&disk); err != nil {
		return memberRevertDisk{}, nil, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return memberRevertDisk{}, nil, err
	}
	if disk.Format != memberRevertFormat || disk.Version != transactionStoreVersion ||
		(disk.Kind != StoreMemberGeneration && disk.Kind != StoreMemberLegacy) ||
		!isOpaqueStoreID(disk.MemberID) || !isLowerHexDigest(disk.SourceDigest) {
		return memberRevertDisk{}, nil, fmt.Errorf("member revert identity is invalid")
	}
	expected, canonical, err := newMemberRevert(disk.Kind, disk.MemberID, disk.SourceDigest)
	if err != nil {
		return memberRevertDisk{}, nil, err
	}
	if disk.RevertDigest != expected.RevertDigest || !bytes.Equal(data, canonical) {
		return memberRevertDisk{}, nil, fmt.Errorf("member revert digest or canonical bytes differ")
	}
	return disk, canonical, nil
}
