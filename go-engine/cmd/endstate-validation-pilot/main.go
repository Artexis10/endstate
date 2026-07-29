// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Command endstate-validation-pilot validates the fixed efficacy-pilot corpus.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/validationpilot"
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(2)
	}
	if os.Args[1] == "detector" {
		os.Exit(runDetector(os.Args[2:]))
	}
	if os.Args[1] == "infrastructure" {
		os.Exit(runInfrastructure(os.Args[2:]))
	}
	if len(os.Args) < 3 {
		os.Exit(2)
	}
	root, err := filepath.Abs(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	manifest, err := validationpilot.LoadManifest(filepath.Join(root, "validation", "ci-efficacy", "pilot-v0", "manifest.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	switch os.Args[1] {
	case "validate-corpus":
		if len(os.Args) != 3 {
			os.Exit(2)
		}
		err = validationpilot.ValidateCorpus(root, manifest)
	case "aggregate":
		if len(os.Args) != 5 {
			os.Exit(2)
		}
		result, aggregateErr := validationpilot.AggregateArtifacts(manifest, os.Args[3])
		if aggregateErr == nil {
			data, marshalErr := json.Marshal(result)
			if marshalErr != nil {
				aggregateErr = marshalErr
			} else if writeErr := os.WriteFile(os.Args[4], append(data, '\n'), 0o600); writeErr != nil {
				aggregateErr = writeErr
			} else if result.Decision != validationpilot.DecisionMeaningfulSignal {
				aggregateErr = fmt.Errorf("aggregate decision %s", result.Decision)
			}
		}
		err = aggregateErr
	default:
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runDetector(args []string) int {
	flags := flag.NewFlagSet("endstate-validation-pilot detector", flag.ContinueOnError)
	request := validationpilot.DetectorRequest{}
	var output string
	flags.BoolVar(&request.Catalog, "catalog", false, "run the catalog matrix")
	flags.StringVar(&request.EnginePath, "engine", "", "absolute engine path")
	flags.StringVar(&request.RepoRoot, "repo", "", "absolute repository path")
	flags.StringVar(&request.Commit, "commit", "", "fixed detector commit")
	flags.StringVar(&request.ModuleID, "module", "", "module id")
	flags.StringVar(&request.ScenarioID, "scenario", "", "scenario id")
	flags.StringVar(&output, "output", "", "structured result path")
	if flags.Parse(args) != nil || output == "" || request.EnginePath == "" || request.RepoRoot == "" || (!request.Catalog && (request.ModuleID == "" || request.ScenarioID == "")) {
		return 2
	}
	request.ResultPath = output
	attempt, err := validationpilot.RunDetector(context.Background(), request)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	data, err := json.Marshal(attempt)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(output, append(data, '\n'), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if attempt.Status != "passed" {
		return 1
	}
	return 0
}

func runInfrastructure(args []string) int {
	flags := flag.NewFlagSet("endstate-validation-pilot infrastructure", flag.ContinueOnError)
	var module, scenario, output, reason string
	flags.StringVar(&module, "module", "", "module id")
	flags.StringVar(&scenario, "scenario", "", "scenario id")
	flags.StringVar(&output, "output", "", "structured result path")
	flags.StringVar(&reason, "reason", "detector setup failed", "bounded infrastructure reason")
	if flags.Parse(args) != nil || output == "" {
		return 2
	}
	data, err := json.Marshal(validationpilot.InfrastructureAttempt(module, scenario, reason))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(output, append(data, '\n'), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
