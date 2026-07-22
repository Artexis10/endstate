// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"fmt"
	"regexp"
	"strings"
)

var stableKebabPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func validateLivePolicy(record *ValidationRecord) error {
	live := &record.Live
	if !knownLiveMode(live.Mode) {
		return validationError(CodeUnknownLiveMode, record.ModuleID, record.FilePath, "unknown live mode %q", live.Mode)
	}
	if live.Mode != LiveHosted {
		if !stableKebabPattern.MatchString(live.ReasonCode) || strings.TrimSpace(live.Explanation) == "" {
			return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "non-hosted live mode requires a kebab-case reasonCode and explanation")
		}
		if live.Driver != "" || live.Ref != "" || live.Seed != "" || live.Comparator != "" || live.ProofMode != "" ||
			live.PRTimeoutMinutes != 0 || live.ScheduledTimeoutMinutes != 0 || live.RunnerLabel != "" || live.Trust != nil {
			return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "non-hosted live mode cannot carry trusted execution policy")
		}
		return nil
	}

	if strings.TrimSpace(live.Driver) == "" || strings.TrimSpace(live.Ref) == "" || strings.TrimSpace(live.RunnerLabel) == "" {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "hosted live policy requires driver, ref, and runnerLabel")
	}
	if live.ProofMode != ProofLiveInstall && live.ProofMode != ProofLiveConfigRoundtrip {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "hosted proofMode must be live-install or live-config-roundtrip")
	}
	if live.PRTimeoutMinutes <= 0 || live.PRTimeoutMinutes > 25 || live.ScheduledTimeoutMinutes <= 0 || live.ScheduledTimeoutMinutes > 45 {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "hosted timeouts must fit PR 25-minute and scheduled 45-minute caps")
	}
	if live.ReasonCode != "" || live.Explanation != "" {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "hosted live policy cannot carry a non-hosted reason")
	}
	if live.ProofMode == ProofLiveConfigRoundtrip && (live.Seed == "" || live.Comparator == "") {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "live-config-roundtrip requires seed and comparator")
	}
	if err := validateNamedTrustHash("seed", live.Seed, trustSeedHash(live.Trust)); err != nil {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "%v", err)
	}
	if err := validateNamedTrustHash("comparator", live.Comparator, trustComparatorHash(live.Trust)); err != nil {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "%v", err)
	}
	return nil
}

func knownLiveMode(mode LiveMode) bool {
	switch mode {
	case LiveHosted, LiveCandidate, LiveBlocked, LiveLab, LiveManual, LiveNotApplicable:
		return true
	default:
		return false
	}
}

func trustSeedHash(trust *TrustHashes) string {
	if trust == nil {
		return ""
	}
	return trust.SeedSHA256
}

func trustComparatorHash(trust *TrustHashes) string {
	if trust == nil {
		return ""
	}
	return trust.ComparatorSHA256
}

func validateNamedTrustHash(name, input, hash string) error {
	if input == "" && hash != "" {
		return fmt.Errorf("%sSha256 cannot be declared without %s", name, name)
	}
	if input != "" && !lowerSHA256Pattern.MatchString(hash) {
		return fmt.Errorf("named %s requires a lowercase SHA-256 trust hash", name)
	}
	return nil
}
