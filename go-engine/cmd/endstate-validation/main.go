// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Command endstate-validation executes one repository-declared synthetic
// validation scenario through an already-built Endstate engine.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/validationci"
	"github.com/Artexis10/endstate/go-engine/internal/validationharness"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

type scenarioRunner func(context.Context, validationharness.Request) (validationharness.Result, error)

func main() {
	os.Exit(runCLICommands(os.Args[1:], os.Stdout, os.Stderr, validationharness.Run))
}

// runCLICommands preserves the original no-subcommand byte contract. New
// subcommands deliberately have their own compact evidence JSON contracts.
func runCLICommands(args []string, stdout, stderr io.Writer, runner scenarioRunner) int {
	if len(args) == 0 || (args[0] != "shard" && args[0] != "catalog" && args[0] != "canary" && args[0] != "aggregate") {
		return runCLI(args, stdout, stderr, runner)
	}
	switch args[0] {
	case "shard":
		return runShard(args[1:], stdout, runner)
	case "catalog":
		return runCatalog(args[1:], stdout)
	case "canary":
		return runCanary(args[1:], stdout, runner)
	default:
		return runAggregate(args[1:], stdout)
	}
}

func runShard(args []string, stdout io.Writer, runner scenarioRunner) int {
	flags := flag.NewFlagSet("endstate-validation shard", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	request := validationci.ShardRequest{}
	flags.StringVar(&request.EnginePath, "engine", "", "absolute built engine path")
	flags.StringVar(&request.RepoRoot, "repo", "", "absolute repository root")
	flags.StringVar(&request.Commit, "commit", "", "exact checked-out commit")
	flags.IntVar(&request.ShardCount, "shards", validationci.ShardCount, "exact shard count")
	flags.IntVar(&request.Shard, "shard", -1, "zero-based shard index")
	flags.StringVar(&request.ResultPath, "result", "", "compact result path")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return writeCommandError(stdout, "invalid shard flags")
	}
	request.Run = validationci.ScenarioRunner(runner)
	result, err := validationci.RunSyntheticShard(request)
	if err != nil {
		return writeJSON(stdout, result, true)
	}
	return writeJSON(stdout, result, result.Status != validationharness.ResultStatusPassed)
}

func runCanary(args []string, stdout io.Writer, runner scenarioRunner) int {
	flags := flag.NewFlagSet("endstate-validation canary", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	request := validationci.CanaryRequest{Run: validationci.ScenarioRunner(runner)}
	flags.StringVar(&request.EnginePath, "engine", "", "absolute built engine path")
	flags.StringVar(&request.RepoRoot, "repo", "", "absolute repository root")
	flags.StringVar(&request.Commit, "commit", "", "exact checked-out commit")
	flags.StringVar(&request.ResultPath, "result", "", "compact result path")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return writeCommandError(stdout, "invalid canary flags")
	}
	result, err := validationci.RunCanary(request)
	if err != nil {
		return writeJSON(stdout, result, true)
	}
	return writeJSON(stdout, result, result.Status != validationharness.ResultStatusPassed)
}

func runCatalog(args []string, stdout io.Writer) int {
	flags := flag.NewFlagSet("endstate-validation catalog", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	request := validationci.CatalogRequest{}
	flags.StringVar(&request.EnginePath, "engine", "", "absolute built engine path")
	flags.StringVar(&request.RepoRoot, "repo", "", "absolute repository root")
	flags.StringVar(&request.Commit, "commit", "", "exact checked-out commit")
	flags.StringVar(&request.ResultPath, "result", "", "compact result path")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return writeCommandError(stdout, "invalid catalog flags")
	}
	result, err := validationci.RunCatalog(request)
	if err != nil {
		return writeJSON(stdout, result, true)
	}
	return writeJSON(stdout, result, result.Status != validationharness.ResultStatusPassed)
}

func runAggregate(args []string, stdout io.Writer) int {
	flags := flag.NewFlagSet("endstate-validation aggregate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	request := validationci.AggregateRequest{}
	flags.StringVar(&request.EnginePath, "engine", "", "absolute built engine path")
	flags.StringVar(&request.RepoRoot, "repo", "", "absolute repository root")
	flags.StringVar(&request.Commit, "commit", "", "exact checked-out commit")
	flags.StringVar(&request.InputDir, "input", "", "runner-temp evidence directory")
	flags.StringVar(&request.ResultPath, "result", "", "compact result path")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return writeCommandError(stdout, "invalid aggregate flags")
	}
	result, err := validationci.Aggregate(request)
	if err != nil {
		return writeJSON(stdout, result, true)
	}
	return writeJSON(stdout, result, result.Status != validationharness.ResultStatusPassed)
}

func writeCommandError(stdout io.Writer, detail string) int {
	return writeJSON(stdout, map[string]any{"schemaVersion": 1, "status": validationharness.ResultStatusFailed, "failure": detail}, true)
}
func writeJSON(stdout io.Writer, value any, failed bool) int {
	if err := json.NewEncoder(stdout).Encode(value); err != nil || failed {
		return 1
	}
	return 0
}

func runCLI(args []string, stdout, _ io.Writer, runner scenarioRunner) int {
	request := validationharness.Request{}
	flags := flag.NewFlagSet("endstate-validation", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&request.EnginePath, "engine", "", "absolute path to the built Endstate engine")
	flags.StringVar(&request.RepoRoot, "repo", "", "absolute repository root")
	flags.StringVar(&request.ModuleID, "module", "", "exact production module ID")
	flags.StringVar(&request.ScenarioID, "scenario", "", "exact declared scenario ID")
	flags.StringVar(&request.ResultPath, "result", "", "absolute validation-owned JSON result path")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		_ = writeResult(stdout, commandFailure(request, "flags", "invalid command flags"))
		return 1
	}
	result, err := runner(context.Background(), request)
	if err != nil {
		result = commandFailure(request, "io", "validation harness I/O failure")
	}
	normalizeResult(&result)
	code := writeResult(stdout, result)
	if code != 0 || result.Status != validationharness.ResultStatusPassed {
		return 1
	}
	return 0
}

func commandFailure(request validationharness.Request, coordinate, detail string) validationharness.Result {
	return validationharness.Result{
		SchemaVersion: validationharness.ResultSchemaVersion,
		ModuleID:      request.ModuleID, ScenarioID: request.ScenarioID,
		Status:      validationharness.ResultStatusFailed,
		ProofLevels: []validationmatrix.ProofLevel{}, AssertionCounts: map[string]int{},
		Failure:      &validationharness.Failure{Code: validationharness.CodeExecutionFailure, Phase: "harness", Coordinate: coordinate, Detail: detail},
		PhaseTimings: map[string]time.Duration{},
	}
}

func normalizeResult(result *validationharness.Result) {
	if result.SchemaVersion == 0 {
		result.SchemaVersion = validationharness.ResultSchemaVersion
	}
	if result.ProofLevels == nil {
		result.ProofLevels = []validationmatrix.ProofLevel{}
	}
	if result.AssertionCounts == nil {
		result.AssertionCounts = map[string]int{}
	}
	if result.PhaseTimings == nil {
		result.PhaseTimings = map[string]time.Duration{}
	}
	if result.Status != validationharness.ResultStatusPassed {
		result.Status = validationharness.ResultStatusFailed
		result.ProofLevels = []validationmatrix.ProofLevel{}
		if result.Failure == nil {
			result.Failure = &validationharness.Failure{Code: validationharness.CodeExecutionFailure, Phase: "harness", Coordinate: "result", Detail: "validation failed without a classified result"}
		}
	}
}

func writeResult(stdout io.Writer, result validationharness.Result) int {
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		return 1
	}
	return 0
}
