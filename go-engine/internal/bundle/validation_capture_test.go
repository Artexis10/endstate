// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestCollectConfigFilesWithValidationReadsSandboxAndNotOriginalHost(t *testing.T) {
	originalAppData := t.TempDir()
	t.Setenv("APPDATA", originalAppData)
	writeCaptureFile(t, filepath.Join(originalAppData, "Vendor", "settings.json"), []byte("original-host"))
	context := activeBundleValidationContext(t, "apps.example")
	virtualAppData, _ := context.VirtualRoot("APPDATA")
	writeCaptureFile(t, filepath.Join(virtualAppData, "Vendor", "settings.json"), []byte("sandbox-sentinel"))

	mod := &modules.Module{ID: "apps.example", Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{
		Source: `%APPDATA%\Vendor\settings.json`, Dest: "apps/example/settings.json",
	}}}}
	staging := bundleValidationWorkRoot(t, context, "legacy-files")
	paths, excluded, err := CollectConfigFilesWithValidation(mod, staging, context)
	if err != nil {
		t.Fatalf("CollectConfigFilesWithValidation: %v", err)
	}
	if excluded != 0 || !reflect.DeepEqual(paths, []string{"configs/example/settings.json"}) {
		t.Fatalf("collection = paths %#v excluded %d", paths, excluded)
	}
	data, err := os.ReadFile(filepath.Join(staging, "configs", "example", "settings.json"))
	if err != nil || string(data) != "sandbox-sentinel" {
		t.Fatalf("staged bytes = %q err=%v", data, err)
	}
	original, err := os.ReadFile(filepath.Join(originalAppData, "Vendor", "settings.json"))
	if err != nil || string(original) != "original-host" {
		t.Fatalf("original host sentinel changed = %q err=%v", original, err)
	}
}

func TestCollectConfigSetWithValidationPreservesProductionInstanceAndFingerprint(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.example")
	virtualAppData, _ := context.VirtualRoot("APPDATA")
	instanceRoot := filepath.Join(virtualAppData, "Vendor", "27.4")
	writeCaptureFile(t, filepath.Join(instanceRoot, "prefs.json"), []byte("sandbox-generation"))
	plan := testConfigSetCapturePlan(instanceRoot, &modules.CaptureDef{Files: []modules.CaptureFile{{
		Source: `${instance.root}/prefs.json`, Dest: "profiles/${instance.version}/prefs.json",
	}}})
	plan.Instance.Evidence = modules.InstanceEvidence{Type: "path", Path: instanceRoot}
	plan.Instance.CanonicalLocator = "path:" + filepath.ToSlash(strings.ToLower(instanceRoot))
	beforeInstance := plan.Instance
	beforeRevision := plan.Module.Revision
	beforeFingerprint := plan.Generation.Fingerprint
	beforeSnapshot := append([]byte(nil), plan.Module.CanonicalSnapshot()...)

	staging := bundleValidationWorkRoot(t, context, "generation-files")
	result, err := CollectConfigSetWithValidation(plan, staging, context)
	if err != nil {
		t.Fatalf("CollectConfigSetWithValidation: %v", err)
	}
	if !reflect.DeepEqual(plan.Instance, beforeInstance) || plan.Module.Revision != beforeRevision ||
		plan.Generation.Fingerprint != beforeFingerprint || !reflect.DeepEqual(plan.Module.CanonicalSnapshot(), beforeSnapshot) {
		t.Fatal("validation collection mutated production provenance")
	}
	payload := filepath.Join(staging, filepath.FromSlash(result.PayloadRoot), "profiles", "27.4", "prefs.json")
	data, err := os.ReadFile(payload)
	if err != nil || string(data) != "sandbox-generation" {
		t.Fatalf("generation payload = %q err=%v", data, err)
	}
}

func TestValidationCollectorsRejectUnsafeSourceBeforeStaging(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.example")
	staging := bundleValidationWorkRoot(t, context, "unsafe-source")
	mod := &modules.Module{ID: "apps.example", Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{
		Source: filepath.Join(t.TempDir(), "host-secret.txt"), Dest: "apps/example/settings.json",
	}}}}
	if _, _, err := CollectConfigFilesWithValidation(mod, staging, context); err == nil {
		t.Fatal("validation collector accepted a hardcoded host source")
	}
	if _, err := os.Stat(filepath.Join(staging, "configs")); !os.IsNotExist(err) {
		t.Fatalf("unsafe source reached staging: %v", err)
	}
}

func TestValidationCollectorErrorsDoNotExposePhysicalRoots(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.example")
	legacy := &modules.Module{ID: "apps.example", Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{
		Source: `%APPDATA%\Vendor\missing.json`, Dest: "apps/example/missing.json",
	}}}}
	_, _, legacyErr := CollectConfigFilesWithValidation(legacy, bundleValidationWorkRoot(t, context, "legacy-missing"), context)
	planRoot, _ := context.VirtualRoot("APPDATA")
	plan := testConfigSetCapturePlan(planRoot, &modules.CaptureDef{Files: []modules.CaptureFile{{
		Source: `${instance.root}/missing.json`, Dest: "missing.json",
	}}})
	_, generationErr := CollectConfigSetWithValidation(plan, bundleValidationWorkRoot(t, context, "generation-missing"), context)
	for name, err := range map[string]error{"legacy": legacyErr, "generation": generationErr} {
		if err == nil {
			t.Fatalf("%s missing source returned nil", name)
		}
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, strings.ToLower(context.Root())) || strings.Contains(lower, strings.ToLower(planRoot)) {
			t.Fatalf("%s error leaked validation root: %v", name, err)
		}
	}
}

func TestValidationCollectorIOFailuresAreIsolationErrorsWithoutPhysicalRoots(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.example")
	appData, _ := context.VirtualRoot("APPDATA")
	writeCaptureFile(t, filepath.Join(appData, "Vendor", "settings.json"), []byte("sandbox"))

	legacy := &modules.Module{ID: "apps.example", Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{
		Source: `%APPDATA%\Vendor\settings.json`, Dest: "settings.json",
	}}}}
	legacyStaging := bundleValidationWorkRoot(t, context, "legacy-io")
	if err := os.WriteFile(filepath.Join(legacyStaging, "configs"), []byte("blocks directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, legacyErr := CollectConfigFilesWithValidation(legacy, legacyStaging, context)

	plan := testConfigSetCapturePlan(appData, &modules.CaptureDef{Files: []modules.CaptureFile{{
		Source: `${instance.root}/Vendor/settings.json`, Dest: "settings.json",
	}}})
	generationStaging := bundleValidationWorkRoot(t, context, "generation-io")
	if err := os.WriteFile(filepath.Join(generationStaging, "configs"), []byte("blocks directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, generationErr := CollectConfigSetWithValidation(plan, generationStaging, context)

	for name, err := range map[string]error{"legacy": legacyErr, "generation": generationErr} {
		var isolation *CaptureIsolationError
		if !errors.As(err, &isolation) {
			t.Fatalf("%s error = %T %v, want CaptureIsolationError", name, err, err)
		}
		if strings.Contains(strings.ToLower(err.Error()), strings.ToLower(context.Root())) {
			t.Fatalf("%s error leaked validation root: %v", name, err)
		}
	}
}

func TestValidationSecretsUseTheSameSandboxBoundary(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.example")
	appData, _ := context.VirtualRoot("APPDATA")
	writeCaptureFile(t, filepath.Join(appData, "Vendor", "settings.json"), []byte("settings"))
	writeCaptureFile(t, filepath.Join(appData, "Vendor", "secret-token.json"), []byte("secret"))
	mod := &modules.Module{
		ID:      "apps.example",
		Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{Source: `%APPDATA%\Vendor`, Dest: "apps/example/Vendor"}}},
		Secrets: &modules.SecretsDef{Files: []string{`%APPDATA%\Vendor\secret-*.json`}},
	}
	staging := bundleValidationWorkRoot(t, context, "secrets")
	_, excluded, err := CollectConfigFilesWithValidation(mod, staging, context)
	if err != nil {
		t.Fatal(err)
	}
	if excluded != 1 {
		t.Fatalf("secrets excluded = %d, want 1", excluded)
	}
	if data, err := os.ReadFile(filepath.Join(staging, "configs", "example", "Vendor", "settings.json")); err != nil || string(data) != "settings" {
		t.Fatalf("settings payload = %q err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(staging, "configs", "example", "Vendor", "secret-token.json")); !os.IsNotExist(err) {
		t.Fatalf("secret was published: %v", err)
	}

	mod.Secrets.Files = []string{`C:\host\secret.json`}
	if _, _, err := CollectConfigFilesWithValidation(mod, bundleValidationWorkRoot(t, context, "unsafe-secrets"), context); err == nil {
		t.Fatal("hardcoded secret pattern bypassed the sandbox boundary")
	}
}

func TestValidationCollectorRejectsReparseEscapeBeforeStaging(t *testing.T) {
	outside := t.TempDir()
	writeCaptureFile(t, filepath.Join(outside, "settings.json"), []byte("outside"))
	context := activeBundleValidationContext(t, "apps.example")
	appData, _ := context.VirtualRoot("APPDATA")
	if err := os.Symlink(outside, filepath.Join(appData, "Linked")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	mod := &modules.Module{ID: "apps.example", Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{
		Source: `%APPDATA%\Linked\settings.json`, Dest: "apps/example/settings.json",
	}}}}
	staging := bundleValidationWorkRoot(t, context, "reparse")
	if _, _, err := CollectConfigFilesWithValidation(mod, staging, context); err == nil {
		t.Fatal("reparse escape reached validation collection")
	}
	if _, err := os.Stat(filepath.Join(staging, "configs")); !os.IsNotExist(err) {
		t.Fatalf("reparse escape reached staging: %v", err)
	}
}

func TestNilValidationCollectorMatchesProductionBytes(t *testing.T) {
	source := filepath.Join(t.TempDir(), "settings.json")
	writeCaptureFile(t, source, []byte("production-bytes\r\n"))
	mod := &modules.Module{ID: "apps.example", Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{
		Source: source, Dest: "apps/example/settings.json",
	}}}}
	left, right := t.TempDir(), t.TempDir()
	leftPaths, leftExcluded, leftErr := CollectConfigFiles(mod, left)
	rightPaths, rightExcluded, rightErr := CollectConfigFilesWithValidation(mod, right, nil)
	if leftErr != nil || rightErr != nil || leftExcluded != rightExcluded || !reflect.DeepEqual(leftPaths, rightPaths) {
		t.Fatalf("nil validation differs: left=%#v/%d/%v right=%#v/%d/%v", leftPaths, leftExcluded, leftErr, rightPaths, rightExcluded, rightErr)
	}
	leftData, _ := os.ReadFile(filepath.Join(left, "configs", "example", "settings.json"))
	rightData, _ := os.ReadFile(filepath.Join(right, "configs", "example", "settings.json"))
	if !reflect.DeepEqual(leftData, rightData) {
		t.Fatalf("nil validation changed production bytes: left=%q right=%q", leftData, rightData)
	}
}

func TestNilValidationGenerationCollectorMatchesProductionBytes(t *testing.T) {
	instanceRoot := t.TempDir()
	writeCaptureFile(t, filepath.Join(instanceRoot, "settings.json"), []byte("production-generation\r\n"))
	plan := testConfigSetCapturePlan(instanceRoot, &modules.CaptureDef{Files: []modules.CaptureFile{{
		Source: `${instance.root}/settings.json`, Dest: "profiles/settings.json",
	}}})
	left, right := t.TempDir(), t.TempDir()
	leftResult, leftErr := CollectConfigSet(plan, left)
	rightResult, rightErr := CollectConfigSetWithValidation(plan, right, nil)
	if leftErr != nil || rightErr != nil || !reflect.DeepEqual(leftResult, rightResult) {
		t.Fatalf("nil validation differs: left=%#v/%v right=%#v/%v", leftResult, leftErr, rightResult, rightErr)
	}
	leftData, leftReadErr := os.ReadFile(filepath.Join(left, filepath.FromSlash(leftResult.PayloadRoot), "profiles", "settings.json"))
	rightData, rightReadErr := os.ReadFile(filepath.Join(right, filepath.FromSlash(rightResult.PayloadRoot), "profiles", "settings.json"))
	if leftReadErr != nil || rightReadErr != nil || !bytes.Equal(leftData, rightData) {
		t.Fatalf("nil validation changed generation bytes: left=%q/%v right=%q/%v", leftData, leftReadErr, rightData, rightReadErr)
	}
}

func TestCreateCaptureBundleWithValidationPublishesSandboxBytesAndSemanticShape(t *testing.T) {
	originalAppData := t.TempDir()
	t.Setenv("APPDATA", originalAppData)
	writeCaptureFile(t, filepath.Join(originalAppData, "Vendor", "settings.json"), []byte("original-host"))
	context := activeBundleValidationContext(t, "apps.example")
	virtualAppData, _ := context.VirtualRoot("APPDATA")
	writeCaptureFile(t, filepath.Join(virtualAppData, "Vendor", "settings.json"), []byte("sandbox-bundle"))
	manifestPath := filepath.Join(context.Root(), "manifests", "input.jsonc")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"version":1,"name":"capture","apps":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(context.Root(), "manifests", "capture.zip")
	mod := &modules.Module{
		ID: "apps.example", DisplayName: "Example",
		Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{Source: `%APPDATA%\Vendor\settings.json`, Dest: "apps/example/settings.json"}}},
		Restore: []modules.RestoreDef{{Type: "copy", Source: "./payload/apps/example/settings.json", Target: `%APPDATA%\Vendor\settings.json`, Backup: true}},
	}
	result, err := CreateCaptureBundle(CaptureBundleRequest{
		ManifestPath: manifestPath, OutputPath: outputPath, EndstateVersion: "test", Modules: []*modules.Module{mod}, ValidationContext: context,
	})
	if err != nil {
		t.Fatalf("CreateCaptureBundle: %v", err)
	}
	if !reflect.DeepEqual(result.ConfigModulesIncluded, []string{"example"}) {
		t.Fatalf("included modules = %#v", result.ConfigModulesIncluded)
	}
	entries := readValidationZip(t, outputPath)
	if string(entries["configs/example/settings.json"]) != "sandbox-bundle" {
		t.Fatalf("captured payload = %q", entries["configs/example/settings.json"])
	}
	for name, data := range entries {
		text := strings.ToLower(string(data))
		if strings.Contains(text, strings.ToLower(context.Root())) || strings.Contains(text, strings.ToLower(context.RegistryNamespace())) {
			t.Fatalf("zip entry %s leaked validation authority", name)
		}
	}
	var published manifest.Manifest
	if err := json.Unmarshal(entries["manifest.jsonc"], &published); err != nil {
		t.Fatal(err)
	}
	if len(published.Restore) != 1 || published.Restore[0].Target != `%APPDATA%\Vendor\settings.json` || published.Restore[0].FromModule != mod.ID {
		t.Fatalf("published restore shape = %#v", published.Restore)
	}
}

func TestCreateCaptureBundleWithValidationPreservesGenerationProvenance(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.example")
	virtualAppData, _ := context.VirtualRoot("APPDATA")
	instanceRoot := filepath.Join(virtualAppData, "Vendor", "27.4")
	writeCaptureFile(t, filepath.Join(instanceRoot, "prefs.json"), []byte("generation-payload"))
	plan := testConfigSetCapturePlan(instanceRoot, &modules.CaptureDef{Files: []modules.CaptureFile{{Source: `${instance.root}/prefs.json`, Dest: "prefs.json"}}})
	plan.Instance.Evidence = modules.InstanceEvidence{Type: "path", Path: instanceRoot}
	plan.Instance.CanonicalLocator = "path:" + filepath.ToSlash(strings.ToLower(instanceRoot))
	manifestPath := filepath.Join(context.Root(), "manifests", "generation-input.jsonc")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"version":1,"name":"capture","apps":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(context.Root(), "manifests", "generation.zip")
	result, err := CreateCaptureBundle(CaptureBundleRequest{
		ManifestPath: manifestPath, OutputPath: outputPath, EndstateVersion: "test", Modules: []*modules.Module{plan.Module},
		GenerationPlans: []ConfigSetCapturePlan{plan}, ValidationContext: context,
	})
	if err != nil {
		t.Fatalf("CreateCaptureBundle: %v", err)
	}
	if len(result.ConfigCaptures) != 1 {
		t.Fatalf("config captures = %#v", result.ConfigCaptures)
	}
	capture := result.ConfigCaptures[0]
	if capture.SourceInstance.ID != plan.Instance.ID || capture.SourceGenerationFingerprint != plan.Generation.Fingerprint || capture.CaptureModule.ContentHash != plan.Module.Revision {
		t.Fatalf("generation provenance changed: %#v", capture)
	}
	entries := readValidationZip(t, outputPath)
	if string(entries[capture.PayloadRoot+"/prefs.json"]) != "generation-payload" {
		t.Fatalf("generation payload missing: entries=%v", reflect.ValueOf(entries).MapKeys())
	}
	for name, data := range entries {
		if strings.Contains(strings.ToLower(string(data)), strings.ToLower(context.Root())) {
			t.Fatalf("zip entry %s leaked sandbox root", name)
		}
	}
}

func TestCreateCaptureBundleWithValidationFailsClosedOnCollectorIsolation(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.example")
	manifestPath := filepath.Join(context.Root(), "manifests", "unsafe-input.jsonc")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"version":1,"name":"capture","apps":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mod := &modules.Module{ID: "apps.example", Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{Source: `C:\host\escape.json`, Dest: "apps/example/settings.json"}}}}
	_, err := CreateCaptureBundle(CaptureBundleRequest{
		ManifestPath: manifestPath, OutputPath: filepath.Join(context.Root(), "manifests", "unsafe.zip"), Modules: []*modules.Module{mod}, ValidationContext: context,
	})
	var isolation *CaptureIsolationError
	if !errors.As(err, &isolation) {
		t.Fatalf("error = %T %v, want CaptureIsolationError", err, err)
	}
}

func TestCreateCaptureBundleWithValidationRedactsPublicationIOFailure(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.example")
	manifestPath := filepath.Join(context.Root(), "manifests", "publication-input.jsonc")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"version":1,"name":"capture","apps":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(context.Root(), "manifests", "existing-directory")
	if err := os.MkdirAll(outputPath, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := CreateCaptureBundle(CaptureBundleRequest{
		ManifestPath: manifestPath, OutputPath: outputPath, ValidationContext: context,
	})
	var isolation *CaptureIsolationError
	if !errors.As(err, &isolation) {
		t.Fatalf("error = %T %v, want CaptureIsolationError", err, err)
	}
	if strings.Contains(strings.ToLower(err.Error()), strings.ToLower(context.Root())) {
		t.Fatalf("publication error leaked validation root: %v", err)
	}
}

func readValidationZip(t *testing.T, path string) map[string][]byte {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	result := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(stream)
		_ = stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		result[file.Name] = data
	}
	return result
}

func activeBundleValidationContext(t *testing.T, moduleID string) *validationmode.Context {
	t.Helper()
	nonce := fmt.Sprintf("bundle-capture-%d", time.Now().UnixNano())
	root := filepath.Join(canonicalBundleTempDir(t), "endstate-validation-"+nonce)
	if err := os.MkdirAll(filepath.Join(root, ".endstate"), 0o700); err != nil {
		t.Fatal(err)
	}
	descriptor := validationmode.Descriptor{
		SchemaVersion: 1, ScenarioID: "bundle-capture", Nonce: nonce, ModuleID: moduleID,
		Inventory: validationmode.Inventory{AppID: "example", Driver: "winget", Ref: "Vendor.Example", DisplayName: "Example", Version: "1.0", Source: "winget", InitialState: "absent"},
	}
	data, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".endstate", "validation-mode.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(validationmode.TestModeEnvironment, "1")
	t.Setenv(validationmode.RootEnvironment, root)
	context, restore, err := validationmode.ActivateFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = restore()
		_ = os.RemoveAll(root)
	})
	return context
}

func canonicalBundleTempDir(t *testing.T) string {
	t.Helper()
	value, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	value, err = filepath.Abs(value)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(value)
}

func bundleValidationWorkRoot(t *testing.T, context *validationmode.Context, name string) string {
	t.Helper()
	root := filepath.Join(context.Root(), "state", name)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}
