// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package validationaudit

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func publishEvidence(root, leaf string, raw []byte, digest string) (PublishedEvidence, error) {
	files, err := openDarwinEvidenceRoot(root)
	if err != nil {
		return PublishedEvidence{}, ErrUnsafeEvidencePath
	}
	defer func() {
		for index := len(files) - 1; index >= 0; index-- {
			files[index].Close()
		}
	}()
	rootFile := files[len(files)-1]
	rootInfo, err := rootFile.Stat()
	if err != nil || !rootInfo.IsDir() {
		return PublishedEvidence{}, ErrUnsafeEvidencePath
	}
	rootStat, rootOK := rootInfo.Sys().(*syscall.Stat_t)
	if !rootOK {
		return PublishedEvidence{}, ErrUnsafeEvidencePath
	}
	leafFD, err := darwinOpenat(int(rootFile.Fd()), leaf, syscall.O_RDWR|syscall.O_CREAT|syscall.O_EXCL|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0o600)
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

func openDarwinEvidenceRoot(root string) ([]*os.File, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || !strings.HasPrefix(root, "/") {
		return nil, ErrUnsafeEvidencePath
	}
	fd, err := syscall.Open("/", syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	files := []*os.File{os.NewFile(uintptr(fd), "/")}
	var mount syscall.Statfs_t
	if err := syscall.Fstatfs(fd, &mount); err != nil {
		files[0].Close()
		return nil, err
	}
	beforeEvidenceRootOpen()
	for _, component := range strings.Split(strings.TrimPrefix(root, "/"), "/") {
		if component == "" || component == "." || component == ".." {
			goto failed
		}
		childFD, openErr := darwinOpenat(int(files[len(files)-1].Fd()), component, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
		if openErr != nil {
			goto failed
		}
		child := os.NewFile(uintptr(childFD), component)
		var childMount syscall.Statfs_t
		if err := syscall.Fstatfs(childFD, &childMount); err != nil || !sameDarwinMount(mount, childMount) {
			child.Close()
			goto failed
		}
		files = append(files, child)
	}
	return files, nil
failed:
	for index := len(files) - 1; index >= 0; index-- {
		files[index].Close()
	}
	return nil, ErrUnsafeEvidencePath
}
