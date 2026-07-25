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

	"github.com/Artexis10/endstate/go-engine/internal/validationharness"
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
