// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"net/url"
	"regexp"
	"strings"
	"time"
)

var datePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

func validateQuarantine(record *ValidationRecord, index int, quarantine *Quarantine, now time.Time) error {
	if !knownProofLevel(quarantine.ProofLevel) || strings.TrimSpace(quarantine.OS) == "" || strings.TrimSpace(quarantine.RunnerImage) == "" ||
		!lowerSHA256Pattern.MatchString(quarantine.FailureFingerprint) || strings.TrimSpace(quarantine.Owner) == "" ||
		!stableKebabPattern.MatchString(quarantine.ReasonCode) {
		return validationError(CodeInvalidQuarantine, record.ModuleID, record.FilePath, "quarantine[%d] must be scoped, owned, fingerprinted, and reason-coded", index)
	}
	issue, err := url.Parse(quarantine.IssueURL)
	if err != nil || issue.Scheme != "https" || issue.Host == "" || issue.Path == "" {
		return validationError(CodeInvalidQuarantine, record.ModuleID, record.FilePath, "quarantine[%d] requires an absolute HTTPS issueUrl", index)
	}
	if !datePattern.MatchString(quarantine.ExpiresOn) {
		return validationError(CodeInvalidQuarantine, record.ModuleID, record.FilePath, "quarantine[%d] expiresOn must be strict YYYY-MM-DD", index)
	}
	expires, err := time.Parse("2006-01-02", quarantine.ExpiresOn)
	if err != nil || !expires.After(now.UTC()) {
		return validationError(CodeInvalidQuarantine, record.ModuleID, record.FilePath, "quarantine[%d] is expired or malformed", index)
	}
	return nil
}

func knownProofLevel(proof ProofLevel) bool {
	switch proof {
	case ProofCatalog, ProofEngineContract, ProofConfigRoundtripV1, ProofConfigRoundtripV2, ProofLiveInstall, ProofLiveConfigRoundtrip:
		return true
	default:
		return false
	}
}
