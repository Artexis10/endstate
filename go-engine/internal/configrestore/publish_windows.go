// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package configrestore

import (
	"errors"

	"golang.org/x/sys/windows"
)

func publishFileNoReplace(temporary, destination string) (publicationState, error) {
	from, err := windows.UTF16PtrFromString(temporary)
	if err != nil {
		return publicationNotDurable, err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return publicationNotDurable, err
	}
	if err := windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return publicationAmbiguous, errors.Join(ErrPublicationAmbiguous, err)
	}
	return publicationDurable, nil
}
