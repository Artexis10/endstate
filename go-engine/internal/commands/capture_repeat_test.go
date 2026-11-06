// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/discovery"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// Repeated capture must keep its portable data while recording the actual run time.
func TestLinuxCaptureIdenticalDiscoveryPreservesPortableBundle(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("production Linux module workflow")
	}
	root := t.TempDir()
	home := filepath.Join(root, "source-home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".ripgreprc"), []byte("# Portable search preferences\n--hidden\n--line-number\n"), 0600); err != nil {
		t.Fatal(err)
	}
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENDSTATE_ROOT", repo)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	oldRoot, oldRealizer, oldDiscovery, oldCatalog := resolveRepoRootFn, newRealizerFn, discoverLinuxFn, loadCaptureModuleCatalogFn
	t.Cleanup(func() {
		resolveRepoRootFn, newRealizerFn, discoverLinuxFn, loadCaptureModuleCatalogFn = oldRoot, oldRealizer, oldDiscovery, oldCatalog
	})
	resolveRepoRootFn = func() string { return repo }
	loadCaptureModuleCatalogFn = modules.GetCatalogWithDiagnostics
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	discoverLinuxFn = func(ctx context.Context, request discovery.Request, _ realizer.Realizer) (discovery.Result, error) {
		request.SelectedSources = []string{discovery.HomeManagerSettingsSource}
		return discovery.Discover(ctx, request)
	}
	out := filepath.Join(root, "repeat.endstate")
	var firstDiscovery []byte
	var firstBundle []stableCaptureMember
	withFakeGenerations(nil, nil, func() {
		for run := 0; run < 2; run++ {
			started := time.Now().UTC().Truncate(time.Second)
			raw, captureErr := RunCapture(CaptureFlags{Out: out, Only: "apps.ripgrep"})
			if captureErr != nil {
				t.Fatalf("capture %d: %+v", run+1, captureErr)
			}
			finished := time.Now().UTC()
			result := raw.(*CaptureResult)
			if result.Discovery == nil || result.Discovery.Counts.SettingsOnly != 1 || result.Discovery.Counts.Selected != 1 {
				t.Fatalf("capture %d lost selected settings-only state: %+v", run+1, result.Discovery)
			}
			discoveryBytes, err := json.Marshal(result.Discovery)
			if err != nil {
				t.Fatal(err)
			}
			members := stableCaptureMembers(t, out, started, finished)
			if run == 0 {
				firstDiscovery, firstBundle = discoveryBytes, members
				continue
			}
			if !bytes.Equal(firstDiscovery, discoveryBytes) {
				t.Fatal("identical input changed serialized discovery")
			}
			if !reflect.DeepEqual(firstBundle, members) {
				t.Fatal("identical input changed bundle structure, metadata or payload")
			}
		}
	})
	for _, member := range firstBundle {
		if (member.Name == "manifest.jsonc" || member.Name == "metadata.json") && bytes.Contains(member.Data, []byte(home)) {
			t.Fatalf("portable %s leaked the source root", member.Name)
		}
	}
}

type stableCaptureMember struct {
	Name   string
	Method uint16
	Mode   os.FileMode
	Data   []byte
}

func stableCaptureMembers(t *testing.T, path string, started, finished time.Time) []stableCaptureMember {
	t.Helper()
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	members := make([]stableCaptureMember, 0, len(archive.File))
	seen := map[string]bool{}
	for _, file := range archive.File {
		if seen[file.Name] {
			t.Fatalf("duplicate archive member %s", file.Name)
		}
		seen[file.Name] = true
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil {
			t.Fatalf("read archive member: %v, %v", readErr, closeErr)
		}
		// Only capture timestamps and ZIP entry times are outside equivalence.
		// Compare every other JSON value, entry order/name/mode/method and payload byte.
		timeField := ""
		switch file.Name {
		case "manifest.jsonc":
			timeField = "captured"
		case "metadata.json":
			timeField = "capturedAt"
		}
		if timeField != "" {
			var document map[string]any
			if err := json.Unmarshal(data, &document); err != nil {
				t.Fatal(err)
			}
			value, _ := document[timeField].(string)
			observed, err := time.Parse(time.RFC3339, value)
			if err != nil || observed.Before(started) || observed.After(finished) {
				t.Fatalf("%s does not describe this capture window: %q", timeField, value)
			}
			delete(document, timeField)
			data, err = json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
		}
		members = append(members, stableCaptureMember{file.Name, file.Method, file.Mode(), data})
	}
	if !seen["manifest.jsonc"] || !seen["metadata.json"] || len(members) < 3 {
		t.Fatal("capture did not include manifest, metadata and configuration payload")
	}
	return members
}
