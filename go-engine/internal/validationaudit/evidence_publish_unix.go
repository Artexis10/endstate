// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package validationaudit

import (
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
)

func canonicalUnixEvidenceRoot(root string) (string, bool) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || filepath.Clean(resolved) != resolved {
		return "", false
	}
	info, err := os.Lstat(resolved)
	return resolved, err == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0
}

func writeAndVerifyUnixEvidence(file *os.File, raw []byte, digest string) (PublishedEvidence, error) {
	before, err := file.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() != 0 || before.Mode().Perm()&0o077 != 0 {
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
	if hexDigest(sha256.Sum256(stored)) != digest {
		return PublishedEvidence{}, ErrEvidencePublication
	}
	return PublishedEvidence{SHA256: digest, Size: int64(len(raw))}, nil
}
