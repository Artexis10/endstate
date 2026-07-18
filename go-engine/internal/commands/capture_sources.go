// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/packagesource"
	"github.com/Artexis10/endstate/go-engine/internal/snapshot"
)

type sourceWingetCaptureEnumerator struct {
	excludeStore     bool
	structuredEvents bool
}

type captureEnumeratorWithWarnings interface {
	EnumerateInstalledWithWarnings() ([]driver.InstalledPackage, []CommandWarning, error)
}

var enumerateWingetSourceFn = enumerateWingetSource

func enumerateWingetSource(source string, structuredEvents bool) ([]driver.InstalledPackage, error) {
	type snapshotResult struct {
		apps []snapshot.SnapshotApp
		err  error
	}
	exportCh, listCh := make(chan snapshotResult, 1), make(chan snapshotResult, 1)
	go func() { apps, err := snapshot.WingetExportSource(source); exportCh <- snapshotResult{apps, err} }()
	go func() { apps, err := snapshot.TakeSnapshotSource(source); listCh <- snapshotResult{apps, err} }()
	exported, listed := <-exportCh, <-listCh
	if exported.err != nil {
		return nil, exported.err
	}
	evidence := make(map[string]snapshot.SnapshotApp, len(listed.apps))
	if listed.err == nil {
		for _, app := range listed.apps {
			evidence[strings.ToLower(app.ID)] = app
		}
	}
	packages := make([]driver.InstalledPackage, 0, len(exported.apps))
	for _, app := range exported.apps {
		if strings.TrimSpace(app.ID) == "" {
			continue
		}
		listedApp := evidence[strings.ToLower(app.ID)]
		name, version := listedApp.Name, listedApp.Version
		if name == "" {
			name = app.Name
		}
		if version == "" {
			version = app.Version
		}
		packages = append(packages, driver.InstalledPackage{Ref: app.ID, DisplayName: name, Version: version, Source: source})
	}
	return packages, nil
}

func (e sourceWingetCaptureEnumerator) EnumerateInstalled() ([]driver.InstalledPackage, error) {
	packages, _, err := e.EnumerateInstalledWithWarnings()
	return packages, err
}

func (e sourceWingetCaptureEnumerator) EnumerateInstalledWithWarnings() ([]driver.InstalledPackage, []CommandWarning, error) {
	sources := []string{packagesource.Winget}
	if !e.excludeStore {
		sources = append(sources, packagesource.MSStore)
	}
	type result struct {
		source   string
		packages []driver.InstalledPackage
		err      error
	}
	run := func() ([]driver.InstalledPackage, map[string]error) {
		results := make(chan result, len(sources))
		var wg sync.WaitGroup
		for _, source := range sources {
			source := source
			wg.Add(1)
			go func() {
				defer wg.Done()
				packages, err := enumerateWingetSourceFn(source, e.structuredEvents)
				results <- result{source, packages, err}
			}()
		}
		wg.Wait()
		close(results)
		var packages []driver.InstalledPackage
		failures := map[string]error{}
		for result := range results {
			if result.err != nil {
				failures[result.source] = result.err
				continue
			}
			for i := range result.packages {
				result.packages[i].Source = packagesource.ResolveWinget(result.packages[i].Ref, result.source)
			}
			packages = append(packages, result.packages...)
		}
		return packages, failures
	}
	packages, failures := run()
	if len(packages) == 0 {
		if !e.structuredEvents {
			fmt.Fprintf(os.Stderr, "Warning: selected winget sources returned 0 packages, retrying after %v...\n", snapshotRetryDelay)
		}
		time.Sleep(snapshotRetryDelay)
		packages, failures = run()
	}
	if len(packages) == 0 && len(failures) == len(sources) {
		return nil, nil, fmt.Errorf("all selected winget sources failed")
	}
	var warnings []CommandWarning
	if len(packages) > 0 {
		if err := failures[packagesource.MSStore]; err != nil {
			warnings = append(warnings, CommandWarning{Code: "store_source_unavailable", Message: "Microsoft Store source is unavailable: " + err.Error(), Driver: "winget", Source: packagesource.MSStore})
		}
		if err := failures[packagesource.Winget]; err != nil {
			warnings = append(warnings, CommandWarning{Code: "winget_source_unavailable", Message: "WinGet community source is unavailable: " + err.Error(), Driver: "winget", Source: packagesource.Winget})
		}
	}
	return dedupeWingetSourcePackages(packages), warnings, nil
}

func dedupeWingetSourcePackages(packages []driver.InstalledPackage) []driver.InstalledPackage {
	byRef := map[string]driver.InstalledPackage{}
	order := []string{}
	for _, pkg := range packages {
		key := strings.ToLower(strings.TrimSpace(pkg.Ref))
		if key == "" {
			continue
		}
		current, exists := byRef[key]
		if !exists {
			byRef[key] = pkg
			order = append(order, key)
			continue
		}
		want := packagesource.Winget
		if packagesource.IsStoreID(pkg.Ref) {
			want = packagesource.MSStore
		}
		if pkg.Source == want && current.Source != want {
			byRef[key] = pkg
		}
	}
	sort.Strings(order)
	result := make([]driver.InstalledPackage, 0, len(order))
	for _, key := range order {
		result = append(result, byRef[key])
	}
	return result
}
