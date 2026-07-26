// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package catalogplan

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestResolve_ValidCommentedBundlePreservesDeclaredMembershipOrder(t *testing.T) {
	root := testCatalogRoot(t, "foo", "bar")
	bundle := writeBundle(t, root, "work.jsonc", `// catalog bundle
{
  "version": 1,
  "id": "work",
  "name": "Work tools",
  "modules": ["bar", "foo"]
}`)

	got, err := Resolve(root, bundle, time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got.Bundle.Path != "bundles/work.jsonc" || got.Bundle.ID != "work" || got.Bundle.Name != "Work tools" || got.Bundle.Version != 1 {
		t.Fatalf("bundle = %+v", got.Bundle)
	}
	if got.ActionCount != 2 || len(got.Actions) != 2 {
		t.Fatalf("action count = %d, actions = %+v", got.ActionCount, got.Actions)
	}
	if want := []string{"apps.bar", "apps.foo"}; !reflect.DeepEqual(actionModuleIDs(got.Actions), want) {
		t.Fatalf("action module IDs = %v, want %v", actionModuleIDs(got.Actions), want)
	}
	for _, action := range got.Actions {
		if action.Status != "resolved" || action.Skipped || action.ModuleRevision == "" || action.ValidationHash == "" || action.ValidationScenarioCount != 1 {
			t.Fatalf("action = %+v", action)
		}
		if action.BundleID != "work" || action.BundleHash != got.Bundle.Hash {
			t.Fatalf("action is not bound to bundle: %+v", action)
		}
	}

	again, err := Resolve(root, bundle, time.Date(2026, 7, 26, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, again) {
		t.Fatalf("repeat projection changed:\n got: %+v\nwant: %+v", again, got)
	}
}

func TestResolve_RejectsNonCanonicalBundleAndMembershipInputs(t *testing.T) {
	root := testCatalogRoot(t, "foo")
	for _, tc := range []struct {
		name    string
		filename string
		body    string
	}{
		{"unknown field", "work.jsonc", `{"version":1,"id":"work","name":"Work","modules":["foo"],"extra":true}`},
		{"duplicate field", "work.jsonc", `{"version":1,"id":"work","name":"Work","name":"Again","modules":["foo"]}`},
		{"trailing value", "work.jsonc", `{"version":1,"id":"work","name":"Work","modules":["foo"]} {}`},
		{"wrong version", "work.jsonc", `{"version":2,"id":"work","name":"Work","modules":["foo"]}`},
		{"blank id", "work.jsonc", `{"version":1,"id":" ","name":"Work","modules":["foo"]}`},
		{"noncanonical id", "work.jsonc", `{"version":1,"id":"Work","name":"Work","modules":["foo"]}`},
		{"blank name", "work.jsonc", `{"version":1,"id":"work","name":" ","modules":["foo"]}`},
		{"mismatched id", "work.jsonc", `{"version":1,"id":"other","name":"Work","modules":["foo"]}`},
		{"empty modules", "work.jsonc", `{"version":1,"id":"work","name":"Work","modules":[]}`},
		{"uppercase membership", "work.jsonc", `{"version":1,"id":"work","name":"Work","modules":["Foo"]}`},
		{"padded membership", "work.jsonc", `{"version":1,"id":"work","name":"Work","modules":[" foo "]}`},
		{"qualified membership", "work.jsonc", `{"version":1,"id":"work","name":"Work","modules":["apps.foo"]}`},
		{"traversal membership", "work.jsonc", `{"version":1,"id":"work","name":"Work","modules":["../foo"]}`},
		{"nested membership", "work.jsonc", `{"version":1,"id":"work","name":"Work","modules":["nested/foo"]}`},
		{"missing membership", "work.jsonc", `{"version":1,"id":"work","name":"Work","modules":["missing"]}`},
		{"duplicate membership", "work.jsonc", `{"version":1,"id":"work","name":"Work","modules":["foo","foo"]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bundle := writeBundle(t, root, tc.filename, tc.body)
			if _, err := Resolve(root, bundle, time.Now().UTC()); err == nil {
				t.Fatal("Resolve unexpectedly succeeded")
			}
		})
	}
}

func TestResolve_RejectsPathsOutsideImmediateBundlesDirectory(t *testing.T) {
	root := testCatalogRoot(t, "foo")
	valid := `{"version":1,"id":"work","name":"Work","modules":["foo"]}`
	for _, tc := range []struct {
		name string
		path string
	}{
		{"outside root", filepath.Join(root, "other.jsonc")},
		{"nested", filepath.Join(root, "bundles", "nested", "work.jsonc")},
		{"traversal", filepath.Join(root, "bundles", "nested") + string(filepath.Separator) + ".." + string(filepath.Separator) + "work.jsonc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.MkdirAll(filepath.Dir(tc.path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(tc.path, []byte(valid), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Resolve(root, tc.path, time.Now().UTC()); err == nil {
				t.Fatal("Resolve unexpectedly succeeded")
			}
		})
	}
}

func TestResolve_RejectsInvalidOrStaleCatalogSidecars(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, root string)
	}{
		{"missing sidecar", func(t *testing.T, root string) { if err := os.Remove(filepath.Join(root, "modules", "apps", "foo", "validation.jsonc")); err != nil { t.Fatal(err) } }},
		{"invalid sidecar", func(t *testing.T, root string) { if err := os.WriteFile(filepath.Join(root, "modules", "apps", "foo", "validation.jsonc"), []byte(`{}`), 0o600); err != nil { t.Fatal(err) } }},
		{"stale sidecar", func(t *testing.T, root string) { path := filepath.Join(root, "modules", "apps", "foo", "validation.jsonc"); data, err := os.ReadFile(path); if err != nil { t.Fatal(err) }; moduleData, err := os.ReadFile(filepath.Join(root, "modules", "apps", "foo", "module.jsonc")); if err != nil { t.Fatal(err) }; revision, err := modules.ComputeModuleRevision(moduleData); if err != nil { t.Fatal(err) }; data = []byte(strings.Replace(string(data), revision, strings.Repeat("b", 64), 1)); if err := os.WriteFile(path, data, 0o600); err != nil { t.Fatal(err) } }},
		{"invalid module", func(t *testing.T, root string) { if err := os.WriteFile(filepath.Join(root, "modules", "apps", "foo", "module.jsonc"), []byte(`{"id":"apps.foo"}`), 0o600); err != nil { t.Fatal(err) } }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := testCatalogRoot(t, "foo")
			tc.mutate(t, root)
			bundle := writeBundle(t, root, "work.jsonc", `{"version":1,"id":"work","name":"Work","modules":["foo"]}`)
			if _, err := Resolve(root, bundle, time.Now().UTC()); err == nil {
				t.Fatal("Resolve unexpectedly succeeded")
			}
		})
	}
}

func TestResolve_ProductionSchemaV1V2AndExecutableOnlyModulesRemainCatalogActions(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	result, err := Resolve(root, "bundles/terminals.jsonc", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	var windowsTerminal, powershellProfile *Action
	for index := range result.Actions {
		switch result.Actions[index].ModuleID {
		case "apps.windows-terminal":
			windowsTerminal = &result.Actions[index]
		case "apps.powershell-profile":
			powershellProfile = &result.Actions[index]
		}
	}
	if windowsTerminal == nil || windowsTerminal.ModuleSchemaVersion != 2 {
		t.Fatalf("schema-v2 action = %+v", windowsTerminal)
	}
	if powershellProfile == nil || powershellProfile.ModuleSchemaVersion != 1 || powershellProfile.Status != "resolved" || powershellProfile.Skipped {
		t.Fatalf("executable-only action = %+v", powershellProfile)
	}
	for _, action := range result.Actions {
		if strings.Contains(action.ModuleID, "winget") {
			t.Fatalf("catalog action synthesized package identity: %+v", action)
		}
	}
}

func TestResolve_RejectsSymlinkBundleWhenSupported(t *testing.T) {
	root := testCatalogRoot(t, "foo")
	target := writeBundle(t, root, "target.jsonc", `{"version":1,"id":"target","name":"Target","modules":["foo"]}`)
	path := filepath.Join(root, "bundles", "work.jsonc")
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Resolve(root, path, time.Now().UTC()); err == nil {
		t.Fatal("Resolve unexpectedly accepted symlink bundle")
	}
}

func actionModuleIDs(actions []Action) []string {
	ids := make([]string, len(actions))
	for index, action := range actions {
		ids[index] = action.ModuleID
	}
	return ids
}

func testCatalogRoot(t *testing.T, slugs ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, slug := range slugs {
		writeModule(t, root, slug)
	}
	return root
}

func writeBundle(t *testing.T, root, name, body string) string {
	t.Helper()
	path := filepath.Join(root, "bundles", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeModule(t *testing.T, root, slug string) {
	t.Helper()
	dir := filepath.Join(root, "modules", "apps", slug)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	module := fmt.Sprintf(`{"id":"apps.%s","displayName":"%s","matches":{"winget":["Vendor.%s"]},"verify":[{"type":"command-exists","command":"%s"}]}`,
		slug, strings.Title(slug), strings.Title(slug), slug)
	revision, err := modules.ComputeModuleRevision([]byte(module))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "module.jsonc"), []byte(module), 0o600); err != nil {
		t.Fatal(err)
	}
	sidecar := fmt.Sprintf(`{"schemaVersion":1,"moduleId":"apps.%s","moduleRevision":"%s","synthetic":{"scenarios":[{"id":"install","mode":"install-contract","fixture":{"type":"auto"},"timeoutSeconds":60,"minimumAssertions":{"appReferences":1,"verify":1}}]},"live":{"mode":"candidate","reasonCode":"test-only","explanation":"test catalog"}}`, slug, revision)
	if err := os.WriteFile(filepath.Join(dir, "validation.jsonc"), []byte(sidecar), 0o600); err != nil {
		t.Fatal(err)
	}
}
