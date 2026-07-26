// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"fmt"

	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

var v1Assertions = map[string]struct{}{
	validationmatrix.AssertionCaptured: {}, validationmatrix.AssertionPayload: {},
	validationmatrix.AssertionProvenance: {}, validationmatrix.AssertionRewrittenRestore: {},
	validationmatrix.AssertionContent: {}, validationmatrix.AssertionRebuild: {},
	validationmatrix.AssertionVerify: {}, validationmatrix.AssertionNestedSummary: {},
	validationmatrix.AssertionRevert: {},
}

var v1Proofs = map[validationmatrix.ProofLevel]struct{}{
	validationmatrix.ProofCatalog: {}, validationmatrix.ProofEngineContract: {}, validationmatrix.ProofConfigRoundtripV1: {},
}

var v2Assertions = map[string]struct{}{
	validationmatrix.AssertionCaptured: {}, validationmatrix.AssertionPayload: {},
	validationmatrix.AssertionProvenance: {}, validationmatrix.AssertionRewrittenRestore: {},
	validationmatrix.AssertionContent: {}, validationmatrix.AssertionRebuild: {},
	validationmatrix.AssertionVerify: {}, validationmatrix.AssertionNestedSummary: {},
	validationmatrix.AssertionRevert: {}, validationmatrix.AssertionGeneration: {},
	validationmatrix.AssertionValidation: {},
}

var v2Proofs = map[validationmatrix.ProofLevel]struct{}{
	validationmatrix.ProofCatalog: {}, validationmatrix.ProofEngineContract: {}, validationmatrix.ProofConfigRoundtripV2: {},
}

func evaluateAssertions(scenario validationmatrix.Scenario, counts map[string]int, operations OperationCounts, proof []validationmatrix.ProofLevel) ([]validationmatrix.ProofLevel, *Failure) {
	if operations.Executed <= 0 || operations.Skipped > 0 && operations.Executed == 0 {
		return nil, fail(CodeAssertionContract, "assertions", "operations", "scenario must execute at least one operation")
	}
	allowedAssertions, allowedProofs := v1Assertions, v1Proofs
	canonical := []validationmatrix.ProofLevel{validationmatrix.ProofCatalog, validationmatrix.ProofEngineContract, validationmatrix.ProofConfigRoundtripV1}
	if scenario.Mode == validationmatrix.ScenarioConfigGenerationV2 {
		allowedAssertions, allowedProofs = v2Assertions, v2Proofs
		canonical = []validationmatrix.ProofLevel{validationmatrix.ProofCatalog, validationmatrix.ProofEngineContract, validationmatrix.ProofConfigRoundtripV2}
	}
	for name, count := range counts {
		if _, known := allowedAssertions[name]; !known || count <= 0 {
			return nil, fail(CodeAssertionContract, "assertions", name, "assertion is unknown or vacuous")
		}
	}
	for name, minimum := range scenario.MinimumAssertions {
		if minimum <= 0 || counts[name] < minimum {
			return nil, fail(CodeAssertionContract, "assertions", name, fmt.Sprintf("observed %d below declared minimum %d", counts[name], minimum))
		}
	}
	seen := map[validationmatrix.ProofLevel]struct{}{}
	for _, level := range proof {
		if _, allowed := allowedProofs[level]; !allowed {
			return nil, fail(CodeAssertionContract, "assertions", "proofLevels", "proof level exceeds this scenario")
		}
		if _, duplicate := seen[level]; duplicate {
			return nil, fail(CodeAssertionContract, "assertions", "proofLevels", "proof level is duplicated")
		}
		seen[level] = struct{}{}
	}
	if len(seen) != len(canonical) {
		return nil, fail(CodeAssertionContract, "assertions", "proofLevels", "passing v1 proof levels are incomplete")
	}
	for _, level := range canonical {
		if _, exists := seen[level]; !exists {
			return nil, fail(CodeAssertionContract, "assertions", "proofLevels", "passing v1 proof levels are incomplete")
		}
	}
	return canonical, nil
}
