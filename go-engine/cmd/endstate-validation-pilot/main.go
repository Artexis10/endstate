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

var (
	loadV0Manifest              = validationpilot.LoadManifest
	validateV0DetectorAuthority = validationpilot.ValidateDetectorAuthority
	validateV0Corpus            = validationpilot.ValidateCorpus
	runV0Detector               = validationpilot.RunDetector
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) < 1 {
		return 2
	}
	switch args[0] {
	case "validate-v1":
		return runValidateV1(args[1:])
	case "run-v1-lane":
		return runV1Lane(args[1:])
	case "aggregate-v1":
		return runAggregateV1(args[1:])
	case "detector":
		return rejectV0Detector()
	case "infrastructure":
		return runInfrastructure(args[1:])
	}
	if len(args) < 2 {
		return 2
	}
	return runV0(args)
}

func runV0(args []string) int {
	if len(args) < 1 {
		return 2
	}
	if args[0] == "detector" {
		return rejectV0Detector()
	}
	if len(args) < 2 {
		return 2
	}
	if args[0] == "infrastructure" {
		return runInfrastructure(args[1:])
	}
	root, err := filepath.Abs(args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	manifest, err := loadV0Manifest(filepath.Join(root, "validation", "ci-efficacy", "pilot-v0", "manifest.json"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	switch args[0] {
	case "validate-corpus":
		if len(args) != 2 {
			return 2
		}
		err = validateV0DetectorAuthority(root, manifest)
		if err == nil {
			err = validateV0Corpus(root, manifest)
		}
	case "aggregate":
		if len(args) != 4 {
			return 2
		}
		result, aggregateErr := validationpilot.AggregateArtifacts(manifest, args[2])
		if aggregateErr == nil {
			data, marshalErr := json.Marshal(result)
			if marshalErr != nil {
				aggregateErr = marshalErr
			} else if writeErr := os.WriteFile(args[3], append(data, '\n'), 0o600); writeErr != nil {
				aggregateErr = writeErr
			} else if result.Decision != validationpilot.DecisionMeaningfulSignal {
				aggregateErr = fmt.Errorf("aggregate decision %s", result.Decision)
			}
		}
		err = aggregateErr
	default:
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runValidateV1(args []string) int {
	flags := flag.NewFlagSet("endstate-validation-pilot validate-v1", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var root, manifest, dispatchCommit string
	flags.StringVar(&root, "root", "", "absolute dispatch repository root")
	flags.StringVar(&manifest, "manifest", "", "absolute v1 manifest path")
	flags.StringVar(&dispatchCommit, "dispatch-commit", "", "trusted exact GitHub dispatch commit")
	if flags.Parse(args) != nil || flags.NArg() != 0 || root == "" || manifest == "" || dispatchCommit == "" {
		return 2
	}
	if _, err := validationpilot.ValidateV1Repository(root, manifest, dispatchCommit); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runV1Lane(args []string) int {
	flags := flag.NewFlagSet("endstate-validation-pilot run-v1-lane", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var root, manifest, dispatchCommit, role, lane, runnerRoot, runnerFamily, runnerImageOS, runnerImageVersion, goCache, goModCache, resultRoot string
	flags.StringVar(&root, "root", "", "absolute dispatch repository root")
	flags.StringVar(&manifest, "manifest", "", "absolute v1 manifest path")
	flags.StringVar(&dispatchCommit, "dispatch-commit", "", "trusted exact GitHub dispatch commit")
	flags.StringVar(&role, "role", "", "fixed v1 role")
	flags.StringVar(&lane, "lane", "", "fixed v1 lane")
	flags.StringVar(&runnerRoot, "runner-root", "", "absolute runner-owned root")
	flags.StringVar(&runnerFamily, "runner-family", "", "runner family")
	flags.StringVar(&runnerImageOS, "runner-image-os", "", "hosted ImageOS metadata")
	flags.StringVar(&runnerImageVersion, "runner-image-version", "", "hosted ImageVersion metadata")
	flags.StringVar(&goCache, "go-cache", "", "shared job GOCACHE")
	flags.StringVar(&goModCache, "go-mod-cache", "", "shared job GOMODCACHE")
	flags.StringVar(&resultRoot, "result-root", "", "fresh runner-owned result root")
	if flags.Parse(args) != nil || flags.NArg() != 0 || root == "" || manifest == "" || dispatchCommit == "" || role == "" || lane == "" || runnerRoot == "" || runnerFamily == "" || runnerImageOS == "" || runnerImageVersion == "" || goCache == "" || goModCache == "" || resultRoot == "" {
		return 2
	}
	runner, err := validationpilot.HostedV1Runner(runnerFamily, runnerImageOS, runnerImageVersion)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	v1Manifest, err := validationpilot.ValidateV1Repository(root, manifest, dispatchCommit)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := validationpilot.RunV1Lane(context.Background(), validationpilot.V1LaneRequest{Root: root, RunnerRoot: runnerRoot, GoCache: goCache, GoModCache: goModCache, Manifest: v1Manifest, DispatchCommit: dispatchCommit, Role: role, Lane: lane, Runner: runner, ResultRoot: resultRoot}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runAggregateV1(args []string) int {
	flags := flag.NewFlagSet("endstate-validation-pilot aggregate-v1", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	var manifestPath, dispatchCommit, evidenceRoot, output string
	flags.StringVar(&manifestPath, "manifest", "", "absolute v1 manifest path")
	flags.StringVar(&dispatchCommit, "dispatch-commit", "", "trusted exact GitHub dispatch commit")
	flags.StringVar(&evidenceRoot, "evidence-root", "", "absolute fixed evidence root")
	flags.StringVar(&output, "output", "", "absolute aggregate output path")
	if flags.Parse(args) != nil || flags.NArg() != 0 || manifestPath == "" || dispatchCommit == "" || evidenceRoot == "" || output == "" {
		return 2
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(manifestPath))))
	manifest, err := validationpilot.ValidateV1Repository(root, manifestPath, dispatchCommit)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	aggregate, aggregateErr := validationpilot.AggregateV1Evidence(manifest, dispatchCommit, evidenceRoot)
	if aggregateErr != nil {
		aggregate = inconclusiveV1Aggregate(manifest)
	}
	if writeErr := validationpilot.WriteV1AggregateNew(output, aggregate); writeErr != nil {
		fmt.Fprintln(os.Stderr, writeErr)
		return 1
	}
	if aggregateErr != nil || aggregate.Decision != validationpilot.DecisionMeaningfulSignal {
		if aggregateErr != nil {
			fmt.Fprintln(os.Stderr, aggregateErr)
		} else {
			fmt.Fprintln(os.Stderr, "aggregate decision "+aggregate.Decision)
		}
		return 1
	}
	return 0
}

func inconclusiveV1Aggregate(manifest validationpilot.V1Manifest) validationpilot.V1Aggregate {
	aggregate := validationpilot.V1Aggregate{SchemaVersion: validationpilot.V1SchemaVersion, Decision: validationpilot.DecisionInconclusive}
	for _, candidate := range manifest.Candidates {
		aggregate.Rows = append(aggregate.Rows, validationpilot.V1AggregateRow{ID: candidate.ID, Classification: validationpilot.ClassificationInfrastructureFailure})
		aggregate.InfrastructureFailures++
	}
	return aggregate
}

func rejectV0Detector() int {
	fmt.Fprintln(os.Stderr, "v0 detector command is decommissioned")
	return 1
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
