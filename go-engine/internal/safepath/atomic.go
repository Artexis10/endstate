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
	input, openedInfo, err := openVerifiedRegularFile(source)
	if err != nil {
		return err
	}
	defer input.Close()
	return atomicWrite(destination, mode, func(file *os.File) error {
		if _, err := io.Copy(file, input); err != nil {
			return err
		}
		return verifyAtomicCopySource(source, openedInfo)
	})
}

func ReadRegularFile(source string) ([]byte, os.FileMode, error) {
	input, openedInfo, err := openVerifiedRegularFile(source)
	if err != nil {
		return nil, 0, err
	}
	defer input.Close()
	data, err := io.ReadAll(input)
	if err != nil {
		return nil, 0, err
	}
	if err := verifyAtomicCopySource(source, openedInfo); err != nil {
		return nil, 0, err
	}
	return data, openedInfo.Mode(), nil
}

func openVerifiedRegularFile(source string) (*os.File, os.FileInfo, error) {
	preOpenInfo, err := os.Lstat(source)
	if err != nil {
		return nil, nil, err
	}
	if isLinkOrReparse(preOpenInfo) {
		return nil, nil, pathError(CodeLinkUnsupported, source, ErrLinkUnsupported)
	}
	if !preOpenInfo.Mode().IsRegular() {
		return nil, nil, pathError(CodeUnsafePath, source, ErrUnsafePath)
	}
	input, err := openAtomicCopySource(source)
	if err != nil {
		return nil, nil, err
	}
	openedInfo, err := input.Stat()
	if err != nil {
		_ = input.Close()
		return nil, nil, err
	}
	if !openedInfo.Mode().IsRegular() {
		_ = input.Close()
		return nil, nil, pathError(CodeSourceChanged, source, ErrSourceChanged)
	}
	if err := verifyAtomicCopySource(source, openedInfo); err != nil {
		_ = input.Close()
		return nil, nil, err
	}
	if !os.SameFile(preOpenInfo, openedInfo) {
		_ = input.Close()
		return nil, nil, pathError(CodeSourceChanged, source, ErrSourceChanged)
	}
	return input, openedInfo, nil
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
