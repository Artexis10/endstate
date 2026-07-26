// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

var stableKebabPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func validateLivePolicy(record *ValidationRecord, mod *modules.Module) error {
	live := &record.Live
	if !knownLiveMode(live.Mode) {
		return validationError(CodeUnknownLiveMode, record.ModuleID, record.FilePath, "unknown live mode %q", live.Mode)
	}
	if live.Mode == LiveCandidate && hasExecutionPolicy(*live) {
		if !stableKebabPattern.MatchString(live.ReasonCode) || strings.TrimSpace(live.Explanation) == "" {
			return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "candidate live mode requires a kebab-case reasonCode and explanation")
		}
		return validateExecutionPolicy(record, mod, live)
	}
	if live.Mode != LiveHosted {
		if !stableKebabPattern.MatchString(live.ReasonCode) || strings.TrimSpace(live.Explanation) == "" {
			return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "non-hosted live mode requires a kebab-case reasonCode and explanation")
		}
		if hasExecutionPolicy(*live) {
			return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "non-hosted live mode cannot carry trusted execution policy")
		}
		return nil
	}

	if live.ReasonCode != "" || live.Explanation != "" {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "hosted live policy cannot carry a non-hosted reason")
	}
	return validateExecutionPolicy(record, mod, live)
}

func hasExecutionPolicy(live LivePolicy) bool {
	return live.Driver != "" || live.Ref != "" || live.Seed != "" || live.Comparator != "" || live.ProofMode != "" ||
		live.PRTimeoutMinutes != 0 || live.ScheduledTimeoutMinutes != 0 || live.RunnerLabel != "" || live.Trust != nil
}

func validateExecutionPolicy(record *ValidationRecord, mod *modules.Module, live *LivePolicy) error {
	if strings.TrimSpace(live.Driver) == "" || strings.TrimSpace(live.Ref) == "" || strings.TrimSpace(live.RunnerLabel) == "" {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "live execution policy requires driver, ref, and runnerLabel")
	}
	if live.Driver != "winget" || !containsExact(mod.Matches.Winget, live.Ref) {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "live policy ref must exactly match a production winget install reference")
	}
	if live.ProofMode != ProofLiveInstall && live.ProofMode != ProofLiveConfigRoundtrip {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "live proofMode must be live-install or live-config-roundtrip")
	}
	if live.PRTimeoutMinutes <= 0 || live.PRTimeoutMinutes > 25 || live.ScheduledTimeoutMinutes <= 0 || live.ScheduledTimeoutMinutes > 45 {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "live timeouts must fit PR 25-minute and scheduled 45-minute caps")
	}
	if live.ProofMode == ProofLiveConfigRoundtrip && (live.Seed == "" || live.Comparator == "") {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "live-config-roundtrip requires seed and comparator")
	}
	if err := validateNamedTrustHash("seed", live.Seed, trustSeedHash(live.Trust)); err != nil {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "%v", err)
	}
	if err := validateBuiltInComparator(live.Comparator, trustComparatorHash(live.Trust)); err != nil {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "%v", err)
	}
	if err := validateSeedFile(record, *live); err != nil {
		return validationError(CodeInvalidLivePolicy, record.ModuleID, record.FilePath, "%v", err)
	}
	return nil
}

func containsExact(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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

func validateBuiltInComparator(comparator BuiltInComparator, hash string) error {
	if comparator == "" {
		if hash != "" {
			return fmt.Errorf("comparatorSha256 cannot be declared without comparator")
		}
		return nil
	}
	if comparator != ComparatorExactBytes {
		return fmt.Errorf("comparator must be the built-in exact-bytes enum")
	}
	if hash != "" {
		return fmt.Errorf("built-in comparator exact-bytes cannot declare comparatorSha256")
	}
	return nil
}

func validateSeedFile(record *ValidationRecord, live LivePolicy) error {
	if live.Seed == "" {
		return nil
	}
	if !isPortableRepositoryRelativePath(live.Seed) {
		return fmt.Errorf("seed must be a safe repository-relative path")
	}
	path := filepath.Join(filepath.Dir(record.FilePath), filepath.FromSlash(live.Seed))
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read hash-bound seed: %w", err)
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != trustSeedHash(live.Trust) {
		return fmt.Errorf("seedSha256 does not match %s", live.Seed)
	}
	return nil
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
