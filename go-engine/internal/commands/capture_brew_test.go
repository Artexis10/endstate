// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// fakeBrewEnumerator is a driver.Driver that also satisfies the capture-lane
// brewEnumerator interface, returning a scripted set of installed brew apps.
type fakeBrewEnumerator struct {
	fakeBrewDriver
	apps  []driver.InstalledPackage
	err   error
	calls int
}

func (f *fakeBrewEnumerator) EnumerateInstalled() ([]driver.InstalledPackage, error) {
	f.calls++
	return f.apps, f.err
}

// withCaptureRealizerAndBrew wires the realizer + a brew factory + forces the
// capture lane to treat the host as darwin (captureGOOSFn override).
func withCaptureRealizerAndBrew(fr *fakeRealizer, brewFn func() (driver.Driver, error), goos string, f func()) {
	origRz := newRealizerFn
	origBrew := newBrewDriverFn
	origGOOS := captureGOOSFn
	newRealizerFn = func() (realizer.Realizer, error) { return fr, nil }
	newBrewDriverFn = brewFn
	captureGOOSFn = func() string { return goos }
	defer func() {
		newRealizerFn = origRz
		newBrewDriverFn = origBrew
		captureGOOSFn = origGOOS
	}()
	f()
}

// readCapturedManifestBrew reads the capture output, exposing the driver field.
func readCapturedManifestBrew(t *testing.T, path string) []struct {
	ID      string            `json:"id"`
	Refs    map[string]string `json:"refs"`
	Driver  string            `json:"driver"`
	Version string            `json:"version"`
} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var mf struct {
		Apps []struct {
			ID      string            `json:"id"`
			Refs    map[string]string `json:"refs"`
			Driver  string            `json:"driver"`
			Version string            `json:"version"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(data, &mf); err != nil {
		t.Fatalf("unmarshal manifest: %v\n%s", err, data)
	}
	return mf.Apps
}

// TestRunCapture_BrewLane_EmitsBrewApps: on darwin, capture enumerates brew
// formulae and casks and emits them as driver:"brew" apps (casks as cask: refs),
// alongside the realizer-captured nix apps (which keep Driver:"").
func TestRunCapture_BrewLane_EmitsBrewApps(t *testing.T) {
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "captured.jsonc")

	fr := &fakeRealizer{currentSet: nixSet("ripgrep")}
	fbe := &fakeBrewEnumerator{apps: []driver.InstalledPackage{
		{DisplayName: "hello", Ref: "hello", Version: "2.12"},
		{DisplayName: "firefox", Ref: "cask:firefox", Version: "122.0"},
	}}

	withCaptureRealizerAndBrew(fr, func() (driver.Driver, error) { return fbe, nil }, "darwin", func() {
		_, e := RunCapture(CaptureFlags{Out: outPath})
		if e != nil {
			t.Fatalf("RunCapture error: %v", e)
		}
	})

	apps := readCapturedManifestBrew(t, outPath)
	byID := map[string]struct {
		ID      string            `json:"id"`
		Refs    map[string]string `json:"refs"`
		Driver  string            `json:"driver"`
		Version string            `json:"version"`
	}{}
	for _, a := range apps {
		byID[a.ID] = a
	}

	// nix app keeps Driver:"".
	if a, ok := byID["ripgrep"]; !ok || a.Driver != "" {
		t.Errorf("ripgrep = %+v, want driver:\"\" (realizer-captured)", a)
	}
	// brew formula → driver:"brew", bare ref under darwin.
	if a, ok := byID["hello"]; !ok || a.Driver != "brew" || a.Refs["darwin"] != "hello" {
		t.Errorf("hello = %+v, want driver:brew ref darwin=hello", a)
	}
	// brew cask → driver:"brew", cask: ref under darwin.
	if a, ok := byID["firefox"]; !ok || a.Driver != "brew" || a.Refs["darwin"] != "cask:firefox" {
		t.Errorf("firefox = %+v, want driver:brew ref darwin=cask:firefox", a)
	}
}

// TestRunCapture_BrewLane_NonDarwin_NoBrewApps: on a non-darwin host the brew
// lane no-ops (the factory returns ErrNoBrewDriver) — only nix apps captured.
func TestRunCapture_BrewLane_NonDarwin_NoBrewApps(t *testing.T) {
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "captured.jsonc")

	fr := &fakeRealizer{currentSet: nixSet("ripgrep")}

	withCaptureRealizerAndBrew(fr, func() (driver.Driver, error) { return nil, ErrNoBrewDriver }, "linux", func() {
		_, e := RunCapture(CaptureFlags{Out: outPath})
		if e != nil {
			t.Fatalf("RunCapture error: %v", e)
		}
	})

	apps := readCapturedManifestBrew(t, outPath)
	for _, a := range apps {
		if a.Driver == "brew" {
			t.Errorf("non-darwin capture must not emit brew apps, got %+v", a)
		}
	}
	if len(apps) != 1 {
		t.Errorf("expected only the nix app, got %+v", apps)
	}
}

func TestRunCapture_DriverFilter_NixOnlySkipsBrew(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "captured.jsonc")
	fr := &fakeRealizer{currentSet: nixSet("ripgrep")}
	fbe := &fakeBrewEnumerator{apps: []driver.InstalledPackage{{DisplayName: "hello", Ref: "hello"}}}

	withCaptureRealizerAndBrew(fr, func() (driver.Driver, error) { return fbe, nil }, "darwin", func() {
		_, eerr := RunCapture(CaptureFlags{Out: outPath, Drivers: []string{"nix"}})
		if eerr != nil {
			t.Fatalf("RunCapture: %v", eerr)
		}
	})

	if fr.currentCalls != 1 {
		t.Fatalf("realizer Current calls = %d, want 1", fr.currentCalls)
	}
	if fbe.calls != 0 {
		t.Fatalf("brew enumeration calls = %d, want 0", fbe.calls)
	}
	apps := readCapturedManifestBrew(t, outPath)
	if len(apps) != 1 || apps[0].ID != "ripgrep" || apps[0].Driver != "" {
		t.Fatalf("captured apps = %+v, want only the nix app", apps)
	}
}

func TestRunCapture_DriverFilter_BrewOnlySkipsNix(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "captured.jsonc")
	fr := &fakeRealizer{currentSet: nixSet("ripgrep")}
	fbe := &fakeBrewEnumerator{apps: []driver.InstalledPackage{{DisplayName: "hello", Ref: "hello", Version: "2.12"}}}

	withCaptureRealizerAndBrew(fr, func() (driver.Driver, error) { return fbe, nil }, "darwin", func() {
		_, eerr := RunCapture(CaptureFlags{Out: outPath, Drivers: []string{"brew"}})
		if eerr != nil {
			t.Fatalf("RunCapture: %v", eerr)
		}
	})

	if fr.currentCalls != 0 {
		t.Fatalf("realizer Current calls = %d, want 0", fr.currentCalls)
	}
	if fbe.calls != 1 {
		t.Fatalf("brew enumeration calls = %d, want 1", fbe.calls)
	}
	apps := readCapturedManifestBrew(t, outPath)
	if len(apps) != 1 || apps[0].ID != "hello" || apps[0].Driver != "brew" {
		t.Fatalf("captured apps = %+v, want only the brew app", apps)
	}
}

func TestRunCapture_DriverFilter_RepeatedNixAndBrewCapturesBoth(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "captured.jsonc")
	fr := &fakeRealizer{currentSet: nixSet("ripgrep")}
	fbe := &fakeBrewEnumerator{apps: []driver.InstalledPackage{{DisplayName: "hello", Ref: "hello"}}}

	withCaptureRealizerAndBrew(fr, func() (driver.Driver, error) { return fbe, nil }, "darwin", func() {
		_, eerr := RunCapture(CaptureFlags{Out: outPath, Drivers: []string{"BREW", "nix", "brew"}})
		if eerr != nil {
			t.Fatalf("RunCapture: %v", eerr)
		}
	})

	apps := readCapturedManifestBrew(t, outPath)
	if len(apps) != 2 || apps[0].ID != "hello" || apps[1].ID != "ripgrep" {
		t.Fatalf("captured apps = %+v, want brew and nix apps", apps)
	}
	if fr.currentCalls != 1 || fbe.calls != 1 {
		t.Fatalf("lane calls = nix:%d brew:%d, want one each", fr.currentCalls, fbe.calls)
	}
}

func TestRunCapture_DriverFilter_BrewOnLinuxFailsWithoutNixFallback(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "captured.jsonc")
	fr := &fakeRealizer{currentSet: nixSet("ripgrep")}
	brewFactoryCalls := 0

	withCaptureRealizerAndBrew(fr, func() (driver.Driver, error) {
		brewFactoryCalls++
		return nil, ErrNoBrewDriver
	}, "linux", func() {
		_, eerr := RunCapture(CaptureFlags{Out: outPath, Drivers: []string{"brew"}})
		if eerr == nil || eerr.Code != "CAPTURE_FAILED" {
			t.Fatalf("RunCapture error = %+v, want CAPTURE_FAILED", eerr)
		}
	})

	if fr.currentCalls != 0 {
		t.Fatalf("realizer Current calls = %d, want 0", fr.currentCalls)
	}
	if brewFactoryCalls != 0 {
		t.Fatalf("brew factory calls = %d, want 0 for unsupported host", brewFactoryCalls)
	}
	if _, err := os.Stat(outPath); !os.IsNotExist(err) {
		t.Fatalf("explicit unsupported driver wrote output: stat error = %v", err)
	}
}

// TestRunCapture_BrewLane_IDCollisionKeepsBoth: package identity never crosses
// manager boundaries; the later Brew entry receives a stable ID suffix.
func TestRunCapture_BrewLane_IDCollisionKeepsBoth(t *testing.T) {
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "captured.jsonc")

	fr := &fakeRealizer{currentSet: nixSet("ripgrep")}
	fbe := &fakeBrewEnumerator{apps: []driver.InstalledPackage{
		{DisplayName: "ripgrep", Ref: "ripgrep", Version: "14.0"}, // collides with nix
		{DisplayName: "hello", Ref: "hello", Version: "2.12"},
	}}

	withCaptureRealizerAndBrew(fr, func() (driver.Driver, error) { return fbe, nil }, "darwin", func() {
		_, e := RunCapture(CaptureFlags{Out: outPath})
		if e != nil {
			t.Fatalf("RunCapture error: %v", e)
		}
	})

	apps := readCapturedManifestBrew(t, outPath)
	ids := map[string]bool{}
	for _, a := range apps {
		ids[a.ID] = true
	}
	if !ids["ripgrep"] || !ids["ripgrep-brew"] {
		t.Errorf("colliding Nix/Brew entries must both remain with deterministic IDs: %+v", apps)
	}
}

func TestRunCapture_Update_BrewIDCollisionWithExistingNixGetsDriverSuffix(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.jsonc")
	out := filepath.Join(dir, "captured.jsonc")
	if err := os.WriteFile(existing, []byte(`{
  "version": 1,
  "name": "existing",
  "apps": [{"id":"foo","refs":{"darwin":"nixpkgs#foo"}}]
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	fr := &fakeRealizer{currentSet: realizer.Set{Elements: map[string]realizer.Element{}}}
	fbe := &fakeBrewEnumerator{apps: []driver.InstalledPackage{{DisplayName: "foo", Ref: "foo"}}}
	withCaptureRealizerAndBrew(fr, func() (driver.Driver, error) { return fbe, nil }, "darwin", func() {
		if _, eerr := RunCapture(CaptureFlags{Out: out, Manifest: existing, Update: true}); eerr != nil {
			t.Fatalf("RunCapture: %v", eerr)
		}
	})

	apps := readCapturedManifestBrew(t, out)
	ids := map[string]bool{}
	for _, app := range apps {
		if ids[strings.ToLower(app.ID)] {
			t.Fatalf("duplicate manifest ID %q in %+v", app.ID, apps)
		}
		ids[strings.ToLower(app.ID)] = true
	}
	if !ids["foo"] || !ids["foo-brew"] {
		t.Fatalf("IDs = %+v, want foo and foo-brew", ids)
	}
}

func TestRunCapture_Update_NixIDCollisionWithExistingBrewGetsNixSuffix(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "existing.jsonc")
	out := filepath.Join(dir, "captured.jsonc")
	if err := os.WriteFile(existing, []byte(`{
  "version": 1,
  "name": "existing",
  "apps": [{"id":"foo","driver":"brew","refs":{"darwin":"old-foo"}}]
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	fr := &fakeRealizer{currentSet: nixSet("foo")}
	fbe := &fakeBrewEnumerator{}
	withCaptureRealizerAndBrew(fr, func() (driver.Driver, error) { return fbe, nil }, "darwin", func() {
		if _, eerr := RunCapture(CaptureFlags{Out: out, Manifest: existing, Update: true}); eerr != nil {
			t.Fatalf("RunCapture: %v", eerr)
		}
	})

	apps := readCapturedManifestBrew(t, out)
	ids := map[string]bool{}
	for _, app := range apps {
		ids[strings.ToLower(app.ID)] = true
	}
	if !ids["foo"] || !ids["foo-nix"] {
		t.Fatalf("IDs = %+v, want foo and foo-nix", ids)
	}
}
