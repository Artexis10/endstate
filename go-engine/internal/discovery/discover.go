// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
	"github.com/Artexis10/endstate/go-engine/internal/packagecatalog"
	"github.com/Artexis10/endstate/go-engine/internal/releaseinputs"
)

// Discover is the production host-discovery entry point used by Linux capture.
func Discover(ctx context.Context, request Request) (Result, error) {
	return DiscoverWithAdapters(ctx, request)
}

// DiscoverWithAdapters adds already-authoritative host sources (for example an
// Endstate-owned Nix profile) to the ordinary native adapter set before one
// deterministic reconciliation pass.
func DiscoverWithAdapters(ctx context.Context, request Request, extraAdapters ...Adapter) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	inputs, err := releaseinputs.Load()
	if err != nil {
		return Result{}, err
	}

	var catalog *packagecatalog.Catalog
	catalogUnavailable := false
	registryUnavailable := false
	var registry *hmregistry.Registry
	root := config.ResolveRepoRoot()
	if root == "" {
		catalogUnavailable = true
		registryUnavailable = true
	} else {
		catalogDir := filepath.Join(root, "catalog", "packages")
		if info, statErr := os.Stat(catalogDir); statErr != nil || !info.IsDir() {
			catalogUnavailable = true
		} else {
			catalog, err = packagecatalog.Load(catalogDir)
			if err != nil {
				return Result{}, err
			}
		}
		registryPath := filepath.Join(root, "catalog", "home-manager", "registry.jsonc")
		if info, statErr := os.Stat(registryPath); statErr != nil || !info.Mode().IsRegular() {
			registryUnavailable = true
		} else {
			registry, err = hmregistry.Load(registryPath)
			if err != nil {
				return Result{}, err
			}
		}
	}

	adapters := []Adapter{
		DebianAdapter{}, RPMAdapter{}, ArchAdapter{}, FlatpakAdapter{}, DesktopAdapter{},
		NixUserProfileAdapter{DefaultInput: inputs.Nixpkgs.FlakeRef},
	}
	if registry != nil {
		home, _ := os.UserHomeDir()
		adapters = append(adapters, HomeManagerAdapter{
			Registry:    registry,
			Environment: config.PathEnvironment{GOOS: runtime.GOOS, Home: home, Env: hostXDGEnvironment()},
		})
	}
	adapters = append(adapters, extraAdapters...)
	result, err := (Orchestrator{
		Adapters:     adapters,
		Catalog:      catalog,
		NixpkgsInput: inputs.Nixpkgs.FlakeRef,
	}).Discover(ctx, request)
	if err != nil {
		return Result{}, err
	}
	if catalogUnavailable {
		result.Warnings = append(result.Warnings, Warning{
			Code:    "package_catalog_unavailable",
			Message: "The reviewed Linux package mapping catalog is unavailable; installed applications remain visible but cannot be made portable.",
		})
	}
	if registryUnavailable {
		result.Warnings = append(result.Warnings, Warning{
			Code:    "home_manager_registry_unavailable",
			Message: "The reviewed Linux settings registry is unavailable; installed settings cannot be captured through Home Manager.",
		})
	}
	return result, nil
}

func hostXDGEnvironment() map[string]string {
	environment := make(map[string]string)
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		if value, ok := os.LookupEnv(name); ok {
			environment[name] = value
		}
	}
	return environment
}
