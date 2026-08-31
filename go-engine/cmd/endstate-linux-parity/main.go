// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// endstate-linux-parity renders the release-time applicability matrix. It is a
// catalog audit tool, not part of ordinary capture or apply.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
	"github.com/Artexis10/endstate/go-engine/internal/linuxparity"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/packagecatalog"
)

func main() {
	repo := flag.String("repo", "", "Endstate repository root (defaults to normal repository resolution)")
	requireReady := flag.Bool("require-ready", false, "exit non-zero unless every application module is supported or reviewed not-applicable")
	flag.Parse()
	if err := run(*repo, *requireReady); err != nil {
		fmt.Fprintln(os.Stderr, "endstate-linux-parity:", err)
		os.Exit(1)
	}
}

func run(repo string, requireReady bool) error {
	if repo == "" {
		repo = config.ResolveRepoRoot()
	}
	if repo == "" {
		return fmt.Errorf("repository root is unavailable; pass --repo")
	}
	moduleCatalog, err := modules.GetCatalog(repo)
	if err != nil {
		return err
	}
	packages, err := packagecatalog.Load(filepath.Join(repo, "catalog", "packages"))
	if err != nil {
		return err
	}
	registry, err := hmregistry.Load(filepath.Join(repo, "catalog", "home-manager", "registry.jsonc"))
	if err != nil {
		return err
	}
	matrix, err := linuxparity.Build(moduleCatalog, packages.Entries(), registry)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(matrix, "", "  ")
	if err != nil {
		return err
	}
	if _, err := os.Stdout.Write(append(encoded, '\n')); err != nil {
		return err
	}
	if requireReady {
		return matrix.RequireReady()
	}
	return nil
}
