// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package validationaudit

import (
	"os"
	"path/filepath"
	"syscall"
)

func publishEvidence(root, leaf string, raw []byte, digest string) (PublishedEvidence, error) {
	canonicalRoot, ok := canonicalUnixEvidenceRoot(root)
	if !ok {
		return PublishedEvidence{}, ErrUnsafeEvidencePath
	}
	rootFD, err := syscall.Open(canonicalRoot, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return PublishedEvidence{}, ErrUnsafeEvidencePath
	}
	rootFile := os.NewFile(uintptr(rootFD), canonicalRoot)
	defer rootFile.Close()
	rootInfo, err := rootFile.Stat()
	if err != nil || !rootInfo.IsDir() {
		return PublishedEvidence{}, ErrUnsafeEvidencePath
	}
	rootStat, rootOK := rootInfo.Sys().(*syscall.Stat_t)
	if !rootOK {
		return PublishedEvidence{}, ErrUnsafeEvidencePath
	}
	leafFD, err := syscall.Openat(rootFD, leaf, syscall.O_RDWR|syscall.O_CREAT|syscall.O_EXCL|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		if err == syscall.EEXIST {
			return PublishedEvidence{}, ErrEvidenceAlreadyExists
		}
		return PublishedEvidence{}, ErrEvidencePublication
	}
	leafFile := os.NewFile(uintptr(leafFD), filepath.Join(canonicalRoot, leaf))
	defer leafFile.Close()
	leafInfo, err := leafFile.Stat()
	if err != nil || !leafInfo.Mode().IsRegular() {
		return PublishedEvidence{}, ErrEvidencePublication
	}
	leafStat, leafOK := leafInfo.Sys().(*syscall.Stat_t)
	if !leafOK || rootStat.Dev != leafStat.Dev {
		return PublishedEvidence{}, ErrEvidencePublication
	}
	return writeAndVerifyUnixEvidence(leafFile, raw, digest)
}
