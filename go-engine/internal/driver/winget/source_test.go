// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package winget

import (
	"os/exec"
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

func TestInstallSourceUsesExactWingetArguments(t *testing.T) {
	var got []string
	d := &WingetDriver{ExecCommand: func(name string, args ...string) *exec.Cmd {
		got = append([]string{name}, args...)
		return fakeUninstallCmd(0, "", "", nil)(name, args...)
	}}

	if _, err := d.InstallSource("9NBLGGH4NNS1", "msstore"); err != nil {
		t.Fatalf("InstallSource error = %v", err)
	}

	want := []string{
		"winget", "install", "--id", "9NBLGGH4NNS1", "--source", "msstore",
		"--accept-source-agreements", "--accept-package-agreements", "-e", "--silent",
	}
	if diff := strings.Join(got, "\x00"); diff != strings.Join(want, "\x00") {
		t.Fatalf("winget argv = %#v, want %#v", got, want)
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
