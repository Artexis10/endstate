// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationaudit

import (
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func publishEvidence(root, leaf string, raw []byte, digest string) (PublishedEvidence, error) {
	handles, canonicalRoot, ok := openWindowsEvidenceRoot(root)
	if !ok {
		return PublishedEvidence{}, ErrUnsafeEvidencePath
	}
	defer func() {
		for index := len(handles) - 1; index >= 0; index-- {
			handles[index].Close()
		}
	}()
	path := filepath.Join(canonicalRoot, leaf)
	handle, err := syscall.CreateFile(
		syscall.StringToUTF16Ptr(path),
		syscall.GENERIC_READ|syscall.GENERIC_WRITE,
		syscall.FILE_SHARE_READ,
		nil,
		syscall.CREATE_NEW,
		syscall.FILE_ATTRIBUTE_NORMAL|syscall.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		if errors.Is(err, syscall.ERROR_FILE_EXISTS) || errors.Is(err, syscall.ERROR_ALREADY_EXISTS) {
			return PublishedEvidence{}, ErrEvidenceAlreadyExists
		}
		return PublishedEvidence{}, ErrEvidencePublication
	}
	file := os.NewFile(uintptr(handle), path)
	defer file.Close()
	return writeAndVerifyEvidence(file, raw, digest)
}

func openWindowsEvidenceRoot(root string) ([]*os.File, string, bool) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, "", false
	}
	abs, err := filepath.Abs(root)
	if err != nil || abs != root {
		return nil, "", false
	}
	volume := filepath.VolumeName(root)
	if volume == "" {
		return nil, "", false
	}
	anchor := volume + string(filepath.Separator)
	beforeEvidenceRootOpen()
	current := anchor
	components := strings.FieldsFunc(strings.TrimPrefix(root, volume), func(value rune) bool { return value == '\\' || value == '/' })
	handles := make([]*os.File, 0, len(components)+1)
	for _, component := range append([]string{""}, components...) {
		if component != "" {
			current = filepath.Join(current, component)
		}
		file, openErr := openWindowsSafePath(current, true)
		if openErr != nil {
			for index := len(handles) - 1; index >= 0; index-- {
				handles[index].Close()
			}
			return nil, "", false
		}
		handles = append(handles, file)
	}
	return handles, root, true
}

func writeAndVerifyEvidence(file *os.File, raw []byte, digest string) (PublishedEvidence, error) {
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() != 0 {
		return PublishedEvidence{}, ErrEvidencePublication
	}
	if written, err := file.Write(raw); err != nil || written != len(raw) || file.Sync() != nil {
		return PublishedEvidence{}, ErrEvidencePublication
	}
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) || after.Size() != int64(len(raw)) {
		return PublishedEvidence{}, ErrEvidencePublication
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return PublishedEvidence{}, ErrEvidencePublication
	}
	stored, err := io.ReadAll(io.LimitReader(file, MaxEvidenceSize+1))
	if err != nil || len(stored) != len(raw) || string(stored) != string(raw) {
		return PublishedEvidence{}, ErrEvidencePublication
	}
	sum := sha256.Sum256(stored)
	if hexDigest(sum) != digest {
		return PublishedEvidence{}, ErrEvidencePublication
	}
	return PublishedEvidence{SHA256: digest, Size: int64(len(raw))}, nil
}
