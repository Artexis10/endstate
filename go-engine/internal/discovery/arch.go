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

var archPackageName = regexp.MustCompile(`^[A-Za-z0-9@][A-Za-z0-9@+._-]*$`)

// ArchAdapter reads pacman's explicit-install reason ledger with versions.
type ArchAdapter struct {
	Runner CommandRunner
}

func (ArchAdapter) ID() string { return "arch-explicit" }

func (adapter ArchAdapter) Inventory(ctx context.Context) (AdapterInventory, error) {
	runner := adapter.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	output, err := runner.Run(ctx, Command{
		Path: "pacman", Args: []string{"--query", "--explicit"}, MaxOutputBytes: MaxCommandOutputBytes,
	})
	if err != nil {
		return AdapterInventory{}, fmt.Errorf("read explicitly installed Arch packages: %w", err)
	}
	items, err := parseArchExplicit(output)
	if err != nil {
		return AdapterInventory{}, err
	}
	return AdapterInventory{Items: items}, nil
}

func parseArchExplicit(output []byte) ([]InventoryItem, error) {
	items := make([]InventoryItem, 0)
	for lineNumber, line := range normalizedLines(output) {
		fields := strings.Fields(line)
		if len(fields) != 2 || !archPackageName.MatchString(fields[0]) || fields[1] == "" {
			return nil, fmt.Errorf("Arch explicit-package inventory row %d is malformed", lineNumber+1)
		}
		items = append(items, InventoryItem{
			Source: "arch", Kind: EvidencePackage, Ref: fields[0], DisplayName: fields[0], Version: fields[1], Explicit: true,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Ref < items[j].Ref })
	return items, nil
}
