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

	"github.com/Artexis10/endstate/go-engine/internal/validationharness"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

type scenarioRunner func(context.Context, validationharness.Request) (validationharness.Result, error)

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr, validationharness.Run))
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
