// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package winget

import (
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/snapshot"
)

var _ driver.InstalledEnumerator = (*WingetDriver)(nil)

var exportInstalledFn = snapshot.WingetExport
var listInstalledPackagesFn = snapshot.TakeSnapshot
var detailsInstalledPackagesFn = snapshot.WingetDetailsSource

// EnumerateInstalled returns Winget's authoritative export ledger, enriched
// with relationship evidence from `winget list --details`. The legacy table is
// only used when details is unavailable or incomplete.
func (w *WingetDriver) EnumerateInstalled() ([]driver.InstalledPackage, error) {
	exported, err := exportInstalledFn()
	if err != nil {
		return nil, err
	}

	details, detailsErr := detailsInstalledPackagesFn("winget")
	needFallback := detailsErr != nil
	if !needFallback {
		for _, app := range exported {
			if strings.TrimSpace(app.ID) != "" {
				if _, ok := details[strings.ToLower(app.ID)]; !ok {
					needFallback = true
					break
				}
			}
		}
	}
	evidence := map[string]snapshot.SnapshotApp{}
	if needFallback {
		listed, listErr := listInstalledPackagesFn()
		if listErr == nil {
			for _, app := range listed {
				evidence[strings.ToLower(app.ID)] = app
			}
		}
	}

	packages := make([]driver.InstalledPackage, 0, len(exported))
	for _, app := range exported {
		if app.ID == "" {
			continue
		}
		name, version := app.Name, app.Version
		if listedApp, ok := evidence[strings.ToLower(app.ID)]; ok {
			if listedApp.Name != "" {
				name = listedApp.Name
			}
			if listedApp.Version != "" {
				version = listedApp.Version
			}
		}
		detail, known := details[strings.ToLower(app.ID)]
		if detailsErr != nil {
			known = false
		}
		if detail.Name != "" {
			name = detail.Name
		}
		packages = append(packages, driver.InstalledPackage{Ref: app.ID, DisplayName: name, Version: version, Source: app.Source, InventoryLocalIdentifiers: detail.InventoryLocalIdentifiers, InventoryRelationshipKnown: known})
	}

	sort.SliceStable(packages, func(i, j int) bool {
		left, right := strings.ToLower(packages[i].Ref), strings.ToLower(packages[j].Ref)
		if left != right {
			return left < right
		}
		return packages[i].Ref < packages[j].Ref
	})
	return packages, nil
}
