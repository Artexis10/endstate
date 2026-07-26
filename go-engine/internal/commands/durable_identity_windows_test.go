//go:build windows

// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/arp"
	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

type arpInventoryFixture struct {
	LocalIdentifier string
	Key             string
	DisplayName     string
	Publisher       string
	Version         string
}

// withMockARPInventory becomes the registry-reader seam once capture and
// presence consume ARP inventory. Keeping the fixture at the command boundary
// makes the reproduced Chrome case hermetic.
func withMockARPInventory(t *testing.T, fixtures []arpInventoryFixture, f func()) {
	t.Helper()
	original := readARPInventoryFn
	entries := make([]arp.Entry, len(fixtures))
	for i, fixture := range fixtures {
		entries[i] = arp.Entry{LocalIdentifier: fixture.LocalIdentifier, Key: fixture.Key, DisplayName: fixture.DisplayName, Publisher: fixture.Publisher, DisplayVersion: fixture.Version}
	}
	readARPInventoryFn = func() []arp.Entry { return entries }
	t.Cleanup(func() { readARPInventoryFn = original })
	f()
}

func TestRunPlan_GoogleChromeFingerprintOutsideWingetIsPresent(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "chrome.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{
  "version": 1,
  "apps": [{
    "id": "google-chrome",
    "refs": {"windows": "Google.Chrome.EXE"},
	    "fingerprint": {"key": "Google Chrome", "publisher": "Google LLC", "version": "136.0"}
  }]
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	withMockARPInventory(t, []arpInventoryFixture{{Key: "Google Chrome", Publisher: "Google LLC", Version: "136.0"}}, func() {
		withMockDriver(&mockDriver{installed: map[string]bool{}}, func() {
			raw, envelopeErr := RunPlan(PlanFlags{Manifest: manifestPath})
			if envelopeErr != nil {
				t.Fatalf("RunPlan: %v", envelopeErr)
			}
			result := raw.(*PlanResult)
			if len(result.Actions) != 1 {
				t.Fatalf("actions = %+v, want one", result.Actions)
			}
			if got := result.Actions[0].PlannedAction; got != "none" {
				t.Fatalf("Google.Chrome.EXE planned action = %q, want none", got)
			}
		})
	})
}

func TestRunPlan_ARPFingerprintDoesNotMaskLedgerInfrastructureError(t *testing.T) {
	path := writeLaneManifest(t, `{"id":"chrome","refs":{"windows":"Google.Chrome"},"fingerprint":{"key":"Google Chrome","publisher":"Google LLC"}}`)
	winget := &detectErrorDriver{name: "winget", detectErr: errors.New("winget database locked")}

	withMockARPInventory(t, []arpInventoryFixture{{Key: "Google Chrome", Publisher: "Google LLC"}}, func() {
		withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget}, nil, func() {
			raw, envelopeErr := RunPlan(PlanFlags{Manifest: path})
			if envelopeErr != nil {
				t.Fatalf("RunPlan: %v", envelopeErr)
			}
			action := raw.(*PlanResult).Actions[0]
			if action.CurrentStatus != driver.StatusFailed || action.PlannedAction != "skip" || !strings.Contains(action.Message, "database locked") {
				t.Fatalf("plan action = %#v, want preserved detection failure", action)
			}
		})
	})
}

func TestRunCapture_RecordsFingerprintForExactARPBinding(t *testing.T) {
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"winget": {packages: []driver.InstalledPackage{{Ref: "Brave.Brave", DisplayName: "Brave", InventoryLocalIdentifiers: []string{`ARP\Machine\X64\BraveSoftware Brave-Browser`}, InventoryRelationshipKnown: true}}},
	}, nil)
	out := filepath.Join(t.TempDir(), "captured.jsonc")
	withMockARPInventory(t, []arpInventoryFixture{{LocalIdentifier: `ARP\Machine\X64\BraveSoftware Brave-Browser`, Key: "BraveSoftware Brave-Browser", DisplayName: "Brave", Publisher: "Brave Software Inc", Version: "150.1"}}, func() {
		withMockCatalog(nil, nil, func() {
			if _, envelopeErr := RunCapture(CaptureFlags{Out: out, Drivers: []string{"winget"}}); envelopeErr != nil {
				t.Fatalf("RunCapture: %v", envelopeErr)
			}
		})
	})
	apps := readManifestApps(t, out)
	fingerprint, ok := apps[0]["fingerprint"].(map[string]interface{})
	if !ok {
		t.Fatalf("fingerprint = %#v, want object", apps[0]["fingerprint"])
	}
	if fingerprint["key"] != "BraveSoftware Brave-Browser" || fingerprint["publisher"] != "Brave Software Inc" || fingerprint["version"] != "150.1" {
		t.Fatalf("fingerprint = %#v", fingerprint)
	}
}

func TestRunCapture_UsesExactARPIdentifierForEverythingCollision(t *testing.T) {
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"winget": {packages: []driver.InstalledPackage{{Ref: "Valve.SteamGame", DisplayName: "Everything", InventoryLocalIdentifiers: []string{`ARP\Machine\X64\SteamGame`}, InventoryRelationshipKnown: true}}},
	}, nil)
	out := filepath.Join(t.TempDir(), "captured.jsonc")
	withMockARPInventory(t, []arpInventoryFixture{
		{LocalIdentifier: `ARP\Machine\X64\SteamGame`, Key: "Steam Game", DisplayName: "Everything", Publisher: "Valve Corporation", Version: "9.0"},
		{LocalIdentifier: `ARP\Machine\X64\Everything`, Key: "Everything", DisplayName: "Everything", Publisher: "voidtools", Version: "1.4"},
	}, func() {
		withMockCatalog(nil, nil, func() {
			if _, envelopeErr := RunCapture(CaptureFlags{Out: out, Drivers: []string{"winget"}}); envelopeErr != nil {
				t.Fatalf("RunCapture: %v", envelopeErr)
			}
		})
	})
	apps := readManifestApps(t, out)
	for _, app := range apps {
		refs := app["refs"].(map[string]interface{})
		if refs["windows"] != "Valve.SteamGame" {
			continue
		}
		fingerprint := app["fingerprint"].(map[string]interface{})
		if fingerprint["key"] != "Steam Game" || fingerprint["publisher"] != "Valve Corporation" {
			t.Fatalf("fingerprint = %#v, want exact ARP row", fingerprint)
		}
		return
	}
	t.Fatalf("Steam ledger app missing from %#v", apps)
}

func TestRunCapture_IncludesInventoryOnlyAppWithoutInstallRef(t *testing.T) {
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"winget": {packages: []driver.InstalledPackage{{Ref: "Git.Git", DisplayName: "Git", InventoryRelationshipKnown: true}}},
	}, nil)
	out := filepath.Join(t.TempDir(), "captured.jsonc")
	withMockARPInventory(t, []arpInventoryFixture{{LocalIdentifier: `ARP\Machine\X64\Google Chrome`, Key: "Google Chrome", DisplayName: "Google Chrome", Publisher: "Google LLC", Version: "136.0"}}, func() {
		withMockCatalog(nil, nil, func() {
			if _, envelopeErr := RunCapture(CaptureFlags{Out: out, Drivers: []string{"winget"}}); envelopeErr != nil {
				t.Fatalf("RunCapture: %v", envelopeErr)
			}
		})
	})
	apps := readManifestApps(t, out)
	if len(apps) != 2 {
		t.Fatalf("apps = %#v, want ledger and inventory entries", apps)
	}
	for _, app := range apps {
		fingerprint, _ := app["fingerprint"].(map[string]interface{})
		if fingerprint["key"] != "Google Chrome" {
			continue
		}
		refs := app["refs"].(map[string]interface{})
		if len(refs) != 0 {
			t.Fatalf("inventory-only refs = %#v, want no install ref", refs)
		}
		planPath := filepath.Join(t.TempDir(), "inventory-only.jsonc")
		data := readCaptureManifestBytes(t, out)
		if err := os.WriteFile(planPath, data, 0o644); err != nil {
			t.Fatal(err)
		}
		withMockDriver(&mockDriver{installed: map[string]bool{"Git.Git": true}}, func() {
			raw, envelopeErr := RunPlan(PlanFlags{Manifest: planPath})
			if envelopeErr != nil {
				t.Fatalf("RunPlan: %v", envelopeErr)
			}
			if actions := raw.(*PlanResult).Actions; len(actions) != 1 || actions[0].Ref != "Git.Git" || actions[0].PlannedAction == "install" {
				t.Fatalf("plan actions = %#v, want no inventory-derived install target", actions)
			}
		})
		return
	}
	t.Fatalf("inventory-only Chrome entry missing from %#v", apps)
}

func TestCaptureFingerprintRequiresKeyAndPublisher(t *testing.T) {
	for _, entry := range []arp.Entry{{LocalIdentifier: "ARP\\Machine\\X64\\Google Chrome", DisplayName: "Chrome", Publisher: "Google LLC"}, {LocalIdentifier: "ARP\\Machine\\X64\\Google Chrome", Key: "Google Chrome", DisplayName: "Chrome"}} {
		if fingerprint := captureFingerprint([]string{"ARP\\Machine\\X64\\Google Chrome"}, []arp.Entry{entry}); fingerprint != nil {
			t.Fatalf("captureFingerprint(%#v) = %#v, want nil", entry, fingerprint)
		}
	}
}

func TestCaptureFingerprintCollapsesRepeatedBindingsByIdentity(t *testing.T) {
	identifiers := []string{`ARP\Machine\X64\App`, `ARP\Machine\X86\App`}
	inventory := []arp.Entry{
		{LocalIdentifier: identifiers[0], Key: "Vendor App", Publisher: "Vendor", DisplayVersion: "1.0"},
		{LocalIdentifier: identifiers[1], Key: " vendor app ", Publisher: " VENDOR ", DisplayVersion: "2.0"},
	}
	if fingerprint := captureFingerprint(identifiers, inventory); fingerprint == nil || fingerprint.Key != "Vendor App" || fingerprint.Publisher != "Vendor" || fingerprint.Version != "" {
		t.Fatalf("fingerprint = %#v, want one identity with ambiguous version", fingerprint)
	}
	inventory[1].Key = "Other App"
	if fingerprint := captureFingerprint(identifiers, inventory); fingerprint != nil {
		t.Fatalf("fingerprint = %#v, want nil for distinct bound identities", fingerprint)
	}
	inventory[1] = arp.Entry{LocalIdentifier: identifiers[1], Key: "Vendor App"}
	if fingerprint := captureFingerprint(identifiers, inventory); fingerprint != nil {
		t.Fatalf("fingerprint = %#v, want nil when one bound row lacks publisher", fingerprint)
	}
}

func TestRunCapture_FilteredRuntimeIsNotReintroducedFromInventory(t *testing.T) {
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"winget": {packages: []driver.InstalledPackage{{Ref: "Microsoft.VCRedist.2015+.x64", DisplayName: "VC++ 2015 Redist", InventoryLocalIdentifiers: []string{`ARP\Machine\X64\VC Redist`}, InventoryRelationshipKnown: true}}},
	}, nil)
	out := filepath.Join(t.TempDir(), "captured.jsonc")
	withMockARPInventory(t, []arpInventoryFixture{{LocalIdentifier: `ARP\Machine\X64\VC Redist`, Key: "VC Redist", DisplayName: "VC++ 2015 Redist", Publisher: "Microsoft"}}, func() {
		withMockCatalog(nil, nil, func() {
			if _, envelopeErr := RunCapture(CaptureFlags{Out: out, Drivers: []string{"winget"}}); envelopeErr != nil {
				t.Fatalf("RunCapture: %v", envelopeErr)
			}
		})
	})
	if apps := readManifestApps(t, out); len(apps) != 0 {
		t.Fatalf("filtered runtime was reintroduced from inventory: %#v", apps)
	}
}

func TestRunCapture_FilteredRuntimeConsumesBoundRowWithoutFingerprint(t *testing.T) {
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"winget": {packages: []driver.InstalledPackage{{Ref: "Microsoft.VCRedist.2015+.x64", DisplayName: "VC++ 2015 Redist", InventoryLocalIdentifiers: []string{`ARP\Machine\X64\VC Redist`}, InventoryRelationshipKnown: true}}},
	}, nil)
	out := filepath.Join(t.TempDir(), "captured.jsonc")
	withMockARPInventory(t, []arpInventoryFixture{{LocalIdentifier: `ARP\Machine\X64\VC Redist`, Key: "VC Redist", DisplayName: "VC++ 2015 Redist"}}, func() {
		withMockCatalog(nil, nil, func() {
			if _, envelopeErr := RunCapture(CaptureFlags{Out: out, Drivers: []string{"winget"}}); envelopeErr != nil {
				t.Fatalf("RunCapture: %v", envelopeErr)
			}
		})
	})
	if matches := captureInventoryMatches([]string{`ARP\Machine\X64\VC Redist`}, []arp.Entry{{LocalIdentifier: `ARP\Machine\X64\VC Redist`, Key: "VC Redist"}}); len(matches) != 1 {
		t.Fatalf("bound row matches = %#v, want one", matches)
	}
	if apps := readManifestApps(t, out); len(apps) != 0 {
		t.Fatalf("filtered runtime was reintroduced from incomplete inventory: %#v", apps)
	}
}

func TestRunCapture_FilteredRuntimeConsumesDuplicateBoundInventoryRows(t *testing.T) {
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"winget": {packages: []driver.InstalledPackage{{Ref: "Microsoft.VCRedist.2015+.x64", DisplayName: "VC++ 2015 Redist", InventoryLocalIdentifiers: []string{`ARP\Machine\X64\VC Redist`}, InventoryRelationshipKnown: true}}},
	}, nil)
	out := filepath.Join(t.TempDir(), "captured.jsonc")
	withMockARPInventory(t, []arpInventoryFixture{
		{LocalIdentifier: `ARP\Machine\X64\VC Redist`, Key: "VC Redist", DisplayName: "VC++ 2015 Redist", Publisher: "Microsoft"},
		{LocalIdentifier: `ARP\Machine\X64\VC Redist`, Key: "VC Redist", DisplayName: "VC++ 2015 Redist", Publisher: "Microsoft"},
	}, func() {
		withMockCatalog(nil, nil, func() {
			if _, envelopeErr := RunCapture(CaptureFlags{Out: out, Drivers: []string{"winget"}}); envelopeErr != nil {
				t.Fatalf("RunCapture: %v", envelopeErr)
			}
		})
	})
	if apps := readManifestApps(t, out); len(apps) != 0 {
		t.Fatalf("duplicate bound inventory rows were reintroduced: %#v", apps)
	}
}

func TestRunCapture_FilteredRuntimeConsumesAllRepeatedDetailsBindings(t *testing.T) {
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"winget": {packages: []driver.InstalledPackage{{Ref: "Microsoft.VCRedist.2015+.x64", DisplayName: "VC++ 2015 Redist", InventoryLocalIdentifiers: []string{`ARP\Machine\X64\VC Redist`, `ARP\Machine\X86\VC Redist`}, InventoryRelationshipKnown: true}}},
	}, nil)
	out := filepath.Join(t.TempDir(), "captured.jsonc")
	withMockARPInventory(t, []arpInventoryFixture{
		{LocalIdentifier: `ARP\Machine\X64\VC Redist`, Key: "VC Redist x64", DisplayName: "VC++ 2015 Redist", Publisher: "Microsoft"},
		{LocalIdentifier: `ARP\Machine\X86\VC Redist`, Key: "VC Redist x86", DisplayName: "VC++ 2015 Redist", Publisher: "Microsoft"},
	}, func() {
		withMockCatalog(nil, nil, func() {
			if _, envelopeErr := RunCapture(CaptureFlags{Out: out, Drivers: []string{"winget"}}); envelopeErr != nil {
				t.Fatalf("RunCapture: %v", envelopeErr)
			}
		})
	})
	if apps := readManifestApps(t, out); len(apps) != 0 {
		t.Fatalf("repeated details bindings were reintroduced: %#v", apps)
	}
}

func TestRunCapture_EmptyWingetLedgerFailsEvenWithARPInventory(t *testing.T) {
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{"winget": {packages: nil}}, nil)
	withMockARPInventory(t, []arpInventoryFixture{{Key: "Google Chrome", Publisher: "Google LLC"}}, func() {
		withMockCatalog(nil, nil, func() {
			if _, envelopeErr := RunCapture(CaptureFlags{Out: filepath.Join(t.TempDir(), "captured.jsonc"), Drivers: []string{"winget"}}); envelopeErr == nil {
				t.Fatal("RunCapture succeeded with an empty Winget ledger")
			}
		})
	})
}

func TestRunCapture_UnknownRelationshipWarnsWithoutARPInventory(t *testing.T) {
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"winget": {packages: []driver.InstalledPackage{{Ref: "Git.Git", DisplayName: "Git"}}},
	}, nil)
	withMockARPInventory(t, nil, func() {
		withMockCatalog(nil, nil, func() {
			raw, envelopeErr := RunCapture(CaptureFlags{Out: filepath.Join(t.TempDir(), "captured.jsonc"), Drivers: []string{"winget"}})
			if envelopeErr != nil {
				t.Fatalf("RunCapture: %v", envelopeErr)
			}
			for _, warning := range raw.(*CaptureResult).Warnings {
				if warning.Code == "inventory_union_skipped" {
					return
				}
			}
			t.Fatalf("warnings = %#v, want inventory_union_skipped", raw.(*CaptureResult).Warnings)
		})
	})
}

func TestRunCapture_KeepsLedgerAppWithoutFingerprintWhenInventoryDoesNotMatch(t *testing.T) {
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"winget": {packages: []driver.InstalledPackage{{Ref: "Git.Git", DisplayName: "Git"}}},
	}, nil)
	out := filepath.Join(t.TempDir(), "captured.jsonc")
	withMockARPInventory(t, []arpInventoryFixture{{Key: "Google Chrome", DisplayName: "Google Chrome", Publisher: "Google LLC", Version: "136.0"}}, func() {
		withMockCatalog(nil, nil, func() {
			if _, envelopeErr := RunCapture(CaptureFlags{Out: out, Drivers: []string{"winget"}}); envelopeErr != nil {
				t.Fatalf("RunCapture: %v", envelopeErr)
			}
		})
	})
	apps := readManifestApps(t, out)
	if _, exists := apps[0]["fingerprint"]; exists {
		t.Fatalf("fingerprint = %#v, want omitted", apps[0]["fingerprint"])
	}
}

func TestRunCaptureUpdateJoinsByFingerprintAndIsIdempotent(t *testing.T) {
	existing := filepath.Join(t.TempDir(), "existing.jsonc")
	if err := os.WriteFile(existing, []byte(`{
  "version": 1,
  "apps": [{"id":"chrome","driver":"chocolatey","refs":{"windows":"Google.Chrome"},"fingerprint":{"key":"Google Chrome","publisher":"Google LLC","version":"135"}}]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"winget": {packages: []driver.InstalledPackage{{Ref: "Google.Chrome.EXE", DisplayName: "Google Chrome", InventoryLocalIdentifiers: []string{`ARP\Machine\X64\Google Chrome`}, InventoryRelationshipKnown: true}}},
	}, nil)
	withMockARPInventory(t, []arpInventoryFixture{{LocalIdentifier: `ARP\Machine\X64\Google Chrome`, Key: "Google Chrome", DisplayName: "Google Chrome", Publisher: "Google LLC", Version: "136"}}, func() {
		withMockCatalog(nil, nil, func() {
			for attempt := 0; attempt < 2; attempt++ {
				if _, envelopeErr := RunCapture(CaptureFlags{Out: existing, Manifest: existing, Update: true, Sanitize: true, Drivers: []string{"winget"}}); envelopeErr != nil {
					t.Fatalf("RunCapture attempt %d: %v", attempt, envelopeErr)
				}
				apps := readManifestApps(t, existing)
				if len(apps) != 1 || apps[0]["id"] != "chrome" {
					t.Fatalf("apps after update = %#v, want one stable Chrome app", apps)
				}
				refs := apps[0]["refs"].(map[string]interface{})
				if refs["windows"] != "Google.Chrome.EXE" {
					t.Fatalf("refs after update = %#v, want refreshed ref", refs)
				}
				if _, exists := apps[0]["driver"]; exists {
					t.Fatalf("driver after Winget ref refresh = %#v, want implicit winget", apps[0]["driver"])
				}
			}
		})
	})
}

func TestRunCaptureUpdateDoesNotJoinAmbiguousFingerprint(t *testing.T) {
	existing := filepath.Join(t.TempDir(), "existing.jsonc")
	if err := os.WriteFile(existing, []byte(`{
  "version": 1,
  "apps": [
    {"id":"chrome-choco","driver":"chocolatey","refs":{"windows":"googlechrome"},"fingerprint":{"key":"Google Chrome","publisher":"Google LLC"}},
    {"id":"chrome-winget","refs":{"windows":"Google.Chrome"},"fingerprint":{"key":"Google Chrome","publisher":"Google LLC"}}
  ]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"winget": {packages: []driver.InstalledPackage{{Ref: "Google.Chrome.EXE", DisplayName: "Google Chrome", InventoryLocalIdentifiers: []string{`ARP\Machine\X64\Google Chrome`}, InventoryRelationshipKnown: true}}},
	}, nil)
	withMockARPInventory(t, []arpInventoryFixture{{LocalIdentifier: `ARP\Machine\X64\Google Chrome`, Key: "Google Chrome", DisplayName: "Google Chrome", Publisher: "Google LLC"}}, func() {
		withMockCatalog(nil, nil, func() {
			if _, envelopeErr := RunCapture(CaptureFlags{Out: existing, Manifest: existing, Update: true, Sanitize: true, Drivers: []string{"winget"}}); envelopeErr != nil {
				t.Fatalf("RunCapture: %v", envelopeErr)
			}
		})
	})
	apps := readManifestApps(t, existing)
	if len(apps) != 2 {
		t.Fatalf("apps = %#v, want two preserved declarations", apps)
	}
	for _, app := range apps {
		refs := app["refs"].(map[string]interface{})
		if refs["windows"] == "Google.Chrome.EXE" {
			t.Fatalf("ambiguous fingerprint rewrote declaration: %#v", app)
		}
	}
}

func TestRunCaptureUpdateInventoryOnlyFingerprintPreservesExistingRef(t *testing.T) {
	existing := filepath.Join(t.TempDir(), "existing.jsonc")
	if err := os.WriteFile(existing, []byte(`{
  "version": 1,
  "apps": [{"id":"chrome","refs":{"windows":"Google.Chrome"},"fingerprint":{"key":"Google Chrome","publisher":"Google LLC","version":"135"}}]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"winget": {packages: []driver.InstalledPackage{{Ref: "Microsoft.VCRedist.2015+.x64", DisplayName: "VC++ 2015 Redist", InventoryLocalIdentifiers: []string{`ARP\Machine\X64\VC Redist`}, InventoryRelationshipKnown: true}}},
	}, nil)
	withMockARPInventory(t, []arpInventoryFixture{
		{LocalIdentifier: `ARP\Machine\X64\VC Redist`, Key: "VC Redist", DisplayName: "VC++ 2015 Redist", Publisher: "Microsoft"},
		{LocalIdentifier: `ARP\Machine\X64\Google Chrome`, Key: "Google Chrome", DisplayName: "Google Chrome", Publisher: "Google LLC", Version: "136"},
	}, func() {
		withMockCatalog(nil, nil, func() {
			for attempt := 0; attempt < 2; attempt++ {
				if _, envelopeErr := RunCapture(CaptureFlags{Out: existing, Manifest: existing, Update: true, Sanitize: true, Drivers: []string{"winget"}}); envelopeErr != nil {
					t.Fatalf("RunCapture attempt %d: %v", attempt, envelopeErr)
				}
				apps := readManifestApps(t, existing)
				if len(apps) != 1 || apps[0]["id"] != "chrome" {
					t.Fatalf("apps after inventory-only update = %#v, want one stable Chrome app", apps)
				}
				refs := apps[0]["refs"].(map[string]interface{})
				if refs["windows"] != "Google.Chrome" {
					t.Fatalf("refs after inventory-only update = %#v, want preserved ref", refs)
				}
			}
		})
	})
}

func TestAppPresentRequiresExactKeyAndPublisher(t *testing.T) {
	app := manifest.App{Fingerprint: &manifest.InstallFingerprint{Key: " Everything ", Publisher: "voidtools"}}
	if appPresent(false, app, []arp.Entry{{Key: "Everything", Publisher: "Valve Corporation"}}) {
		t.Fatal("publisher mismatch established presence")
	}
	if !appPresent(false, app, []arp.Entry{{Key: " everything ", Publisher: " VOIDTOOLS "}}) {
		t.Fatal("trimmed case-insensitive exact key and publisher did not establish presence")
	}
}

func TestAppPresentIgnoresVersionAndDisplayName(t *testing.T) {
	app := manifest.App{Fingerprint: &manifest.InstallFingerprint{Key: "7-Zip", Publisher: "Igor Pavlov", Version: "25.01"}}
	if !appPresent(false, app, []arp.Entry{{Key: "7-Zip", Publisher: "Igor Pavlov", DisplayName: "7-Zip 24.09 (x64)", DisplayVersion: "24.09"}}) {
		t.Fatal("version and display name affected identity matching")
	}
}

func TestObserveAppPresenceSuppressesAmbiguousARPVersionUnlessDesiredMatches(t *testing.T) {
	app := manifest.App{Fingerprint: &manifest.InstallFingerprint{Key: "7-Zip", Publisher: "Igor Pavlov"}}
	inventory := []arp.Entry{{Key: "7-Zip", Publisher: "Igor Pavlov", DisplayVersion: "24.09"}, {Key: "7-Zip", Publisher: "Igor Pavlov", DisplayVersion: "25.01"}}
	if got := observeAppPresence(false, "", app, inventory); !got.Present || got.Version != "" {
		t.Fatalf("ambiguous observation = %#v, want present without version", got)
	}
	app.Version = "25.01"
	if got := observeAppPresence(false, "", app, inventory); !got.Present || got.Version != "25.01" {
		t.Fatalf("desired-version observation = %#v, want 25.01", got)
	}
}

func TestRunVerifyReportsARPVersionDrift(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "chrome.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{
  "version": 1,
  "apps": [{"id":"chrome","refs":{"windows":"Google.Chrome"},"version":"136","fingerprint":{"key":"Google Chrome","publisher":"Google LLC"}}]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	withMockARPInventory(t, []arpInventoryFixture{{Key: "Google Chrome", Publisher: "Google LLC", Version: "135"}}, func() {
		withMockDriver(&mockDriver{installed: map[string]bool{}}, func() {
			raw, envelopeErr := RunVerify(VerifyFlags{Manifest: manifestPath})
			if envelopeErr != nil {
				t.Fatalf("RunVerify: %v", envelopeErr)
			}
			result := raw.(*VerifyResult)
			if len(result.Results) != 1 || result.Results[0].Reason != driver.ReasonVersionDrift || result.Results[0].Version != "135" || result.Results[0].Expected != "136" {
				t.Fatalf("verify result = %#v, want ARP version drift", result.Results)
			}
		})
	})
}

func TestRunApplyRepinUsesARPVersionEvidence(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "chrome.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{
  "version": 1,
  "apps": [{"id":"chrome","refs":{"windows":"Google.Chrome"},"version":"136","fingerprint":{"key":"Google Chrome","publisher":"Google LLC"}}]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	md := &mockDriver{installed: map[string]bool{}}
	withMockARPInventory(t, []arpInventoryFixture{{Key: "Google Chrome", Publisher: "Google LLC", Version: "135"}}, func() {
		withMockDriver(md, func() {
			if _, envelopeErr := RunApply(ApplyFlags{Manifest: manifestPath, Repin: true, Confirm: true}); envelopeErr != nil {
				t.Fatalf("RunApply: %v", envelopeErr)
			}
		})
	})
	if md.reinstallVersionCalls != 1 || md.lastReinstallVersion != "136" {
		t.Fatalf("reinstall calls = %d, version = %q", md.reinstallVersionCalls, md.lastReinstallVersion)
	}
}

func TestRunApplyRefreshesARPAfterInstallAttempt(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "app.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{"version":1,"apps":[{"id":"app","refs":{"windows":"Vendor.App"}}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	original := readARPInventoryFn
	reads := 0
	readARPInventoryFn = func() []arp.Entry {
		reads++
		return nil
	}
	t.Cleanup(func() { readARPInventoryFn = original })
	withMockDriver(&mockDriver{installed: map[string]bool{}}, func() {
		if _, envelopeErr := RunApply(ApplyFlags{Manifest: manifestPath}); envelopeErr != nil {
			t.Fatalf("RunApply: %v", envelopeErr)
		}
	})
	if reads != 2 {
		t.Fatalf("ARP reads = %d, want initial and post-mutation refresh", reads)
	}
}

func TestAppPresentDoesNotInferPreFingerprintIdentity(t *testing.T) {
	if appPresent(false, manifest.App{}, []arp.Entry{{Key: "BraveSoftware Brave-Browser", Publisher: "Brave Software Inc"}}) {
		t.Fatal("inventory established presence for a pre-fingerprint app")
	}
}

func TestComputeDriverLanePlan_ChocolateyFingerprintOutsideLedgerIsPresent(t *testing.T) {
	original := newNamedDriverFn
	newNamedDriverFn = func(name string) (driver.Driver, error) {
		if name != "chocolatey" {
			t.Fatalf("requested driver = %q", name)
		}
		return &mockDriver{installed: map[string]bool{}}, nil
	}
	t.Cleanup(func() { newNamedDriverFn = original })

	mf := &manifest.Manifest{Apps: []manifest.App{{
		ID: "ripgrep", Driver: "chocolatey", Refs: map[string]string{"windows": "ripgrep"},
		Fingerprint: &manifest.InstallFingerprint{Key: "ripgrep", Publisher: "BurntSushi"},
	}}}
	plan, _, err := computeDriverLanePlanWithInventory(mf, nil, []arp.Entry{{Key: "ripgrep", Publisher: "BurntSushi"}})
	if err != nil {
		t.Fatalf("computeDriverLanePlanWithInventory: %v", err)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].CurrentStatus != "present" || plan.Actions[0].PlannedAction != "none" {
		t.Fatalf("plan actions = %#v, want present chocolatey no-op", plan.Actions)
	}
}

func TestRunApply_GoogleChromeFingerprintOutsideWingetHasNoFailure(t *testing.T) {
	manifestPath := filepath.Join(t.TempDir(), "chrome.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{
  "version": 1,
  "apps": [{"id":"google-chrome","refs":{"windows":"Google.Chrome.EXE"},"fingerprint":{"key":"Google Chrome","publisher":"Google LLC"}}]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	withMockARPInventory(t, []arpInventoryFixture{{Key: "Google Chrome", DisplayName: "Google Chrome", Publisher: "Google LLC"}}, func() {
		withMockDriver(&mockDriver{installed: map[string]bool{}}, func() {
			raw, envelopeErr := RunApply(ApplyFlags{Manifest: manifestPath})
			if envelopeErr != nil {
				t.Fatalf("RunApply: %v", envelopeErr)
			}
			result := raw.(*ApplyResult)
			if result.Summary.Failed != 0 || len(result.Actions) != 1 || result.Actions[0].Status != "present" {
				t.Fatalf("apply result = %#v, want present Chrome without failure", result)
			}
		})
	})
}
