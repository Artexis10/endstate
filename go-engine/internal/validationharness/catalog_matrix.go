// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/catalogplan"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

const catalogMatrixSchemaVersion = 1

// CatalogMatrixRequest identifies one built engine and one immutable repository
// authority. The matrix deliberately only consumes the public catalog-plan CLI.
type CatalogMatrixRequest struct {
	EnginePath string
	RepoRoot   string
	ResultPath string
}

// CatalogMatrixResult is distinct from a module scenario Result because a
// catalog proof binds a bundle set and two CLI executions per bundle.
type CatalogMatrixResult struct {
	SchemaVersion   int                           `json:"schemaVersion"`
	Status          string                        `json:"status"`
	ProofLevels     []validationmatrix.ProofLevel `json:"proofLevels"`
	CatalogCount    int                           `json:"catalogCount"`
	Attempted       int                           `json:"attempted"`
	Passed          int                           `json:"passed"`
	Failed          int                           `json:"failed"`
	MembershipCount int                           `json:"membershipCount"`
	UniqueModules   int                           `json:"uniqueModules"`
	Reuse           []CatalogReuse                `json:"reuse"`
	Rows            []CatalogMatrixRow            `json:"rows"`
	EngineHash      string                        `json:"engineHash,omitempty"`
	RepositoryHash  string                        `json:"repositoryHash,omitempty"`
	Failure         *Failure                      `json:"failure,omitempty"`
	PhaseTimings    map[string]time.Duration      `json:"phaseTimings"`
}

type CatalogMatrixRow struct {
	SchemaVersion   int                           `json:"schemaVersion"`
	BundleID        string                        `json:"bundleId"`
	BundleHash      string                        `json:"bundleHash"`
	BundleVersion   int                           `json:"bundleVersion"`
	Status          string                        `json:"status"`
	ProofLevels     []validationmatrix.ProofLevel `json:"proofLevels"`
	MembershipCount int                           `json:"membershipCount"`
	Actions         []catalogplan.Action          `json:"actions"`
	PlanExecutions  int                           `json:"planExecutions"`
	AssertionCounts map[string]int                `json:"assertionCounts"`
	Failure         *Failure                      `json:"failure,omitempty"`
	PhaseTimings    map[string]time.Duration      `json:"phaseTimings"`
}

type CatalogReuse struct {
	ModuleID string   `json:"moduleId"`
	Bundles  []string `json:"bundles"`
}

type catalogEnvelope struct {
	SchemaVersion string          `json:"schemaVersion"`
	CLIVersion    string          `json:"cliVersion"`
	Command       string          `json:"command"`
	RunID         string          `json:"runId"`
	TimestampUTC  string          `json:"timestampUtc"`
	Success       bool            `json:"success"`
	TestMode      json.RawMessage `json:"testMode"`
	Data          json.RawMessage `json:"data"`
	Error         json.RawMessage `json:"error"`
}

func RunCatalogMatrix(ctx context.Context, request CatalogMatrixRequest) (CatalogMatrixResult, error) {
	result := CatalogMatrixResult{SchemaVersion: catalogMatrixSchemaVersion, Status: ResultStatusFailed, ProofLevels: []validationmatrix.ProofLevel{}, Rows: []CatalogMatrixRow{}, Reuse: []CatalogReuse{}, PhaseTimings: map[string]time.Duration{}}
	started := time.Now()
	engine, repo, failure := validateCatalogMatrixRequest(request)
	if failure != nil {
		result.Failure = failure
		result.PhaseTimings["setup"] = time.Since(started)
		return persistCatalogMatrixResult(request.ResultPath, result)
	}
	repoBoundary, err := snapshotBoundaryTree(repo)
	if err != nil {
		return result, err
	}
	engineBoundary, err := snapshotBoundaryFile(engine)
	if err != nil {
		return result, err
	}
	result.RepositoryHash = catalogBoundaryHash(repoBoundary)
	result.EngineHash = hex.EncodeToString(engineBoundary.Digest[:])
	bundles, failure := discoverCatalogBundles(repo)
	if failure != nil {
		result.Failure = failure
		result.PhaseTimings["discovery"] = time.Since(started)
		return persistCatalogMatrixResult(request.ResultPath, result)
	}
	result.CatalogCount = len(bundles)
	for _, bundle := range bundles {
		row := runCatalogMatrixRow(ctx, engine, repo, bundle)
		result.Rows = append(result.Rows, row)
		result.Attempted++
		if row.Status == ResultStatusPassed {
			result.Passed++
			result.MembershipCount += row.MembershipCount
		} else {
			result.Failed++
		}
	}
	if after, snapshotErr := snapshotBoundaryTree(repo); snapshotErr != nil || firstBoundaryDifference(repoBoundary, after) != "" {
		result.Failure = fail(CodeIsolationFailure, "guard", "repository", "repository changed during catalog matrix execution")
	}
	if after, snapshotErr := snapshotBoundaryFile(engine); snapshotErr != nil || boundaryEntryDifference(engineBoundary, after) != "" {
		result.Failure = fail(CodeIsolationFailure, "guard", "engine", "engine executable changed during catalog matrix execution")
	}
	result.Reuse, result.UniqueModules = catalogReuse(result.Rows)
	if result.Failure == nil && result.CatalogCount > 0 && result.Attempted == result.CatalogCount && result.Attempted == result.Passed+result.Failed && result.Failed == 0 {
		result.Status = ResultStatusPassed
		result.ProofLevels = []validationmatrix.ProofLevel{validationmatrix.ProofCatalog}
	} else if result.Failure == nil {
		result.Failure = fail(CodeAssertionContract, "aggregate", "rows", "catalog aggregate is incomplete or contains failed rows")
	}
	result.PhaseTimings["total"] = time.Since(started)
	return persistCatalogMatrixResult(request.ResultPath, result)
}

func validateCatalogMatrixRequest(request CatalogMatrixRequest) (string, string, *Failure) {
	if request.EnginePath == "" || request.RepoRoot == "" || !filepath.IsAbs(request.EnginePath) || !filepath.IsAbs(request.RepoRoot) || filepath.Clean(request.EnginePath) != request.EnginePath || filepath.Clean(request.RepoRoot) != request.RepoRoot {
		return "", "", fail(CodeInvalidEngine, "setup", "identity", "engine and repository paths must be canonical absolute paths")
	}
	if entry, err := snapshotBoundaryFile(request.EnginePath); err != nil || entry.Kind != "file" {
		return "", "", fail(CodeInvalidEngine, "setup", "engine", "engine executable is not a regular stable file")
	}
	if err := safepath.ValidateRoot(request.RepoRoot); err != nil {
		return "", "", fail(CodeIsolationFailure, "setup", "repository", "repository root is not a safe regular directory")
	}
	return request.EnginePath, request.RepoRoot, nil
}

func discoverCatalogBundles(repo string) ([]string, *Failure) {
	directory := filepath.Join(repo, "bundles")
	info, err := os.Lstat(directory)
	if err != nil || safepath.IsLinkOrReparse(info) || !info.IsDir() {
		return nil, fail(CodeScenarioSelection, "discovery", "bundles", "tracked bundle directory is not a regular directory")
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fail(CodeScenarioSelection, "discovery", "bundles", "cannot enumerate tracked bundle directory")
	}
	var bundles []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".jsonc" {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		info, err := os.Lstat(path)
		if err != nil || safepath.IsLinkOrReparse(info) || !info.Mode().IsRegular() {
			return nil, fail(CodeScenarioSelection, "discovery", "bundle", "tracked bundle is not a regular immediate child")
		}
		bundles = append(bundles, path)
	}
	sort.Strings(bundles)
	if len(bundles) == 0 {
		return nil, fail(CodeScenarioSelection, "discovery", "bundles", "tracked bundle catalog is empty")
	}
	return bundles, nil
}

func runCatalogMatrixRow(ctx context.Context, engine, repo, bundle string) CatalogMatrixRow {
	row := CatalogMatrixRow{SchemaVersion: catalogMatrixSchemaVersion, Status: ResultStatusFailed, ProofLevels: []validationmatrix.ProofLevel{}, Actions: []catalogplan.Action{}, AssertionCounts: map[string]int{}, PhaseTimings: map[string]time.Duration{}}
	firstStarted := time.Now()
	first, failure := invokeCatalogPlan(ctx, engine, repo, bundle)
	row.PhaseTimings["firstPlan"] = time.Since(firstStarted)
	if failure != nil {
		row.Failure = failure
		return row
	}
	if failure := validateCatalogPlanBundleIdentity(first, bundle); failure != nil {
		row.Failure = failure
		return row
	}
	row.BundleID, row.BundleHash, row.BundleVersion = first.Bundle.ID, first.Bundle.Hash, first.Bundle.Version
	row.MembershipCount, row.Actions, row.PlanExecutions = first.MembershipCount, first.Actions, 1
	row.AssertionCounts["actions"] = first.ActionCount
	secondStarted := time.Now()
	second, failure := invokeCatalogPlan(ctx, engine, repo, bundle)
	row.PhaseTimings["secondPlan"] = time.Since(secondStarted)
	if failure != nil {
		row.Failure = failure
		return row
	}
	if failure := validateCatalogPlanBundleIdentity(second, bundle); failure != nil {
		row.Failure = failure
		return row
	}
	row.PlanExecutions++
	row.AssertionCounts["catalogPlan"] = row.PlanExecutions
	if !catalogProjectionEqual(first, second) {
		row.Failure = fail(CodeAssertionContract, "stability", "data", "catalog plan data projection differs between executions")
		return row
	}
	row.Status = ResultStatusPassed
	row.ProofLevels = []validationmatrix.ProofLevel{validationmatrix.ProofCatalog}
	return row
}

func invokeCatalogPlan(ctx context.Context, engine, repo, bundle string) (catalogplan.Result, *Failure) {
	work, err := os.MkdirTemp("", "endstate-catalog-matrix-")
	if err != nil {
		return catalogplan.Result{}, fail(CodeIsolationFailure, "execution", "workingDirectory", "create validation child working directory")
	}
	defer os.RemoveAll(work)
	bounded, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	command := exec.CommandContext(bounded, engine, "catalog-plan", "--bundle", bundle, "--json", "--events", "jsonl")
	command.Dir = work
	command.Env = catalogChildEnvironment(repo)
	stdout, stderr := &boundedBuffer{limit: maxCommandOutputBytes}, &boundedBuffer{limit: maxCommandOutputBytes}
	command.Stdout, command.Stderr = stdout, stderr
	processErr := command.Run()
	if stdout.exceeded || stderr.exceeded {
		return catalogplan.Result{}, fail(CodeExecutionFailure, "catalog-plan", "output", "engine output exceeded the validation limit")
	}
	if bounded.Err() != nil {
		return catalogplan.Result{}, fail(CodeExecutionFailure, "catalog-plan", "timeout", "catalog plan command exceeded the timeout")
	}
	result, runID, failure := decodeCatalogEnvelope(stdout.Bytes())
	if failure != nil {
		return catalogplan.Result{}, failure
	}
	if failure := decodeCatalogEvents(stderr.Bytes(), runID, result); failure != nil {
		return catalogplan.Result{}, failure
	}
	if processErr != nil {
		return catalogplan.Result{}, fail(CodeExecutionFailure, "catalog-plan", "exit", "catalog plan exited unsuccessfully despite a success envelope")
	}
	return result, nil
}

func catalogChildEnvironment(repo string) []string {
	blocked := []string{"ENDSTATE_TESTMODE", "ENDSTATE_ROOT", "GITHUB_TOKEN", "GH_TOKEN", "GIT_ASKPASS", "SSH_AUTH_SOCK", "AWS_SECRET_ACCESS_KEY", "AZURE_CLIENT_SECRET"}
	result := make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		name := value[:strings.IndexByte(value, '=')]
		skip := false
		for _, blockedName := range blocked {
			if strings.EqualFold(name, blockedName) {
				skip = true
				break
			}
		}
		if !skip {
			result = append(result, value)
		}
	}
	return append(result, "ENDSTATE_ROOT="+repo)
}

func decodeCatalogEnvelope(stdout []byte) (catalogplan.Result, string, *Failure) {
	decoder := json.NewDecoder(bytes.NewReader(stdout))
	decoder.DisallowUnknownFields()
	var envelope catalogEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return catalogplan.Result{}, "", fail(CodeEnvelopeContract, "catalog-plan", "stdout", "malformed JSON envelope")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return catalogplan.Result{}, "", fail(CodeEnvelopeContract, "catalog-plan", "stdout", "stdout must contain exactly one JSON envelope")
	}
	if envelope.SchemaVersion != "1.0" || strings.TrimSpace(envelope.CLIVersion) == "" || envelope.Command != "catalog-plan" || strings.TrimSpace(envelope.RunID) == "" || !envelope.Success || string(envelope.Error) != "null" || len(envelope.TestMode) != 0 && string(envelope.TestMode) != "null" {
		return catalogplan.Result{}, "", fail(CodeEnvelopeContract, "catalog-plan", "envelope", "catalog envelope identity is invalid")
	}
	if _, err := time.Parse(time.RFC3339, envelope.TimestampUTC); err != nil {
		return catalogplan.Result{}, "", fail(CodeEnvelopeContract, "catalog-plan", "timestampUtc", "catalog envelope timestamp is invalid")
	}
	dataDecoder := json.NewDecoder(bytes.NewReader(envelope.Data))
	dataDecoder.DisallowUnknownFields()
	var result catalogplan.Result
	if err := dataDecoder.Decode(&result); err != nil {
		return catalogplan.Result{}, "", fail(CodeEnvelopeContract, "catalog-plan", "data", "catalog data projection is malformed")
	}
	if err := dataDecoder.Decode(&trailing); err != io.EOF {
		return catalogplan.Result{}, "", fail(CodeEnvelopeContract, "catalog-plan", "data", "catalog data contains multiple JSON values")
	}
	if failure := validateCatalogPlanData(result); failure != nil {
		return catalogplan.Result{}, "", failure
	}
	return result, envelope.RunID, nil
}

func validateCatalogPlanData(result catalogplan.Result) *Failure {
	if result.Proof != "catalog" || result.Bundle.ID == "" || result.Bundle.Hash == "" || result.Bundle.Version != 1 || result.Bundle.Path == "" || result.MembershipCount <= 0 || result.ActionCount != result.MembershipCount || len(result.Actions) != result.MembershipCount || len(result.Failures) != 0 {
		return fail(CodeEnvelopeContract, "catalog-plan", "data", "catalog data is empty, partial, or has an invalid proof identity")
	}
	seen := make(map[string]struct{}, len(result.Actions))
	for _, action := range result.Actions {
		if action.BundleID != result.Bundle.ID || action.BundleHash != result.Bundle.Hash || action.ModuleID == "" || action.ModuleRevision == "" || action.ModuleSchemaVersion < 1 || action.ValidationHash == "" || action.ValidationScenarioCount < 1 || action.Status != "resolved" || action.Skipped {
			return fail(CodeEnvelopeContract, "catalog-plan", "actions", "catalog action lacks an exact resolution identity")
		}
		if _, duplicate := seen[action.ModuleID]; duplicate {
			return fail(CodeEnvelopeContract, "catalog-plan", "actions", "catalog action set contains a duplicate module")
		}
		seen[action.ModuleID] = struct{}{}
	}
	return nil
}

func validateCatalogPlanBundleIdentity(result catalogplan.Result, bundlePath string) *Failure {
	data, err := os.ReadFile(bundlePath)
	if err != nil {
		return fail(CodeIsolationFailure, "identity", "bundle", "bundle bytes cannot be re-read safely")
	}
	normalized := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	sum := sha256.Sum256(normalized)
	wantID := strings.TrimSuffix(filepath.Base(bundlePath), filepath.Ext(bundlePath))
	wantPath := filepath.ToSlash(filepath.Join("bundles", filepath.Base(bundlePath)))
	if result.Bundle.ID != wantID || result.Bundle.Path != wantPath || result.Bundle.Hash != hex.EncodeToString(sum[:]) {
		return fail(CodeEnvelopeContract, "identity", "bundle", "catalog plan bundle identity differs from the tracked input bytes")
	}
	return nil
}

func decodeCatalogEvents(stderr []byte, runID string, result catalogplan.Result) *Failure {
	scanner := bufio.NewScanner(bytes.NewReader(stderr))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	var events []map[string]any
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			return fail(CodeEventContract, "events", "stderr", "catalog event segment contains a blank line")
		}
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.UseNumber()
		var event map[string]any
		if err := decoder.Decode(&event); err != nil {
			return fail(CodeEventContract, "events", "stderr", "catalog event is not valid JSONL")
		}
		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			return fail(CodeEventContract, "events", "stderr", "catalog event contains multiple JSON values")
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil || len(events) != len(result.Actions)+2 {
		return fail(CodeEventContract, "events", "stderr", "catalog event segment is missing, extra, or oversized")
	}
	for index, event := range events {
		if event["version"] != json.Number("1") || event["runId"] != runID || !catalogEventTimestamp(event["timestamp"]) {
			return fail(CodeEventContract, "events", "identity", "catalog event identity is foreign or invalid")
		}
		switch index {
		case 0:
			if event["event"] != "phase" || event["phase"] != "plan" || !catalogExactFields(event, "phase") {
				return fail(CodeEventContract, "events", "phase", "catalog event segment must start with one plan phase")
			}
		case len(events) - 1:
			if event["event"] != "summary" || event["phase"] != "plan" || event["total"] != json.Number(fmt.Sprint(result.ActionCount)) || event["success"] != json.Number(fmt.Sprint(result.ActionCount)) || event["skipped"] != json.Number("0") || event["failed"] != json.Number("0") || !catalogExactFields(event, "summary") {
				return fail(CodeEventContract, "events", "summary", "catalog summary does not bind the exact action count")
			}
		default:
			action := result.Actions[index-1]
			if event["event"] != "item" || event["id"] != action.ModuleID || event["driver"] != "catalog" || event["status"] != "present" || event["reason"] != "detected" || !catalogExactFields(event, "item") {
				return fail(CodeEventContract, "events", "item", "catalog item event differs from the ordered action projection")
			}
		}
	}
	return nil
}

func catalogEventTimestamp(value any) bool {
	_, err := time.Parse(time.RFC3339Nano, fmt.Sprint(value))
	return err == nil
}

func catalogExactFields(event map[string]any, kind string) bool {
	allowed := map[string]map[string]bool{
		"phase":   {"version": true, "runId": true, "timestamp": true, "event": true, "phase": true},
		"summary": {"version": true, "runId": true, "timestamp": true, "event": true, "phase": true, "total": true, "success": true, "skipped": true, "failed": true},
		"item":    {"version": true, "runId": true, "timestamp": true, "event": true, "id": true, "driver": true, "name": true, "status": true, "reason": true, "message": true, "rebootRequired": true},
	}[kind]
	for field := range event {
		if !allowed[field] {
			return false
		}
	}
	return true
}

func catalogProjectionEqual(first, second catalogplan.Result) bool {
	left, leftErr := json.Marshal(first)
	right, rightErr := json.Marshal(second)
	return leftErr == nil && rightErr == nil && bytes.Equal(left, right)
}

func catalogReuse(rows []CatalogMatrixRow) ([]CatalogReuse, int) {
	owners := map[string][]string{}
	for _, row := range rows {
		if row.Status != ResultStatusPassed {
			continue
		}
		for _, action := range row.Actions {
			owners[action.ModuleID] = append(owners[action.ModuleID], row.BundleID)
		}
	}
	keys := make([]string, 0, len(owners))
	for module := range owners {
		keys = append(keys, module)
	}
	sort.Strings(keys)
	reuse := []CatalogReuse{}
	for _, module := range keys {
		sort.Strings(owners[module])
		if len(owners[module]) > 1 {
			reuse = append(reuse, CatalogReuse{ModuleID: module, Bundles: owners[module]})
		}
	}
	return reuse, len(owners)
}

func catalogBoundaryHash(tree boundaryTree) string {
	keys := make([]string, 0, len(tree))
	for key := range tree {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	hash := sha256.New()
	for _, key := range keys {
		entry := tree[key]
		_, _ = fmt.Fprintf(hash, "%s\x00%s\x00%o\x00%d\x00%x\n", key, entry.Kind, entry.Mode, entry.Size, entry.Digest)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func persistCatalogMatrixResult(path string, result CatalogMatrixResult) (CatalogMatrixResult, error) {
	if path == "" {
		return result, nil
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(filepath.Dir(path)) != "endstate-validation-results" {
		return result, fmt.Errorf("catalog result path must be inside an existing validation-owned result directory")
	}
	parent := filepath.Dir(path)
	info, err := os.Lstat(parent)
	if err != nil || safepath.IsLinkOrReparse(info) || !info.IsDir() {
		return result, fmt.Errorf("catalog result directory is not a stable validation-owned directory")
	}
	data, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	if err := safepath.AtomicWriteFile(path, append(data, '\n'), 0o600); err != nil {
		return result, err
	}
	return result, nil
}
