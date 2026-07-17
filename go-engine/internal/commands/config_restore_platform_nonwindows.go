// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package commands

import (
	"context"
	"fmt"
	"runtime"

	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
)

type unsupportedConfigRestoreRegistry struct{}

func (unsupportedConfigRestoreRegistry) ReadValue(context.Context, string, string) (configrestore.RegistryReadResult, error) {
	return configrestore.RegistryReadResult{}, fmt.Errorf("registry configuration restore is unsupported on %s", runtime.GOOS)
}

func (unsupportedConfigRestoreRegistry) SetValue(context.Context, string, string, uint32, []byte) error {
	return fmt.Errorf("registry configuration restore is unsupported on %s", runtime.GOOS)
}

func (unsupportedConfigRestoreRegistry) DeleteValue(context.Context, string, string) error {
	return fmt.Errorf("registry configuration restore is unsupported on %s", runtime.GOOS)
}
