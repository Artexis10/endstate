// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestCaptureIDIsStableScopedOpaqueAndPortable(t *testing.T) {
	got := CaptureID("apps.example", "preferences", "instance-a")
	if got != CaptureID("apps.example", "preferences", "instance-a") {
		t.Fatal("CaptureID changed for the same identity tuple")
	}
	if !regexp.MustCompile(`^capture-[0-9a-f]{64}$`).MatchString(got) {
		t.Fatalf("CaptureID = %q, want safe opaque capture-<sha256>", got)
	}
	for name, changed := range map[string]string{
		"module":        CaptureID("apps.other", "preferences", "instance-a"),
		"set":           CaptureID("apps.example", "presets", "instance-a"),
		"instance":      CaptureID("apps.example", "preferences", "instance-b"),
		"tuple framing": CaptureID("apps.exampl", "epreferences", "instance-a"),
	} {
		if changed == got {
			t.Errorf("%s change did not scope capture ID", name)
		}
	}
	if CaptureID("a", "bc", "d") == CaptureID("ab", "c", "d") {
		t.Fatal("CaptureID tuple framing is ambiguous")
	}
}

func TestCollectConfigSetPreservesNestedHierarchyAndSameBasenames(t *testing.T) {
	instanceRoot := t.TempDir()
	writeCaptureFile(t, filepath.Join(instanceRoot, "source", "one", "settings.json"), []byte("one\r\n"))
	writeCaptureFile(t, filepath.Join(instanceRoot, "source", "two", "settings.json"), []byte("two\n"))
	writeCaptureFile(t, filepath.Join(instanceRoot, "explicit.json"), []byte("explicit"))

	plan := testConfigSetCapturePlan(instanceRoot, &modules.CaptureDef{Files: []modules.CaptureFile{
		{Source: `${instance.root}/source`, Dest: "profiles/main"},
		{Source: `${instance.root}/explicit.json`, Dest: "explicit/deep/settings.json"},
	}})
	staging := t.TempDir()
	result, err := CollectConfigSet(plan, staging)
	if err != nil {
		t.Fatalf("CollectConfigSet: %v", err)
	}
	// CaptureID keeps the full opaque identity; PayloadRoot points at the
	// readable directory (sanitized module id + short hash suffix).
	wantCaptureID := CaptureID(plan.Module.ID, plan.Set.ID, plan.Instance.ID)
	wantRoot := "configs/" + readableConfigDirName(plan.Module.ID, wantCaptureID)
	if result.CaptureID != wantCaptureID || result.PayloadRoot != wantRoot {
		t.Fatalf("collection identity = %+v, want capture id %q payload root %q", result, wantCaptureID, wantRoot)
	}
	wantFiles := []string{
		"explicit/deep/settings.json",
		"profiles/main/one/settings.json",
		"profiles/main/two/settings.json",
	}
	if strings.Join(result.Files, "|") != strings.Join(wantFiles, "|") || result.FilesCollected != len(wantFiles) {
		t.Fatalf("collected files = %+v count=%d, want %+v", result.Files, result.FilesCollected, wantFiles)
	}
	for _, relative := range wantFiles {
		if _, err := os.Stat(filepath.Join(staging, filepath.FromSlash(wantRoot), filepath.FromSlash(relative))); err != nil {
			t.Errorf("missing staged hierarchy %q: %v", relative, err)
		}
	}
	if got, err := os.ReadFile(filepath.Join(instanceRoot, "source", "one", "settings.json")); err != nil || string(got) != "one\r\n" {
		t.Fatalf("source bytes changed: %q err=%v", got, err)
	}
}

func TestCollectConfigSetOptionalAndRequiredMissing(t *testing.T) {
	root := t.TempDir()
	optional := testConfigSetCapturePlan(root, &modules.CaptureDef{Files: []modules.CaptureFile{{
		Source: `${instance.root}/missing.json`, Dest: "missing.json", Optional: true,
	}}})
	result, err := CollectConfigSet(optional, t.TempDir())
	if err != nil || result.FilesCollected != 0 {
		t.Fatalf("optional missing = result %+v err %v", result, err)
	}

	required := testConfigSetCapturePlan(root, &modules.CaptureDef{Files: []modules.CaptureFile{{
		Source: `${instance.root}/missing.json`, Dest: "missing.json",
	}}})
	if _, err := CollectConfigSet(required, t.TempDir()); err == nil || !strings.Contains(err.Error(), "missing required") || ConfigCaptureDiagnosticCode(err) != ConfigCaptureMissingRequired {
		t.Fatalf("required missing error = %v code=%q", err, ConfigCaptureDiagnosticCode(err))
	}
}

func TestCollectConfigSetRejectsMismatchedProvenanceBeforeStaging(t *testing.T) {
	root := t.TempDir()
	writeCaptureFile(t, filepath.Join(root, "prefs.json"), []byte("value"))
	newPlan := func() ConfigSetCapturePlan {
		return testConfigSetCapturePlan(root, &modules.CaptureDef{Files: []modules.CaptureFile{{
			Source: `${instance.root}/prefs.json`, Dest: "prefs.json",
		}}})
	}
	tests := []struct {
		name   string
		mutate func(*ConfigSetCapturePlan)
	}{
		{"schema v1 module", func(plan *ConfigSetCapturePlan) { plan.Module.ModuleSchemaVersion = 1 }},
		{"instance from another module", func(plan *ConfigSetCapturePlan) { plan.Instance.ModuleID = "apps.other" }},
		{"set absent from module", func(plan *ConfigSetCapturePlan) { plan.Set = &modules.ConfigSetDef{ID: "presets"} }},
		{"detached set pointer", func(plan *ConfigSetCapturePlan) {
			detached := *plan.Set
			plan.Set = &detached
		}},
		{"generation absent from set", func(plan *ConfigSetCapturePlan) {
			plan.Generation = &modules.GenerationDef{ID: "g2", Fingerprint: strings.Repeat("a", 64), Capture: plan.Generation.Capture}
		}},
		{"detached generation pointer", func(plan *ConfigSetCapturePlan) {
			detached := *plan.Generation
			plan.Generation = &detached
		}},
		{"generation fingerprint changed", func(plan *ConfigSetCapturePlan) { plan.Generation.Fingerprint = strings.Repeat("f", 64) }},
		{"generation definition changed after pin", func(plan *ConfigSetCapturePlan) {
			plan.Generation.Capture = &modules.CaptureDef{Files: []modules.CaptureFile{{Source: `${instance.root}/prefs.json`, Dest: "other.json"}}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := newPlan()
			tt.mutate(&plan)
			staging := t.TempDir()
			_, err := CollectConfigSet(plan, staging)
			if ConfigCaptureDiagnosticCode(err) != ConfigCaptureInvalidPlan {
				t.Fatalf("provenance mismatch = %T %v code=%q", err, err, ConfigCaptureDiagnosticCode(err))
			}
			entries, readErr := os.ReadDir(staging)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("mismatched provenance staged files: %+v", entries)
			}
		})
	}
}

func TestCollectConfigSetAcceptsPinnedFingerprintWithExplicitDefaultFields(t *testing.T) {
	root := t.TempDir()
	writeCaptureFile(t, filepath.Join(root, "prefs.json"), []byte("value"))
	plan := testGenerationCapturePlan(t, "apps.explicit-default", "instance-a", root, false, false)
	if _, err := CollectConfigSet(plan, t.TempDir()); err != nil {
		t.Fatalf("valid parsed generation with explicit optional:false rejected: %v", err)
	}
}

func TestCollectConfigSetRemovesPartialPayloadOnCopyFailure(t *testing.T) {
	root := t.TempDir()
	writeCaptureFile(t, filepath.Join(root, "a.json"), []byte("first"))
	writeCaptureFile(t, filepath.Join(root, "b.json"), []byte("second"))
	plan := testConfigSetCapturePlan(root, &modules.CaptureDef{Files: []modules.CaptureFile{
		{Source: `${instance.root}/a.json`, Dest: "a.json"},
		{Source: `${instance.root}/b.json`, Dest: strings.Repeat("z", 300) + ".json"},
	}})
	staging := t.TempDir()
	_, err := CollectConfigSet(plan, staging)
	if err == nil || ConfigCaptureDiagnosticCode(err) != ConfigCaptureIO {
		t.Fatalf("copy failure = %v code=%q", err, ConfigCaptureDiagnosticCode(err))
	}
	payloadRoot := filepath.Join(staging, "configs", readableConfigDirName(plan.Module.ID, CaptureID(plan.Module.ID, plan.Set.ID, plan.Instance.ID)))
	if _, statErr := os.Lstat(payloadRoot); !os.IsNotExist(statErr) {
		t.Fatalf("partial payload root survived collection error: %v", statErr)
	}
}

func TestCollectConfigSetRejectsDestinationCollisionsBeforeCopy(t *testing.T) {
	root := t.TempDir()
	writeCaptureFile(t, filepath.Join(root, "a.json"), []byte("a"))
	writeCaptureFile(t, filepath.Join(root, "b.json"), []byte("b"))
	writeCaptureFile(t, filepath.Join(root, "tree", "nested", "a.json"), []byte("tree"))

	tests := []struct {
		name  string
		files []modules.CaptureFile
	}{
		{"explicit duplicate", []modules.CaptureFile{
			{Source: `${instance.root}/a.json`, Dest: "same.json"},
			{Source: `${instance.root}/b.json`, Dest: "same.json"},
		}},
		{"case folded duplicate", []modules.CaptureFile{
			{Source: `${instance.root}/a.json`, Dest: "Prefs/Settings.JSON"},
			{Source: `${instance.root}/b.json`, Dest: `prefs\settings.json`},
		}},
		{"directory actual file collision", []modules.CaptureFile{
			{Source: `${instance.root}/tree`, Dest: "payload"},
			{Source: `${instance.root}/b.json`, Dest: "payload/nested/a.json"},
		}},
		{"file parent collision", []modules.CaptureFile{
			{Source: `${instance.root}/a.json`, Dest: "payload"},
			{Source: `${instance.root}/b.json`, Dest: "payload/nested.json"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			staging := t.TempDir()
			plan := testConfigSetCapturePlan(root, &modules.CaptureDef{Files: tt.files})
			if _, err := CollectConfigSet(plan, staging); err == nil || !strings.Contains(strings.ToLower(err.Error()), "destination") {
				t.Fatalf("collision error = %v", err)
			}
			entries, err := os.ReadDir(staging)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("collision copied before preflight: %+v", entries)
			}
		})
	}
}

func TestCollectConfigSetRejectsUnsafeAndUnknownPaths(t *testing.T) {
	root := t.TempDir()
	writeCaptureFile(t, filepath.Join(root, "a.json"), []byte("a"))
	tests := []struct {
		name   string
		source string
		dest   string
	}{
		{"destination traversal", `${instance.root}/a.json`, "../escape.json"},
		{"destination absolute unix", `${instance.root}/a.json`, "/escape.json"},
		{"destination absolute windows", `${instance.root}/a.json`, `C:\escape.json`},
		{"unknown destination placeholder", `${instance.root}/a.json`, `${instance.home}/a.json`},
		{"source traversal", `${instance.root}/../a.json`, "a.json"},
		{"unknown source placeholder", `${instance.home}/a.json`, "a.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := testConfigSetCapturePlan(root, &modules.CaptureDef{Files: []modules.CaptureFile{{Source: tt.source, Dest: tt.dest}}})
			if _, err := CollectConfigSet(plan, t.TempDir()); err == nil {
				t.Fatal("CollectConfigSet accepted unsafe path")
			}
		})
	}
}

func TestCollectConfigSetRejectsSourceAndStagingLinks(t *testing.T) {
	t.Run("source link", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		writeCaptureFile(t, filepath.Join(outside, "secret.json"), []byte("outside"))
		link := filepath.Join(root, "linked")
		requireCaptureSymlink(t, outside, link)
		plan := testConfigSetCapturePlan(root, &modules.CaptureDef{Files: []modules.CaptureFile{{
			Source: `${instance.root}/linked/secret.json`, Dest: "secret.json",
		}}})
		if _, err := CollectConfigSet(plan, t.TempDir()); err == nil || !strings.Contains(strings.ToLower(err.Error()), "link") {
			t.Fatalf("source link error = %v", err)
		}
	})

	t.Run("staging link", func(t *testing.T) {
		root := t.TempDir()
		writeCaptureFile(t, filepath.Join(root, "a.json"), []byte("a"))
		staging := t.TempDir()
		outside := t.TempDir()
		requireCaptureSymlink(t, outside, filepath.Join(staging, "configs"))
		plan := testConfigSetCapturePlan(root, &modules.CaptureDef{Files: []modules.CaptureFile{{
			Source: `${instance.root}/a.json`, Dest: "a.json",
		}}})
		if _, err := CollectConfigSet(plan, staging); err == nil || !strings.Contains(strings.ToLower(err.Error()), "link") {
			t.Fatalf("staging link error = %v", err)
		}
		entries, err := os.ReadDir(outside)
		if err != nil || len(entries) != 0 {
			t.Fatalf("staging link escaped: entries=%v err=%v", entries, err)
		}
	})
}

func TestCollectConfigSetRejectsSourceSwappedToLinkAfterPreflight(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "prefs.json")
	outside := filepath.Join(t.TempDir(), "secret.json")
	writeCaptureFile(t, source, []byte("approved"))
	writeCaptureFile(t, outside, []byte("secret"))
	probe := filepath.Join(root, "probe-link")
	requireCaptureSymlink(t, outside, probe)
	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}

	originalOpen := openConfigCaptureSource
	called := false
	openConfigCaptureSource = func(path string) (*os.File, error) {
		called = true
		if err := os.Remove(path); err != nil {
			return nil, err
		}
		if err := os.Symlink(outside, path); err != nil {
			return nil, err
		}
		return os.Open(path)
	}
	t.Cleanup(func() { openConfigCaptureSource = originalOpen })

	plan := testConfigSetCapturePlan(root, &modules.CaptureDef{Files: []modules.CaptureFile{{
		Source: `${instance.root}/prefs.json`, Dest: "prefs.json",
	}}})
	staging := t.TempDir()
	_, err := CollectConfigSet(plan, staging)
	if !called {
		t.Fatal("capture source open seam was not called")
	}
	if ConfigCaptureDiagnosticCode(err) != ConfigCaptureLinkUnsupported {
		t.Fatalf("swapped source error = %v code=%q", err, ConfigCaptureDiagnosticCode(err))
	}
	payloadRoot := filepath.Join(staging, "configs", readableConfigDirName(plan.Module.ID, CaptureID(plan.Module.ID, plan.Set.ID, plan.Instance.ID)))
	if _, statErr := os.Lstat(payloadRoot); !os.IsNotExist(statErr) {
		t.Fatalf("swapped source published payload: %v", statErr)
	}
}

func TestCollectConfigSetRejectsSourceSwappedToDifferentRegularFileAfterPreflight(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "prefs.json")
	replacement := filepath.Join(root, "replacement.json")
	writeCaptureFile(t, source, []byte("approved"))
	writeCaptureFile(t, replacement, []byte("secret"))

	originalOpen := openConfigCaptureSource
	called := false
	openConfigCaptureSource = func(path string) (*os.File, error) {
		called = true
		if err := os.Remove(path); err != nil {
			return nil, err
		}
		if err := os.Rename(replacement, path); err != nil {
			return nil, err
		}
		return os.Open(path)
	}
	t.Cleanup(func() { openConfigCaptureSource = originalOpen })

	plan := testConfigSetCapturePlan(root, &modules.CaptureDef{Files: []modules.CaptureFile{{
		Source: `${instance.root}/prefs.json`, Dest: "prefs.json",
	}}})
	staging := t.TempDir()
	_, err := CollectConfigSet(plan, staging)
	if !called {
		t.Fatal("capture source open seam was not called")
	}
	if ConfigCaptureDiagnosticCode(err) != ConfigCaptureUnsafePath {
		t.Fatalf("swapped source error = %v code=%q", err, ConfigCaptureDiagnosticCode(err))
	}
	payloadRoot := filepath.Join(staging, "configs", readableConfigDirName(plan.Module.ID, CaptureID(plan.Module.ID, plan.Set.ID, plan.Instance.ID)))
	if _, statErr := os.Lstat(payloadRoot); !os.IsNotExist(statErr) {
		t.Fatalf("swapped source published payload: %v", statErr)
	}
}

func TestCollectConfigSetRetainsSecretExcludeAndBloatSafety(t *testing.T) {
	root := t.TempDir()
	writeCaptureFile(t, filepath.Join(root, "keep", "prefs.json"), []byte("keep"))
	secretPath := filepath.Join(root, "keep", "token.json")
	writeCaptureFile(t, secretPath, []byte("secret"))
	writeCaptureFile(t, filepath.Join(root, "keep", "debug.log"), []byte("log"))
	writeCaptureFile(t, filepath.Join(root, "keep", "Cache", "cache.bin"), []byte("cache"))
	installer := filepath.Join(root, "keep", "update.exe")
	writeCaptureFile(t, installer, nil)
	if err := os.Truncate(installer, captureBloatBinaryMaxBytes+1); err != nil {
		t.Fatal(err)
	}
	plan := testConfigSetCapturePlanWithSecrets(root, &modules.CaptureDef{
		Files:        []modules.CaptureFile{{Source: `${instance.root}/keep`, Dest: "keep"}},
		ExcludeGlobs: []string{"*.log"},
	}, &modules.SecretsDef{Files: []string{secretPath}})
	result, err := CollectConfigSet(plan, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if result.SecretsExcluded != 1 || result.FilesCollected != 1 || len(result.Files) != 1 || result.Files[0] != "keep/prefs.json" {
		t.Fatalf("safety filtering result = %+v", result)
	}
}

func TestCollectConfigSetRegistryCaptureIsExplicitlyUnsupported(t *testing.T) {
	root := t.TempDir()
	plan := testConfigSetCapturePlan(root, &modules.CaptureDef{RegistryKeys: []modules.CaptureRegistryKey{{
		Key: "HKCU\\Software\\Vendor", Dest: "settings.reg",
	}}})
	_, err := CollectConfigSet(plan, t.TempDir())
	if err == nil {
		t.Fatal("generation registry capture was silently ignored")
	}
	var unsupported *UnsupportedConfigCaptureError
	if !errors.As(err, &unsupported) || unsupported.Code != ConfigCaptureRegistryUnsupported {
		t.Fatalf("registry error = %T %v", err, err)
	}
}

func testConfigSetCapturePlan(root string, capture *modules.CaptureDef) ConfigSetCapturePlan {
	return testConfigSetCapturePlanWithSecrets(root, capture, nil)
}

func testConfigSetCapturePlanWithSecrets(root string, capture *modules.CaptureDef, secrets *modules.SecretsDef) ConfigSetCapturePlan {
	moduleValue := &modules.Module{
		ModuleSchemaVersion: 2,
		ID:                  "apps.example",
		DisplayName:         "Example",
		Secrets:             secrets,
		Config: &modules.ConfigDef{Sets: []modules.ConfigSetDef{{
			ID:          "preferences",
			DisplayName: "Preferences",
			Generations: []modules.GenerationDef{{ID: "g1", Order: 1, Capture: capture}},
		}}},
	}
	data, err := json.Marshal(moduleValue)
	if err != nil {
		panic(err)
	}
	mod, err := modules.ParseModuleJSON(data)
	if err != nil {
		panic(err)
	}
	set := &mod.Config.Sets[0]
	generation := &set.Generations[0]
	return ConfigSetCapturePlan{
		Module:     mod,
		Set:        set,
		Generation: generation,
		Instance:   modules.ConfigInstance{ID: "instance-a", ModuleID: mod.ID, DetectorID: "path", Root: root, Version: modules.NewVersionEvidence("27.4")},
	}
}

func writeCaptureFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func requireCaptureSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable on this host: %v", err)
	}
}
