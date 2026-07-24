// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestProjectCapturePlanningManifestUsesProductionProjection(t *testing.T) {
	plan := testGenerationCapturePlan(t, "apps.v2", "instance-a", filepath.Join(t.TempDir(), "instance-a"), true, false)
	legacyJSON := []byte(`{
  "id":"apps.legacy","displayName":"Legacy","sensitivity":"low",
  "matches":{"winget":["Vendor.Legacy"]},
  "verify":[{"type":"file-exists","path":"%APPDATA%\\Legacy\\settings.json"}],
  "restore":[{"type":"copy","source":"./payload/apps/legacy/settings.json","target":"%APPDATA%\\Legacy\\settings.json","backup":true}],
  "capture":{"files":[{"source":"%APPDATA%\\Legacy\\settings.json","dest":"apps/legacy/settings.json","optional":true}],"excludeGlobs":[]}
}`)
	legacy, err := modules.ParseModuleJSON(legacyJSON)
	if err != nil {
		t.Fatal(err)
	}
	apps := []manifest.App{{ID: "legacy", Driver: "winget", Refs: map[string]string{"windows": "Vendor.Legacy"}}}

	projected, err := ProjectCapturePlanningManifest(apps, []*modules.Module{legacy}, []ConfigSetCapturePlan{plan})
	if err != nil {
		t.Fatal(err)
	}
	if projected.Version != 2 || len(projected.ConfigCaptures) != 1 || len(projected.LegacyConfigLanes) != 1 {
		t.Fatalf("projected topology = %+v", projected)
	}
	capture := projected.ConfigCaptures[0]
	if capture.CaptureID != CaptureID(plan.Module.ID, plan.Set.ID, plan.Instance.ID) || capture.PayloadRoot != "configs/"+capture.CaptureID {
		t.Fatalf("projected generation capture = %+v", capture)
	}
	snapshot, err := WriteModuleSnapshot(t.TempDir(), plan.Module)
	if err != nil {
		t.Fatal(err)
	}
	if capture.CaptureModule.ContentHash != snapshot.ContentHash || capture.CaptureModule.SnapshotPath != snapshot.Path {
		t.Fatalf("projected provenance = %+v, production snapshot = %+v", capture.CaptureModule, snapshot)
	}
	if len(projected.Restore) != 1 || projected.Restore[0].Source != "./configs/"+LegacyCaptureID(legacy.ID)+"/settings.json" || projected.Restore[0].FromModule != legacy.ID {
		t.Fatalf("projected legacy restore = %+v", projected.Restore)
	}
	if len(projected.Verify) != 1 || projected.Verify[0].Type != "file-exists" {
		t.Fatalf("projected verify = %+v", projected.Verify)
	}

	before, _ := json.Marshal([]interface{}{apps, legacy.CanonicalSnapshot(), plan.Module.CanonicalSnapshot()})
	if _, err := ProjectCapturePlanningManifest(apps, []*modules.Module{legacy}, []ConfigSetCapturePlan{plan}); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal([]interface{}{apps, legacy.CanonicalSnapshot(), plan.Module.CanonicalSnapshot()})
	if string(before) != string(after) {
		t.Fatal("planning projection mutated its inputs")
	}
}
