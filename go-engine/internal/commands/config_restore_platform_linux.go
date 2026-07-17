// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package commands

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
)

type linuxConfigRestoreProcessObserver struct{}

func newConfigRestorePlatformAdapters() (configrestore.RegistryMutator, configrestore.ProcessObserver) {
	return unsupportedConfigRestoreRegistry{}, linuxConfigRestoreProcessObserver{}
}

func (linuxConfigRestoreProcessObserver) RunningProcessBasenames(ctx context.Context) ([]string, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.ParseUint(entry.Name(), 10, 64); err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
		if err != nil {
			continue
		}
		if name := strings.TrimSpace(string(data)); name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}
