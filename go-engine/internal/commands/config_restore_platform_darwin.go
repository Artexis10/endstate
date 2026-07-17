// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package commands

import (
	"bytes"
	"context"
	"sort"

	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"golang.org/x/sys/unix"
)

type darwinConfigRestoreProcessObserver struct{}

func newConfigRestorePlatformAdapters() (configrestore.RegistryMutator, configrestore.ProcessObserver) {
	return unsupportedConfigRestoreRegistry{}, darwinConfigRestoreProcessObserver{}
}

func (darwinConfigRestoreProcessObserver) RunningProcessBasenames(ctx context.Context) ([]string, error) {
	processes, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(processes))
	for _, process := range processes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw := process.Proc.P_comm[:]
		if end := bytes.IndexByte(raw, 0); end >= 0 {
			raw = raw[:end]
		}
		if len(raw) > 0 {
			names = append(names, string(raw))
		}
	}
	sort.Strings(names)
	return names, nil
}
