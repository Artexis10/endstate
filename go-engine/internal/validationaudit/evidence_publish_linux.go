// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package validationaudit

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func publishEvidence(root, leaf string, raw []byte, digest string) (PublishedEvidence, error) {
	rootFile, err := openLinuxEvidenceRoot(root)
	if err != nil {
		return PublishedEvidence{}, ErrUnsafeEvidencePath
	}
	defer rootFile.Close()
	rootInfo, err := rootFile.Stat()
	if err != nil || !rootInfo.IsDir() {
		return PublishedEvidence{}, ErrUnsafeEvidencePath
	}
	rootStat, rootOK := rootInfo.Sys().(*syscall.Stat_t)
	if !rootOK {
		return PublishedEvidence{}, ErrUnsafeEvidencePath
	}
	leafFD, err := syscall.Openat(int(rootFile.Fd()), leaf, syscall.O_RDWR|syscall.O_CREAT|syscall.O_EXCL|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		if err == syscall.EEXIST {
			return PublishedEvidence{}, ErrEvidenceAlreadyExists
		}
		return PublishedEvidence{}, ErrEvidencePublication
	}
	leafFile := os.NewFile(uintptr(leafFD), filepath.Join(root, leaf))
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

func openLinuxEvidenceRoot(root string) (*os.File, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !strings.HasPrefix(root, "/") {
		return nil, ErrUnsafeEvidencePath
	}
	anchorFD, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	anchor := os.NewFile(uintptr(anchorFD), "/")
	defer anchor.Close()
	beforeEvidenceRootOpen()
	path := strings.TrimPrefix(root, "/")
	if path == "" {
		duplicate, duplicateErr := syscall.Dup(anchorFD)
		if duplicateErr != nil {
			return nil, duplicateErr
		}
		return os.NewFile(uintptr(duplicate), root), nil
	}
	fd, err := linuxOpenat2(anchorFD, path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, resolveBeneath|resolveNoSymlinks|resolveNoMagic|resolveNoXDev)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), root), nil
}
