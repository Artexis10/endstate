// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows && !linux && !darwin

package commands

import (
	"context"
	"fmt"
	"runtime"

	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
)

type unsupportedConfigRestoreProcessObserver struct{}

func newConfigRestorePlatformAdapters() (configrestore.RegistryMutator, configrestore.ProcessObserver) {
	return unsupportedConfigRestoreRegistry{}, unsupportedConfigRestoreProcessObserver{}
}

func (unsupportedConfigRestoreProcessObserver) RunningProcessBasenames(context.Context) ([]string, error) {
	return nil, fmt.Errorf("process observation is unsupported on %s", runtime.GOOS)
}
