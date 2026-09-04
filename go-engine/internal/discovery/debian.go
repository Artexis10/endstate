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

var debianPackageName = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]*(?::[a-z0-9][a-z0-9-]*)?$`)

// DebianAdapter joins apt's explicit/manual intent ledger with dpkg's installed
// version evidence. Dependency-only dpkg rows are counted, never presented as
// user applications.
type DebianAdapter struct {
	Runner CommandRunner
}

func (DebianAdapter) ID() string { return "debian-explicit" }

func (adapter DebianAdapter) Inventory(ctx context.Context) (AdapterInventory, error) {
	runner := adapter.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	manualOutput, err := runner.Run(ctx, Command{
		Path: "apt-mark", Args: []string{"showmanual"}, MaxOutputBytes: 1 << 20,
	})
	if err != nil {
		return AdapterInventory{}, fmt.Errorf("read explicitly installed Debian packages: %w", err)
	}
	manual, err := parseDebianManual(manualOutput)
	if err != nil {
		return AdapterInventory{}, err
	}
	dpkgOutput, err := runner.Run(ctx, Command{
		Path:           "dpkg-query",
		Args:           []string{"--show", `--showformat=${binary:Package}\t${Version}\t${db:Status-Abbrev}\t${Essential}\t${Priority}\t${Section}\n`},
		MaxOutputBytes: MaxCommandOutputBytes,
	})
	if err != nil {
		return AdapterInventory{}, fmt.Errorf("read installed Debian package versions: %w", err)
	}
	installed, err := parseDpkgInstalled(dpkgOutput)
	if err != nil {
		return AdapterInventory{}, err
	}

	manualRefs := make([]string, 0, len(manual))
	for ref := range manual {
		manualRefs = append(manualRefs, ref)
	}
	sort.Strings(manualRefs)
	items := make([]InventoryItem, 0, len(manualRefs))
	ignored := 0
	for _, ref := range manualRefs {
		pkg, installedOK := installed[ref]
		if installedOK && pkg.systemOwned() {
			ignored++
			continue
		}
		items = append(items, InventoryItem{
			Source: "debian", Kind: EvidencePackage, Ref: ref, DisplayName: ref,
			Version: pkg.Version, Explicit: true,
		})
	}
	for ref := range installed {
		if !manual[ref] {
			ignored++
		}
	}
	return AdapterInventory{Items: items, Ignored: ignored}, nil
}

func parseDebianManual(output []byte) (map[string]bool, error) {
	manual := make(map[string]bool)
	for lineNumber, line := range strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !debianPackageName.MatchString(line) {
			return nil, fmt.Errorf("Debian explicit-package inventory row %d has an invalid package name", lineNumber+1)
		}
		manual[debianBasePackage(line)] = true
	}
	return manual, nil
}

type debianInstalledPackage struct {
	Version   string
	Essential string
	Priority  string
	Section   string
}

func (pkg debianInstalledPackage) systemOwned() bool {
	return strings.EqualFold(pkg.Essential, "yes") ||
		strings.EqualFold(pkg.Priority, "required") ||
		strings.EqualFold(pkg.Section, "metapackages")
}

func parseDpkgInstalled(output []byte) (map[string]debianInstalledPackage, error) {
	installed := make(map[string]debianInstalledPackage)
	for lineNumber, line := range strings.Split(strings.ReplaceAll(string(output), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 6 {
			return nil, fmt.Errorf("Debian installed-package inventory row %d has %d fields, want 6", lineNumber+1, len(fields))
		}
		name := strings.TrimSpace(fields[0])
		version := strings.TrimSpace(fields[1])
		status := strings.TrimSpace(fields[2])
		pkg := debianInstalledPackage{
			Version: version, Essential: strings.TrimSpace(fields[3]),
			Priority: strings.TrimSpace(fields[4]), Section: strings.TrimSpace(fields[5]),
		}
		if !debianPackageName.MatchString(name) || version == "" {
			return nil, fmt.Errorf("Debian installed-package inventory row %d has invalid name or version", lineNumber+1)
		}
		if status != "ii" {
			continue
		}
		base := debianBasePackage(name)
		if existing, ok := installed[base]; !ok || name == base || version < existing.Version {
			installed[base] = pkg
		}
	}
	return installed, nil
}

func debianBasePackage(name string) string {
	if index := strings.LastIndexByte(name, ':'); index > 0 {
		return name[:index]
	}
	return name
}
