// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package safepath

import (
	"io"
	"os"
	"path/filepath"
)

func AtomicWriteFile(destination string, data []byte, mode os.FileMode) error {
	return atomicWrite(destination, mode, func(file *os.File) error {
		_, err := file.Write(data)
		return err
	})
}

func AtomicCopyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	return atomicWrite(destination, mode, func(file *os.File) error {
		_, err := io.Copy(file, input)
		return err
	})
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
