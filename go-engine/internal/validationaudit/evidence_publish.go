// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationaudit

import (
	"os"
	"path/filepath"
	"strings"
)

var beforeEvidenceRootOpen = func() {}

// PublishEvidence creates one new evidence leaf below a pre-existing canonical
// result root and verifies the exact persisted bytes before returning.
func PublishEvidence(resultRoot, leaf string, evidence AttemptEvidence) (PublishedEvidence, error) {
	raw, digest, err := EncodeAttemptEvidence(evidence)
	if err != nil {
		return PublishedEvidence{}, ErrInvalidEvidence
	}
	if !validEvidenceLeaf(leaf) {
		return PublishedEvidence{}, ErrUnsafeEvidencePath
	}
	return publishEvidence(resultRoot, leaf, raw, digest)
}

func validEvidenceLeaf(value string) bool {
	if !strings.HasSuffix(value, ".json") || strings.ContainsAny(value, `/\\:`) {
		return false
	}
	name := strings.TrimSuffix(value, ".json")
	return name != "" && identifierPattern.MatchString(name) && !strings.HasSuffix(name, ".") && !strings.HasSuffix(name, " ") && !windowsReservedDeviceName(name) && filepath.Base(value) == value
}

// IsUnsafePath reports links and platform-specific reparse points without
// resolving them. Callers use it while walking an owned path boundary.
func IsUnsafePath(path string) bool {
	info, err := os.Lstat(path)
	return err != nil || unsafePathInfo(path, info)
}
