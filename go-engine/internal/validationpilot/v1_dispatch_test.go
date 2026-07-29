// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationpilot

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateV1RepositoryHydratesCommittedDispatchAuthority(t *testing.T) {
	root, manifestPath, dispatch := v1CommittedDispatchFixture(t)
	manifest, err := ValidateV1Repository(root, manifestPath, dispatch.Commit)
	if err != nil {
		t.Fatalf("ValidateV1Repository() = %v", err)
	}
	if manifest.Authorities.Dispatch != dispatch {
		t.Fatalf("dispatch authority = %#v, want %#v", manifest.Authorities.Dispatch, dispatch)
	}
	if _, err := ValidateV1Repository(root, manifestPath, strings.Repeat("f", 40)); err == nil {
		t.Fatal("ValidateV1Repository() accepted a mismatched dispatch commit")
	}
}

func TestDecodeV1ManifestRejectsPersistedDispatchAuthority(t *testing.T) {
	manifest, _ := validV1Proof()
	raw, _, err := EncodeV1Manifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeV1Manifest(raw); err != nil {
		t.Fatalf("DecodeV1Manifest() = %v\n%s", err, raw)
	}
	if strings.Contains(string(raw), `"dispatch"`) {
		t.Fatal("EncodeV1Manifest() persisted the runtime dispatch authority")
	}
	if _, err := DecodeV1Manifest([]byte(strings.Replace(string(raw), `"corpus":`, `"dispatch":{"commit":"`+strings.Repeat("d", 40)+`","tree":"`+strings.Repeat("d", 40)+`"},"corpus":`, 1))); err == nil {
		t.Fatal("DecodeV1Manifest() accepted a persisted dispatch authority")
	}
}

func TestValidateV1RepositoryRejectsDirtyDispatchCheckout(t *testing.T) {
	root, manifestPath, dispatch := v1CommittedDispatchFixture(t)
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, append(raw, ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateV1Repository(root, manifestPath, dispatch.Commit); err == nil {
		t.Fatal("ValidateV1Repository() accepted a dirty dispatch checkout")
	}
}

func TestRunV1LaneRejectsCallerSuppliedStaleManifest(t *testing.T) {
	root, manifestPath, dispatch := v1CommittedDispatchFixture(t)
	manifest, err := ValidateV1Repository(root, manifestPath, dispatch.Commit)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Candidates[0].FaultDescription = "stale but syntactically valid description"
	runner, err := HostedV1Runner("windows", "windows", "2025")
	if err != nil {
		t.Fatal(err)
	}
	runnerRoot := t.TempDir()
	request := V1LaneRequest{Root: root, RunnerRoot: runnerRoot, GoCache: filepath.Join(runnerRoot, "go-build"), GoModCache: filepath.Join(runnerRoot, "go-mod"), Manifest: manifest, DispatchCommit: dispatch.Commit, Role: V1KindComparator, Lane: V1LaneWindowsGo, Runner: runner, ResultRoot: filepath.Join(runnerRoot, "results")}
	if err := RunV1Lane(context.Background(), request); err == nil {
		t.Fatal("RunV1Lane() accepted a caller-supplied stale manifest")
	}
}

func TestAggregateV1EvidenceRequiresExactDispatchCommit(t *testing.T) {
	manifest, _ := validV1Proof()
	if _, err := AggregateV1Evidence(manifest, strings.Repeat("f", 40), t.TempDir()); !errors.Is(err, ErrV1EvidenceInventory) {
		t.Fatalf("AggregateV1Evidence() error = %v, want dispatch mismatch rejection", err)
	}
}

func v1CommittedDispatchFixture(t *testing.T) (string, string, V1Reference) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runV1TestGit(t, root, "init")
	for _, candidate := range []V1Candidate{validV1CandidateForDispatch(t, 0), validV1CandidateForDispatch(t, 1), validV1CandidateForDispatch(t, 2)} {
		slug := strings.TrimPrefix(candidate.Target.ModuleID, "apps.")
		mode := lifecycleV1Mode(candidate.Lifecycle)
		path := filepath.Join(root, "modules", "apps", slug, "validation.jsonc")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"synthetic":{"scenarios":[{"id":"`+candidate.Target.ScenarioID+`","mode":"`+mode+`"}]}}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runV1TestGit(t, root, "add", ".")
	runV1TestGit(t, root, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "-m", "evaluated")
	evaluated := v1TestReference(t, root, "HEAD")
	runV1TestGit(t, root, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "freeze")
	freeze := v1TestReference(t, root, "HEAD")

	manifest, _ := validV1Proof()
	manifest.Authorities = V1Authorities{Evaluated: evaluated, Freeze: freeze}
	for index := range manifest.Candidates {
		candidate := &manifest.Candidates[index]
		patch := []byte("diff --git a/go-engine/internal/" + candidate.ProductionFile + " b/go-engine/internal/" + candidate.ProductionFile + "\n--- a/go-engine/internal/" + candidate.ProductionFile + "\n+++ b/go-engine/internal/" + candidate.ProductionFile + "\n@@ -1 +1 @@\n-old\n+new-" + string(rune('a'+index)) + "\n")
		candidate.PatchSHA256 = v1RepositoryDigest(patch)
		patchPath := filepath.Join(root, filepath.FromSlash(V1CorpusRoot), "patches", candidate.ID+".patch")
		if err := os.MkdirAll(filepath.Dir(patchPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(patchPath, patch, 0o600); err != nil {
			t.Fatal(err)
		}
		v1WriteDispatchReview(t, root, candidate)
	}
	runV1TestGit(t, root, "add", ".")
	runV1TestGit(t, root, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "-m", "corpus")
	manifest.Authorities.Corpus = v1TestReference(t, root, "HEAD")
	manifest.ComparatorContractSHA256 = V1ComparatorContractSHA256()
	manifest.DetectorContractSHA256 = V1DetectorContractSHA256()
	raw, _, err := EncodeV1Manifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeV1Manifest(raw); err != nil {
		t.Fatalf("DecodeV1Manifest(fixture) = %v\n%s", err, raw)
	}
	manifestPath := filepath.Join(root, filepath.FromSlash(V1CorpusRoot), "manifest.json")
	if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	runV1TestGit(t, root, "add", ".")
	runV1TestGit(t, root, "-c", "user.name=test", "-c", "user.email=test@example.invalid", "commit", "-m", "dispatch")
	return root, manifestPath, v1TestReference(t, root, "HEAD")
}

func validV1CandidateForDispatch(t *testing.T, index int) V1Candidate {
	t.Helper()
	manifest, _ := validV1Proof()
	return manifest.Candidates[index]
}

func v1TestReference(t *testing.T, root, revision string) V1Reference {
	t.Helper()
	return V1Reference{Commit: strings.TrimSpace(runV1TestGit(t, root, "rev-parse", revision+"^{commit}")), Tree: strings.TrimSpace(runV1TestGit(t, root, "rev-parse", revision+"^{tree}"))}
}

func v1WriteDispatchReview(t *testing.T, root string, candidate *V1Candidate) {
	t.Helper()
	record := v1ReviewRecord{CandidateID: candidate.ID, PatchSHA256: candidate.PatchSHA256, OperatorFingerprint: candidate.OperatorFingerprint, InvariantFingerprint: candidate.InvariantFingerprint, Target: candidate.Target, ProductionFile: candidate.ProductionFile, Lifecycle: candidate.Lifecycle, Expected: candidate.Expected, Realistic: true, NonEquivalent: true, Disjoint: true, PatchScope: true, FailureIdentity: true, ProductionReachability: true, Ordering: true}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	candidate.ReviewRecordSHA256 = v1RepositoryDigest(raw)
	path := filepath.Join(root, filepath.FromSlash(V1CorpusRoot), "reviews", candidate.ID+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
