// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Command endstate-validation-pilot validates the fixed efficacy-pilot corpus.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/validationpilot"
)

func main() {
	if len(os.Args) != 3 || os.Args[1] != "validate-corpus" {
		fmt.Fprintln(os.Stderr, "usage: endstate-validation-pilot validate-corpus <repository-root>")
		os.Exit(2)
	}
	root, err := filepath.Abs(os.Args[2])
	if err == nil {
		manifest, loadErr := validationpilot.LoadManifest(filepath.Join(root, "validation", "ci-efficacy", "pilot-v0", "manifest.json"))
		if loadErr != nil { err = loadErr } else { err = validationpilot.ValidateCorpus(root, manifest) }
	}
	if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
}
