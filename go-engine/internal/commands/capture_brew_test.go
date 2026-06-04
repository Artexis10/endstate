// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/driver/brew"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// fakeBrewEnumerator is a driver.Driver that also satisfies the capture-lane
// brewEnumerator interface, returning a scripted set of installed brew apps.
type fakeBrewEnumerator struct {
	fakeBrewDriver
	apps []brew.InstalledApp
	err  error
}

func (f *fakeBrewEnumerator) EnumerateInstalled() ([]brew.InstalledApp, error) {
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
	fbe := &fakeBrewEnumerator{apps: []brew.InstalledApp{
		{Name: "hello", Ref: "hello", Cask: false, Version: "2.12"},
		{Name: "firefox", Ref: "cask:firefox", Cask: true, Version: "122.0"},
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

// TestRunCapture_BrewLane_DedupByID: a brew app whose ID collides with a
// realizer-captured app does not duplicate (realizer wins, brew is skipped).
func TestRunCapture_BrewLane_DedupByID(t *testing.T) {
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "captured.jsonc")

	fr := &fakeRealizer{currentSet: nixSet("ripgrep")}
	fbe := &fakeBrewEnumerator{apps: []brew.InstalledApp{
		{Name: "ripgrep", Ref: "ripgrep", Cask: false, Version: "14.0"}, // collides with nix
		{Name: "hello", Ref: "hello", Cask: false, Version: "2.12"},
	}}

	withCaptureRealizerAndBrew(fr, func() (driver.Driver, error) { return fbe, nil }, "darwin", func() {
		_, e := RunCapture(CaptureFlags{Out: outPath})
		if e != nil {
			t.Fatalf("RunCapture error: %v", e)
		}
	})

	apps := readCapturedManifestBrew(t, outPath)
	count := 0
	for _, a := range apps {
		if a.ID == "ripgrep" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("ripgrep must appear exactly once (dedup), got %d (%+v)", count, apps)
	}
}
