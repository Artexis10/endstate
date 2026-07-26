// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunCatalogPlan_EmitsCatalogOnlyNonSkippedModuleActions(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	t.Setenv("ENDSTATE_ROOT", root)

	raw, err := RunCatalogPlan(CatalogPlanFlags{Bundle: "bundles/dev-tools.jsonc"})
	if err != nil {
		t.Fatal(err)
	}
	data, ok := raw.(*CatalogPlanResult)
	if !ok {
		t.Fatalf("result = %T, want *CatalogPlanResult", raw)
	}
	if data.Proof != "catalog" || data.ActionCount == 0 || data.ActionCount != data.MembershipCount || len(data.Actions) != data.ActionCount {
		t.Fatalf("catalog plan = %+v", data)
	}
	if data.Bundle.Path != "bundles/dev-tools.jsonc" || strings.Contains(data.Bundle.Path, root) {
		t.Fatalf("bundle path = %q", data.Bundle.Path)
	}
	for _, action := range data.Actions {
		if action.Skipped || action.Status != "resolved" || !strings.HasPrefix(action.ModuleID, "apps.") || action.ValidationScenarioCount == 0 {
			t.Fatalf("action = %+v", action)
		}
	}
}

func TestRunCatalogPlan_ReturnsSafePartialFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		bundle string
		mutate func(t *testing.T, root string)
		want   string
	}{
		{name: "missing module", bundle: `{"version":1,"id":"work","name":"Work","modules":["missing"]}`, want: "missing_module"},
		{name: "stale sidecar", bundle: `{"version":1,"id":"work","name":"Work","modules":["foo"]}`, want: "stale_validation_sidecar", mutate: makeCommandSidecarStale},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := commandCatalogRoot(t)
			if tc.mutate != nil {
				tc.mutate(t, root)
			}
			writeCommandBundle(t, root, tc.bundle)
			t.Setenv("ENDSTATE_ROOT", root)

			raw, envelopeErr := RunCatalogPlan(CatalogPlanFlags{Bundle: "bundles/work.jsonc"})
			if envelopeErr == nil || envelopeErr.Code != "CATALOG_PLAN_INVALID" {
				t.Fatalf("error = %+v", envelopeErr)
			}
			data, ok := raw.(*CatalogPlanResult)
			if !ok || len(data.Failures) != 1 {
				t.Fatalf("result = %#v", raw)
			}
			failure := data.Failures[0]
			if failure.Reason != tc.want || failure.ModuleID == "" {
				t.Fatalf("failure = %+v", failure)
			}
			encoded, err := json.Marshal(struct {
				Data   *CatalogPlanResult `json:"data"`
				Detail interface{}        `json:"detail"`
			}{data, envelopeErr.Detail})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), root) {
				t.Fatalf("failure output leaked catalog root: %s", encoded)
			}
		})
	}
}

func TestRunCatalogPlan_EmitsContractValidJSONLEvents(t *testing.T) {
	root := commandCatalogRoot(t)
	writeCommandBundle(t, root, `{"version":1,"id":"work","name":"Work","modules":["foo"]}`)
	t.Setenv("ENDSTATE_ROOT", root)

	var stream bytes.Buffer
	restoreEvents := events.ActivateDefaultWriter(&stream)
	t.Cleanup(restoreEvents)
	if _, err := RunCatalogPlan(CatalogPlanFlags{Bundle: "bundles/work.jsonc", Events: "jsonl"}); err != nil {
		t.Fatal(err)
	}
	var item map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(stream.String()), "\n") {
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("invalid JSONL event %q: %v", line, err)
		}
		if event["event"] == "item" {
			item = event
		}
	}
	if item == nil || item["status"] != "present" || item["reason"] != "detected" {
		t.Fatalf("catalog item event = %#v", item)
	}
}

func commandCatalogRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	directory := filepath.Join(root, "modules", "apps", "foo")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	module := `{"id":"apps.foo","displayName":"Foo","matches":{"winget":["Vendor.Foo"]},"verify":[{"type":"command-exists","command":"foo"}]}`
	revision, err := modules.ComputeModuleRevision([]byte(module))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "module.jsonc"), []byte(module), 0o600); err != nil {
		t.Fatal(err)
	}
	sidecar := fmt.Sprintf(`{"schemaVersion":1,"moduleId":"apps.foo","moduleRevision":"%s","synthetic":{"scenarios":[{"id":"install","mode":"install-contract","fixture":{"type":"auto"},"timeoutSeconds":60,"minimumAssertions":{"appReferences":1,"verify":1}}]},"live":{"mode":"candidate","reasonCode":"test-only","explanation":"test catalog"}}`, revision)
	if err := os.WriteFile(filepath.Join(directory, "validation.jsonc"), []byte(sidecar), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeCommandBundle(t *testing.T, root, body string) {
	t.Helper()
	path := filepath.Join(root, "bundles", "work.jsonc")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func makeCommandSidecarStale(t *testing.T, root string) {
	t.Helper()
	modulePath := filepath.Join(root, "modules", "apps", "foo", "module.jsonc")
	module, err := os.ReadFile(modulePath)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := modules.ComputeModuleRevision(module)
	if err != nil {
		t.Fatal(err)
	}
	sidecarPath := filepath.Join(root, "modules", "apps", "foo", "validation.jsonc")
	sidecar, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sidecarPath, []byte(strings.Replace(string(sidecar), revision, strings.Repeat("b", 64), 1)), 0o600); err != nil {
		t.Fatal(err)
	}
}
