// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/validationci"
	"github.com/Artexis10/endstate/go-engine/internal/validationharness"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

func TestRunCLIEmitsOneResultAndUsesExactRequest(t *testing.T) {
	args := []string{"--engine", `C:\build\endstate.exe`, "--repo", `C:\repo`, "--module", "apps.fixture", "--scenario", "default-v1", "--result", `C:\tmp\result.json`}
	var got validationharness.Request
	runner := func(_ context.Context, request validationharness.Request) (validationharness.Result, error) {
		got = request
		return validationharness.Result{SchemaVersion: 1, ModuleID: request.ModuleID, ScenarioID: request.ScenarioID,
			Status: validationharness.ResultStatusPassed, ProofLevels: nil, AssertionCounts: map[string]int{}, PhaseTimings: map[string]time.Duration{}}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runCLI(args, &stdout, &stderr, runner); code != 0 {
		t.Fatalf("exit = %d, stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if got.EnginePath != args[1] || got.RepoRoot != args[3] || got.ModuleID != args[5] || got.ScenarioID != args[7] || got.ResultPath != args[9] {
		t.Fatalf("request = %+v", got)
	}
	decodeOneResult(t, stdout.Bytes())
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunCLIFailsClosedAsOneJSONResult(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		runner scenarioRunner
	}{
		{"failed result", []string{"--engine", "x"}, func(context.Context, validationharness.Request) (validationharness.Result, error) {
			return validationharness.Result{SchemaVersion: 1, Status: validationharness.ResultStatusFailed,
				AssertionCounts: map[string]int{}, PhaseTimings: map[string]time.Duration{}}, nil
		}},
		{"runner error", []string{"--engine", "x"}, func(context.Context, validationharness.Request) (validationharness.Result, error) {
			return validationharness.Result{}, errors.New("secret host path")
		}},
		{"unknown flag", []string{"--future"}, func(context.Context, validationharness.Request) (validationharness.Result, error) {
			t.Fatal("runner called for malformed flags")
			return validationharness.Result{}, nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := runCLI(tt.args, &stdout, &stderr, tt.runner); code == 0 {
				t.Fatalf("exit = 0, stdout=%s", stdout.String())
			}
			result := decodeOneResult(t, stdout.Bytes())
			if result.Status != validationharness.ResultStatusFailed || bytes.Contains(stdout.Bytes(), []byte("secret host path")) {
				t.Fatalf("unsafe failed result = %+v, raw=%s", result, stdout.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr = %q", stderr.String())
			}
		})
	}
}

func TestRunCLICommandsKeepsLegacyScenarioFlags(t *testing.T) {
	args := []string{"--engine", `C:\\build\\endstate.exe`, "--repo", `C:\\repo`, "--module", "apps.fixture", "--scenario", "default-v1", "--result", `C:\\tmp\\result.json`}
	called := false
	runner := func(_ context.Context, request validationharness.Request) (validationharness.Result, error) {
		called = true
		return validationharness.Result{SchemaVersion: 1, ModuleID: request.ModuleID, ScenarioID: request.ScenarioID, Status: validationharness.ResultStatusPassed, ProofLevels: []validationmatrix.ProofLevel{validationmatrix.ProofEngineContract}, AssertionCounts: map[string]int{}, PhaseTimings: map[string]time.Duration{}}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runCLICommands(args, &stdout, &stderr, runner); code != 0 || !called {
		t.Fatalf("legacy exit = %d called=%t stdout=%s", code, called, stdout.String())
	}
	decodeOneResult(t, stdout.Bytes())
}

func TestRunCLICommandsDispatchesCanaryFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runCLICommands([]string{"canary", "--unknown"}, &stdout, &stderr, nil); code == 0 {
		t.Fatalf("exit = 0, stdout=%s", stdout.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["failure"] != "invalid canary flags" {
		t.Fatalf("canary dispatch result = %s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunCLICommandsClassifiesCatalogSetupFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	commit := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	code := runCLICommands([]string{
		"catalog",
		"--engine", `C:\missing\endstate.exe`,
		"--repo", `C:\missing\repo`,
		"--commit", commit,
		"--result", `C:\missing\endstate-validation-results\catalog.json`,
	}, &stdout, &stderr, nil)
	if code == 0 {
		t.Fatalf("exit = 0, stdout=%s", stdout.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["schemaVersion"] != float64(1) || result["status"] != validationharness.ResultStatusFailed || result["failure"] == nil || result["failure"] == "" {
		t.Fatalf("unclassified catalog failure = %s", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestValidationCICommandFailureRedactsUnknownPaths(t *testing.T) {
	if got := safeValidationCICommandFailure(errors.New(`persist C:\runner\secret\result.json`)); got != "validation CI command failed" {
		t.Fatalf("unsafe validation CI failure = %q", got)
	}
	if got := safeValidationCICommandFailure(errors.New("engine authority is unsafe")); got != "engine authority is unsafe" {
		t.Fatalf("classified validation CI failure = %q", got)
	}
	if got := safeValidationCICommandFailure(errors.New("failed shard evidence")); got != "failed shard evidence" {
		t.Fatalf("failed shard classification = %q", got)
	}
}

func TestAggregateCommandFailurePreservesClassifiedEvidence(t *testing.T) {
	request := validationci.AggregateRequest{Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	classified := validationci.AggregateResult{
		SchemaVersion: validationci.SchemaVersion,
		Commit:        request.Commit,
		Status:        validationharness.ResultStatusFailed,
		Modules:       validationci.PassedEligible{Passed: 3, Eligible: 4},
		Scenarios:     validationci.PassedEligible{Passed: 8, Eligible: 9},
		Failure:       "foreign shard evidence",
	}
	got := aggregateCommandFailure(request, classified, errors.New("foreign shard evidence"))
	if got.Modules != classified.Modules || got.Scenarios != classified.Scenarios || got.Failure != classified.Failure {
		t.Fatalf("classified aggregate evidence was replaced: got=%+v want=%+v", got, classified)
	}

	got = aggregateCommandFailure(request, validationci.AggregateResult{}, errors.New(`open C:\runner\secret\aggregate.json`))
	if got.SchemaVersion != validationci.SchemaVersion || got.Commit != request.Commit || got.Status != validationharness.ResultStatusFailed || got.Failure != "validation CI command failed" {
		t.Fatalf("unclassified aggregate setup failure = %+v", got)
	}
}

func decodeOneResult(t *testing.T, data []byte) validationharness.Result {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	var result validationharness.Result
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatalf("stdout contains more than one JSON value: %q", data)
	}
	return result
}
