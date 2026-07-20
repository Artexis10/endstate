// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestIsBloatDirSegment(t *testing.T) {
	cases := []struct {
		rel  string
		want bool
	}{
		{"Cache/data.bin", true},
		{"config/UPDATES/installer", true}, // case-insensitive
		{"powertoys/Updates/setup.exe", true},
		{"Code Cache/index", true}, // multi-word segment
		{"Crash Reports/x.dmp", true},
		{"logs/app.log", true},
		{"config.json", false},
		{"settings/keybindings.json", false},
		{"cached_data.json", false}, // substring, not a full segment → kept
	}
	for _, c := range cases {
		if got := isBloatDirSegment(c.rel); got != c.want {
			t.Errorf("isBloatDirSegment(%q) = %v, want %v", c.rel, got, c.want)
		}
	}
}

func TestIsOversizedInstaller(t *testing.T) {
	const big = captureBloatBinaryMaxBytes + 1
	const small = captureBloatBinaryMaxBytes - 1
	cases := []struct {
		path string
		size int64
		want bool
	}{
		{"powertoysusersetup-0.98.1-x64.exe", big, true},
		{"installer.msi", big, true},
		{"app.MSIX", big, true},       // case-insensitive ext
		{"helper.exe", small, false},  // small binary rides along
		{"settings.json", big, false}, // not an installer ext
		{"notes.txt", big * 100, false},
	}
	for _, c := range cases {
		if got := isOversizedInstaller(c.path, c.size); got != c.want {
			t.Errorf("isOversizedInstaller(%q, %d) = %v, want %v", c.path, c.size, got, c.want)
		}
	}
}

// The bloat-dir baseline is inherited by every module — a module with NO
// excludeGlobs still skips Updates/Crashpad/etc. within a captured directory.
func TestCollectConfigFiles_BloatBaselineInherited(t *testing.T) {
	dir := t.TempDir()
	stagingDir := filepath.Join(dir, "staging")
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(dir, "source")
	for _, d := range []string{"Updates", "Crashpad"} {
		if err := os.MkdirAll(filepath.Join(srcDir, d), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(srcDir, "config.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "Updates", "setup.dat"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "Crashpad", "dump"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	// NB: no ExcludeGlobs on the module — the baseline must apply anyway.
	mod := &modules.Module{
		ID: "apps.test",
		Capture: &modules.CaptureDef{
			Files: []modules.CaptureFile{{Source: srcDir, Dest: "apps/test/source"}},
		},
	}

	if _, _, err := CollectConfigFiles(mod, stagingDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	base := filepath.Join(stagingDir, "configs", "test", "source")
	if _, err := os.Stat(filepath.Join(base, "config.json")); err != nil {
		t.Errorf("config.json should be kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "Updates", "setup.dat")); !os.IsNotExist(err) {
		t.Error("Updates/ should be excluded by the inherited baseline")
	}
	if _, err := os.Stat(filepath.Join(base, "Crashpad", "dump")); !os.IsNotExist(err) {
		t.Error("Crashpad/ should be excluded by the inherited baseline")
	}
}

// An oversized installer inside a captured directory is skipped even when it's
// not under a bloat dir (the size guard), while config + small files are kept.
func TestCollectConfigFiles_OversizedInstallerSkipped(t *testing.T) {
	dir := t.TempDir()
	stagingDir := filepath.Join(dir, "staging")
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(dir, "source")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "config.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	big := make([]byte, captureBloatBinaryMaxBytes+1)
	if err := os.WriteFile(filepath.Join(srcDir, "huge-installer.exe"), big, 0644); err != nil {
		t.Fatal(err)
	}

	mod := &modules.Module{
		ID: "apps.test",
		Capture: &modules.CaptureDef{
			Files: []modules.CaptureFile{{Source: srcDir, Dest: "apps/test/source"}},
		},
	}

	if _, _, err := CollectConfigFiles(mod, stagingDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	base := filepath.Join(stagingDir, "configs", "test", "source")
	if _, err := os.Stat(filepath.Join(base, "config.json")); err != nil {
		t.Errorf("config.json should be kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "huge-installer.exe")); !os.IsNotExist(err) {
		t.Error("oversized installer should be skipped by the size guard")
	}
}
