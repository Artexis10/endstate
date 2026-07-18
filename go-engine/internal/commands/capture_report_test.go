// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
)

func TestCaptureMetadataFromBundleReport_PreservesModuleEvidenceExactly(t *testing.T) {
	report := bundle.BundleReport{
		SensitiveExcludedCount: 2,
		Modules: []bundle.ModuleCollectionResult{{
			ID: "apps.partial", AppID: "partial", DisplayName: "Partial",
			WingetRefs: []string{"Vendor.Partial"}, ChocolateyRefs: []string{"partial"},
			Paths: []string{"configs/partial/settings.json"}, FilesCaptured: 1, Status: "error",
			Warnings: []string{"optional value unavailable"}, Errors: []string{"required file unavailable"},
			SensitiveExcludedCount: 2,
		}},
	}

	results, _, _, _, sensitive := captureMetadataFromBundleReport(report, nil)
	if len(results) != 1 {
		t.Fatalf("results = %+v", results)
	}
	got := results[0]
	wire, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]interface{}
	if err := json.Unmarshal(wire, &fields); err != nil {
		t.Fatal(err)
	}
	warnings, warningsOK := fields["warnings"].([]interface{})
	errors, errorsOK := fields["errors"].([]interface{})
	if !warningsOK || len(warnings) != 1 || warnings[0] != report.Modules[0].Warnings[0] ||
		!errorsOK || len(errors) != 1 || errors[0] != report.Modules[0].Errors[0] {
		t.Fatalf("public warnings/errors wire = %s, report = %v/%v", wire, report.Modules[0].Warnings, report.Modules[0].Errors)
	}
	if got.FilesCaptured != 1 || len(got.Paths) != 1 || got.Paths[0] != report.Modules[0].Paths[0] || sensitive != 2 {
		t.Fatalf("public module evidence = %+v sensitive=%d", got, sensitive)
	}
}
