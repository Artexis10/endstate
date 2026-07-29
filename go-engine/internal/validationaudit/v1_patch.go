// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationaudit

import (
	"crypto/sha256"
	"errors"
	"strings"
)

var (
	ErrInvalidV1PatchScope = errors.New("validation audit invalid v1 patch scope")
)

// V1PatchRequest is the controller-derived patch authority. Patch paths are
// never supplied by callers: the bounded candidate identifier fixes the leaf.
type V1PatchRequest struct {
	CandidateID    string
	Family         string
	PatchSHA256    string
	ModuleID       string
	BundleID       string
	ProductionFile string
	ScenarioID     string
	DetectorID     string
}

type V1PatchIdentity struct {
	SHA256       string
	TouchedPaths []string
}

// LoadV1CandidatePatch accepts only
// validation/ci-efficacy/pilot-v1/patches/<candidate-id>.patch and derives its
// scope from parsed patch bytes rather than redundant manifest metadata.
func LoadV1CandidatePatch(repositoryRoot string, request V1PatchRequest) (V1PatchIdentity, error) {
	if !validAuditIdentifier(request.CandidateID) || !sha256Pattern.MatchString(request.PatchSHA256) {
		return V1PatchIdentity{}, ErrInvalidV1PatchScope
	}
	root, err := canonicalRepositoryRoot(repositoryRoot)
	if err != nil {
		return V1PatchIdentity{}, ErrUnsafePatchPath
	}
	declared := "validation/ci-efficacy/pilot-v1/patches/" + request.CandidateID + ".patch"
	path, err := candidatePatchPath(root, "pilot-v1", declared)
	if err != nil {
		return V1PatchIdentity{}, ErrUnsafePatchPath
	}
	raw, err := readSafePatch(root, declared, path)
	if err != nil {
		return V1PatchIdentity{}, ErrUnsafePatchPath
	}
	digest := hexDigest(sha256.Sum256(raw))
	if digest != request.PatchSHA256 {
		return V1PatchIdentity{}, ErrPatchIdentityMismatch
	}
	paths, err := parsePatch(raw)
	if err != nil {
		return V1PatchIdentity{}, ErrInvalidPatch
	}
	if !validV1PatchScope(request, paths, string(raw)) {
		return V1PatchIdentity{}, ErrInvalidV1PatchScope
	}
	return V1PatchIdentity{SHA256: digest, TouchedPaths: paths}, nil
}

func validV1PatchScope(request V1PatchRequest, paths []string, raw string) bool {
	path := "go-engine/internal/" + request.ProductionFile
	return request.Family == "production-go" && validV1ProductionFile(request.ProductionFile) && len(paths) == 1 && paths[0] == path && validV1PatchContent(request, raw)
}

func validV1PatchContent(request V1PatchRequest, raw string) bool {
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		added := strings.ToLower(line[1:])
		if strings.Contains(added, "//go:build") || strings.Contains(added, "// +build") || strings.Contains(added, "endstate_testmode") || strings.Contains(added, "endstate_validation") {
			return false
		}
		if request.CandidateID != "" && strings.Contains(line, request.CandidateID) || request.ModuleID != "" && strings.Contains(line, request.ModuleID) || request.ScenarioID != "" && strings.Contains(line, request.ScenarioID) || request.DetectorID != "" && strings.Contains(line, request.DetectorID) {
			return false
		}
	}
	return true
}

func validV1ProductionFile(file string) bool {
	for _, allowed := range []string{
		"bundle/capture_bundle.go", "bundle/collect.go", "bundle/config_capture.go", "bundle/create.go", "bundle/module_snapshot.go", "bundle/payload_manifest.go",
		"restore/append.go", "restore/backup.go", "restore/copy.go", "restore/delete_glob.go", "restore/merge_ini.go", "restore/merge_json.go", "restore/registry_import.go", "restore/restore.go", "restore/revert.go", "restore/target_safety.go",
	} {
		if file == allowed {
			return true
		}
	}
	return false
}
