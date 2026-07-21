// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package winget

import (
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/snapshot"
)

func TestSourceScopedWingetCommands(t *testing.T) {
	tests := []struct {
		name string
		run  func(*WingetDriver) error
		verb string
	}{
		{"detect", func(d *WingetDriver) error { _, _, err := d.DetectSource("9NBLGGH4NNS1", "msstore"); return err }, "list"},
		{"install", func(d *WingetDriver) error { _, err := d.InstallSource("9NBLGGH4NNS1", "msstore"); return err }, "install"},
		{"uninstall", func(d *WingetDriver) error { _, err := d.UninstallSource("9NBLGGH4NNS1", "msstore"); return err }, "uninstall"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var args []string
			d := &WingetDriver{ExecCommand: fakeUninstallCmd(0, "", "", &args)}
			if err := tc.run(d); err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(args, " ")
			if !strings.Contains(joined, tc.verb) || !strings.Contains(joined, "--source msstore") {
				t.Fatalf("argv = %q, want %s scoped to msstore", joined, tc.verb)
			}
		})
	}
}

func TestDetectBatchSourceDoesNotCrossSatisfy(t *testing.T) {
	orig := takeSnapshotSourceFn
	t.Cleanup(func() { takeSnapshotSourceFn = orig })
	takeSnapshotSourceFn = func(source string) ([]snapshot.SnapshotApp, error) {
		if source == "winget" {
			return []snapshot.SnapshotApp{{ID: "Same.Ref", Name: "Community", Source: source}}, nil
		}
		return nil, nil
	}
	d := New()
	community, err := d.DetectBatchSource([]string{"Same.Ref"}, "winget")
	if err != nil || !community["Same.Ref"].Installed {
		t.Fatalf("community = %+v, err=%v", community, err)
	}
	store, err := d.DetectBatchSource([]string{"Same.Ref"}, "msstore")
	if err != nil || store["Same.Ref"].Installed {
		t.Fatalf("store = %+v, err=%v", store, err)
	}
	var _ driver.SourceBatchDetector = d
}
