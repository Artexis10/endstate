// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/settingscodec"
)

func TestObservedConfigCaptureDoesNotReadOtherDeclaredSources(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	source := filepath.Join(root, ".ripgreprc")
	data := []byte("# preferences\r\n --hidden \r\n")
	writeCaptureFile(t, source, data)
	// A directory is legal for the ordinary collector but must never be walked
	// when it was absent from the selected observation set.
	writeCaptureFile(t, filepath.Join(root, "unselected", "private"), []byte("private"))
	sum := sha256.Sum256(data)
	plan := testConfigSetCapturePlan(root, &modules.CaptureDef{Files: []modules.CaptureFile{
		{Source: "${home}/.ripgreprc", Dest: "selected"},
		{Source: "${instance.root}/unselected", Dest: "unselected"},
	}})
	plan.ObservedFiles = []SettingsFileCapturePlan{{CandidateID: plan.Module.ID, Codec: settingscodec.RipgrepPortableArgumentsV1, Target: "${home}/.ripgreprc", Source: filepath.Join(root, ".ripgreprc"), ObservedSize: int64(len(data)), ObservedSHA256: hex.EncodeToString(sum[:])}}
	staging := t.TempDir()
	got, err := CollectConfigSet(plan, staging)
	if err != nil {
		t.Fatal(err)
	}
	if got.FilesCollected != 1 {
		t.Fatalf("collected %d files, want only selected observation", got.FilesCollected)
	}
	output, err := os.ReadFile(filepath.Join(staging, filepath.FromSlash(got.PayloadRoot), "selected"))
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "--hidden\n" {
		t.Fatalf("payload was not canonical: %q", output)
	}
}

func TestObservedConfigCaptureRejectsChangedAndUnsupportedContent(t *testing.T) {
	for _, test := range []struct {
		name              string
		observed, current string
	}{
		{"changed", "--hidden\n", "--smart-case\n"},
		{"unsupported", "--pre=external-command\n", "--pre=external-command\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", root)
			source := filepath.Join(root, ".ripgreprc")
			writeCaptureFile(t, source, []byte(test.current))
			plan := testConfigSetCapturePlan(root, &modules.CaptureDef{Files: []modules.CaptureFile{{Source: "${home}/.ripgreprc", Dest: "config"}}})
			sum := sha256.Sum256([]byte(test.observed))
			plan.ObservedFiles = []SettingsFileCapturePlan{{CandidateID: plan.Module.ID, Codec: settingscodec.RipgrepPortableArgumentsV1, Target: "${home}/.ripgreprc", Source: source, ObservedSize: int64(len(test.observed)), ObservedSHA256: hex.EncodeToString(sum[:])}}
			staging := t.TempDir()
			if _, err := CollectConfigSet(plan, staging); err == nil {
				t.Fatal("unsafe content was captured")
			}
			entries, err := os.ReadDir(staging)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatal("failed collection left payload")
			}
		})
	}
}

func TestObservedConfigCaptureRejectsLinkedParent(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	source := filepath.Join(root, ".config", "ripgrep", "config")
	data := []byte("--hidden\n")
	writeCaptureFile(t, filepath.Join(outside, "ripgrep", "config"), data)
	if err := os.Symlink(outside, filepath.Join(root, ".config")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	plan := testConfigSetCapturePlan(root, &modules.CaptureDef{Files: []modules.CaptureFile{{Source: "${xdg.config}/ripgrep/config", Dest: "config"}}})
	sum := sha256.Sum256(data)
	plan.ObservedFiles = []SettingsFileCapturePlan{{CandidateID: plan.Module.ID, Codec: settingscodec.RipgrepPortableArgumentsV1, Target: "${xdg.config}/ripgrep/config", Source: source, ObservedSize: int64(len(data)), ObservedSHA256: hex.EncodeToString(sum[:])}}
	if _, err := CollectConfigSet(plan, t.TempDir()); ConfigCaptureDiagnosticCode(err) != ConfigCaptureUnsafePath {
		t.Fatalf("linked parent bypassed source authority: %v", err)
	}
}
