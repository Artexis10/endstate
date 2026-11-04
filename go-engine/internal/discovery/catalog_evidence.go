// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/packagecatalog"
)

// CatalogExecutableAdapter observes only the exact executable names declared
// in the reviewed catalog. Looking up a name never executes the program.
type CatalogExecutableAdapter struct{ Catalog *packagecatalog.Catalog }

func (CatalogExecutableAdapter) ID() string { return "catalog-executables" }

func (adapter CatalogExecutableAdapter) Inventory(ctx context.Context) (AdapterInventory, error) {
	if adapter.Catalog == nil {
		return AdapterInventory{}, ErrNotAvailable
	}
	result := AdapterInventory{}
	for _, entry := range adapter.Catalog.Entries() {
		for _, name := range entry.Aliases["executable"] {
			if err := ctx.Err(); err != nil {
				return AdapterInventory{}, err
			}
			if _, err := exec.LookPath(name); err != nil {
				if errors.Is(err, exec.ErrNotFound) || errors.Is(err, exec.ErrDot) {
					continue
				}
				return AdapterInventory{}, fmt.Errorf("inspect catalog executable: %w", err)
			}
			result.Items = append(result.Items, InventoryItem{Source: "executable", Kind: EvidenceExecutable, Ref: name, DisplayName: entry.DisplayName, UserFacing: true})
		}
	}
	return result, nil
}

// CatalogConfigAdapter observes presence at catalog-declared coordinates. It
// never opens configuration files or treats presence as execution authority.
type CatalogConfigAdapter struct {
	Catalog     *packagecatalog.Catalog
	Environment config.PathEnvironment
}

func (CatalogConfigAdapter) ID() string { return "catalog-config-paths" }

func (adapter CatalogConfigAdapter) Inventory(ctx context.Context) (AdapterInventory, error) {
	if adapter.Catalog == nil {
		return AdapterInventory{}, ErrNotAvailable
	}
	result := AdapterInventory{}
	for _, entry := range adapter.Catalog.Entries() {
		var evidence []Evidence
		for _, coordinate := range entry.Aliases["config"] {
			if err := ctx.Err(); err != nil {
				return AdapterInventory{}, err
			}
			path, err := config.ResolveEnginePath(coordinate, adapter.Environment)
			if err != nil {
				return AdapterInventory{}, fmt.Errorf("resolve catalog config coordinate: %w", err)
			}
			// Lstat observes even a managed symlink without opening its target. Safe
			// content collection remains the trusted settings adapter's responsibility.
			if _, err := os.Lstat(path); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return AdapterInventory{}, fmt.Errorf("inspect catalog config coordinate: %w", err)
			}
			evidence = append(evidence, Evidence{Source: "config", Kind: EvidenceConfig, Ref: coordinate})
		}
		if len(evidence) == 0 {
			continue
		}
		id := entry.ConfigModule
		if id == "" {
			id = entry.ID
		}
		// Catalog association alone grants no capture/restore/verify/revert action.
		result.Settings = append(result.Settings, SettingsCandidate{
			ID: id, ModuleID: entry.ConfigModule, PackageID: entry.ID, DisplayName: entry.DisplayName,
			Portability: PortabilityConfigOnly, Evidence: evidence,
		})
	}
	return result, nil
}
