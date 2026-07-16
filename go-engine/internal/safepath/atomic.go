// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package safepath

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var openAtomicCopySource = os.Open

func AtomicWriteFile(destination string, data []byte, mode os.FileMode) error {
	return atomicWrite(destination, mode, func(file *os.File) error {
		_, err := file.Write(data)
		return err
	})
}

func AtomicCopyFile(source, destination string, mode os.FileMode) error {
	preOpenInfo, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if isLinkOrReparse(preOpenInfo) {
		return pathError(CodeLinkUnsupported, source, ErrLinkUnsupported)
	}
	if !preOpenInfo.Mode().IsRegular() {
		return pathError(CodeUnsafePath, source, ErrUnsafePath)
	}
	input, err := openAtomicCopySource(source)
	if err != nil {
		return err
	}
	defer input.Close()
	openedInfo, err := input.Stat()
	if err != nil {
		return err
	}
	if !openedInfo.Mode().IsRegular() {
		return pathError(CodeSourceChanged, source, ErrSourceChanged)
	}
	if err := verifyAtomicCopySource(source, openedInfo); err != nil {
		return err
	}
	if !os.SameFile(preOpenInfo, openedInfo) {
		return pathError(CodeSourceChanged, source, ErrSourceChanged)
	}
	return atomicWrite(destination, mode, func(file *os.File) error {
		if _, err := io.Copy(file, input); err != nil {
			return err
		}
		return verifyAtomicCopySource(source, openedInfo)
	})
}

func verifyAtomicCopySource(source string, openedInfo os.FileInfo) error {
	currentInfo, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if isLinkOrReparse(currentInfo) {
		return pathError(CodeLinkUnsupported, source, ErrLinkUnsupported)
	}
	if !currentInfo.Mode().IsRegular() || !os.SameFile(currentInfo, openedInfo) {
		return pathError(CodeSourceChanged, source, fmt.Errorf("%w: file identity differs", ErrSourceChanged))
	}
	return nil
}

func AtomicRename(source, destination string) error {
	return atomicReplace(source, destination)
}

func atomicWrite(destination string, mode os.FileMode, write func(*os.File) error) (resultErr error) {
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".endstate-write-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if resultErr != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(mode.Perm()); err != nil {
		return err
	}
	if err := write(temporary); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return atomicReplace(temporaryPath, destination)
}
