// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

type capturedPackageEvent struct {
	Event  string `json:"event"`
	ID     string `json:"id"`
	Driver string `json:"driver"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func assertCapturedPackageEventsAreDetected(t *testing.T, stream string, wantIdentities ...string) {
	t.Helper()
	want := make(map[string]bool, len(wantIdentities))
	for _, identity := range wantIdentities {
		want[identity] = true
	}
	seen := make(map[string]bool, len(wantIdentities))

	for _, line := range strings.Split(stream, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event capturedPackageEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("capture event is not valid JSON: %v\n%s", err, line)
		}
		if event.Event != "item" {
			continue
		}
		identity := event.Driver + ":" + event.ID
		if !want[identity] {
			continue
		}
		seen[identity] = true
		if event.Status != "present" || event.Reason != "detected" {
			t.Errorf("capture item %s emitted status=%q reason=%q, want status=%q reason=%q", identity, event.Status, event.Reason, "present", "detected")
		}
	}

	for _, identity := range wantIdentities {
		if !seen[identity] {
			t.Errorf("capture stream did not contain item event %s", identity)
		}
	}
}

func TestRunCapture_WindowsPackageEventsUsePresentDetected(t *testing.T) {
	withCaptureEnumerators(t, map[string]fakeInstalledEnumerator{
		"winget":     {packages: []driver.InstalledPackage{{Ref: "Git.Git", DisplayName: "Git"}}},
		"chocolatey": {packages: []driver.InstalledPackage{{Ref: "ripgrep", DisplayName: "ripgrep"}}},
	}, nil)

	stderr := captureStderr(t, func() {
		emptyCatalog(func() {
			if _, eerr := RunCapture(CaptureFlags{
				Out:    filepath.Join(t.TempDir(), "windows-capture.jsonc"),
				Events: "jsonl",
			}); eerr != nil {
				t.Fatalf("RunCapture: %v", eerr)
			}
		})
	})

	assertCapturedPackageEventsAreDetected(t, stderr, "winget:Git.Git", "chocolatey:ripgrep")
}

func TestRunCapture_RealizerPackageEventsUsePresentDetected(t *testing.T) {
	fr := &fakeRealizer{currentSet: nixSet("ripgrep")}
	brew := &fakeBrewEnumerator{apps: []driver.InstalledPackage{{Ref: "hello", DisplayName: "hello"}}}

	var stderr string
	withCaptureRealizerAndBrew(fr, func() (driver.Driver, error) { return brew, nil }, "darwin", func() {
		stderr = captureStderr(t, func() {
			emptyCatalog(func() {
				if _, eerr := RunCapture(CaptureFlags{
					Out:    filepath.Join(t.TempDir(), "realizer-capture.jsonc"),
					Events: "jsonl",
				}); eerr != nil {
					t.Fatalf("RunCapture: %v", eerr)
				}
			})
		})
	})

	assertCapturedPackageEventsAreDetected(t, stderr, "nix:ripgrep", "brew:hello")
}
