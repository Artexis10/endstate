// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/snapshot"
)

func captureWithObservedMatcher(
	t *testing.T,
	flags CaptureFlags,
	exported []snapshot.SnapshotApp,
	installed []snapshot.SnapshotApp,
) ([]manifest.App, []map[string]interface{}) {
	t.Helper()
	if flags.Out == "" {
		flags.Out = filepath.Join(t.TempDir(), "capture.jsonc")
	}

	originalMatcher := matchModulesForAppsFn
	var observed []manifest.App
	matchModulesForAppsFn = func(_ map[string]*modules.Module, apps []manifest.App) []*modules.Module {
		observed = append([]manifest.App(nil), apps...)
		return nil
	}
	t.Cleanup(func() { matchModulesForAppsFn = originalMatcher })

	withMockInstalledApps(t, installed)
	withMockSnapshot(exported, nil, func() {
		withMockCatalog(map[string]*modules.Module{
			"apps.observer": {ID: "apps.observer", DisplayName: "Observer"},
		}, nil, func() {
			if _, captureErr := RunCapture(flags); captureErr != nil {
				t.Fatalf("RunCapture: %+v", captureErr)
			}
		})
	})
	return observed, readManifestApps(t, flags.Out)
}

func observedAppByWindowsRef(t *testing.T, apps []manifest.App, ref string) manifest.App {
	t.Helper()
	for _, app := range apps {
		if app.Refs["windows"] == ref {
			return app
		}
	}
	t.Fatalf("observed matcher apps do not include %q: %+v", ref, apps)
	return manifest.App{}
}

func outputAppByWindowsRef(t *testing.T, apps []map[string]interface{}, ref string) map[string]interface{} {
	t.Helper()
	for _, app := range apps {
		refs, _ := app["refs"].(map[string]interface{})
		if refs["windows"] == ref {
			return app
		}
	}
	t.Fatalf("output apps do not include %q: %+v", ref, apps)
	return nil
}

func TestRunCapture_NoPinPassesRawInstalledEvidenceToModuleMatcher(t *testing.T) {
	exported := []snapshot.SnapshotApp{{ID: "Vendor.App", Name: "Vendor App", Source: "winget"}}
	installed := []snapshot.SnapshotApp{{ID: "Vendor.App", Name: "Vendor App", Version: " 027.04.0 ", Source: "winget"}}

	observed, output := captureWithObservedMatcher(t, CaptureFlags{}, exported, installed)
	app := observedAppByWindowsRef(t, observed, "Vendor.App")
	if !app.Installed || app.InstalledVersion != " 027.04.0 " || app.Version != " 027.04.0 " {
		t.Fatalf("matcher installed evidence = %+v", app)
	}
	if app.Driver != "winget" || app.Backend != "winget" {
		t.Fatalf("matcher backend evidence = driver %q, backend %q", app.Driver, app.Backend)
	}
	serialized := outputAppByWindowsRef(t, output, "Vendor.App")
	if _, exists := serialized["version"]; exists {
		t.Fatalf("unpinned output unexpectedly serialized version: %+v", serialized)
	}
	if _, exists := serialized["driver"]; exists {
		t.Fatalf("runtime Winget driver leaked into output: %+v", serialized)
	}
}

func TestRunCapture_PinPassesAndSerializesRawInstalledVersion(t *testing.T) {
	exported := []snapshot.SnapshotApp{{ID: "Vendor.App", Name: "Vendor App", Source: "winget"}}
	installed := []snapshot.SnapshotApp{{ID: "Vendor.App", Name: "Vendor App", Version: " 27.4-beta ", Source: "winget"}}

	observed, output := captureWithObservedMatcher(t, CaptureFlags{Pin: true}, exported, installed)
	app := observedAppByWindowsRef(t, observed, "Vendor.App")
	if !app.Installed || app.InstalledVersion != " 27.4-beta " || app.Version != " 27.4-beta " {
		t.Fatalf("matcher pin evidence = %+v", app)
	}
	serialized := outputAppByWindowsRef(t, output, "Vendor.App")
	if serialized["version"] != " 27.4-beta " {
		t.Fatalf("serialized version = %#v, want raw installed value", serialized["version"])
	}
}

func TestRunCapture_InstalledVersionFallsBackToExportSnapshot(t *testing.T) {
	exported := []snapshot.SnapshotApp{{ID: "Vendor.App", Name: "Vendor App", Version: "export-raw-v1", Source: "winget"}}
	installed := []snapshot.SnapshotApp{{ID: "Vendor.App", Name: "Vendor App", Source: "winget"}}

	observed, output := captureWithObservedMatcher(t, CaptureFlags{}, exported, installed)
	app := observedAppByWindowsRef(t, observed, "Vendor.App")
	if app.InstalledVersion != "export-raw-v1" || app.Version != "export-raw-v1" {
		t.Fatalf("matcher export fallback evidence = %+v", app)
	}
	if _, exists := outputAppByWindowsRef(t, output, "Vendor.App")["version"]; exists {
		t.Fatal("export fallback must remain runtime-only without --pin")
	}
}

func TestRunCapture_UpdateOnlyUsesCurrentDetectionAsInstalledEvidence(t *testing.T) {
	tmpDir := t.TempDir()
	existingPath := filepath.Join(tmpDir, "existing.jsonc")
	existing := `{
  "version": 1,
  "apps": [
    {"id":"present","driver":"winget","version":"declared-present","refs":{"windows":"Present.App"}},
    {"id":"absent","driver":"winget","version":"declared-absent","refs":{"windows":"Absent.App"}}
  ]
}`
	if err := os.WriteFile(existingPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	exported := []snapshot.SnapshotApp{{ID: "Present.App", Name: "Present App", Source: "winget"}}
	installed := []snapshot.SnapshotApp{{ID: "Present.App", Name: "Present App", Version: "current-present", Source: "winget"}}
	flags := CaptureFlags{Manifest: existingPath, Update: true}
	observed, output := captureWithObservedMatcher(t, flags, exported, installed)

	present := observedAppByWindowsRef(t, observed, "Present.App")
	if !present.Installed || present.Version != "current-present" || present.InstalledVersion != "current-present" || present.Backend != "winget" {
		t.Fatalf("currently installed existing app evidence = %+v", present)
	}
	absent := observedAppByWindowsRef(t, observed, "Absent.App")
	if absent.Installed || absent.Version != "" || absent.InstalledVersion != "" || absent.Backend != "" {
		t.Fatalf("desired-only app acquired installed evidence from declared pin: %+v", absent)
	}
	if absent.Driver != "winget" {
		t.Fatalf("declared driver was dropped during matcher conversion: %+v", absent)
	}

	if got := outputAppByWindowsRef(t, output, "Present.App")["version"]; got != "declared-present" {
		t.Fatalf("unpinned update rewrote present declared pin to %#v", got)
	}
	if got := outputAppByWindowsRef(t, output, "Absent.App")["version"]; got != "declared-absent" {
		t.Fatalf("unpinned update did not preserve absent declared pin: %#v", got)
	}
}

func TestRunCapture_UpdateJoinsWingetEvidenceCaseInsensitivelyWithoutDuplicate(t *testing.T) {
	tmpDir := t.TempDir()
	existingPath := filepath.Join(tmpDir, "existing.jsonc")
	existing := `{
  "version": 1,
  "apps": [
    {"id":"vendor-app","driver":"winget","version":"declared-pin","refs":{"windows":"Vendor.App"}}
  ]
}`
	if err := os.WriteFile(existingPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	exported := []snapshot.SnapshotApp{{ID: "vendor.app", Name: "Export Name", Source: "winget"}}
	installed := []snapshot.SnapshotApp{{ID: "VENDOR.APP", Name: "Installed Name", Version: "Current-Raw", Source: "winget"}}
	observed, output := captureWithObservedMatcher(t, CaptureFlags{Manifest: existingPath, Update: true}, exported, installed)

	if len(observed) != 1 || len(output) != 1 {
		t.Fatalf("case variants created duplicate apps: matcher=%+v output=%+v", observed, output)
	}
	app := observedAppByWindowsRef(t, observed, "Vendor.App")
	if !app.Installed || app.Version != "Current-Raw" || app.InstalledVersion != "Current-Raw" || app.Backend != "winget" {
		t.Fatalf("case-insensitive current evidence join = %+v", app)
	}
	serialized := outputAppByWindowsRef(t, output, "Vendor.App")
	if got := serialized["version"]; got != "declared-pin" {
		t.Fatalf("unpinned update changed desired pin to %#v", got)
	}
	refs := serialized["refs"].(map[string]interface{})
	if refs["windows"] != "Vendor.App" {
		t.Fatalf("serialized ref casing changed: %+v", refs)
	}
}
