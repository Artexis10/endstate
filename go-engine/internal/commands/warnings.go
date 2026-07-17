// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"strings"
)

// CommandWarning is a non-fatal, machine-readable command outcome. Driver and
// Ref are omitted when a warning does not belong to a specific package lane.
type CommandWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Driver  string `json:"driver,omitempty"`
	Ref     string `json:"ref,omitempty"`
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

		duplicate := false
		for _, earlier := range seen {
			if earlier.driverName != route.driverName && strings.EqualFold(earlier.displayName, displayName) {
				duplicate = true
				break
			}
		}
		if duplicate {
			warnings = append(warnings, CommandWarning{
				Code:    "possible_duplicate",
				Message: fmt.Sprintf("%s declares the same display name %q as another package driver; both entries were preserved", route.driverName, displayName),
				Driver:  route.driverName,
				Ref:     route.ref,
			})
		}
		seen = append(seen, ownershipObservation{displayName: displayName, driverName: route.driverName})
	}
	return warnings
}
