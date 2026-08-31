// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var flatpakApplicationID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.[A-Za-z0-9._-]+$`)

// FlatpakAdapter lists applications separately from runtimes so SDK/base
// closures are counted but never presented as user applications.
type FlatpakAdapter struct {
	Runner CommandRunner
}

func (FlatpakAdapter) ID() string { return "flatpak-applications" }

func (adapter FlatpakAdapter) Inventory(ctx context.Context) (AdapterInventory, error) {
	runner := adapter.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	appsOutput, err := runner.Run(ctx, Command{
		Path: "flatpak", Args: []string{"list", "--app", "--columns=application,version"}, MaxOutputBytes: MaxCommandOutputBytes,
	})
	if err != nil {
		return AdapterInventory{}, fmt.Errorf("read installed Flatpak applications: %w", err)
	}
	runtimesOutput, err := runner.Run(ctx, Command{
		Path: "flatpak", Args: []string{"list", "--runtime", "--columns=application"}, MaxOutputBytes: MaxCommandOutputBytes,
	})
	if err != nil {
		return AdapterInventory{}, fmt.Errorf("count installed Flatpak runtimes: %w", err)
	}
	items, err := parseFlatpakApplications(appsOutput)
	if err != nil {
		return AdapterInventory{}, err
	}
	return AdapterInventory{Items: items, Ignored: len(normalizedLines(runtimesOutput))}, nil
}

func parseFlatpakApplications(output []byte) ([]InventoryItem, error) {
	items := make([]InventoryItem, 0)
	seen := make(map[string]bool)
	for lineNumber, line := range normalizedLines(output) {
		fields := strings.Split(line, "\t")
		if len(fields) != 2 {
			return nil, fmt.Errorf("Flatpak application inventory row %d has %d fields, want 2", lineNumber+1, len(fields))
		}
		id := strings.TrimSpace(fields[0])
		if !flatpakApplicationID.MatchString(id) {
			return nil, fmt.Errorf("Flatpak application inventory row %d has an invalid application ID", lineNumber+1)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		items = append(items, InventoryItem{
			Source: "flatpak", Kind: EvidencePackage, Ref: id, DisplayName: id,
			Version: strings.TrimSpace(fields[1]), UserFacing: true, Explicit: true,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Ref < items[j].Ref })
	return items, nil
}
