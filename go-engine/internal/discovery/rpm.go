// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var rpmPackageName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9+._-]*$`)

// RPMAdapter reads DNF's user-installed reason ledger. It never falls back to
// an unfiltered rpm -qa sweep: dependency closure is not portable user intent.
type RPMAdapter struct {
	Runner CommandRunner
}

func (RPMAdapter) ID() string { return "rpm-explicit" }

func (adapter RPMAdapter) Inventory(ctx context.Context) (AdapterInventory, error) {
	runner := adapter.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	queries := []Command{
		{Path: "dnf5", Args: []string{"repoquery", "--installed", "--userinstalled", `--queryformat=%{name}\t%{evr}\t%{arch}\n`}, MaxOutputBytes: MaxCommandOutputBytes},
		{Path: "dnf", Args: []string{"repoquery", "--installed", "--userinstalled", `--qf=%{name}\t%{evr}\t%{arch}\n`}, MaxOutputBytes: MaxCommandOutputBytes},
	}
	var failures []error
	for _, query := range queries {
		output, err := runner.Run(ctx, query)
		if errors.Is(err, ErrNotAvailable) {
			continue
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("%s user-installed query: %w", query.Path, err))
			continue
		}
		items, ignored, parseErr := parseRPMExplicit(output)
		if parseErr != nil {
			return AdapterInventory{}, parseErr
		}
		return AdapterInventory{Items: items, Ignored: ignored}, nil
	}
	if len(failures) > 0 {
		return AdapterInventory{}, errors.Join(failures...)
	}
	return AdapterInventory{}, ErrNotAvailable
}

func parseRPMExplicit(output []byte) ([]InventoryItem, int, error) {
	byName := make(map[string]InventoryItem)
	ignored := 0
	for lineNumber, line := range normalizedLines(output) {
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			return nil, 0, fmt.Errorf("RPM explicit-package inventory row %d has %d fields, want 3", lineNumber+1, len(fields))
		}
		name := strings.TrimSpace(fields[0])
		version := strings.TrimSpace(fields[1])
		arch := strings.TrimSpace(fields[2])
		if !rpmPackageName.MatchString(name) || version == "" || arch == "" {
			return nil, 0, fmt.Errorf("RPM explicit-package inventory row %d has an invalid name, version, or architecture", lineNumber+1)
		}
		candidate := InventoryItem{Source: "rpm", Kind: EvidencePackage, Ref: name, DisplayName: name, Version: version, Explicit: true}
		if existing, ok := byName[name]; ok {
			ignored++
			if candidate.Version < existing.Version {
				continue
			}
		}
		byName[name] = candidate
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	items := make([]InventoryItem, 0, len(names))
	for _, name := range names {
		items = append(items, byName[name])
	}
	return items, ignored, nil
}

func normalizedLines(output []byte) []string {
	raw := strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
