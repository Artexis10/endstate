// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Command endstate-validation-pilot validates the fixed efficacy-pilot corpus.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/validationpilot"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: endstate-validation-pilot validate-corpus <repository-root> | aggregate <repository-root> <evidence-root> <output>")
		os.Exit(2)
	}
	root, err := filepath.Abs(os.Args[2]); if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
	manifest, err := validationpilot.LoadManifest(filepath.Join(root, "validation", "ci-efficacy", "pilot-v0", "manifest.json"))
	if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
	switch os.Args[1] {
	case "validate-corpus":
		if len(os.Args) != 3 { os.Exit(2) }
		err = validationpilot.ValidateCorpus(root, manifest)
	case "aggregate":
		if len(os.Args) != 5 { os.Exit(2) }
		result, aggregateErr := validationpilot.AggregateArtifacts(manifest, os.Args[3])
		if aggregateErr == nil { data, marshalErr := json.Marshal(result); if marshalErr != nil { aggregateErr = marshalErr } else { aggregateErr = os.WriteFile(os.Args[4], append(data, '\n'), 0o600) }; if result.Decision != validationpilot.DecisionMeaningfulSignal && aggregateErr == nil { aggregateErr = fmt.Errorf("aggregate decision %s", result.Decision) } }
		err = aggregateErr
	default: os.Exit(2)
	}
	if err != nil { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
}
