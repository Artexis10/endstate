// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func packageDetectorModule(detectors ...InstanceDetectorDef) *Module {
	return &Module{
		ModuleSchemaVersion: 2,
		ID:                  "apps.example",
		DisplayName:         "Example",
		Matches:             MatchCriteria{Winget: []string{"Vendor.Example"}},
		Config: &ConfigDef{
			InstanceDetectors: detectors,
			Sets: []ConfigSetDef{{
				ID: "preferences",
				Generations: []GenerationDef{{
					ID:      "g1",
					Order:   1,
					Capture: &CaptureDef{Files: []CaptureFile{{Source: "prefs.json", Dest: "prefs.json"}}},
				}},
			}},
		},
	}
}

func TestDiscoverInstances_PackageBackendsPreserveEvidence(t *testing.T) {
	mod := packageDetectorModule(InstanceDetectorDef{ID: "installed", Type: "package"})
	packages := []PackageEvidence{
		{AppID: "windows-app", Backend: "winget", Platform: "windows", Ref: "Vendor.Example", Driver: "winget", RawVersion: " 027.04.0 "},
		{AppID: "brew-app", Backend: "brew", Platform: "darwin", Ref: "cask:example", Driver: "brew", RawVersion: "v4-beta"},
		{AppID: "nix-app", Backend: "nix", Platform: "linux", Ref: "example", Driver: "", RawVersion: ""},
	}

	instances, err := DiscoverInstances(mod, packages, DiscoveryOptions{})
	if err != nil {
		t.Fatalf("DiscoverInstances: %v", err)
	}
	if len(instances) != 3 {
		t.Fatalf("instances = %+v, want 3", instances)
	}
	byBackend := make(map[string]ConfigInstance)
	for _, instance := range instances {
		byBackend[instance.Evidence.Backend] = instance
		if instance.ID == "" || instance.DetectorID != "installed" || instance.ModuleID != mod.ID {
			t.Errorf("incomplete instance identity: %+v", instance)
		}
	}
	winget := byBackend["winget"]
	if winget.Evidence.Ref != "Vendor.Example" || winget.Evidence.Driver != "winget" || winget.Evidence.Platform != "windows" {
		t.Errorf("winget evidence lost: %+v", winget.Evidence)
	}
	if winget.Version.Raw != " 027.04.0 " || winget.Version.Normalized != "27.4" || !winget.Version.Numeric {
		t.Errorf("winget version = %+v", winget.Version)
	}
	if brew := byBackend["brew"]; brew.Version.Raw != "v4-beta" || brew.Version.Numeric {
		t.Errorf("irregular brew version = %+v", brew.Version)
	}
	if nix := byBackend["nix"]; nix.Version.Raw != "" || nix.Version.Numeric {
		t.Errorf("empty nix version = %+v", nix.Version)
	}
}

func TestDiscoverInstances_DeduplicatesAndSortsByDetectorAndLocator(t *testing.T) {
	mod := packageDetectorModule(
		InstanceDetectorDef{ID: "z-installed", Type: "package"},
		InstanceDetectorDef{ID: "a-installed", Type: "package"},
	)
	packages := []PackageEvidence{
		{AppID: "z", Backend: "winget", Ref: "Vendor.Z", RawVersion: "1"},
		{AppID: "a", Backend: "winget", Ref: "Vendor.A", RawVersion: "99"},
		{AppID: "a-duplicate", Backend: "WINGET", Ref: "Vendor.A", Driver: "winget", RawVersion: "99"},
	}

	instances, err := DiscoverInstances(mod, packages, DiscoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 4 {
		t.Fatalf("instances = %d, want 2 locators x 2 detectors", len(instances))
	}
	gotOrder := make([]string, 0, len(instances))
	for _, instance := range instances {
		gotOrder = append(gotOrder, instance.DetectorID+"|"+instance.CanonicalLocator)
	}
	wantOrder := []string{
		"a-installed|package:winget:vendor.a",
		"a-installed|package:winget:vendor.z",
		"z-installed|package:winget:vendor.a",
		"z-installed|package:winget:vendor.z",
	}
	if !reflect.DeepEqual(gotOrder, wantOrder) {
		t.Errorf("order = %#v, want %#v", gotOrder, wantOrder)
	}
	if instances[0].Version.Raw != "99" {
		t.Errorf("duplicate evidence lost usable version: %+v", instances[0].Version)
	}
	reversed := []PackageEvidence{packages[2], packages[1], packages[0]}
	reversedInstances, err := DiscoverInstances(mod, reversed, DiscoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(instances, reversedInstances) {
		t.Errorf("input order changed deterministic evidence:\nforward=%+v\nreverse=%+v", instances, reversedInstances)
	}
}

func TestDiscoverInstances_WingetRefsDeduplicateCaseInsensitively(t *testing.T) {
	mod := packageDetectorModule(InstanceDetectorDef{ID: "installed", Type: "package"})
	packages := []PackageEvidence{
		{AppID: "upper", Backend: "Winget", Platform: "windows", Ref: "Vendor.App", RawVersion: "1"},
		{AppID: "lower", Backend: "WINGET", Platform: "windows", Ref: "vendor.app", Driver: "winget", RawVersion: "1"},
	}

	instances, err := DiscoverInstances(mod, packages, DiscoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 {
		t.Fatalf("case variants produced %d Winget instances: %+v", len(instances), instances)
	}
	if instances[0].CanonicalLocator != "package:winget:vendor.app" {
		t.Errorf("canonical locator = %q", instances[0].CanonicalLocator)
	}
	reversed, err := DiscoverInstances(mod, []PackageEvidence{packages[1], packages[0]}, DiscoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(instances, reversed) {
		t.Errorf("Winget evidence changed with input order:\nforward=%+v\nreverse=%+v", instances, reversed)
	}
}

func TestDiscoverInstances_CaseSensitiveBackendRefsRemainDistinct(t *testing.T) {
	mod := packageDetectorModule(InstanceDetectorDef{ID: "installed", Type: "package"})
	packages := []PackageEvidence{
		{Backend: "brew", Ref: "Vendor/App", RawVersion: "1"},
		{Backend: "BREW", Ref: "vendor/app", RawVersion: "1"},
	}

	instances, err := DiscoverInstances(mod, packages, DiscoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 2 {
		t.Fatalf("case-sensitive Brew refs collapsed: %+v", instances)
	}
}

func TestStableInstanceID_IsDeterministicAndScoped(t *testing.T) {
	first := StableInstanceID("apps.example", "installed", "package:winget:Vendor.App")
	second := StableInstanceID("apps.example", "installed", "package:winget:Vendor.App")
	if first == "" || first != second {
		t.Fatalf("stable ID mismatch: %q != %q", first, second)
	}
	if first == StableInstanceID("apps.other", "installed", "package:winget:Vendor.App") {
		t.Error("module scope did not affect stable ID")
	}
	if first == StableInstanceID("apps.example", "other", "package:winget:Vendor.App") {
		t.Error("detector scope did not affect stable ID")
	}
}

func TestConfigInstanceMarshalOmitsMachineLocalDiscoveryPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "private-root")
	locator := "path:canonical-private-locator"
	instance := ConfigInstance{
		ID:               "instance-1",
		ModuleID:         "apps.example",
		DetectorID:       "profiles",
		Root:             root,
		CanonicalLocator: locator,
		Evidence:         InstanceEvidence{Type: "path", Path: root},
	}

	payload, err := json.Marshal(instance)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(payload, &object); err != nil {
		t.Fatal(err)
	}
	if leakedRoot, exists := object["root"]; exists {
		t.Fatalf("serialized instance leaked machine-local root %q: %s", leakedRoot, payload)
	}
	evidence, ok := object["evidence"].(map[string]any)
	if !ok {
		t.Fatalf("serialized instance evidence missing: %s", payload)
	}
	if leakedPath, exists := evidence["path"]; exists {
		t.Fatalf("serialized evidence leaked machine-local path %q: %s", leakedPath, payload)
	}
	if leakedLocator, exists := object["canonicalLocator"]; exists {
		t.Fatalf("serialized instance leaked canonical locator %q: %s", leakedLocator, payload)
	}
}

func TestDiscoverInstances_PathSideBySideNeverChoosesNewest(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"App 9", "App 10"} {
		if err := osMkdir(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	mod := packageDetectorModule(InstanceDetectorDef{
		ID:             "profiles",
		Type:           "path",
		Glob:           filepath.Join(root, "App *"),
		VersionPattern: `^App (?P<version>[0-9.]+)$`,
	})

	instances, err := DiscoverInstances(mod, nil, DiscoveryOptions{})
	if err != nil {
		t.Fatalf("DiscoverInstances: %v", err)
	}
	if len(instances) != 2 {
		t.Fatalf("instances = %+v, want both side-by-side roots", instances)
	}
	versions := []string{instances[0].Version.Raw, instances[1].Version.Raw}
	if !reflect.DeepEqual(versions, []string{"10", "9"}) {
		t.Errorf("versions/order = %#v; want canonical path order, not numeric-newest selection", versions)
	}
	if instances[0].Root == instances[1].Root || instances[0].ID == instances[1].ID {
		t.Errorf("side-by-side roots collapsed: %+v", instances)
	}
}

func TestDiscoverInstances_PathExtractionMismatchProducesNoInstance(t *testing.T) {
	root := t.TempDir()
	if err := osMkdir(filepath.Join(root, "App current")); err != nil {
		t.Fatal(err)
	}
	mod := packageDetectorModule(InstanceDetectorDef{
		ID:             "profiles",
		Type:           "path",
		Glob:           filepath.Join(root, "App *"),
		VersionPattern: `^App (?P<version>[0-9.]+)$`,
	})
	instances, err := DiscoverInstances(mod, nil, DiscoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 0 {
		t.Fatalf("nonmatching basename produced instances: %+v", instances)
	}
}

func TestDiscoverInstances_PathExpandsEnvironmentAndUsesInjectedGlob(t *testing.T) {
	root := t.TempDir()
	match := filepath.Join(root, "App 7")
	envRoot := "$ENDSTATE_INSTANCE_ROOT"
	if runtime.GOOS == "windows" {
		envRoot = "%ENDSTATE_INSTANCE_ROOT%"
	}
	t.Setenv("ENDSTATE_INSTANCE_ROOT", root)

	var receivedPattern string
	mod := packageDetectorModule(InstanceDetectorDef{
		ID:             "profiles",
		Type:           "path",
		Glob:           filepath.Join(envRoot, "App *"),
		VersionPattern: `^App (?P<version>[0-9]+)$`,
	})
	instances, err := DiscoverInstances(mod, nil, DiscoveryOptions{
		Glob: func(pattern string) ([]string, error) {
			receivedPattern = pattern
			return []string{match, filepath.Clean(match)}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(receivedPattern) != filepath.Clean(filepath.Join(root, "App *")) {
		t.Errorf("glob received %q before engine environment expansion", receivedPattern)
	}
	if len(instances) != 1 || instances[0].Version.Raw != "7" {
		t.Fatalf("canonical dedup/version extraction = %+v", instances)
	}
}

func TestDiscoverInstances_PathRejectsMalformedAndUnsupportedPatterns(t *testing.T) {
	tests := []string{
		filepath.Join(t.TempDir(), "["),
		filepath.Join(t.TempDir(), "**", "App *"),
		filepath.Join(t.TempDir(), "{App,Tool} *"),
	}
	for _, glob := range tests {
		t.Run(glob, func(t *testing.T) {
			mod := packageDetectorModule(InstanceDetectorDef{ID: "profiles", Type: "path", Glob: glob})
			if _, err := DiscoverInstances(mod, nil, DiscoveryOptions{}); err == nil {
				t.Fatalf("DiscoverInstances accepted unsupported glob %q", glob)
			}
		})
	}

	mod := packageDetectorModule(InstanceDetectorDef{
		ID:             "profiles",
		Type:           "path",
		Glob:           filepath.Join(t.TempDir(), "App *"),
		VersionPattern: `^App ([0-9.]+)$`,
	})
	if _, err := DiscoverInstances(mod, nil, DiscoveryOptions{}); err == nil || !strings.Contains(err.Error(), "named") {
		t.Fatalf("missing named version capture error = %v", err)
	}
}

func TestIsGenerationCaptureEligible_PathOnlyDoesNotRequireApps(t *testing.T) {
	mod := packageDetectorModule(InstanceDetectorDef{ID: "profiles", Type: "path", Glob: filepath.Join(t.TempDir(), "App *")})
	if !IsGenerationCaptureEligible(mod) {
		t.Fatal("schema-v2 module with generation capture should be eligible")
	}
	if instances, err := DiscoverInstances(mod, nil, DiscoveryOptions{}); err != nil || len(instances) != 0 {
		t.Fatalf("path-only empty discovery = %+v, %v", instances, err)
	}
	legacy := &Module{ID: "apps.legacy", Capture: &CaptureDef{Files: []CaptureFile{{Source: "a", Dest: "b"}}}}
	if IsGenerationCaptureEligible(legacy) {
		t.Fatal("schema-v1 module must not be generation-capture eligible")
	}
}

func TestExpandInstancePath_AllowedPlaceholdersAndRoles(t *testing.T) {
	root := filepath.Join(t.TempDir(), "App 27")
	instance := ConfigInstance{ID: "instance-1", Root: root, Version: NewVersionEvidence("27.4")}

	host, err := ExpandInstancePath(`${instance.root}/prefs/${instance.id}.json`, instance, HostPath)
	if err != nil {
		t.Fatalf("host expansion: %v", err)
	}
	wantHost := filepath.Join(root, "prefs", "instance-1.json")
	if filepath.Clean(host) != filepath.Clean(wantHost) {
		t.Errorf("host path = %q, want %q", host, wantHost)
	}

	portable, err := ExpandInstancePath(`profiles/${instance.version}/prefs.json`, instance, PortableRelativePath)
	if err != nil {
		t.Fatalf("portable expansion: %v", err)
	}
	if filepath.ToSlash(portable) != "profiles/27.4/prefs.json" {
		t.Errorf("portable path = %q", portable)
	}
}

func TestInstancePlaceholderExpansion_DoesNotRecursivelyExpandReplacementValues(t *testing.T) {
	root := filepath.Join(t.TempDir(), `${instance.id}`)
	instance := ConfigInstance{
		ID:      "resolved-id",
		Root:    root,
		Version: NewVersionEvidence(`${instance.id}`),
	}
	wantPath := filepath.Join(root, "prefs", `${instance.id}.json`)
	wantTemplate := root + `\` + `${instance.id}`

	for iteration := 0; iteration < 100; iteration++ {
		gotPath, err := ExpandInstancePath(
			`${instance.root}/prefs/${instance.version}.json`,
			instance,
			HostPath,
		)
		if err != nil {
			t.Fatalf("path expansion %d: %v", iteration, err)
		}
		if gotPath != wantPath {
			t.Fatalf("path expansion %d = %q, want %q", iteration, gotPath, wantPath)
		}

		gotTemplate, err := ExpandInstanceTemplate(`${instance.root}\${instance.version}`, instance)
		if err != nil {
			t.Fatalf("template expansion %d: %v", iteration, err)
		}
		if gotTemplate != wantTemplate {
			t.Fatalf("template expansion %d = %q, want %q", iteration, gotTemplate, wantTemplate)
		}
	}
}

func TestExpandInstancePath_RejectsMissingAndUnknownPlaceholders(t *testing.T) {
	tests := []struct {
		name     string
		template string
		instance ConfigInstance
	}{
		{"missing root", `${instance.root}/prefs`, ConfigInstance{ID: "id", Version: NewVersionEvidence("1")}},
		{"missing version", `prefs/${instance.version}`, ConfigInstance{ID: "id", Root: t.TempDir()}},
		{"missing id", `prefs/${instance.id}`, ConfigInstance{Root: t.TempDir(), Version: NewVersionEvidence("1")}},
		{"unknown", `${instance.home}/prefs`, ConfigInstance{ID: "id", Root: t.TempDir(), Version: NewVersionEvidence("1")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ExpandInstancePath(tt.template, tt.instance, PortableRelativePath); err == nil {
				t.Fatalf("ExpandInstancePath accepted %q", tt.template)
			}
		})
	}
}

func TestExpandInstancePath_RejectsAbsoluteVolumeUNCAndTraversalCrossPlatform(t *testing.T) {
	instance := ConfigInstance{ID: "id", Root: t.TempDir(), Version: NewVersionEvidence("1")}
	unsafe := []string{
		`/etc/app/prefs.json`,
		`C:\\Users\\me\\prefs.json`,
		`C:/Users/me/prefs.json`,
		`C:relative-volume-path`,
		`\\\\server\\share\\prefs.json`,
		`//server/share/prefs.json`,
		`\\rooted\\prefs.json`,
		`../prefs.json`,
		`safe/../../prefs.json`,
		`safe\\..\\prefs.json`,
		`${instance.root}/prefs.json`,
		`%APPDATA%/prefs.json`,
		`$HOME/prefs.json`,
	}
	for _, path := range unsafe {
		t.Run(path, func(t *testing.T) {
			if _, err := ExpandInstancePath(path, instance, PortableRelativePath); err == nil {
				t.Fatalf("portable role accepted unsafe path %q", path)
			}
		})
	}

	if _, err := ExpandInstancePath(`${instance.root}/../escape`, instance, HostPath); err == nil {
		t.Fatal("host role allowed traversal outside instance root")
	}
	poisoned := instance
	poisoned.Version = NewVersionEvidence("../escape")
	if _, err := ExpandInstancePath(`${instance.root}/${instance.version}`, poisoned, HostPath); err == nil {
		t.Fatal("host role allowed placeholder expansion outside instance root")
	}
}

// osMkdir keeps the test setup terse while still exercising real filesystem
// discovery rather than a mocked glob implementation.
func osMkdir(path string) error {
	return os.MkdirAll(path, 0o755)
}
