// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package configrestore

import (
	"errors"
	"os"
	"path/filepath"
)

func publishFileNoReplace(temporary, destination string) (publicationState, error) {
	return publishFileNoReplaceUnix(temporary, destination, unixPublicationOps{
		link: os.Link, remove: os.Remove, syncDirectory: syncDurableDirectory,
	})
}

type unixPublicationOps struct {
	link          func(string, string) error
	remove        func(string) error
	syncDirectory func(string) error
}

func publishFileNoReplaceUnix(temporary, destination string, ops unixPublicationOps) (publicationState, error) {
	if err := ops.link(temporary, destination); err != nil {
		return publicationNotDurable, err
	}
	directory := filepath.Dir(destination)
	if err := ops.syncDirectory(directory); err != nil {
		removeErr := ops.remove(destination)
		if os.IsNotExist(removeErr) {
			removeErr = nil
		}
		removeSyncErr := ops.syncDirectory(directory)
		if removeErr == nil && removeSyncErr == nil {
			return publicationNotDurable, err
		}
		return publicationAmbiguous, errors.Join(ErrPublicationAmbiguous, err, removeErr, removeSyncErr)
	}

	// The destination is authoritative once its directory entry is durable.
	// Temporary-link cleanup is best effort and cannot turn terminal success
	// into a rollback instruction.
	if err := ops.remove(temporary); err == nil {
		_ = ops.syncDirectory(directory)
	}
	return publicationDurable, nil
}
