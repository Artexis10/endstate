// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func TestBuiltExecutableValidationFileAndRegistryLifecycle(t *testing.T) {
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

	root := cliValidationRootWithInventory(t, validationmode.Inventory{
		AppID: "vendor-notepadplusplus", Driver: "winget", Ref: "Vendor.NotepadPlusPlus",
		DisplayName: "Notepad++", Version: "8.8.2", Source: "winget", InitialState: "present",
	})
	nonce := strings.TrimPrefix(filepath.Base(root), "endstate-validation-")
	originalAppData := t.TempDir()
	originalFile := filepath.Join(originalAppData, "EndstateE2E", "settings.json")
	writeLifecycleFile(t, originalFile, "original-host-sentinel")
	sandboxFile := filepath.Join(root, "sandbox", "appdata", "EndstateE2E", "settings.json")
	writeLifecycleFile(t, sandboxFile, "captured-file-sentinel")

	semanticRegistry := `HKCU\Software\Endstate\ValidationE2E`
	mappedSubkey := `Software\Endstate\Validation\` + nonce + `\Software\Endstate\ValidationE2E`
	registryEnabled, registryReason := seedLifecycleRegistry(mappedSubkey, "captured-registry-sentinel")
	if registryEnabled {
		t.Cleanup(func() { _ = registry.DeleteKey(registry.CURRENT_USER, mappedSubkey) })
	} else {
		if !errors.Is(registryReason, windows.ERROR_ACCESS_DENIED) {
			t.Fatalf("seed validation registry: %v", registryReason)
		}
		t.Logf("registry leg unavailable in managed host: %v", registryReason)
	}

	moduleDir := filepath.Join(root, "modules", "apps", "notepad-plus-plus")
	if err := os.MkdirAll(moduleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	moduleJSON := lifecycleModuleJSON(semanticRegistry, registryEnabled)
	if err := os.WriteFile(filepath.Join(moduleDir, "module.jsonc"), []byte(moduleJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestOut := filepath.Join(root, "manifests", "captured.jsonc")
	if err := os.MkdirAll(filepath.Dir(manifestOut), 0o700); err != nil {
		t.Fatal(err)
	}
	zipPath := strings.TrimSuffix(manifestOut, filepath.Ext(manifestOut)) + ".zip"

	childEnvironment := withoutEnvironment(os.Environ(),
		validationmode.TestModeEnvironment, validationmode.RootEnvironment, "APPDATA", "GOCACHE", "GOTELEMETRY")
	childEnvironment = append(childEnvironment,
		validationmode.TestModeEnvironment+"=1",
		validationmode.RootEnvironment+"="+root,
		"APPDATA="+originalAppData,
		"GOCACHE="+filepath.Join(buildRoot, "child-gocache"),
		"GOTELEMETRY=off",
	)
	run := func(args ...string) map[string]interface{} {
		t.Helper()
		cmd := exec.Command(binaryPath, args...)
		cmd.Dir = moduleRoot
		cmd.Env = childEnvironment
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("%v failed: %v\nstdout=%s\nstderr=%s", args, err, stdout.String(), stderr.String())
		}
		for _, output := range []string{stdout.String(), stderr.String()} {
			for _, forbidden := range []string{root, filepath.ToSlash(root), strings.ReplaceAll(root, `\`, `\\`), nonce} {
				if forbidden != "" && strings.Contains(strings.ToLower(output), strings.ToLower(forbidden)) {
					t.Fatalf("%v output leaked validation authority %q: %s", args, forbidden, output)
				}
			}
		}
		decoded := decodeCLIEnvelope(t, stdout.String())
		if decoded["success"] != true {
			t.Fatalf("%v envelope = %s", args, stdout.String())
		}
		return decoded
	}

	capture := run("capture", "--out", manifestOut, "--only", "vendor-notepadplusplus,apps.notepad-plus-plus", "--json")
	captureData := capture["data"].(map[string]interface{})
	if captureData["outputFormat"] != "zip" || captureData["outputPath"] == "" {
		t.Fatalf("capture data = %#v", captureData)
	}
	manifestBytes, entries := inspectLifecycleBundle(t, zipPath)
	if !bytes.Contains(manifestBytes, []byte(`"fromModule": "apps.notepad-plus-plus"`)) ||
		!bytes.Contains(manifestBytes, []byte(`%APPDATA%\\EndstateE2E\\settings.json`)) {
		t.Fatalf("captured manifest lacks production provenance/semantic target: %s", manifestBytes)
	}
	if !bundleContainsValue(entries, "captured-file-sentinel") {
		t.Fatalf("captured bundle lacks exact file sentinel: %#v", entries)
	}
	if registryEnabled && !bundleContainsValue(entries, "Windows Registry Editor Version 5.00") {
		t.Fatalf("captured bundle lacks semantic registry export: %#v", entries)
	}
	verifyManifest := filepath.Join(root, "manifests", "captured-verify.jsonc")
	if err := os.WriteFile(verifyManifest, manifestBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	writeLifecycleFile(t, sandboxFile, "mutated-file-sentinel")
	if registryEnabled {
		writeLifecycleRegistry(t, mappedSubkey, "mutated-registry-sentinel")
	}
	firstRebuild := run("rebuild", "--from", zipPath, "--confirm", "--json")
	assertLifecycleNestedSuccess(t, firstRebuild)
	assertLifecycleFile(t, sandboxFile, "captured-file-sentinel")
	if registryEnabled {
		assertLifecycleRegistry(t, mappedSubkey, "captured-registry-sentinel")
	}

	run("revert", "--json")
	assertLifecycleFile(t, sandboxFile, "mutated-file-sentinel")
	if registryEnabled {
		assertLifecycleRegistry(t, mappedSubkey, "mutated-registry-sentinel")
	}

	secondRebuild := run("rebuild", "--from", zipPath, "--confirm", "--json")
	assertLifecycleNestedSuccess(t, secondRebuild)
	assertLifecycleFile(t, sandboxFile, "captured-file-sentinel")
	thirdRebuild := run("rebuild", "--from", zipPath, "--confirm", "--json")
	assertLifecycleNestedSuccess(t, thirdRebuild)
	assertLifecycleFile(t, sandboxFile, "captured-file-sentinel")
	if registryEnabled {
		assertLifecycleRegistry(t, mappedSubkey, "captured-registry-sentinel")
	}

	writeLifecycleFile(t, sandboxFile, "failed-verifier-sentinel")
	if err := os.Remove(sandboxFile); err != nil {
		t.Fatal(err)
	}
	failedVerify := run("verify", "--manifest", verifyManifest, "--json")
	verifyData := failedVerify["data"].(map[string]interface{})
	verifySummary := verifyData["summary"].(map[string]interface{})
	if verifySummary["fail"].(float64) < 1 {
		t.Fatalf("failed verifier passed vacuously: %#v", verifyData)
	}

	assertLifecycleFile(t, originalFile, "original-host-sentinel")
}

func lifecycleModuleJSON(semanticRegistry string, includeRegistry bool) string {
	verify := `[{"type":"file-exists","path":"%APPDATA%\\EndstateE2E\\settings.json"}]`
	restore := `[{"type":"copy","source":"./payload/apps/notepad-plus-plus/settings.json","target":"%APPDATA%\\EndstateE2E\\settings.json","backup":true}]`
	capture := `{"files":[{"source":"%APPDATA%\\EndstateE2E\\settings.json","dest":"apps/notepad-plus-plus/settings.json"}],"excludeGlobs":[]}`
	if includeRegistry {
		verify = `[{"type":"file-exists","path":"%APPDATA%\\EndstateE2E\\settings.json"},{"type":"registry-value-equals","path":"` + semanticRegistry + `","valueName":"Theme","valueType":"REG_SZ","data":"captured-registry-sentinel"}]`
		restore = `[{"type":"copy","source":"./payload/apps/notepad-plus-plus/settings.json","target":"%APPDATA%\\EndstateE2E\\settings.json","backup":true},{"type":"registry-import","source":"./payload/apps/notepad-plus-plus/settings.reg","target":"` + semanticRegistry + `","backup":true}]`
		capture = `{"files":[{"source":"%APPDATA%\\EndstateE2E\\settings.json","dest":"apps/notepad-plus-plus/settings.json"}],"registryKeys":[{"key":"` + semanticRegistry + `","dest":"apps/notepad-plus-plus/settings.reg"}],"excludeGlobs":[]}`
	}
	return `{"id":"apps.notepad-plus-plus","displayName":"Notepad++","sensitivity":"low","matches":{"winget":["Vendor.NotepadPlusPlus"]},"verify":` + verify + `,"restore":` + restore + `,"capture":` + capture + `}`
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

func seedLifecycleRegistry(subkey, value string) (bool, error) {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, subkey, registry.ALL_ACCESS)
	if err != nil {
		return false, err
	}
	defer key.Close()
	if err := key.SetStringValue("Theme", value); err != nil {
		return false, err
	}
	return true, nil
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
	key, err := registry.OpenKey(registry.CURRENT_USER, subkey, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Close()
	got, _, err := key.GetStringValue("Theme")
	if err != nil || got != want {
		t.Fatalf("registry Theme = %q err=%v, want %q", got, err, want)
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
