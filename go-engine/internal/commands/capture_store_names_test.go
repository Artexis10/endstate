// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// TestRunCapture_EnrichesStoreDisplayNamesFromWingetMetadata reproduces the
// real bug: a Microsoft Store entry arrives from enumeration labelled with the
// raw product ID (winget export supplies the ID, and the concurrent `winget
// list --source msstore` evidence was lost), so the name equals the ID or is
// empty. The best-effort enrichment pass must re-resolve the friendly name from
// winget metadata and flow it to item events, appsIncluded, and the manifest.
func TestRunCapture_EnrichesStoreDisplayNamesFromWingetMetadata(t *testing.T) {
	origResolve, origRealizer, origGOOS, origNames := resolveCaptureEnumeratorFn, newRealizerFn, captureGOOSFn, resolveStoreDisplayNamesFn
	t.Cleanup(func() {
		resolveCaptureEnumeratorFn = origResolve
		newRealizerFn = origRealizer
		captureGOOSFn = origGOOS
		resolveStoreDisplayNamesFn = origNames
	})
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	captureGOOSFn = func() string { return "windows" }
	resolveCaptureEnumeratorFn = func(string, bool) (driver.InstalledEnumerator, error) {
		return sourceCaptureFixture{packages: []driver.InstalledPackage{
			// Name == raw ID: the list evidence lost the friendly name.
			{Ref: "XP89DCGQ3K6VLD", DisplayName: "XP89DCGQ3K6VLD", Source: "msstore"},
			// Empty name: no evidence at all.
			{Ref: "XP9CSRSZ9PS7X0", Source: "msstore"},
		}}, nil
	}
	resolveStoreDisplayNamesFn = func() map[string]string {
		return map[string]string{
			"xp89dcgq3k6vld": "PowerToys (Preview) x64",
			"xp9csrsz9ps7x0": "Qobuz",
		}
	}

	out := filepath.Join(t.TempDir(), "capture.jsonc")
	var res *CaptureResult
	stderr := captureStderr(t, func() {
		emptyCatalog(func() {
			raw, eerr := RunCapture(CaptureFlags{Out: out, Drivers: []string{"winget"}, Events: "jsonl"})
			if eerr != nil {
				t.Fatalf("capture: %+v", eerr)
			}
			res = raw.(*CaptureResult)
		})
	})

	// 1. appsIncluded carries the friendly name.
	includedNames := map[string]string{}
	for _, app := range res.AppsIncluded {
		includedNames[app.ID] = app.Name
	}
	if includedNames["XP89DCGQ3K6VLD"] != "PowerToys (Preview) x64" || includedNames["XP9CSRSZ9PS7X0"] != "Qobuz" {
		t.Fatalf("appsIncluded names = %v, want enriched store names", includedNames)
	}

	// 2. Item events carry the friendly name.
	itemNames := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event["event"] != "item" {
			continue
		}
		if id, _ := event["id"].(string); id != "" {
			name, _ := event["name"].(string)
			itemNames[id] = name
		}
	}
	if itemNames["XP89DCGQ3K6VLD"] != "PowerToys (Preview) x64" || itemNames["XP9CSRSZ9PS7X0"] != "Qobuz" {
		t.Fatalf("item event names = %v, want enriched store names", itemNames)
	}
}

// TestEnrichStoreDisplayNames_FillsManifestDisplayFieldForStoreOnly exercises
// the enrichment pass at the capturedApp layer — the value carried into the
// written manifest (_name) and module matching (DisplayName). Only Store entries
// with a missing or ID-equal name are rewritten; community and already-named
// entries are left untouched.
func TestEnrichStoreDisplayNames_FillsManifestDisplayFieldForStoreOnly(t *testing.T) {
	orig := resolveStoreDisplayNamesFn
	t.Cleanup(func() { resolveStoreDisplayNamesFn = orig })
	resolveStoreDisplayNamesFn = func() map[string]string {
		return map[string]string{"xp89dcgq3k6vld": "PowerToys (Preview) x64"}
	}

	apps := []capturedApp{
		// Store, name == raw ID: must be enriched.
		{ID: "xp89dcgq3k6vld", Driver: "winget", Source: "msstore", Name: "XP89DCGQ3K6VLD", Refs: map[string]string{"windows": "XP89DCGQ3K6VLD"}},
		// Community winget with a real name: must be untouched.
		{ID: "git-git", Driver: "winget", Name: "Git", Refs: map[string]string{"windows": "Git.Git"}},
		// Store with a real name already: must be untouched (no fabrication).
		{ID: "xp9csrsz9ps7x0", Driver: "winget", Source: "msstore", Name: "Qobuz", Refs: map[string]string{"windows": "XP9CSRSZ9PS7X0"}},
	}
	enrichStoreDisplayNames(apps)

	if apps[0].Name != "PowerToys (Preview) x64" {
		t.Fatalf("store entry name = %q, want enriched name in the manifest display field", apps[0].Name)
	}
	if apps[1].Name != "Git" {
		t.Fatalf("community entry name = %q, want untouched", apps[1].Name)
	}
	if apps[2].Name != "Qobuz" {
		t.Fatalf("already-named store entry = %q, want untouched", apps[2].Name)
	}
}

// TestRunCapture_StoreNameEnrichmentStaysRawWhenMetadataAbsent proves the pass
// is additive and never fabricates: when winget exposes no friendly name, the
// raw ID is preserved and capture still succeeds.
func TestRunCapture_StoreNameEnrichmentStaysRawWhenMetadataAbsent(t *testing.T) {
	origResolve, origRealizer, origGOOS, origNames := resolveCaptureEnumeratorFn, newRealizerFn, captureGOOSFn, resolveStoreDisplayNamesFn
	t.Cleanup(func() {
		resolveCaptureEnumeratorFn = origResolve
		newRealizerFn = origRealizer
		captureGOOSFn = origGOOS
		resolveStoreDisplayNamesFn = origNames
	})
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	captureGOOSFn = func() string { return "windows" }
	resolveCaptureEnumeratorFn = func(string, bool) (driver.InstalledEnumerator, error) {
		return sourceCaptureFixture{packages: []driver.InstalledPackage{
			{Ref: "XP89DCGQ3K6VLD", DisplayName: "XP89DCGQ3K6VLD", Source: "msstore"},
		}}, nil
	}
	called := false
	resolveStoreDisplayNamesFn = func() map[string]string { called = true; return nil }

	out := filepath.Join(t.TempDir(), "capture.jsonc")
	var res *CaptureResult
	emptyCatalog(func() {
		raw, eerr := RunCapture(CaptureFlags{Out: out, Drivers: []string{"winget"}})
		if eerr != nil {
			t.Fatalf("capture: %+v", eerr)
		}
		res = raw.(*CaptureResult)
	})

	if !called {
		t.Fatal("expected enrichment to attempt metadata resolution for a raw-ID store entry")
	}
	for _, app := range res.AppsIncluded {
		if app.ID == "XP89DCGQ3K6VLD" && app.Name != "XP89DCGQ3K6VLD" {
			t.Fatalf("store name = %q, want raw ID preserved when no metadata resolves", app.Name)
		}
	}
}

// TestRunCapture_StoreNameEnrichmentSkippedForCommunityApps proves the pass is
// bounded to Store-sourced entries: community winget apps never trigger a
// metadata lookup, so the enrichment adds no cost when no store name is missing.
func TestRunCapture_StoreNameEnrichmentSkippedForCommunityApps(t *testing.T) {
	origResolve, origRealizer, origGOOS, origNames := resolveCaptureEnumeratorFn, newRealizerFn, captureGOOSFn, resolveStoreDisplayNamesFn
	t.Cleanup(func() {
		resolveCaptureEnumeratorFn = origResolve
		newRealizerFn = origRealizer
		captureGOOSFn = origGOOS
		resolveStoreDisplayNamesFn = origNames
	})
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	captureGOOSFn = func() string { return "windows" }
	resolveCaptureEnumeratorFn = func(string, bool) (driver.InstalledEnumerator, error) {
		return sourceCaptureFixture{packages: []driver.InstalledPackage{
			{Ref: "Git.Git", DisplayName: "Git", Source: "winget"},
			// A store app that already has a proper name must not be re-resolved.
			{Ref: "XP89DCGQ3K6VLD", DisplayName: "PowerToys (Preview) x64", Source: "msstore"},
		}}, nil
	}
	called := false
	resolveStoreDisplayNamesFn = func() map[string]string { called = true; return nil }

	out := filepath.Join(t.TempDir(), "capture.jsonc")
	emptyCatalog(func() {
		if _, eerr := RunCapture(CaptureFlags{Out: out, Drivers: []string{"winget"}}); eerr != nil {
			t.Fatalf("capture: %+v", eerr)
		}
	})
	if called {
		t.Fatal("enrichment ran a winget lookup even though no store name was missing")
	}
}
