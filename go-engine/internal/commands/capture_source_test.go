// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

func TestSourceWingetCapture_DefaultDualSourceAndPartialWarning(t *testing.T) {
	orig := enumerateWingetSourceFn
	t.Cleanup(func() { enumerateWingetSourceFn = orig })
	var mu sync.Mutex
	var sources []string
	enumerateWingetSourceFn = func(source string, _ bool) ([]driver.InstalledPackage, error) {
		mu.Lock()
		sources = append(sources, source)
		mu.Unlock()
		if source == "msstore" {
			return nil, errors.New("store disabled by policy")
		}
		return []driver.InstalledPackage{{Ref: "Vendor.App", DisplayName: "App", Source: source}}, nil
	}
	enumerator := sourceWingetCaptureEnumerator{}
	packages, warnings, err := enumerator.EnumerateInstalledWithWarnings()
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 1 || packages[0].Source != "winget" {
		t.Fatalf("packages = %+v", packages)
	}
	if len(warnings) != 1 || warnings[0].Code != "store_source_unavailable" || warnings[0].Source != "msstore" {
		t.Fatalf("warnings = %+v", warnings)
	}
	joined := strings.Join(sources, ",")
	if !strings.Contains(joined, "winget") || !strings.Contains(joined, "msstore") {
		t.Fatalf("sources = %v", sources)
	}
}

func TestSourceWingetCapture_ExcludeStoreAvoidsAccess(t *testing.T) {
	orig := enumerateWingetSourceFn
	t.Cleanup(func() { enumerateWingetSourceFn = orig })
	var sources []string
	enumerateWingetSourceFn = func(source string, _ bool) ([]driver.InstalledPackage, error) {
		sources = append(sources, source)
		return []driver.InstalledPackage{{Ref: "Vendor.App", Source: source}}, nil
	}
	enumerator := sourceWingetCaptureEnumerator{excludeStore: true}
	_, _, err := enumerator.EnumerateInstalledWithWarnings()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(sources, ",") != "winget" {
		t.Fatalf("sources = %v, want only winget", sources)
	}
}

func TestSourceWingetCapture_RetriesOnlyWhenAggregateIsEmpty(t *testing.T) {
	orig, origDelay := enumerateWingetSourceFn, snapshotRetryDelay
	t.Cleanup(func() { enumerateWingetSourceFn = orig; snapshotRetryDelay = origDelay })
	snapshotRetryDelay = 0
	calls := map[string]int{}
	var mu sync.Mutex
	enumerateWingetSourceFn = func(source string, _ bool) ([]driver.InstalledPackage, error) {
		mu.Lock()
		calls[source]++
		n := calls[source]
		mu.Unlock()
		if source == "winget" && n == 2 {
			return []driver.InstalledPackage{{Ref: "Vendor.App", Source: source}}, nil
		}
		return []driver.InstalledPackage{}, nil
	}
	packages, warnings, err := (sourceWingetCaptureEnumerator{}).EnumerateInstalledWithWarnings()
	if err != nil || len(packages) != 1 || len(warnings) != 0 {
		t.Fatalf("packages=%+v warnings=%+v err=%v", packages, warnings, err)
	}
	if calls["winget"] != 2 || calls["msstore"] != 2 {
		t.Fatalf("calls = %v, want one retry per selected source", calls)
	}
}

func TestSourceWingetCapture_SuccessfulEmptyStoreHasNoUnavailableWarning(t *testing.T) {
	orig := enumerateWingetSourceFn
	t.Cleanup(func() { enumerateWingetSourceFn = orig })
	enumerateWingetSourceFn = func(source string, _ bool) ([]driver.InstalledPackage, error) {
		if source == "winget" {
			return []driver.InstalledPackage{{Ref: "Vendor.App", Source: source}}, nil
		}
		return []driver.InstalledPackage{}, nil
	}
	_, warnings, err := (sourceWingetCaptureEnumerator{}).EnumerateInstalledWithWarnings()
	if err != nil || len(warnings) != 0 {
		t.Fatalf("warnings=%+v err=%v", warnings, err)
	}
}

func TestSourceWingetCapture_StoreOnlySuccessWarnsCommunityUnavailable(t *testing.T) {
	orig := enumerateWingetSourceFn
	t.Cleanup(func() { enumerateWingetSourceFn = orig })
	enumerateWingetSourceFn = func(source string, _ bool) ([]driver.InstalledPackage, error) {
		if source == "winget" {
			return nil, errors.New("community source blocked")
		}
		return []driver.InstalledPackage{{Ref: "9NBLGGH4NNS1", Source: source}}, nil
	}
	packages, warnings, err := (sourceWingetCaptureEnumerator{}).EnumerateInstalledWithWarnings()
	if err != nil || len(packages) != 1 || len(warnings) != 1 || warnings[0].Code != "winget_source_unavailable" || warnings[0].Source != "winget" {
		t.Fatalf("packages=%+v warnings=%+v err=%v", packages, warnings, err)
	}
}

func TestSourceWingetCapture_AllSourcesFailAfterRetry(t *testing.T) {
	orig, origDelay := enumerateWingetSourceFn, snapshotRetryDelay
	t.Cleanup(func() { enumerateWingetSourceFn = orig; snapshotRetryDelay = origDelay })
	snapshotRetryDelay = 0
	calls := 0
	var mu sync.Mutex
	enumerateWingetSourceFn = func(source string, _ bool) ([]driver.InstalledPackage, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return nil, errors.New(source + " failed")
	}
	_, _, err := (sourceWingetCaptureEnumerator{}).EnumerateInstalledWithWarnings()
	if err == nil || calls != 4 {
		t.Fatalf("err=%v calls=%d, want structured failure after retry", err, calls)
	}
}

func TestDedupeWingetSourcePackages_UsesRefSpecificSourcePrecedence(t *testing.T) {
	packages := dedupeWingetSourcePackages([]driver.InstalledPackage{
		{Ref: "9NBLGGH4NNS1", Source: "winget"}, {Ref: "9NBLGGH4NNS1", Source: "msstore"},
		{Ref: "Vendor.App", Source: "msstore"}, {Ref: "Vendor.App", Source: "winget"},
	})
	if len(packages) != 2 {
		t.Fatalf("packages=%+v", packages)
	}
	for _, pkg := range packages {
		if pkg.Ref == "9NBLGGH4NNS1" && pkg.Source != "msstore" {
			t.Fatalf("store ref winner=%+v", pkg)
		}
		if pkg.Ref == "Vendor.App" && pkg.Source != "winget" {
			t.Fatalf("community ref winner=%+v", pkg)
		}
	}
}

func TestPossibleDuplicateWarnings_DifferentRefsAcrossSourcesAreRetained(t *testing.T) {
	apps := []capturedApp{
		{Refs: map[string]string{"windows": "Vendor.App"}, Name: "Same Name"},
		{Driver: "winget", Source: "msstore", Refs: map[string]string{"windows": "9NBLGGH4NNS1"}, Name: "Same Name"},
	}
	warnings := possibleDuplicateWarnings(apps)
	if len(warnings) != 1 || warnings[0].Code != "possible_duplicate" || warnings[0].Source != "msstore" {
		t.Fatalf("warnings = %+v", warnings)
	}
}

func TestRunCapture_RuntimeFilterAppliesAcrossWingetSources(t *testing.T) {
	origResolve, origRealizer, origGOOS := resolveCaptureEnumeratorFn, newRealizerFn, captureGOOSFn
	t.Cleanup(func() {
		resolveCaptureEnumeratorFn = origResolve
		newRealizerFn = origRealizer
		captureGOOSFn = origGOOS
	})
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	captureGOOSFn = func() string { return "windows" }
	resolveCaptureEnumeratorFn = func(string, bool) (driver.InstalledEnumerator, error) {
		return sourceCaptureFixture{packages: []driver.InstalledPackage{
			{Ref: "Microsoft.DotNet.Runtime.8", Source: "winget"},
			{Ref: "Microsoft.DotNet.DesktopRuntime.8", Source: "msstore"},
			{Ref: "9NBLGGH4NNS1", Source: "msstore"},
		}}, nil
	}
	emptyCatalog(func() {
		raw, eerr := RunCapture(CaptureFlags{Out: filepath.Join(t.TempDir(), "runtime.jsonc"), Drivers: []string{"winget"}})
		if eerr != nil {
			t.Fatal(eerr)
		}
		result := raw.(*CaptureResult)
		if result.Counts.FilteredRuntimes != 2 || result.Counts.Included != 1 {
			t.Fatalf("counts=%+v", result.Counts)
		}
	})
}

func TestRunCapture_PreservesStoreSourceAndOmitsStorePin(t *testing.T) {
	origResolve := resolveCaptureEnumeratorFn
	origRealizer := newRealizerFn
	origGOOS := captureGOOSFn
	t.Cleanup(func() {
		resolveCaptureEnumeratorFn = origResolve
		newRealizerFn = origRealizer
		captureGOOSFn = origGOOS
	})
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	captureGOOSFn = func() string { return "windows" }
	resolveCaptureEnumeratorFn = func(name string, structured bool) (driver.InstalledEnumerator, error) {
		return sourceCaptureFixture{packages: []driver.InstalledPackage{
			{Ref: "Vendor.App", DisplayName: "Community", Version: "1.2.3", Source: "winget"},
			{Ref: "9NBLGGH4NNS1", DisplayName: "Store App", Version: "9.9.9", Source: "msstore"},
		}}, nil
	}
	out := filepath.Join(t.TempDir(), "capture.jsonc")
	emptyCatalog(func() {
		raw, eerr := RunCapture(CaptureFlags{Out: out, Pin: true, Drivers: []string{"winget"}})
		if eerr != nil {
			t.Fatalf("capture: %+v", eerr)
		}
		res := raw.(*CaptureResult)
		if len(res.Warnings) != 1 || res.Warnings[0].Code != "store_version_unpinned" || res.Warnings[0].Source != "msstore" {
			t.Fatalf("warnings = %+v", res.Warnings)
		}
		data := readCaptureManifestBytes(t, out)
		text := string(data)
		if !strings.Contains(text, `"source": "msstore"`) || strings.Contains(text, `"version": "9.9.9"`) || !strings.Contains(text, `"version": "1.2.3"`) {
			t.Fatalf("manifest = %s", text)
		}
	})
}

func TestRunCapture_UpdatePinClearsExistingStoreVersion(t *testing.T) {
	origResolve, origRealizer, origGOOS := resolveCaptureEnumeratorFn, newRealizerFn, captureGOOSFn
	t.Cleanup(func() {
		resolveCaptureEnumeratorFn = origResolve
		newRealizerFn = origRealizer
		captureGOOSFn = origGOOS
	})
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	captureGOOSFn = func() string { return "windows" }
	resolveCaptureEnumeratorFn = func(string, bool) (driver.InstalledEnumerator, error) {
		return sourceCaptureFixture{packages: []driver.InstalledPackage{{
			Ref: "Vendor.StoreAlias", DisplayName: "Store App", Version: "9.9.9", Source: "msstore",
		}}}, nil
	}

	dir := t.TempDir()
	existingPath := filepath.Join(dir, "existing.jsonc")
	if err := os.WriteFile(existingPath, []byte(`{
  "version": 1,
  "apps": [{
    "id": "store-app",
    "driver": "winget",
    "source": "msstore",
    "version": "1.2.3",
    "refs": {"windows": "Vendor.StoreAlias"}
  }]
}`), 0644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "capture.jsonc")
	emptyCatalog(func() {
		_, eerr := RunCapture(CaptureFlags{
			Out: out, Manifest: existingPath, Update: true, Pin: true, Drivers: []string{"winget"},
		})
		if eerr != nil {
			t.Fatalf("capture: %+v", eerr)
		}
	})

	version, found := manifestAppVersion(readManifestApps(t, out), "Vendor.StoreAlias")
	if !found {
		t.Fatal("expected Store app in merged manifest")
	}
	if version != "" {
		t.Fatalf("Store version = %q, want omitted under --update --pin", version)
	}
}

func TestRunCapture_StoreItemEventUsesWingetDriverAndCanonicalStatus(t *testing.T) {
	origResolve, origRealizer, origGOOS := resolveCaptureEnumeratorFn, newRealizerFn, captureGOOSFn
	t.Cleanup(func() {
		resolveCaptureEnumeratorFn = origResolve
		newRealizerFn = origRealizer
		captureGOOSFn = origGOOS
	})
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	captureGOOSFn = func() string { return "windows" }
	resolveCaptureEnumeratorFn = func(string, bool) (driver.InstalledEnumerator, error) {
		return sourceCaptureFixture{packages: []driver.InstalledPackage{{
			Ref: "9NBLGGH4NNS1", DisplayName: "Store App", Source: "msstore",
		}}}, nil
	}

	stderr := captureStderr(t, func() {
		emptyCatalog(func() {
			_, eerr := RunCapture(CaptureFlags{
				Out: filepath.Join(t.TempDir(), "capture.jsonc"), Drivers: []string{"winget"}, Events: "jsonl",
			})
			if eerr != nil {
				t.Fatalf("capture: %+v", eerr)
			}
		})
	})

	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode event: %v\n%s", err, line)
		}
		if event["event"] != "item" || event["id"] != "9NBLGGH4NNS1" {
			continue
		}
		if event["driver"] != "winget" || event["status"] != "present" || event["reason"] != "detected" {
			t.Fatalf("Store item event = %+v, want winget present/detected", event)
		}
		return
	}
	t.Fatalf("Store item event missing from stream:\n%s", stderr)
}

type sourceCaptureFixture struct{ packages []driver.InstalledPackage }

func (s sourceCaptureFixture) EnumerateInstalled() ([]driver.InstalledPackage, error) {
	return s.packages, nil
}
