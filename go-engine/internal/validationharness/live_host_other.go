// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package validationharness

import "fmt"

type LiveVersionSource interface {
	FileVersion(string) (string, error)
}

func NewWindowsLiveObserver(LiveVersionSource) (LiveObserver, error) {
	return LiveObserver{}, fmt.Errorf("live Windows observer is unavailable on this platform")
}
