// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationaudit

import (
	"crypto/sha256"
	"errors"
	"regexp"
	"strings"
)

var (
	ErrInvalidV1PatchScope = errors.New("validation audit invalid v1 patch scope")
	v1RevisionLine         = regexp.MustCompile(`^[+-]\s*"moduleRevision"\s*:\s*"[^"]+"\s*,?\s*$`)
)

// V1PatchRequest is the controller-derived patch authority. Patch paths are
// never supplied by callers: the bounded candidate identifier fixes the leaf.
type V1PatchRequest struct {
	CandidateID string
	Family      string
	PatchSHA256 string
	ModuleID    string
	BundleID    string
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
	switch request.Family {
	case "catalog":
		return validAuditIdentifier(request.BundleID) && len(paths) == 1 && paths[0] == "bundles/"+request.BundleID+".jsonc"
	case "module":
		if !strings.HasPrefix(request.ModuleID, "apps.") {
			return false
		}
		slug := strings.TrimPrefix(request.ModuleID, "apps.")
		modulePath := "modules/apps/" + slug + "/module.jsonc"
		sidecarPath := "modules/apps/" + slug + "/validation.jsonc"
		if len(paths) == 1 {
			return paths[0] == modulePath
		}
		return len(paths) == 2 && paths[0] == modulePath && paths[1] == sidecarPath && mechanicalV1RevisionSidecar(raw, sidecarPath)
	default:
		return false
	}
}

func mechanicalV1RevisionSidecar(raw, sidecarPath string) bool {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	inSidecar, removed, added := false, false, false
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			inSidecar = strings.Contains(line, " a/"+sidecarPath+" b/"+sidecarPath)
			continue
		}
		if !inSidecar || strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "@@") || strings.HasPrefix(line, "index ") {
			continue
		}
		if strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-") {
			if !v1RevisionLine.MatchString(line) {
				return false
			}
			removed = removed || strings.HasPrefix(line, "-")
			added = added || strings.HasPrefix(line, "+")
		}
	}
	return removed && added
}
