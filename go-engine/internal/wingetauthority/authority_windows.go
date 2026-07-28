// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package wingetauthority

import (
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

type strictWindowsBinding struct {
	handles []windows.Handle
}

func (binding *strictWindowsBinding) close() {
	for index := len(binding.handles) - 1; index >= 0; index-- {
		_ = windows.CloseHandle(binding.handles[index])
	}
	binding.handles = nil
}

func bindStrict(path string, expected [32]byte) (func(), error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errInvalidAuthority
	}
	binding := &strictWindowsBinding{}
	fail := func() (func(), error) {
		binding.close()
		return nil, errInvalidAuthority
	}
	volume := filepath.VolumeName(path)
	root := volume + `\`
	relative := strings.TrimPrefix(path, root)
	parts := strings.Split(relative, `\`)
	if volume == "" || len(parts) < 2 || anyEmpty(parts) {
		return fail()
	}
	rootHandle, err := openStrictWindowsPath(root, true, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)
	if err != nil {
		return fail()
	}
	binding.handles = append(binding.handles, rootHandle)
	appsRoot, _ := strictWindowsAppsRoot()
	current := root
	for _, component := range parts[:len(parts)-1] {
		current = filepath.Join(current, component)
		if appsRoot != "" && strings.EqualFold(current, appsRoot) {
			handle, accessible, err := openStrictWindowsAppsAncestor(current)
			if err != nil {
				return fail()
			}
			if accessible {
				binding.handles = append(binding.handles, handle)
			}
			continue
		}
		handle, err := openStrictWindowsPath(current, true, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)
		if err != nil {
			return fail()
		}
		binding.handles = append(binding.handles, handle)
	}
	executable, err := openStrictWindowsPath(path, false, windows.FILE_SHARE_READ)
	if err != nil {
		return fail()
	}
	binding.handles = append(binding.handles, executable)
	final, err := strictWindowsFinalPath(executable)
	if err != nil || !strings.EqualFold(final, path) {
		return fail()
	}
	digest, err := strictWindowsSHA256(executable)
	if err != nil || digest != expected {
		return fail()
	}
	return binding.close, nil
}

func anyEmpty(parts []string) bool {
	for _, part := range parts {
		if part == "" {
			return true
		}
	}
	return false
}

func openStrictWindowsPath(path string, directory bool, share uint32) (windows.Handle, error) {
	wide, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	flags := uint32(windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if directory {
		flags |= windows.FILE_FLAG_BACKUP_SEMANTICS
	}
	access := uint32(windows.FILE_READ_ATTRIBUTES)
	if !directory {
		access = windows.GENERIC_READ
	}
	handle, err := windows.CreateFile(wide, access, share, nil, windows.OPEN_EXISTING, flags, 0)
	if err != nil {
		return 0, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || (directory && info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0) || (!directory && info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) {
		_ = windows.CloseHandle(handle)
		return 0, errors.New("unsafe Winget path")
	}
	return handle, nil
}

func strictWindowsSHA256(handle windows.Handle) ([32]byte, error) {
	current := windows.CurrentProcess()
	var duplicate windows.Handle
	if err := windows.DuplicateHandle(current, handle, current, &duplicate, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return [32]byte{}, err
	}
	file := os.NewFile(uintptr(duplicate), "winget-authority")
	if file == nil {
		_ = windows.CloseHandle(duplicate)
		return [32]byte{}, errors.New("trusted executable unavailable")
	}
	hash := sha256.New()
	_, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return [32]byte{}, copyErr
	}
	if closeErr != nil {
		return [32]byte{}, closeErr
	}
	var digest [32]byte
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}

func strictWindowsFinalPath(handle windows.Handle) (string, error) {
	buffer := make([]uint16, 32768)
	count, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
	if err != nil || count == 0 || count >= uint32(len(buffer)) {
		return "", errors.New("final path unavailable")
	}
	return filepath.Clean(strings.TrimPrefix(windows.UTF16ToString(buffer[:count]), `\\?\`)), nil
}

func strictWindowsAppsRoot() (string, error) {
	volume := filepath.VolumeName(os.Getenv("SystemRoot"))
	if volume == "" {
		return "", errors.New("Windows system volume unavailable")
	}
	return filepath.Join(volume+`\`, "Program Files", "WindowsApps"), nil
}

func openStrictWindowsAppsAncestor(path string) (windows.Handle, bool, error) {
	wide, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, false, err
	}
	attributes, err := windows.GetFileAttributes(wide)
	if err != nil || attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || attributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return 0, false, errors.New("unsafe WindowsApps ancestor")
	}
	handle, err := openStrictWindowsPath(path, true, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)
	if err == nil {
		return handle, true, nil
	}
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return 0, false, err
	}
	mutation, mutationErr := windows.CreateFile(wide, windows.FILE_GENERIC_WRITE, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if mutationErr == nil {
		_ = windows.CloseHandle(mutation)
		return 0, false, errors.New("WindowsApps ancestor is mutable")
	}
	if !errors.Is(mutationErr, windows.ERROR_ACCESS_DENIED) {
		return 0, false, mutationErr
	}
	return 0, false, nil
}
