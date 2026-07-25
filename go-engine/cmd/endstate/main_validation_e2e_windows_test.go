// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func TestBuiltExecutableValidationTrackedModuleLifecycles(t *testing.T) {
	moduleRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	buildRoot := t.TempDir()
	binaryPath := filepath.Join(buildRoot, "endstate-validation-lifecycle.exe")
	build := exec.Command("go", "build", "-o", binaryPath, "./cmd/endstate")
	build.Dir = moduleRoot
	build.Env = append(withoutEnvironment(os.Environ(), "GOCACHE", "GOTELEMETRY"),
		"GOCACHE="+filepath.Join(buildRoot, "gocache"), "GOTELEMETRY=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("fresh Windows build failed: %v\n%s", err, output)
	}

	t.Run("file lifecycle - tracked Notepad++", func(t *testing.T) {
		runTrackedNotepadLifecycle(t, moduleRoot, buildRoot, binaryPath)
	})
	t.Run("registry lifecycle - tracked 7-Zip", func(t *testing.T) {
		runTracked7ZipRegistryLifecycle(t, moduleRoot, buildRoot, binaryPath)
	})
}

func runTrackedNotepadLifecycle(t *testing.T, moduleRoot, buildRoot, binaryPath string) {
	root := lifecycleValidationRoot(t, "apps.notepad-plus-plus", validationmode.Inventory{
		AppID: "notepad++-notepad++", Driver: "winget", Ref: "Notepad++.Notepad++",
		DisplayName: "Notepad++", Version: "8.8.2", Source: "winget", InitialState: "present",
	})
	mod := installTrackedLifecycleModule(t, moduleRoot, root, "notepad-plus-plus", "apps.notepad-plus-plus", "Notepad++.Notepad++")

	originalAppData := t.TempDir()
	originalFile := filepath.Join(originalAppData, "Notepad++", "config.xml")
	writeLifecycleFile(t, originalFile, "original-host-sentinel")
	appData := filepath.Join(root, "sandbox", "appdata", "Notepad++")
	fixtures := map[string][2]string{
		filepath.Join(appData, "config.xml"):                 {"captured-config-sentinel", "mutated-config-sentinel"},
		filepath.Join(appData, "shortcuts.xml"):              {"captured-shortcuts-sentinel", "mutated-shortcuts-sentinel"},
		filepath.Join(appData, "langs.xml"):                  {"captured-langs-sentinel", "mutated-langs-sentinel"},
		filepath.Join(appData, "stylers.xml"):                {"captured-stylers-sentinel", "mutated-stylers-sentinel"},
		filepath.Join(appData, "contextMenu.xml"):            {"captured-context-menu-sentinel", "mutated-context-menu-sentinel"},
		filepath.Join(appData, "userDefineLangs", "e2e.xml"): {"captured-user-language-sentinel", "mutated-user-language-sentinel"},
	}
	for path, values := range fixtures {
		writeLifecycleFile(t, path, values[0])
	}
	assertTrackedCaptureSourcesPresent(t, root, mod)

	toolRoot := filepath.Join(root, "state", "e2e-tools")
	commandPath := filepath.Join(toolRoot, "notepad++.exe")
	writeLifecycleFile(t, commandPath, "validation command sentinel")
	harness := newLifecycleHarness(t, moduleRoot, buildRoot, binaryPath, root, originalAppData, toolRoot)
	zipPath, verifyManifest := harness.capture("notepad++-notepad++,apps.notepad-plus-plus", mod)
	_, entries := inspectLifecycleBundle(t, zipPath)
	for _, values := range fixtures {
		if !bundleContainsValue(entries, values[0]) {
			t.Fatalf("captured bundle lacks exact tracked-module file sentinel %q", values[0])
		}
	}

	for path, values := range fixtures {
		writeLifecycleFile(t, path, values[1])
	}
	assertLifecycleNestedSuccess(t, harness.run("rebuild", "--from", zipPath, "--confirm", "--json"))
	assertLifecycleFiles(t, fixtures, 0)
	harness.run("revert", "--json")
	assertLifecycleFiles(t, fixtures, 1)
	assertLifecycleNestedSuccess(t, harness.run("rebuild", "--from", zipPath, "--confirm", "--json"))
	assertLifecycleFiles(t, fixtures, 0)
	assertLifecycleNestedSuccess(t, harness.run("rebuild", "--from", zipPath, "--confirm", "--json"))
	assertLifecycleFiles(t, fixtures, 0)

	if err := os.Remove(commandPath); err != nil {
		t.Fatal(err)
	}
	assertLifecycleVerifyFails(t, harness.run("verify", "--manifest", verifyManifest, "--json"))
	assertLifecycleFile(t, originalFile, "original-host-sentinel")
}

func runTracked7ZipRegistryLifecycle(t *testing.T, moduleRoot, buildRoot, binaryPath string) {
	root := lifecycleValidationRoot(t, "apps.7zip", validationmode.Inventory{
		AppID: "7zip-7zip", Driver: "winget", Ref: "7zip.7zip", DisplayName: "7-Zip",
		Version: "24.09", Source: "winget", InitialState: "present",
	})
	mod := installTrackedLifecycleModule(t, moduleRoot, root, "7zip", "apps.7zip", "7zip.7zip")
	nonce := strings.TrimPrefix(filepath.Base(root), "endstate-validation-")
	semanticSubkey := `Software\7-Zip`
	mappedSubkey := `Software\Endstate\Validation\` + nonce + `\` + semanticSubkey

	restoreOriginal, err := seedOriginalRegistrySentinel(semanticSubkey, "EndstateE2EOriginalSentinel", "original-registry-sentinel")
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Skipf("registry lifecycle unavailable: seed original semantic HKCU sentinel: %v", err)
	}
	if err != nil {
		t.Fatalf("seed original semantic HKCU sentinel: %v", err)
	}
	t.Cleanup(func() { restoreOriginal() })
	if _, err := seedLifecycleRegistry(mappedSubkey, "captured-registry-sentinel"); errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Skipf("registry lifecycle unavailable: seed mapped disposable HKCU sentinel: %v", err)
	} else if err != nil {
		t.Fatalf("seed mapped disposable HKCU sentinel: %v", err)
	}
	t.Cleanup(func() {
		if key, err := registry.OpenKey(registry.CURRENT_USER, mappedSubkey, registry.SET_VALUE); err == nil {
			_ = key.DeleteValue("Theme")
			_ = key.Close()
		}
		_ = registry.DeleteKey(registry.CURRENT_USER, mappedSubkey)
	})
	assertNamedLifecycleRegistry(t, semanticSubkey, "EndstateE2EOriginalSentinel", "original-registry-sentinel")

	verifierTarget := filepath.Join(root, "sandbox", "program-files", "7-Zip", "7z.exe")
	writeLifecycleFile(t, verifierTarget, "tracked-7zip-verifier-sentinel")
	originalAppData := t.TempDir()
	harness := newLifecycleHarness(t, moduleRoot, buildRoot, binaryPath, root, originalAppData, "")
	zipPath, verifyManifest := harness.capture("7zip-7zip,apps.7zip", mod)
	_, entries := inspectLifecycleBundle(t, zipPath)
	if !bundleContainsValue(entries, "Windows Registry Editor Version 5.00") ||
		!bundleContainsValue(entries, "captured-registry-sentinel") || bundleContainsValue(entries, nonce) {
		t.Fatalf("registry artifact lacks semantic export or leaks mapped namespace: %#v", entries)
	}

	writeLifecycleRegistry(t, mappedSubkey, "mutated-registry-sentinel")
	assertLifecycleNestedSuccess(t, harness.run("rebuild", "--from", zipPath, "--confirm", "--json"))
	assertLifecycleRegistry(t, mappedSubkey, "captured-registry-sentinel")
	assertNamedLifecycleRegistry(t, semanticSubkey, "EndstateE2EOriginalSentinel", "original-registry-sentinel")
	harness.run("revert", "--json")
	assertLifecycleRegistry(t, mappedSubkey, "mutated-registry-sentinel")
	assertNamedLifecycleRegistry(t, semanticSubkey, "EndstateE2EOriginalSentinel", "original-registry-sentinel")
	assertLifecycleNestedSuccess(t, harness.run("rebuild", "--from", zipPath, "--confirm", "--json"))
	assertLifecycleRegistry(t, mappedSubkey, "captured-registry-sentinel")
	assertLifecycleNestedSuccess(t, harness.run("rebuild", "--from", zipPath, "--confirm", "--json"))
	assertLifecycleRegistry(t, mappedSubkey, "captured-registry-sentinel")
	assertNamedLifecycleRegistry(t, semanticSubkey, "EndstateE2EOriginalSentinel", "original-registry-sentinel")

	if err := os.Remove(verifierTarget); err != nil {
		t.Fatal(err)
	}
	assertLifecycleVerifyFails(t, harness.run("verify", "--manifest", verifyManifest, "--json"))
	assertNamedLifecycleRegistry(t, semanticSubkey, "EndstateE2EOriginalSentinel", "original-registry-sentinel")
}

type lifecycleHarness struct {
	t           *testing.T
	moduleRoot  string
	binaryPath  string
	root        string
	nonce       string
	environment []string
}

func newLifecycleHarness(t *testing.T, moduleRoot, buildRoot, binaryPath, root, originalAppData, toolRoot string) *lifecycleHarness {
	t.Helper()
	pathParts := []string{}
	if toolRoot != "" {
		pathParts = append(pathParts, toolRoot)
	}
	if systemRoot := os.Getenv("SystemRoot"); systemRoot != "" {
		pathParts = append(pathParts, filepath.Join(systemRoot, "System32"), systemRoot)
	}
	environment := withoutEnvironment(os.Environ(), validationmode.TestModeEnvironment, validationmode.RootEnvironment,
		"APPDATA", "PATH", "GOCACHE", "GOTELEMETRY")
	environment = append(environment,
		validationmode.TestModeEnvironment+"=1", validationmode.RootEnvironment+"="+root,
		"APPDATA="+originalAppData, "PATH="+strings.Join(pathParts, string(os.PathListSeparator)),
		"GOCACHE="+filepath.Join(buildRoot, "child-gocache"), "GOTELEMETRY=off")
	return &lifecycleHarness{t: t, moduleRoot: moduleRoot, binaryPath: binaryPath, root: root,
		nonce: strings.TrimPrefix(filepath.Base(root), "endstate-validation-"), environment: environment}
}

func (h *lifecycleHarness) run(args ...string) map[string]interface{} {
	h.t.Helper()
	cmd := exec.Command(h.binaryPath, args...)
	cmd.Dir = h.moduleRoot
	cmd.Env = h.environment
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		h.t.Fatalf("%v failed: %v\nstdout=%s\nstderr=%s", args, err, stdout.String(), stderr.String())
	}
	for _, output := range []string{stdout.String(), stderr.String()} {
		for _, forbidden := range []string{h.root, filepath.ToSlash(h.root), strings.ReplaceAll(h.root, `\`, `\\`), h.nonce} {
			if forbidden != "" && strings.Contains(strings.ToLower(output), strings.ToLower(forbidden)) {
				h.t.Fatalf("%v output leaked validation authority %q: %s", args, forbidden, output)
			}
		}
	}
	decoded := decodeCLIEnvelope(h.t, stdout.String())
	if decoded["success"] != true {
		h.t.Fatalf("%v envelope = %s", args, stdout.String())
	}
	return decoded
}

func (h *lifecycleHarness) capture(only string, mod *modules.Module) (string, string) {
	h.t.Helper()
	manifestOut := filepath.Join(h.root, "manifests", "captured.jsonc")
	if err := os.MkdirAll(filepath.Dir(manifestOut), 0o700); err != nil {
		h.t.Fatal(err)
	}
	zipPath := strings.TrimSuffix(manifestOut, filepath.Ext(manifestOut)) + ".zip"
	capture := h.run("capture", "--out", manifestOut, "--only", only, "--json")
	captureData := capture["data"].(map[string]interface{})
	if captureData["outputFormat"] != "zip" || captureData["outputPath"] == "" {
		h.t.Fatalf("capture data = %#v", captureData)
	}
	manifestBytes, _ := inspectLifecycleBundle(h.t, zipPath)
	verifyManifest := filepath.Join(h.root, "manifests", "captured-verify.jsonc")
	if err := os.WriteFile(verifyManifest, manifestBytes, 0o600); err != nil {
		h.t.Fatal(err)
	}
	assertTrackedModuleProjection(h.t, verifyManifest, mod)
	return zipPath, verifyManifest
}

func lifecycleValidationRoot(t *testing.T, moduleID string, inventory validationmode.Inventory) string {
	t.Helper()
	root, err := os.MkdirTemp(os.TempDir(), "endstate-validation-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	descriptor := validationmode.Descriptor{SchemaVersion: 1, ScenarioID: "built-engine-e2e",
		Nonce: strings.TrimPrefix(filepath.Base(root), "endstate-validation-"), ModuleID: moduleID, Inventory: inventory}
	data, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".endstate"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".endstate", "validation-mode.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func installTrackedLifecycleModule(t *testing.T, moduleRoot, validationRoot, slug, expectedID, expectedRef string) *modules.Module {
	t.Helper()
	source := filepath.Join(filepath.Dir(moduleRoot), "modules", "apps", slug, "module.jsonc")
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	sourceModule, err := modules.ParseModuleJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(validationRoot, "modules", "apps", slug, "module.jsonc")
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	copied, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	sourceDigest, copiedDigest := sha256.Sum256(raw), sha256.Sum256(copied)
	if !bytes.Equal(raw, copied) || sourceDigest != copiedDigest {
		t.Fatalf("tracked module changed in validation root: source=%s copied=%s",
			hex.EncodeToString(sourceDigest[:]), hex.EncodeToString(copiedDigest[:]))
	}
	catalog, err := modules.LoadCatalog(filepath.Join(validationRoot, "modules", "apps"))
	if err != nil {
		t.Fatal(err)
	}
	loaded := catalog[expectedID]
	if loaded == nil || loaded.Revision != sourceModule.Revision || !bytes.Equal(loaded.CanonicalSnapshot(), sourceModule.CanonicalSnapshot()) {
		t.Fatalf("loaded tracked module provenance mismatch: source=%s loaded=%v", sourceModule.Revision, loaded)
	}
	if !containsStringFold(loaded.Matches.Winget, expectedRef) {
		t.Fatalf("tracked module %s matcher %q missing from %q", expectedID, expectedRef, loaded.Matches.Winget)
	}
	return loaded
}

func assertTrackedCaptureSourcesPresent(t *testing.T, root string, mod *modules.Module) {
	t.Helper()
	t.Setenv(validationmode.TestModeEnvironment, "1")
	t.Setenv(validationmode.RootEnvironment, root)
	context, err := validationmode.LoadFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	for _, capture := range mod.Capture.Files {
		resolved, err := context.ResolveHostPath(capture.Source, validationmode.HostPathPolicy{})
		if err != nil {
			t.Fatalf("resolve tracked capture source %q: %v", capture.Source, err)
		}
		if _, err := os.Stat(resolved); err != nil {
			t.Fatalf("tracked capture source %q resolved absent before child: %v", capture.Source, err)
		}
	}
}

func assertTrackedModuleProjection(t *testing.T, manifestPath string, mod *modules.Module) {
	t.Helper()
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var captured manifest.Manifest
	if err := json.Unmarshal(data, &captured); err != nil {
		t.Fatal(err)
	}
	if len(captured.ConfigModules) != 1 || captured.ConfigModules[0] != mod.ID {
		t.Fatalf("artifact module provenance = %q, want %q; apps=%+v manifest=%s", captured.ConfigModules, mod.ID, captured.Apps, data)
	}
	if len(captured.Verify) != len(mod.Verify) || len(captured.Restore) != len(mod.Restore) {
		t.Fatalf("artifact contract differs from tracked module: verify=%d/%d restore=%d/%d",
			len(captured.Verify), len(mod.Verify), len(captured.Restore), len(mod.Restore))
	}
	for _, definition := range mod.Verify {
		found := false
		for _, projected := range captured.Verify {
			if projected.Type == definition.Type && projected.Command == definition.Command && projected.Path == definition.Path {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("artifact verify contract missing tracked definition %+v", definition)
		}
	}
	for _, definition := range mod.Restore {
		found := false
		for _, projected := range captured.Restore {
			if projected.FromModule == mod.ID && projected.Type == definition.Type && projected.Target == definition.Target {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("artifact restore contract missing tracked definition %+v", definition)
		}
	}
}

func containsStringFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}

func withoutEnvironment(values []string, names ...string) []string {
	blocked := make(map[string]struct{}, len(names))
	for _, name := range names {
		blocked[strings.ToLower(name)] = struct{}{}
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		name := strings.SplitN(value, "=", 2)[0]
		if _, exists := blocked[strings.ToLower(name)]; !exists {
			result = append(result, value)
		}
	}
	return result
}

func inspectLifecycleBundle(t *testing.T, path string) ([]byte, map[string]string) {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	entries := make(map[string]string, len(reader.File))
	var manifestBytes []byte
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
		entries[file.Name] = string(data)
		if file.Name == "manifest.jsonc" {
			manifestBytes = data
		}
	}
	if len(manifestBytes) == 0 {
		t.Fatal("bundle has no manifest.jsonc")
	}
	return manifestBytes, entries
}

func bundleContainsValue(entries map[string]string, value string) bool {
	for _, data := range entries {
		if strings.Contains(data, value) {
			return true
		}
	}
	return false
}

func writeLifecycleFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertLifecycleFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != want {
		t.Fatalf("file %s = %q err=%v, want %q", filepath.Base(path), data, err, want)
	}
}

func assertLifecycleFiles(t *testing.T, fixtures map[string][2]string, index int) {
	t.Helper()
	for path, values := range fixtures {
		assertLifecycleFile(t, path, values[index])
	}
}

func seedLifecycleRegistry(subkey, value string) (bool, error) {
	key, existing, err := registry.CreateKey(registry.CURRENT_USER, subkey, registry.ALL_ACCESS)
	if err != nil {
		return false, err
	}
	defer key.Close()
	if err := key.SetStringValue("Theme", value); err != nil {
		return false, err
	}
	return existing, nil
}

func seedOriginalRegistrySentinel(subkey, name, value string) (func(), error) {
	key, existingKey, err := registry.CreateKey(registry.CURRENT_USER, subkey, registry.ALL_ACCESS)
	if err != nil {
		return nil, err
	}
	previous, _, previousErr := key.GetStringValue(name)
	previousExists := previousErr == nil
	if previousErr != nil && !errors.Is(previousErr, registry.ErrNotExist) {
		_ = key.Close()
		return nil, previousErr
	}
	if err := key.SetStringValue(name, value); err != nil {
		_ = key.Close()
		if !existingKey {
			_ = registry.DeleteKey(registry.CURRENT_USER, subkey)
		}
		return nil, err
	}
	_ = key.Close()
	return func() {
		key, _, err := registry.CreateKey(registry.CURRENT_USER, subkey, registry.ALL_ACCESS)
		if err != nil {
			return
		}
		if previousExists {
			_ = key.SetStringValue(name, previous)
		} else {
			_ = key.DeleteValue(name)
		}
		_ = key.Close()
		if !existingKey {
			_ = registry.DeleteKey(registry.CURRENT_USER, subkey)
		}
	}, nil
}

func writeLifecycleRegistry(t *testing.T, subkey, value string) {
	t.Helper()
	key, err := registry.OpenKey(registry.CURRENT_USER, subkey, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Close()
	if err := key.SetStringValue("Theme", value); err != nil {
		t.Fatal(err)
	}
}

func assertLifecycleRegistry(t *testing.T, subkey, want string) {
	t.Helper()
	assertNamedLifecycleRegistry(t, subkey, "Theme", want)
}

func assertNamedLifecycleRegistry(t *testing.T, subkey, name, want string) {
	t.Helper()
	key, err := registry.OpenKey(registry.CURRENT_USER, subkey, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Close()
	got, _, err := key.GetStringValue(name)
	if err != nil || got != want {
		t.Fatalf("registry %s = %q err=%v, want %q", name, got, err, want)
	}
}

func assertLifecycleNestedSuccess(t *testing.T, envelopeValue map[string]interface{}) {
	t.Helper()
	data := envelopeValue["data"].(map[string]interface{})
	apply := data["apply"].(map[string]interface{})
	applySummary := apply["summary"].(map[string]interface{})
	if applySummary["failed"].(float64) != 0 {
		t.Fatalf("nested apply failed: %#v", apply)
	}
	verify := data["verify"].(map[string]interface{})
	verifySummary := verify["summary"].(map[string]interface{})
	if verifySummary["fail"].(float64) != 0 || verifySummary["pass"].(float64) < 2 {
		t.Fatalf("nested verify failed or vacuous: %#v", verify)
	}
}

func assertLifecycleVerifyFails(t *testing.T, envelopeValue map[string]interface{}) {
	t.Helper()
	data := envelopeValue["data"].(map[string]interface{})
	summary := data["summary"].(map[string]interface{})
	if summary["fail"].(float64) < 1 {
		t.Fatalf("failed verifier passed vacuously: %#v", data)
	}
}
