// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"strings"
)

// CommandWarning is a non-fatal, machine-readable command outcome. Driver and
// Ref are omitted when a warning does not belong to a specific package lane;
// ModuleID is omitted when it does not belong to a specific config module.
type CommandWarning struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Driver   string `json:"driver,omitempty"`
	Source   string `json:"source,omitempty"`
	Ref      string `json:"ref,omitempty"`
	ModuleID string `json:"moduleId,omitempty"`
}

func possibleDuplicatePackageWarnings(routes []*routedDriverApp) []CommandWarning {
	type ownershipObservation struct {
		displayName string
		driverName  string
	}

	seen := make([]ownershipObservation, 0, len(routes))
	var warnings []CommandWarning
	for _, route := range routes {
		if route == nil || route.isManual {
			continue
		}
		displayName := strings.TrimSpace(route.app.DisplayName)
		if displayName == "" {
			continue
		}

		otherDriver := ""
		for _, earlier := range seen {
			if earlier.driverName != route.driverName && strings.EqualFold(earlier.displayName, displayName) {
				otherDriver = earlier.driverName
				break
			}
		}
		if otherDriver != "" {
			warnings = append(warnings, CommandWarning{
				Code:    "possible_duplicate",
				Message: fmt.Sprintf("%s is listed twice in the manifest: once for %s and once for %s. Endstate kept both entries and handles each one with its own package manager. If they are the same app, remove one entry so it is managed only once.", displayName, otherDriver, route.driverName),
				Driver:  route.driverName,
				Ref:     route.ref,
			})
		}
		seen = append(seen, ownershipObservation{displayName: displayName, driverName: route.driverName})
	}
	return warnings
}
