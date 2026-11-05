// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/packagecatalog"
	"github.com/Artexis10/endstate/go-engine/internal/releaseinputs"
)

//go:embed nix_catalog.nix
var nixCatalogExpression string

func runNixMappings(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("endstate-validation nix-mappings", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	repo, system := "", ""
	flags.StringVar(&repo, "repo", "", "repository root")
	flags.StringVar(&system, "system", "", "Nix target system")
	// These are the two Linux architecture targets fixed by the release contract.
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || repo == "" || (system != "x86_64-linux" && system != "aarch64-linux") {
		return writeCommandError(stdout, "invalid nix-mappings flags")
	}
	catalog, err := packagecatalog.Load(filepath.Join(repo, "catalog", "packages"))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return writeCommandError(stdout, "invalid package catalog")
	}
	entries := catalog.Entries()
	if len(entries) == 0 {
		return writeCommandError(stdout, "empty package catalog")
	}
	inputs, err := releaseinputs.Load()
	if err != nil {
		return writeCommandError(stdout, "invalid release inputs")
	}
	// Pass data through native string arguments, never through Nix source interpolation.
	catalogJSON, err := json.Marshal(entries)
	if err != nil {
		return writeCommandError(stdout, "cannot encode package catalog")
	}
	inputJSON, err := json.Marshal(inputs.Nixpkgs)
	if err != nil {
		return writeCommandError(stdout, "cannot encode release input")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, "nix", "--extra-experimental-features", "nix-command flakes", "eval", "--json", "--expr", nixCatalogExpression,
		"--argstr", "system", system, "--argstr", "inputJSON", string(inputJSON), "--argstr", "catalogJSON", string(catalogJSON), "result")
	var result bytes.Buffer
	command.Stdout, command.Stderr = &result, stderr
	if err := command.Run(); err != nil {
		fmt.Fprintln(stderr, err)
		return writeCommandError(stdout, "Nix mapping evaluation failed")
	}
	if _, err := stdout.Write(result.Bytes()); err != nil {
		return 1
	}
	return 0
}
