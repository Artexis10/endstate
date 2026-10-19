// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/registryfile"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

// verifyReasonNotRestored marks a verify assertion about a captured value that
// the same run did not restore.
const verifyReasonNotRestored = "not_restored"

// restoredRegistryValues returns the registry-set targets an apply run acted
// on, in registryfile.MatchKey form. A failed write is included, so verify
// still reports it.
func restoredRegistryValues(items []restore.RestoreResult) map[string]struct{} {
	restored := map[string]struct{}{}
	for _, item := range items {
		if item.RestoreType == "registry-set" {
			restored[registryfile.MatchKey(item.Target)] = struct{}{}
		}
	}
	return restored
}

// scopeRestoredVerifies splits manifest verify entries into those to evaluate
// and visible skipped results. An entry that carries fromModule asserts a value
// captured on the old PC; it holds only after this run restored that value, so
// without that restore it is skipped rather than reported as a failure.
func scopeRestoredVerifies(entries []manifest.VerifyEntry, restored map[string]struct{}) ([]manifest.VerifyEntry, []VerifyItem) {
	evaluate := make([]manifest.VerifyEntry, 0, len(entries))
	var skipped []VerifyItem
	for _, entry := range entries {
		if entry.FromModule == "" {
			evaluate = append(evaluate, entry)
			continue
		}
		target := entry.Path + `\` + entry.ValueName
		if _, ok := restored[registryfile.MatchKey(target)]; ok {
			evaluate = append(evaluate, entry)
			continue
		}
		skipped = append(skipped, VerifyItem{
			Type: entry.Type, Status: driver.StatusSkipped, Reason: verifyReasonNotRestored,
			Message: fmt.Sprintf("%s was not restored in this run, so its captured value was not checked", target),
		})
	}
	return evaluate, skipped
}
