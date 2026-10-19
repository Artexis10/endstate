// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"slices"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/registryfile"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

// verifyReasonNotRestored marks a verify assertion about a captured value that
// the same run did not restore.
const verifyReasonNotRestored = "not_restored"

// registryValueIdentity names one registry value by its key and its value
// name, kept apart. A value name may hold a backslash, so the joined text
// cannot tell value `B\C` on key `A` from value `C` on key `A\B`.
type registryValueIdentity struct{ key, valueName string }

// names reports whether identity is the value valueName of key, compared as
// the registry compares names. No lower-cased form can serve as a map key:
// Go's lower-casing merges names that the registry keeps apart.
func (identity registryValueIdentity) names(key, valueName string) bool {
	return registryfile.SameKey(identity.key, key) && registryfile.SameValueName(identity.valueName, valueName)
}

// restoredRegistryValues returns the registry-set values an apply run acted
// on. A failed write is included, so verify still reports it.
func restoredRegistryValues(items []restore.RestoreResult) []registryValueIdentity {
	var restored []registryValueIdentity
	for _, item := range items {
		if item.RestoreType == "registry-set" {
			restored = append(restored, registryValueIdentity{key: item.RegistryKey, valueName: item.RegistryValueName})
		}
	}
	return restored
}

// scopeRestoredVerifies splits manifest verify entries into those to evaluate
// and visible skipped results. An entry that carries fromModule asserts a value
// captured on the old PC; it holds only after this run restored that value, so
// without that restore it is skipped rather than reported as a failure.
func scopeRestoredVerifies(entries []manifest.VerifyEntry, restored []registryValueIdentity) ([]manifest.VerifyEntry, []VerifyItem) {
	evaluate := make([]manifest.VerifyEntry, 0, len(entries))
	var skipped []VerifyItem
	for _, entry := range entries {
		if entry.FromModule == "" {
			evaluate = append(evaluate, entry)
			continue
		}
		target := entry.Path + `\` + entry.ValueName
		if slices.ContainsFunc(restored, func(identity registryValueIdentity) bool { return identity.names(entry.Path, entry.ValueName) }) {
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
