// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostedLiveTargetsSnapshotBindsExactSixDeclaredTargetsWithoutHostPaths(t *testing.T) {
	definition, appData := hostedLiveTargetsDefinition(t)
	writeHostedLiveComparableTargets(t, definition, appData)
	directory := hostedLiveTargetPath(t, definition, appData, "apps/notepad-plus-plus/userDefineLangs")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}

	snapshot, err := snapshotHostedLiveTargets(definition, appData)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.files) != 5 || snapshot.directory.identity != "apps/notepad-plus-plus/userDefineLangs" || !snapshot.directory.present {
		t.Fatalf("snapshot target coverage = %+v", snapshot)
	}
	if strings.Contains(fmt.Sprintf("%+v", snapshot), appData) {
		t.Fatalf("target snapshot retained APPDATA path: %+v", snapshot)
	}
	for _, target := range snapshot.files {
		if target.absent || target.size == 0 || target.sha256 == "" || len(target.bytes) == 0 || !target.mode.IsRegular() {
			t.Fatalf("file target proof = %+v", target)
		}
	}
}

func TestHostedLiveTargetsSeedAndAbsentAssertionsAreExact(t *testing.T) {
	definition, appData := hostedLiveTargetsDefinition(t)
	for _, identity := range []string{"apps/notepad-plus-plus/config.xml", "apps/notepad-plus-plus/shortcuts.xml"} {
		path := hostedLiveTargetPath(t, definition, appData, identity)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(identity), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := snapshotHostedLiveTargets(definition, appData)
	if err != nil {
		t.Fatal(err)
	}
	if err := snapshot.RequireSeeded(); err != nil {
		t.Fatalf("RequireSeeded() error = %v", err)
	}
	if err := snapshot.RequireAbsent(); err == nil {
		t.Fatal("RequireAbsent() accepted seeded targets")
	}

	for _, target := range definition.DeclaredTargets {
		if err := os.RemoveAll(hostedLiveTargetPath(t, definition, appData, target.Identity)); err != nil {
			t.Fatal(err)
		}
	}
	absent, err := snapshotHostedLiveTargets(definition, appData)
	if err != nil {
		t.Fatal(err)
	}
	if err := absent.RequireAbsent(); err != nil {
		t.Fatalf("RequireAbsent() error = %v", err)
	}
}

func TestHostedLiveTargetsRejectHostileTargetState(t *testing.T) {
	for _, test := range []struct {
		name string
		set  func(t *testing.T, definition LiveDefinition, appData string)
		seed bool
	}{
		{"wrong file kind", func(t *testing.T, definition LiveDefinition, appData string) {
			path := hostedLiveTargetPath(t, definition, appData, "apps/notepad-plus-plus/config.xml")
			if err := os.MkdirAll(path, 0o700); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"linked file", func(t *testing.T, definition LiveDefinition, appData string) {
			path := hostedLiveTargetPath(t, definition, appData, "apps/notepad-plus-plus/config.xml")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			if output, err := exec.Command("cmd", "/d", "/c", "mklink", "/J", path, outside).CombinedOutput(); err != nil {
				t.Fatalf("mklink /J: %v: %s", err, output)
			}
		}, false},
		{"oversize file", func(t *testing.T, definition LiveDefinition, appData string) {
			path := hostedLiveTargetPath(t, definition, appData, "apps/notepad-plus-plus/config.xml")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, bytes.Repeat([]byte("x"), maxHostedLiveTargetBytes+1), 0o600); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"empty required seed", func(t *testing.T, definition LiveDefinition, appData string) {
			writeHostedLiveSeedTargets(t, definition, appData)
			path := hostedLiveTargetPath(t, definition, appData, "apps/notepad-plus-plus/config.xml")
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"extra seeded target", func(t *testing.T, definition LiveDefinition, appData string) {
			writeHostedLiveSeedTargets(t, definition, appData)
			path := hostedLiveTargetPath(t, definition, appData, "apps/notepad-plus-plus/langs.xml")
			if err := os.WriteFile(path, []byte("unexpected"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"seeded directory", func(t *testing.T, definition LiveDefinition, appData string) {
			writeHostedLiveSeedTargets(t, definition, appData)
			if err := os.MkdirAll(hostedLiveTargetPath(t, definition, appData, "apps/notepad-plus-plus/userDefineLangs"), 0o700); err != nil {
				t.Fatal(err)
			}
		}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition, appData := hostedLiveTargetsDefinition(t)
			test.set(t, definition, appData)
			snapshot, err := snapshotHostedLiveTargets(definition, appData)
			if !test.seed {
				if err == nil {
					t.Fatal("snapshotHostedLiveTargets() accepted hostile target state")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := snapshot.RequireSeeded(); err == nil {
				t.Fatal("RequireSeeded() accepted hostile target state")
			}
		})
	}
}

func TestHostedLiveTargetsCompareRestoredRecoveryAndConvergenceExactly(t *testing.T) {
	definition, appData := hostedLiveTargetsDefinition(t)
	writeHostedLiveComparableTargets(t, definition, appData)
	before, err := snapshotHostedLiveTargets(definition, appData)
	if err != nil {
		t.Fatal(err)
	}
	after, err := snapshotHostedLiveTargets(definition, appData)
	if err != nil {
		t.Fatal(err)
	}
	for _, compare := range []func(hostedLiveTargets, hostedLiveTargets) error{compareHostedLiveRestoredTargets, compareHostedLiveRecoveryTargets, compareHostedLiveConvergenceTargets} {
		if err := compare(before, after); err != nil {
			t.Fatalf("exact target compare error = %v", err)
		}
	}
	path := hostedLiveTargetPath(t, definition, appData, "apps/notepad-plus-plus/shortcuts.xml")
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := snapshotHostedLiveTargets(definition, appData)
	if err != nil {
		t.Fatal(err)
	}
	for _, compare := range []func(hostedLiveTargets, hostedLiveTargets) error{compareHostedLiveRestoredTargets, compareHostedLiveRecoveryTargets, compareHostedLiveConvergenceTargets} {
		if err := compare(before, changed); err == nil {
			t.Fatal("exact target compare accepted changed bytes")
		}
	}
}

func hostedLiveTargetsDefinition(t *testing.T) (LiveDefinition, string) {
	t.Helper()
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	appData := t.TempDir()
	withWindowsLiveTestAppData(t, appData)
	return definition, appData
}

func hostedLiveTargetPath(t *testing.T, definition LiveDefinition, appData, identity string) string {
	t.Helper()
	targets, err := resolveWindowsLiveDeclaredTargets(definition, appData)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if target.identity == identity {
			return target.path
		}
	}
	t.Fatalf("declared target %q is absent", identity)
	return ""
}

func writeHostedLiveSeedTargets(t *testing.T, definition LiveDefinition, appData string) {
	t.Helper()
	for _, identity := range []string{"apps/notepad-plus-plus/config.xml", "apps/notepad-plus-plus/shortcuts.xml"} {
		path := hostedLiveTargetPath(t, definition, appData, identity)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(identity), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func writeHostedLiveComparableTargets(t *testing.T, definition LiveDefinition, appData string) {
	t.Helper()
	for _, target := range definition.DeclaredTargets {
		if target.Kind != LiveDeclaredTargetFile {
			continue
		}
		path := hostedLiveTargetPath(t, definition, appData, target.Identity)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(target.Identity), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHostedLiveTargetsProjectsOnlyFileSnapshotsForCapture(t *testing.T) {
	targets := hostedLiveTargets{files: []hostedLiveTargetFile{{identity: "apps/notepad-plus-plus/config.xml", mode: 0o600, size: 2, sha256: "a", bytes: []byte("ok")}, {identity: "apps/notepad-plus-plus/contextMenu.xml", absent: true}, {identity: "apps/notepad-plus-plus/langs.xml", absent: true}, {identity: "apps/notepad-plus-plus/shortcuts.xml", mode: 0o600, size: 2, sha256: "b", bytes: []byte("go")}, {identity: "apps/notepad-plus-plus/stylers.xml", absent: true}}, directory: hostedLiveTargetDirectory{identity: "apps/notepad-plus-plus/userDefineLangs"}}
	snapshots, err := targets.captureSnapshots()
	if err != nil || len(snapshots) != 5 || snapshots[0].Identity != "apps/notepad-plus-plus/config.xml" || string(snapshots[0].Bytes) != "ok" {
		t.Fatalf("captureSnapshots() = %+v, %v", snapshots, err)
	}
	targets.files[0].bytes[0] = 'X'
	if string(snapshots[0].Bytes) != "ok" {
		t.Fatal("capture snapshots retained mutable target bytes")
	}
}
