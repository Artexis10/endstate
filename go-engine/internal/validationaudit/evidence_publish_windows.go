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
	"syscall"
)

func publishEvidence(root, leaf string, raw []byte, digest string) (PublishedEvidence, error) {
	canonicalRoot, ok := canonicalEvidenceRoot(root)
	if !ok {
		return PublishedEvidence{}, ErrUnsafeEvidencePath
	}
	rootHandle, err := openWindowsSafePath(canonicalRoot, true)
	if err != nil {
		return PublishedEvidence{}, ErrUnsafeEvidencePath
	}
	defer rootHandle.Close()

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

func canonicalEvidenceRoot(root string) (string, bool) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return "", false
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	info, err := os.Lstat(abs)
	if err != nil || !info.IsDir() || unsafePathInfo(abs, info) {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil || filepath.Clean(resolved) != resolved {
		return "", false
	}
	resolvedInfo, err := os.Lstat(resolved)
	if err != nil || !resolvedInfo.IsDir() || unsafePathInfo(resolved, resolvedInfo) {
		return "", false
	}
	return resolved, true
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
