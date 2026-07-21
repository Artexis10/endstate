// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

func TestSourceWingetCapture_DefaultDualSourceAndPartialWarning(t *testing.T) {
	orig, origDelay := enumerateWingetSourceFn, snapshotRetryDelay
	t.Cleanup(func() { enumerateWingetSourceFn = orig; snapshotRetryDelay = origDelay })
	snapshotRetryDelay = 0
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
	orig, origDelay := enumerateWingetSourceFn, snapshotRetryDelay
	t.Cleanup(func() { enumerateWingetSourceFn = orig; snapshotRetryDelay = origDelay })
	snapshotRetryDelay = 0
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

// A transient community-source failure (e.g. exit status 0x8a150001) that
// clears within the retry budget must recover the full community list and emit
// no unavailable warning.
func TestSourceWingetCapture_RetriesTransientCommunityFailureThenSucceeds(t *testing.T) {
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
		if source == "winget" && n < 3 {
			return nil, errors.New("winget list --source winget failed: exit status 0x8a150001")
		}
		if source == "msstore" {
			return []driver.InstalledPackage{{Ref: "9NBLGGH4NNS1", Source: source}}, nil
		}
		return []driver.InstalledPackage{{Ref: "Vendor.App", Source: source}}, nil
	}
	packages, warnings, err := (sourceWingetCaptureEnumerator{}).EnumerateInstalledWithWarnings()
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings=%+v, want none after transient recovery", warnings)
	}
	var hasCommunity bool
	for _, pkg := range packages {
		if pkg.Ref == "Vendor.App" {
			hasCommunity = true
		}
	}
	if !hasCommunity {
		t.Fatalf("packages=%+v, want community app recovered after retry", packages)
	}
	if calls["winget"] != 3 {
		t.Fatalf("winget calls=%d, want 3 (initial + 2 retries)", calls["winget"])
	}
}

// When every retry attempt fails, the community source is declared unavailable
// exactly once — no duplicate warnings from the extra attempts.
func TestSourceWingetCapture_CommunityUnavailableWarnsOnceAfterExhaustingRetries(t *testing.T) {
	orig, origDelay := enumerateWingetSourceFn, snapshotRetryDelay
	t.Cleanup(func() { enumerateWingetSourceFn = orig; snapshotRetryDelay = origDelay })
	snapshotRetryDelay = 0
	calls := map[string]int{}
	var mu sync.Mutex
	enumerateWingetSourceFn = func(source string, _ bool) ([]driver.InstalledPackage, error) {
		mu.Lock()
		calls[source]++
		mu.Unlock()
		if source == "winget" {
			return nil, errors.New("exit status 0x8a150001")
		}
		return []driver.InstalledPackage{{Ref: "9NBLGGH4NNS1", Source: source}}, nil
	}
	packages, warnings, err := (sourceWingetCaptureEnumerator{}).EnumerateInstalledWithWarnings()
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 1 {
		t.Fatalf("packages=%+v, want store-only survivor", packages)
	}
	if len(warnings) != 1 || warnings[0].Code != "winget_source_unavailable" || warnings[0].Source != "winget" {
		t.Fatalf("warnings=%+v, want exactly one winget_source_unavailable", warnings)
	}
	if calls["winget"] != 3 {
		t.Fatalf("winget calls=%d, want 3 (initial + 2 retries) before degrading", calls["winget"])
	}
}

// A missing winget binary is not a transient failure and must never be retried:
// doing so would slow every capture on winget-less machines for no benefit.
func TestSourceWingetCapture_DoesNotRetryWhenWingetMissing(t *testing.T) {
	orig, origDelay := enumerateWingetSourceFn, snapshotRetryDelay
	t.Cleanup(func() { enumerateWingetSourceFn = orig; snapshotRetryDelay = origDelay })
	snapshotRetryDelay = 0
	calls := map[string]int{}
	var mu sync.Mutex
	enumerateWingetSourceFn = func(source string, _ bool) ([]driver.InstalledPackage, error) {
		mu.Lock()
		calls[source]++
		mu.Unlock()
		return nil, &exec.Error{Name: "winget", Err: exec.ErrNotFound}
	}
	_, _, err := (sourceWingetCaptureEnumerator{}).EnumerateInstalledWithWarnings()
	if err == nil {
		t.Fatal("expected error when winget binary is missing")
	}
	if calls["winget"] != 1 || calls["msstore"] != 1 {
		t.Fatalf("calls=%v, want a single attempt per source (no retry on missing binary)", calls)
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
	// Two sources, each attempted three times (initial + 2 retries) before the
	// aggregate is declared a structured failure.
	if err == nil || calls != 6 {
		t.Fatalf("err=%v calls=%d, want structured failure after per-source retries", err, calls)
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
	// Community + Store are both the winget manager. Two rows for one physical
	// package (differing only by winget source) are the same manager surfacing
	// the app twice, not a cross-manager collision, so no warning fires.
	apps := []capturedApp{
		{Refs: map[string]string{"windows": "Vendor.App"}, Name: "Same Name"},
		{Driver: "winget", Source: "msstore", Refs: map[string]string{"windows": "9NBLGGH4NNS1"}, Name: "Same Name"},
	}
	if warnings := possibleDuplicateWarnings(apps); len(warnings) != 0 {
		t.Fatalf("expected no warning for same-manager rows, got %+v", warnings)
	}
}

func TestPossibleDuplicateWarnings_EnrichedStoreNameDoesNotCollideWithCommunity(t *testing.T) {
	// A single Store-installed app surfaces as two winget rows — the community
	// ref (Microsoft.PowerToys) and the msstore ref (XP89DCGQ3K6VLD) — and
	// enrichStoreDisplayNames rewrites the Store row's raw product ID to the
	// same friendly display name. Both rows share the effective winget manager,
	// so this is one package captured twice, not a genuine duplicate, and must
	// not warn (regression: false positive introduced by v2.27.1 enrichment).
	apps := []capturedApp{
		{Refs: map[string]string{"windows": "Microsoft.PowerToys"}, Name: "PowerToys (Preview) x64"},
		{Driver: "winget", Source: "msstore", Refs: map[string]string{"windows": "XP89DCGQ3K6VLD"}, Name: "PowerToys (Preview) x64"},
	}
	if warnings := possibleDuplicateWarnings(apps); len(warnings) != 0 {
		t.Fatalf("expected no possible_duplicate warning for one Store app across winget sources, got %+v", warnings)
	}
}

func TestPossibleDuplicateWarnings_CrossManagerWarnsWithManagerNames(t *testing.T) {
	// A genuine cross-manager collision: the same display name is captured from
	// two different package managers. Both copies survive to the manifest, so a
	// single warning is emitted naming both managers in friendly language.
	apps := []capturedApp{
		{Refs: map[string]string{"windows": "Vendor.App"}, Name: "Shared Name"},
		{Driver: "chocolatey", Refs: map[string]string{"windows": "shared-app"}, Name: "Shared Name"},
	}
	warnings := possibleDuplicateWarnings(apps)
	if len(warnings) != 1 || warnings[0].Code != "possible_duplicate" {
		t.Fatalf("expected exactly one cross-manager warning, got %+v", warnings)
	}
	msg := warnings[0].Message
	if !strings.Contains(msg, "winget") || !strings.Contains(msg, "chocolatey") {
		t.Fatalf("message should name both managers, got %q", msg)
	}
	if strings.Contains(msg, "package driver") {
		t.Fatalf("message should avoid jargon, got %q", msg)
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
