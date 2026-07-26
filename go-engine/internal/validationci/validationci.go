// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package validationci owns compact, commit-bound CI evidence for the existing
// production validation planner and harness.
package validationci

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationharness"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

const (
	SchemaVersion = 1
	ShardCount    = 8
	maxResultSize = 64 * 1024
)

var commitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

type ScenarioRunner func(context.Context, validationharness.Request) (validationharness.Result, error)

type ShardRequest struct {
	EnginePath string
	RepoRoot   string
	Commit     string
	ShardCount int
	Shard      int
	ResultPath string
	Run        ScenarioRunner
}

type ShardResult struct {
	SchemaVersion  int        `json:"schemaVersion"`
	Commit         string     `json:"commit"`
	EngineSHA256   string     `json:"engineSha256"`
	RepositoryHash string     `json:"repositoryHash"`
	ShardCount     int        `json:"shardCount"`
	Shard          int        `json:"shard"`
	Status         string     `json:"status"`
	Rows           []ShardRow `json:"rows"`
	Failure        string     `json:"failure,omitempty"`
}

type ShardRow struct {
	Identity validationmatrix.SyntheticRow `json:"identity"`
	Result   validationharness.Result      `json:"result"`
}

type CanaryRequest struct {
	EnginePath string
	RepoRoot   string
	Commit     string
	ResultPath string
	Run        ScenarioRunner
}

// CanaryResult is the compact, commit-bound wrapper for the fixed synthetic
// Notepad++ scenario. It deliberately retains the planned row identity.
type CanaryResult struct {
	SchemaVersion  int                           `json:"schemaVersion"`
	Commit         string                        `json:"commit"`
	EngineSHA256   string                        `json:"engineSha256"`
	RepositoryHash string                        `json:"repositoryHash"`
	Identity       validationmatrix.SyntheticRow `json:"identity"`
	Result         validationharness.Result      `json:"result"`
	Status         string                        `json:"status"`
	Failure        string                        `json:"failure,omitempty"`
}

type CatalogRequest struct {
	EnginePath string
	RepoRoot   string
	Commit     string
	ResultPath string
}

type CatalogResult struct {
	SchemaVersion  int    `json:"schemaVersion"`
	Commit         string `json:"commit"`
	EngineSHA256   string `json:"engineSha256"`
	RepositoryHash string `json:"repositoryHash"`
	Status         string `json:"status"`
	CatalogCount   int    `json:"catalogCount"`
	Passed         int    `json:"passed"`
	Failed         int    `json:"failed"`
	Memberships    int    `json:"memberships"`
	UniqueModules  int    `json:"uniqueModules"`
	Failure        string `json:"failure,omitempty"`
}

type AggregateRequest struct {
	EnginePath string
	RepoRoot   string
	Commit     string
	InputDir   string
	ResultPath string
}

type AggregateResult struct {
	SchemaVersion int            `json:"schemaVersion"`
	Commit        string         `json:"commit"`
	Status        string         `json:"status"`
	Modules       PassedEligible `json:"modules"`
	Scenarios     PassedEligible `json:"scenarios"`
	Bundles       PassedEligible `json:"bundles"`
	Failure       string         `json:"failure,omitempty"`
}

type PassedEligible struct {
	Passed   int `json:"passed"`
	Eligible int `json:"eligible"`
}

func RunSyntheticShard(request ShardRequest) (ShardResult, error) {
	count := request.ShardCount
	if count == 0 {
		count = ShardCount
	}
	if count < 1 || count > validationmatrix.MaxSyntheticShardCount || request.Shard < 0 || request.Shard >= count {
		return ShardResult{}, errors.New("invalid shard bounds")
	}
	if err := validateAuthority(request.EnginePath, request.RepoRoot, request.Commit, request.ResultPath, fmt.Sprintf("shard-%d.json", request.Shard)); err != nil {
		return ShardResult{}, err
	}
	engineHash, err := fileSHA256(request.EnginePath)
	if err != nil {
		return ShardResult{}, errors.New("read engine identity")
	}
	repositoryHash, err := repositorySHA256(request.RepoRoot)
	if err != nil {
		return ShardResult{}, errors.New("read repository identity")
	}
	catalog, err := validationmatrix.LoadCatalog(request.RepoRoot, time.Now().UTC())
	if err != nil {
		return ShardResult{}, errors.New("load production catalog")
	}
	plan, err := validationmatrix.PlanSynthetic(catalog, validationmatrix.SyntheticPlanOptions{ShardCount: count})
	if err != nil {
		return ShardResult{}, err
	}
	result := ShardResult{SchemaVersion: SchemaVersion, Commit: request.Commit, EngineSHA256: engineHash, RepositoryHash: repositoryHash, ShardCount: count, Shard: request.Shard, Status: validationharness.ResultStatusPassed, Rows: []ShardRow{}}
	run := request.Run
	if run == nil {
		run = validationharness.Run
	}
	seen := map[string]struct{}{}
	rowNumber := 0
	for _, row := range plan.Rows {
		if row.Shard != request.Shard {
			continue
		}
		key := row.ModuleID + "\x00" + row.ModuleRevision + "\x00" + row.ScenarioID + "\x00" + row.ScenarioDigest
		if _, duplicate := seen[key]; duplicate {
			return ShardResult{}, errors.New("duplicate planned row")
		}
		seen[key] = struct{}{}
		rowPath := fmt.Sprintf("row-%d.json", rowNumber)
		rowNumber++
		harnessResult, runErr := runHarnessScenario(run, request.EnginePath, request.RepoRoot, row.ModuleID, row.ScenarioID, rowPath)
		if runErr != nil {
			harnessResult = failedRow(row, "harness I/O failure")
		}
		if !matchesRow(row, harnessResult) {
			return ShardResult{}, errors.New("row result identity drift")
		}
		if !validRowResult(row, harnessResult) {
			return ShardResult{}, errors.New("impossible proof or status combination")
		}
		result.Rows = append(result.Rows, ShardRow{Identity: row, Result: harnessResult})
		if harnessResult.Status != validationharness.ResultStatusPassed {
			result.Status = validationharness.ResultStatusFailed
			result.Failure = "scenario failed"
		}
	}
	if after, afterErr := fileSHA256(request.EnginePath); afterErr != nil || after != engineHash {
		return ShardResult{}, errors.New("engine changed during shard")
	}
	if after, afterErr := repositorySHA256(request.RepoRoot); afterErr != nil || after != repositoryHash {
		return ShardResult{}, errors.New("repository changed during shard")
	}
	if err := persist(request.ResultPath, result); err != nil {
		return ShardResult{}, err
	}
	return result, nil
}

func RunCanary(request CanaryRequest) (CanaryResult, error) {
	if err := validateAuthority(request.EnginePath, request.RepoRoot, request.Commit, request.ResultPath, "canary.json"); err != nil {
		return CanaryResult{}, err
	}
	engineHash, err := fileSHA256(request.EnginePath)
	if err != nil {
		return CanaryResult{}, errors.New("read engine identity")
	}
	repositoryHash, err := repositorySHA256(request.RepoRoot)
	if err != nil {
		return CanaryResult{}, errors.New("read repository identity")
	}
	catalog, err := validationmatrix.LoadCatalog(request.RepoRoot, time.Now().UTC())
	if err != nil {
		return CanaryResult{}, errors.New("load production catalog")
	}
	plan, err := validationmatrix.PlanSynthetic(catalog, validationmatrix.SyntheticPlanOptions{ShardCount: ShardCount})
	if err != nil {
		return CanaryResult{}, err
	}
	var row validationmatrix.SyntheticRow
	for _, candidate := range plan.Rows {
		if candidate.ModuleID == "apps.notepad-plus-plus" && candidate.ScenarioID == "default-v1" {
			if row.ModuleID != "" {
				return CanaryResult{}, errors.New("duplicate planned canary")
			}
			row = candidate
		}
	}
	if row.ModuleID == "" {
		return CanaryResult{}, errors.New("missing planned canary")
	}
	run := request.Run
	if run == nil {
		run = validationharness.Run
	}
	harnessResult, runErr := runHarnessScenario(run, request.EnginePath, request.RepoRoot, row.ModuleID, row.ScenarioID, "canary-harness.json")
	if runErr != nil {
		harnessResult = failedRow(row, "harness I/O failure")
	}
	if !matchesRow(row, harnessResult) || !validRowResult(row, harnessResult) {
		return CanaryResult{}, errors.New("impossible canary proof or status combination")
	}
	result := CanaryResult{SchemaVersion: SchemaVersion, Commit: request.Commit, EngineSHA256: engineHash, RepositoryHash: repositoryHash, Identity: row, Result: harnessResult, Status: harnessResult.Status}
	if result.Status != validationharness.ResultStatusPassed {
		result.Failure = "scenario failed"
	}
	if after, afterErr := fileSHA256(request.EnginePath); afterErr != nil || after != engineHash {
		return CanaryResult{}, errors.New("engine changed during canary")
	}
	if after, afterErr := repositorySHA256(request.RepoRoot); afterErr != nil || after != repositoryHash {
		return CanaryResult{}, errors.New("repository changed during canary")
	}
	if err := persist(request.ResultPath, result); err != nil {
		return CanaryResult{}, err
	}
	return result, nil
}

func RunCatalog(request CatalogRequest) (CatalogResult, error) {
	if err := validateAuthority(request.EnginePath, request.RepoRoot, request.Commit, request.ResultPath, "catalog.json"); err != nil {
		return CatalogResult{}, err
	}
	engineHash, err := fileSHA256(request.EnginePath)
	if err != nil {
		return CatalogResult{}, errors.New("read engine identity")
	}
	repositoryHash, err := repositorySHA256(request.RepoRoot)
	if err != nil {
		return CatalogResult{}, errors.New("read repository identity")
	}
	raw, err := validationharness.RunCatalogMatrix(context.Background(), validationharness.CatalogMatrixRequest{EnginePath: request.EnginePath, RepoRoot: request.RepoRoot})
	if err != nil {
		return CatalogResult{}, errors.New("catalog harness I/O failure")
	}
	result := CatalogResult{SchemaVersion: SchemaVersion, Commit: request.Commit, EngineSHA256: engineHash, RepositoryHash: repositoryHash, Status: raw.Status, CatalogCount: raw.CatalogCount, Passed: raw.Passed, Failed: raw.Failed, Memberships: raw.MembershipCount, UniqueModules: raw.UniqueModules}
	if raw.Failure != nil {
		result.Failure = raw.Failure.Code
	}
	if after, afterErr := fileSHA256(request.EnginePath); afterErr != nil || after != engineHash {
		return CatalogResult{}, errors.New("engine changed during catalog")
	}
	if after, afterErr := repositorySHA256(request.RepoRoot); afterErr != nil || after != repositoryHash {
		return CatalogResult{}, errors.New("repository changed during catalog")
	}
	if err := persist(request.ResultPath, result); err != nil {
		return CatalogResult{}, err
	}
	return result, nil
}

func Aggregate(request AggregateRequest) (AggregateResult, error) {
	if err := validateAuthority(request.EnginePath, request.RepoRoot, request.Commit, request.ResultPath, "aggregate.json"); err != nil {
		return AggregateResult{}, err
	}
	if err := validateInputDirectory(request.InputDir); err != nil {
		return AggregateResult{}, err
	}
	if err := validateEvidenceInventory(request.InputDir); err != nil {
		return AggregateResult{}, err
	}
	engineHash, err := fileSHA256(request.EnginePath)
	if err != nil {
		return AggregateResult{}, errors.New("read engine identity")
	}
	repositoryHash, err := repositorySHA256(request.RepoRoot)
	if err != nil {
		return AggregateResult{}, errors.New("read repository identity")
	}
	catalog, err := validationmatrix.LoadCatalog(request.RepoRoot, time.Now().UTC())
	if err != nil {
		return AggregateResult{}, errors.New("load production catalog")
	}
	plan, err := validationmatrix.PlanSynthetic(catalog, validationmatrix.SyntheticPlanOptions{ShardCount: ShardCount})
	if err != nil {
		return AggregateResult{}, err
	}
	result := AggregateResult{SchemaVersion: SchemaVersion, Commit: request.Commit, Status: validationharness.ResultStatusFailed, Modules: PassedEligible{Eligible: len(catalog.Modules)}, Scenarios: PassedEligible{Eligible: len(plan.Rows)}}
	expected := map[string]validationmatrix.SyntheticRow{}
	for _, row := range plan.Rows {
		expected[rowKey(row)] = row
	}
	seen := map[string]struct{}{}
	for shard := 0; shard < ShardCount; shard++ {
		var evidence ShardResult
		if err := readBounded(filepath.Join(request.InputDir, fmt.Sprintf("shard-%d.json", shard)), &evidence); err != nil {
			return aggregateFailure(request, result, "missing or malformed shard evidence")
		}
		if evidence.SchemaVersion != SchemaVersion || evidence.Commit != request.Commit || evidence.EngineSHA256 != engineHash || evidence.RepositoryHash != repositoryHash || evidence.ShardCount != ShardCount || evidence.Shard != shard || evidence.Status != validationharness.ResultStatusPassed || evidence.Failure != "" {
			return aggregateFailure(request, result, "foreign shard evidence")
		}
		for _, row := range evidence.Rows {
			key := rowKey(row.Identity)
			expectedRow, ok := expected[key]
			if row.Identity.Shard != shard || !ok || !reflect.DeepEqual(expectedRow, row.Identity) {
				return aggregateFailure(request, result, "row proof identity drift")
			}
			if _, duplicate := seen[key]; duplicate {
				return aggregateFailure(request, result, "duplicate row evidence")
			}
			if !matchesRow(row.Identity, row.Result) || !validPassedRow(row.Identity, row.Result) {
				return aggregateFailure(request, result, "failed row evidence")
			}
			seen[key] = struct{}{}
		}
	}
	if len(seen) != len(expected) {
		return aggregateFailure(request, result, "missing row evidence")
	}
	result.Scenarios.Passed = len(seen)
	modulePass := map[string]struct{}{}
	for _, row := range expected {
		modulePass[row.ModuleID] = struct{}{}
	}
	result.Modules.Passed = len(modulePass)
	var catalogEvidence CatalogResult
	if err := readBounded(filepath.Join(request.InputDir, "catalog.json"), &catalogEvidence); err != nil || catalogEvidence.SchemaVersion != SchemaVersion || catalogEvidence.Commit != request.Commit || catalogEvidence.EngineSHA256 != engineHash || catalogEvidence.RepositoryHash != repositoryHash || catalogEvidence.Status != validationharness.ResultStatusPassed || catalogEvidence.Failure != "" || catalogEvidence.Failed != 0 || catalogEvidence.Passed != catalogEvidence.CatalogCount {
		return aggregateFailure(request, result, "missing or failed catalog evidence")
	}
	expectedBundles, err := filepath.Glob(filepath.Join(request.RepoRoot, "bundles", "*.jsonc"))
	if err != nil {
		return AggregateResult{}, err
	}
	result.Bundles.Eligible = len(expectedBundles)
	if catalogEvidence.CatalogCount != result.Bundles.Eligible {
		return aggregateFailure(request, result, "catalog bundle count drift")
	}
	result.Bundles.Passed = catalogEvidence.Passed
	var canary CanaryResult
	if err := readBounded(filepath.Join(request.InputDir, "canary.json"), &canary); err != nil || !validCanary(canary, request.Commit, engineHash, repositoryHash, expected) {
		return aggregateFailure(request, result, "missing or failed synthetic canary")
	}
	result.Status = validationharness.ResultStatusPassed
	if err := persist(request.ResultPath, result); err != nil {
		return AggregateResult{}, err
	}
	return result, nil
}

func aggregateFailure(request AggregateRequest, result AggregateResult, detail string) (AggregateResult, error) {
	result.Failure = detail
	_ = persist(request.ResultPath, result)
	return result, errors.New(detail)
}

func failedRow(row validationmatrix.SyntheticRow, detail string) validationharness.Result {
	return validationharness.Result{SchemaVersion: validationharness.ResultSchemaVersion, ModuleID: row.ModuleID, ModuleRevision: row.ModuleRevision, ScenarioID: row.ScenarioID, Kind: row.ScenarioKind, Status: validationharness.ResultStatusFailed, ProofLevels: []validationmatrix.ProofLevel{}, AssertionCounts: map[string]int{}, Failure: &validationharness.Failure{Code: validationharness.CodeExecutionFailure, Phase: "harness", Detail: detail}, PhaseTimings: map[string]time.Duration{}}
}
func matchesRow(row validationmatrix.SyntheticRow, result validationharness.Result) bool {
	return result.SchemaVersion == validationharness.ResultSchemaVersion && result.ModuleID == row.ModuleID && result.ModuleRevision == row.ModuleRevision && result.ScenarioID == row.ScenarioID && result.Kind == row.ScenarioKind
}
func validRowResult(row validationmatrix.SyntheticRow, result validationharness.Result) bool {
	if result.Status == validationharness.ResultStatusPassed {
		return validPassedRow(row, result)
	}
	return result.Status == validationharness.ResultStatusFailed && result.Failure != nil && len(result.ProofLevels) == 0 && len(result.AssertionCounts) == 0
}

func validPassedRow(row validationmatrix.SyntheticRow, result validationharness.Result) bool {
	if result.Status != validationharness.ResultStatusPassed || result.Failure != nil || len(result.AssertionCounts) == 0 || !reflect.DeepEqual(result.ProofLevels, canonicalProofs(row.ScenarioKind)) {
		return false
	}
	allowed := allowedAssertions(row.ScenarioKind)
	for name, count := range result.AssertionCounts {
		if _, ok := allowed[name]; !ok || count <= 0 {
			return false
		}
	}
	for name, minimum := range row.Scenario.MinimumAssertions {
		if minimum <= 0 || result.AssertionCounts[name] < minimum {
			return false
		}
	}
	return true
}

func canonicalProofs(kind validationmatrix.ScenarioKind) []validationmatrix.ProofLevel {
	switch kind {
	case validationmatrix.ScenarioConfigRoundtripV1:
		return []validationmatrix.ProofLevel{validationmatrix.ProofCatalog, validationmatrix.ProofEngineContract, validationmatrix.ProofConfigRoundtripV1}
	case validationmatrix.ScenarioConfigGenerationV2, validationmatrix.ScenarioConfigMigrationV2:
		return []validationmatrix.ProofLevel{validationmatrix.ProofCatalog, validationmatrix.ProofEngineContract, validationmatrix.ProofConfigRoundtripV2}
	case validationmatrix.ScenarioInstallContract, validationmatrix.ScenarioCaptureContract:
		return []validationmatrix.ProofLevel{validationmatrix.ProofCatalog, validationmatrix.ProofEngineContract}
	case validationmatrix.ScenarioRestoreContract:
		return []validationmatrix.ProofLevel{validationmatrix.ProofEngineContract}
	default:
		return nil
	}
}

func allowedAssertions(kind validationmatrix.ScenarioKind) map[string]struct{} {
	all := func(names ...string) map[string]struct{} {
		values := make(map[string]struct{}, len(names))
		for _, name := range names {
			values[name] = struct{}{}
		}
		return values
	}
	roundtrip := []string{validationmatrix.AssertionCaptured, validationmatrix.AssertionPayload, validationmatrix.AssertionProvenance, validationmatrix.AssertionRewrittenRestore, validationmatrix.AssertionContent, validationmatrix.AssertionRebuild, validationmatrix.AssertionVerify, validationmatrix.AssertionNestedSummary, validationmatrix.AssertionRevert}
	switch kind {
	case validationmatrix.ScenarioConfigRoundtripV1:
		return all(roundtrip...)
	case validationmatrix.ScenarioConfigGenerationV2, validationmatrix.ScenarioConfigMigrationV2:
		return all(append(roundtrip, validationmatrix.AssertionGeneration, validationmatrix.AssertionValidation, validationmatrix.AssertionMigration)...)
	case validationmatrix.ScenarioInstallContract:
		return all(validationmatrix.AssertionAppReferences, validationmatrix.AssertionVerify)
	case validationmatrix.ScenarioCaptureContract:
		return all(validationmatrix.AssertionCaptured, validationmatrix.AssertionContent, validationmatrix.AssertionPayload, validationmatrix.AssertionProvenance)
	case validationmatrix.ScenarioRestoreContract:
		return all(validationmatrix.AssertionRestored, validationmatrix.AssertionContent, validationmatrix.AssertionNestedSummary, validationmatrix.AssertionRevert, validationmatrix.AssertionVerify)
	default:
		return nil
	}
}

func validCanary(canary CanaryResult, commit, engineHash, repositoryHash string, expected map[string]validationmatrix.SyntheticRow) bool {
	if canary.SchemaVersion != SchemaVersion || canary.Commit != commit || canary.EngineSHA256 != engineHash || canary.RepositoryHash != repositoryHash || canary.Status != validationharness.ResultStatusPassed || canary.Failure != "" || canary.Identity.ModuleID != "apps.notepad-plus-plus" || canary.Identity.ScenarioID != "default-v1" {
		return false
	}
	row, ok := expected[rowKey(canary.Identity)]
	return ok && reflect.DeepEqual(row, canary.Identity) && matchesRow(row, canary.Result) && validPassedRow(row, canary.Result)
}
func rowKey(row validationmatrix.SyntheticRow) string {
	return strings.Join([]string{row.ModuleID, row.ModuleRevision, row.ScenarioID, string(row.ScenarioKind), row.ScenarioDigest}, "\x00")
}

func validateAuthority(engine, repo, commit, resultPath, leaf string) error {
	if !commitPattern.MatchString(commit) {
		return errors.New("commit must be exact lowercase SHA-1")
	}
	if engine == "" || repo == "" || !filepath.IsAbs(engine) || !filepath.IsAbs(repo) || filepath.Clean(engine) != engine || filepath.Clean(repo) != repo {
		return errors.New("engine and repository must be canonical absolute paths")
	}
	info, err := os.Lstat(engine)
	if err != nil || !info.Mode().IsRegular() || safepath.IsLinkOrReparse(info) {
		return errors.New("engine authority is unsafe")
	}
	info, err = os.Lstat(repo)
	if err != nil || !info.IsDir() || safepath.IsLinkOrReparse(info) {
		return errors.New("repository authority is unsafe")
	}
	return validateResultPath(resultPath, leaf)
}
func validateInputDirectory(path string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || !withinEvidenceRoot(path) {
		return errors.New("input directory is outside runner temp")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || safepath.IsLinkOrReparse(info) {
		return errors.New("input directory is unsafe")
	}
	return nil
}

func validateEvidenceInventory(path string) error {
	expected := map[string]struct{}{"catalog.json": {}, "canary.json": {}}
	for shard := 0; shard < ShardCount; shard++ {
		expected[fmt.Sprintf("shard-%d.json", shard)] = struct{}{}
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return errors.New("read input directory")
	}
	if len(entries) != len(expected) {
		return errors.New("evidence inventory is incomplete or has extra files")
	}
	for _, entry := range entries {
		if entry.IsDir() || !entry.Type().IsRegular() {
			return errors.New("evidence inventory contains unsafe entry")
		}
		if _, ok := expected[entry.Name()]; !ok {
			return errors.New("evidence inventory has unexpected file")
		}
	}
	return nil
}
func validateResultPath(path, leaf string) error {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) != leaf || filepath.Base(filepath.Dir(path)) != "endstate-validation-results" || !withinEvidenceRoot(path) {
		return errors.New("result path is unsafe")
	}
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil || !info.IsDir() || safepath.IsLinkOrReparse(info) {
		return errors.New("result directory is unsafe")
	}
	return nil
}
func withinEvidenceRoot(path string) bool {
	root, err := evidenceRoot()
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(root, filepath.Clean(path))
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func evidenceRoot() (string, error) {
	root := os.TempDir()
	if runnerTemp, set := os.LookupEnv("RUNNER_TEMP"); set {
		root = runnerTemp
	}
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return "", errors.New("runner temp is not canonical")
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || safepath.IsLinkOrReparse(info) {
		return "", errors.New("runner temp is unsafe")
	}
	return root, nil
}
func persist(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(data) > maxResultSize {
		return errors.New("result exceeds compact evidence limit")
	}
	return safepath.AtomicWriteFile(path, append(data, '\n'), 0o600)
}
func readBounded(path string, target any) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || safepath.IsLinkOrReparse(info) || info.Size() > maxResultSize {
		return errors.New("evidence file is unsafe or oversized")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("evidence contains multiple JSON values")
		}
		return err
	}
	return nil
}

func runHarnessScenario(run ScenarioRunner, engine, repo, moduleID, scenarioID, leaf string) (result validationharness.Result, err error) {
	scratch, err := os.MkdirTemp(os.TempDir(), "endstate-validation-ci-")
	if err != nil {
		return validationharness.Result{}, err
	}
	resultPath := filepath.Join(scratch, leaf)
	result, runErr := run(context.Background(), validationharness.Request{EnginePath: engine, RepoRoot: repo, ModuleID: moduleID, ScenarioID: scenarioID, ResultPath: resultPath})
	if cleanupErr := cleanupHarnessScratch(scratch, resultPath); cleanupErr != nil {
		return result, cleanupErr
	}
	return result, runErr
}

func cleanupHarnessScratch(root, resultPath string) error {
	if !strictTempDescendant(root) || filepath.Dir(resultPath) != root || filepath.Base(root) == "" || !strings.HasPrefix(filepath.Base(root), "endstate-validation-ci-") {
		return errors.New("temporary harness scratch is unsafe")
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || safepath.IsLinkOrReparse(info) {
		return errors.New("temporary harness scratch changed type")
	}
	if resultInfo, resultErr := os.Lstat(resultPath); resultErr == nil {
		if !resultInfo.Mode().IsRegular() || safepath.IsLinkOrReparse(resultInfo) {
			return errors.New("temporary harness result changed type")
		}
		if err := os.Remove(resultPath); err != nil {
			return err
		}
	} else if !os.IsNotExist(resultErr) {
		return resultErr
	}
	return os.Remove(root)
}

func strictTempDescendant(path string) bool {
	relative, err := filepath.Rel(filepath.Clean(os.TempDir()), filepath.Clean(path))
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}
func repositorySHA256(root string) (string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == ".git" || strings.HasPrefix(relative, ".git"+string(filepath.Separator)) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("repository contains link")
		}
		if !entry.IsDir() {
			paths = append(paths, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, relative := range paths {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "%s\x00%x\n", relative, sha256.Sum256(data))
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
