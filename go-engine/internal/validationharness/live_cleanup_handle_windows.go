// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var windowsLiveRoamingAppData = windowsLiveKnownRoamingAppData
var windowsLiveRunnerTemp = windowsLiveEnvironmentRunnerTemp
var windowsLiveCleanupBeforeChildOpen func(string)
var windowsLiveCleanupAfterChildOpen func(string)
var windowsLiveCleanupBeforeAttemptRootOpen func(string)
var windowsLiveCleanupReadDir = func(file *os.File, count int) ([]os.DirEntry, error) {
	return file.ReadDir(count)
}

const (
	maxWindowsLiveCleanupDepth   = 32
	maxWindowsLiveCleanupEntries = 1024
)

type windowsLiveCleanupBudget struct {
	maxDepth   int
	maxEntries int
}

type windowsLiveCleanupState struct {
	budget  windowsLiveCleanupBudget
	entries int
}

type windowsLiveFileDispositionInfo struct{ deleteFile uint32 }

type windowsLiveObjectIdentity struct {
	volume, indexHigh, indexLow uint32
	canonical                   string
}

func windowsLiveObjectIdentityForPath(path string, directory bool) (windowsLiveObjectIdentity, error) {
	handle, err := openWindowsLiveCleanupHandle(path, directory)
	if err != nil {
		return windowsLiveObjectIdentity{}, err
	}
	defer windows.CloseHandle(handle)
	return windowsLiveObjectIdentityForHandle(handle)
}

func windowsLiveObjectIdentityForHandle(handle windows.Handle) (windowsLiveObjectIdentity, error) {
	var information windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &information); err != nil || information.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || information.FileIndexHigh == 0 && information.FileIndexLow == 0 {
		return windowsLiveObjectIdentity{}, fmt.Errorf("live owned root is unsafe")
	}
	canonical, err := windowsLiveFinalHandlePath(handle)
	if err != nil {
		return windowsLiveObjectIdentity{}, err
	}
	return windowsLiveObjectIdentity{volume: information.VolumeSerialNumber, indexHigh: information.FileIndexHigh, indexLow: information.FileIndexLow, canonical: canonical}, nil
}

func removeWindowsLiveDirectoryWithBudget(ctx context.Context, path string, budget windowsLiveCleanupBudget) error {
	if budget.maxDepth < 1 || budget.maxDepth > maxWindowsLiveCleanupDepth || budget.maxEntries < 1 || budget.maxEntries > maxWindowsLiveCleanupEntries {
		return fmt.Errorf("live cleanup budget is invalid")
	}
	state := windowsLiveCleanupState{budget: budget}
	return state.removeDirectory(ctx, filepath.Clean(path), 0)
}

func removeWindowsLiveExactLeaf(ctx context.Context, path string, directory bool) error {
	state := windowsLiveCleanupState{budget: windowsLiveCleanupBudget{maxDepth: maxWindowsLiveCleanupDepth, maxEntries: maxWindowsLiveCleanupEntries}}
	return state.removeLeaf(ctx, filepath.Clean(path), directory)
}

func removeWindowsLiveDirectoryHandleWithBudget(ctx context.Context, handle windows.Handle, path string, budget windowsLiveCleanupBudget, want windowsLiveObjectIdentity) error {
	if budget.maxDepth < 1 || budget.maxDepth > maxWindowsLiveCleanupDepth || budget.maxEntries < 1 || budget.maxEntries > maxWindowsLiveCleanupEntries {
		return fmt.Errorf("live cleanup budget is invalid")
	}
	directoryFile := os.NewFile(uintptr(handle), path)
	if directoryFile == nil {
		windows.CloseHandle(handle)
		return fmt.Errorf("live cleanup directory handle is unavailable")
	}
	defer directoryFile.Close()
	if err := validateWindowsLiveCleanupHandle(handle, path, true); err != nil {
		return err
	}
	identity, err := windowsLiveObjectIdentityForHandle(handle)
	if err != nil || identity != want {
		return fmt.Errorf("live owned root identity changed")
	}
	state := windowsLiveCleanupState{budget: budget}
	return state.removeDirectoryHandle(ctx, handle, directoryFile, path, 0)
}

func (state *windowsLiveCleanupState) removeDirectory(ctx context.Context, path string, depth int) error {
	if err := ctx.Err(); err != nil || depth > state.budget.maxDepth {
		return fmt.Errorf("live cleanup budget exhausted")
	}
	handle, err := openWindowsLiveCleanupHandle(path, true)
	if err != nil {
		return err
	}
	directoryFile := os.NewFile(uintptr(handle), path)
	if directoryFile == nil {
		windows.CloseHandle(handle)
		return fmt.Errorf("live cleanup directory handle is unavailable")
	}
	defer directoryFile.Close()
	if err := validateWindowsLiveCleanupHandle(handle, path, true); err != nil {
		return err
	}
	if windowsLiveCleanupAfterChildOpen != nil {
		windowsLiveCleanupAfterChildOpen(path)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("live cleanup budget exhausted")
	}
	return state.removeDirectoryHandle(ctx, handle, directoryFile, path, depth)
}

func (state *windowsLiveCleanupState) removeDirectoryHandle(ctx context.Context, handle windows.Handle, directoryFile *os.File, path string, depth int) error {
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("live cleanup budget exhausted")
		}
		remaining := state.budget.maxEntries - state.entries
		if remaining < 1 {
			// Probe once for EOF without processing another entry. A tree that
			// exceeds the budget may have had its permitted prefix removed.
			entries, err := windowsLiveCleanupReadDir(directoryFile, 1)
			if err != nil && err != io.EOF {
				return err
			}
			if len(entries) == 0 && err == io.EOF {
				break
			}
			return fmt.Errorf("live cleanup budget exhausted")
		}
		chunk := 64
		if remaining < chunk {
			chunk = remaining
		}
		entries, err := windowsLiveCleanupReadDir(directoryFile, chunk)
		if err != nil && err != io.EOF {
			return err
		}
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("live cleanup budget exhausted")
			}
			state.entries++
			if state.entries > state.budget.maxEntries || entry.Name() == "" || len(entry.Name()) > 255 {
				return fmt.Errorf("live cleanup budget exhausted")
			}
			child := filepath.Join(path, entry.Name())
			if windowsLiveCleanupBeforeChildOpen != nil {
				windowsLiveCleanupBeforeChildOpen(child)
			}
			if entry.IsDir() {
				if err := state.removeDirectory(ctx, child, depth+1); err != nil {
					return err
				}
				continue
			}
			if err := state.removeLeaf(ctx, child, false); err != nil {
				return err
			}
		}
		if err == io.EOF {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("live cleanup budget exhausted")
	}
	return deleteWindowsLiveCleanupHandle(handle)
}

func (state *windowsLiveCleanupState) removeLeaf(ctx context.Context, path string, directory bool) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("live cleanup budget exhausted")
	}
	handle, err := openWindowsLiveCleanupHandle(path, directory)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	if err := validateWindowsLiveCleanupHandle(handle, path, directory); err != nil {
		return err
	}
	if windowsLiveCleanupAfterChildOpen != nil {
		windowsLiveCleanupAfterChildOpen(path)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("live cleanup budget exhausted")
	}
	return deleteWindowsLiveCleanupHandle(handle)
}

func openWindowsLiveCleanupHandle(path string, directory bool) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	attributes := uint32(windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if directory {
		attributes |= windows.FILE_FLAG_BACKUP_SEMANTICS
	}
	access := uint32(windows.DELETE | windows.FILE_READ_ATTRIBUTES)
	if directory {
		access |= windows.FILE_LIST_DIRECTORY
	}
	handle, err := windows.CreateFile(name, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, attributes, 0)
	if err != nil {
		return windows.InvalidHandle, err
	}
	return handle, nil
}

func validateWindowsLiveCleanupHandle(handle windows.Handle, want string, directory bool) error {
	var information windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &information); err != nil {
		return err
	}
	if information.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || information.FileIndexHigh == 0 && information.FileIndexLow == 0 {
		return fmt.Errorf("live cleanup target is unsafe")
	}
	if directory != (information.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) {
		return fmt.Errorf("live cleanup target type changed")
	}
	final, err := windowsLiveFinalHandlePath(handle)
	want, wantErr := windowsLiveLongPath(want)
	if err != nil || wantErr != nil || !strings.EqualFold(filepath.Clean(final), filepath.Clean(want)) {
		return fmt.Errorf("live cleanup target identity changed")
	}
	return nil
}

func windowsLiveLongPath(path string) (string, error) {
	encoded, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	buffer := make([]uint16, 32768)
	size, err := windows.GetLongPathName(encoded, &buffer[0], uint32(len(buffer)))
	if err != nil || size == 0 || int(size) >= len(buffer) {
		return "", fmt.Errorf("live cleanup long path is unavailable")
	}
	return windows.UTF16ToString(buffer[:size]), nil
}

func windowsLiveFinalHandlePath(handle windows.Handle) (string, error) {
	buffer := make([]uint16, 32768)
	size, err := windows.GetFinalPathNameByHandle(handle, &buffer[0], uint32(len(buffer)), 0)
	if err != nil || size == 0 || int(size) >= len(buffer) {
		return "", fmt.Errorf("live cleanup final path is unavailable")
	}
	value := windows.UTF16ToString(buffer[:size])
	value = strings.TrimPrefix(value, `\\?\`)
	return value, nil
}

func deleteWindowsLiveCleanupHandle(handle windows.Handle) error {
	info := windowsLiveFileDispositionInfo{deleteFile: 1}
	return windows.SetFileInformationByHandle(handle, windows.FileDispositionInfo, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
}

func defaultWindowsLiveCleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 15*time.Second)
}

func windowsLiveKnownRoamingAppData() (string, error) {
	return windows.KnownFolderPath(windows.FOLDERID_RoamingAppData, 0)
}

func windowsLiveEnvironmentRunnerTemp() (string, error) {
	root := os.Getenv("RUNNER_TEMP")
	if root == "" {
		return "", fmt.Errorf("RUNNER_TEMP is unavailable")
	}
	return root, nil
}
