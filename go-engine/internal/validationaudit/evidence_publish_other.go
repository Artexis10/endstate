// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows && !linux && !darwin

package validationaudit

func publishEvidence(_ string, _ string, _ []byte, _ string) (PublishedEvidence, error) {
	return PublishedEvidence{}, ErrEvidencePublication
}
