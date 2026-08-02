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
	if len(args) == 0 || (args[0] != "shard" && args[0] != "catalog" && args[0] != "canary" && args[0] != "aggregate" && args[0] != "sync-revisions") {
		return runCLI(args, stdout, stderr, runner)
	}
	switch args[0] {
	case "shard":
		return runShard(args[1:], stdout, runner)
	case "catalog":
		return runCatalog(args[1:], stdout)
	case "canary":
		return runCanary(args[1:], stdout, runner)
	case "sync-revisions":
		return runSyncRevisions(args[1:], stdout)
	default:
		return runAggregate(args[1:], stdout)
	}
}

func runSyncRevisions(args []string, stdout io.Writer) int {
	flags := flag.NewFlagSet("endstate-validation sync-revisions", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	repoRoot := ""
	write := false
	flags.StringVar(&repoRoot, "repo", "", "absolute repository root")
	flags.BoolVar(&write, "write", false, "update stale revision pins")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || repoRoot == "" {
		return writeCommandError(stdout, "invalid sync-revisions flags")
	}
	result, err := validationmatrix.SyncRevisions(repoRoot, write, time.Now().UTC())
	if err != nil {
		failure := "invalid sync-revisions catalog"
		if validationmatrix.ErrorCode(err) == validationmatrix.CodeStaleSidecar {
			failure = "stale validation sidecars"
		}
		return writeJSON(stdout, map[string]any{
			"schemaVersion": 1, "status": validationharness.ResultStatusFailed,
			"stale": result.Stale, "updated": result.Updated, "failure": failure,
		}, true)
	}
	return writeJSON(stdout, map[string]any{
		"schemaVersion": 1, "status": validationharness.ResultStatusPassed,
		"stale": result.Stale, "updated": result.Updated,
	}, false)
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
		shardCount := request.ShardCount
		if shardCount == 0 {
			shardCount = validationci.ShardCount
		}
		result = validationci.ShardResult{
			SchemaVersion: validationci.SchemaVersion,
			Commit:        request.Commit,
			ShardCount:    shardCount,
			Shard:         request.Shard,
			Status:        validationharness.ResultStatusFailed,
			Rows:          []validationci.ShardRow{},
			Failure:       safeValidationCICommandFailure(err),
		}
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
		result = validationci.CanaryResult{
			SchemaVersion: validationci.SchemaVersion,
			Commit:        request.Commit,
			Status:        validationharness.ResultStatusFailed,
			Failure:       safeValidationCICommandFailure(err),
		}
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
		result = validationci.CatalogResult{
			SchemaVersion: validationci.SchemaVersion,
			Commit:        request.Commit,
			Status:        validationharness.ResultStatusFailed,
			Failure:       safeValidationCICommandFailure(err),
		}
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
		result = aggregateCommandFailure(request, result, err)
		return writeJSON(stdout, result, true)
	}
	return writeJSON(stdout, result, result.Status != validationharness.ResultStatusPassed)
}

func aggregateCommandFailure(request validationci.AggregateRequest, result validationci.AggregateResult, err error) validationci.AggregateResult {
	if result.SchemaVersion == validationci.SchemaVersion {
		result.Status = validationharness.ResultStatusFailed
		if result.Failure == "" {
			result.Failure = safeValidationCICommandFailure(err)
		}
		return result
	}
	return validationci.AggregateResult{
		SchemaVersion: validationci.SchemaVersion,
		Commit:        request.Commit,
		Status:        validationharness.ResultStatusFailed,
		Failure:       safeValidationCICommandFailure(err),
	}
}

// safeValidationCICommandFailure exposes only fixed, path-free classifications
// emitted by validationci. Unexpected I/O details stay private to the runner.
func safeValidationCICommandFailure(err error) string {
	if err == nil {
		return "validation CI command failed"
	}
	switch detail := err.Error(); detail {
	case "invalid shard bounds",
		"read engine identity",
		"read repository identity",
		"load production catalog",
		"duplicate planned row",
		"row result identity drift",
		"impossible proof or status combination",
		"engine changed during shard",
		"repository changed during shard",
		"duplicate planned canary",
		"missing planned canary",
		"impossible canary proof or status combination",
		"engine changed during canary",
		"repository changed during canary",
		"catalog harness I/O failure",
		"engine changed during catalog",
		"repository changed during catalog",
		"commit must be exact lowercase SHA-1",
		"engine and repository must be canonical absolute paths",
		"engine authority is unsafe",
		"repository authority is unsafe",
		"input directory is outside runner temp",
		"input directory is unsafe",
		"read input directory",
		"evidence inventory is incomplete or has extra files",
		"evidence inventory contains unsafe entry",
		"evidence inventory has unexpected file",
		"result path is unsafe",
		"result directory is unsafe",
		"missing or malformed shard evidence",
		"foreign shard evidence",
		"failed shard evidence",
		"row proof identity drift",
		"duplicate row evidence",
		"failed row evidence",
		"missing row evidence",
		"missing or failed catalog evidence",
		"catalog bundle count drift",
		"missing or failed synthetic canary":
		return detail
	default:
		return "validation CI command failed"
	}
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
