// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bootstrap"
	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
)

func resultWarnings(t *testing.T, result interface{}) ([]CommandWarning, string) {
	t.Helper()
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Warnings []CommandWarning `json:"warnings"`
	}
	if err := json.Unmarshal(b, &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Warnings, string(b)
}

func withChocolateyAvailable(t *testing.T, fn func()) {
	t.Helper()
	orig := bootstrapBackendsFn
	bootstrapBackendsFn = func(needed []bootstrap.Backend, _ bool, _ Consent, _ *events.Emitter) (map[bootstrap.Backend]bool, *envelope.Error) {
		return map[bootstrap.Backend]bool{bootstrap.BackendChocolatey: true}, nil
	}
	defer func() { bootstrapBackendsFn = orig }()
	fn()
}

func duplicateOwnershipManifest(t *testing.T) string {
	t.Helper()
	return writeLaneManifest(t, `
		{"id":"winget-tool","displayName":" Example Tool ","refs":{"windows":"Vendor.Tool"}},
		{"id":"choco-tool","driver":"chocolatey","displayName":"example tool","refs":{"windows":"example-tool"}}`)
}

func TestRunPlan_PossibleDuplicateWarningPreservesActions(t *testing.T) {
	winget := &laneTestDriver{name: "winget", installed: map[string]bool{}}
	choco := &laneTestDriver{name: "chocolatey", installed: map[string]bool{}}
	path := duplicateOwnershipManifest(t)

	withChocolateyAvailable(t, func() {
		withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget, "chocolatey": choco}, nil, func() {
			raw, eerr := RunPlan(PlanFlags{Manifest: path})
			if eerr != nil {
				t.Fatalf("RunPlan error = %v", eerr)
			}
			result := raw.(*PlanResult)
			warnings, _ := resultWarnings(t, result)
			if len(warnings) != 1 || warnings[0].Code != "possible_duplicate" {
				t.Fatalf("warnings = %+v, want one possible_duplicate", warnings)
			}
			if len(result.Actions) != 2 || result.Plan.Total != 2 || result.Plan.ToInstall != 2 {
				t.Fatalf("plan changed by warning: actions=%+v summary=%+v", result.Actions, result.Plan)
			}
		})
	})
}

func TestRunPlan_EmptyWarningsOmittedFromJSON(t *testing.T) {
	winget := &laneTestDriver{name: "winget", installed: map[string]bool{}}
	path := writeLaneManifest(t, `{"id":"tool","displayName":"Tool","refs":{"windows":"Vendor.Tool"}}`)

	withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget}, nil, func() {
		raw, eerr := RunPlan(PlanFlags{Manifest: path})
		if eerr != nil {
			t.Fatalf("RunPlan error = %v", eerr)
		}
		_, encoded := resultWarnings(t, raw)
		if strings.Contains(encoded, `"warnings"`) {
			t.Fatalf("empty warnings field was not omitted: %s", encoded)
		}
	})
}

func TestRunApply_PossibleDuplicateWarningPreservesDryRunAndLiveResults(t *testing.T) {
	for _, dryRun := range []bool{true, false} {
		name := "live"
		if dryRun {
			name = "dry-run"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("ENDSTATE_ROOT", t.TempDir())
			winget := &laneTestDriver{name: "winget", installed: map[string]bool{}}
			choco := &laneTestDriver{name: "chocolatey", installed: map[string]bool{}}
			path := duplicateOwnershipManifest(t)

			withChocolateyAvailable(t, func() {
				withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget, "chocolatey": choco}, nil, func() {
					raw, eerr := RunApply(ApplyFlags{Manifest: path, DryRun: dryRun})
					if eerr != nil {
						t.Fatalf("RunApply error = %v", eerr)
					}
					result := raw.(*ApplyResult)
					if len(result.Warnings) != 1 || result.Warnings[0].Code != "possible_duplicate" {
						t.Fatalf("warnings = %+v, want one possible_duplicate", result.Warnings)
					}
					if len(result.Actions) != 2 || result.Summary.Total != 2 {
						t.Fatalf("apply changed by warning: actions=%+v summary=%+v", result.Actions, result.Summary)
					}
					if dryRun {
						if result.Summary != (ApplySummary{Total: 2}) || len(winget.installCalls) != 0 || len(choco.installCalls) != 0 {
							t.Fatalf("dry-run outcome changed: summary=%+v winget=%v choco=%v", result.Summary, winget.installCalls, choco.installCalls)
						}
					} else if result.Summary.Success != 2 || len(winget.installCalls) != 1 || len(choco.installCalls) != 1 {
						t.Fatalf("live outcome changed: summary=%+v winget=%v choco=%v", result.Summary, winget.installCalls, choco.installCalls)
					}
				})
			})
		})
	}
}

func TestRunApply_PossibleDuplicateAppendsExistingWarnings(t *testing.T) {
	winget := &laneTestDriver{name: "winget", installed: map[string]bool{}}
	path := duplicateOwnershipManifest(t)
	orig := bootstrapBackendsFn
	bootstrapBackendsFn = func([]bootstrap.Backend, bool, Consent, *events.Emitter) (map[bootstrap.Backend]bool, *envelope.Error) {
		return map[bootstrap.Backend]bool{bootstrap.BackendChocolatey: false}, nil
	}
	defer func() { bootstrapBackendsFn = orig }()

	withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget}, nil, func() {
		raw, eerr := RunApply(ApplyFlags{Manifest: path, DryRun: true})
		if eerr != nil {
			t.Fatalf("RunApply error = %v", eerr)
		}
		result := raw.(*ApplyResult)
		if len(result.Warnings) != 2 || result.Warnings[0].Code != "optional_driver_unavailable" || result.Warnings[1].Code != "possible_duplicate" {
			t.Fatalf("warnings = %+v, want existing warning followed by possible_duplicate", result.Warnings)
		}
		if len(result.Actions) != 2 || result.Summary.Total != 2 || result.Summary.Skipped != 1 {
			t.Fatalf("unavailable-lane outcome changed: actions=%+v summary=%+v", result.Actions, result.Summary)
		}
	})
}

func TestRunApply_OnlyExcludesUnselectedDuplicate(t *testing.T) {
	winget := &laneTestDriver{name: "winget", installed: map[string]bool{}}
	choco := &laneTestDriver{name: "chocolatey", installed: map[string]bool{}}
	path := duplicateOwnershipManifest(t)

	withChocolateyAvailable(t, func() {
		withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget, "chocolatey": choco}, nil, func() {
			raw, eerr := RunApply(ApplyFlags{Manifest: path, DryRun: true, Only: "winget-tool"})
			if eerr != nil {
				t.Fatalf("RunApply error = %v", eerr)
			}
			result := raw.(*ApplyResult)
			if len(result.Warnings) != 0 {
				t.Fatalf("warnings = %+v, excluded entry must not participate", result.Warnings)
			}
			if len(result.Actions) != 1 || result.Actions[0].ID != "winget-tool" || result.Summary.Total != 1 {
				t.Fatalf("--only outcome = actions=%+v summary=%+v", result.Actions, result.Summary)
			}
		})
	})
}

func TestRunVerify_PossibleDuplicateWarningPreservesResults(t *testing.T) {
	winget := &laneTestDriver{name: "winget", installed: map[string]bool{"Vendor.Tool": true}}
	choco := &laneTestDriver{name: "chocolatey", installed: map[string]bool{"example-tool": true}}
	path := duplicateOwnershipManifest(t)

	withChocolateyAvailable(t, func() {
		withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget, "chocolatey": choco}, nil, func() {
			raw, eerr := RunVerify(VerifyFlags{Manifest: path})
			if eerr != nil {
				t.Fatalf("RunVerify error = %v", eerr)
			}
			result := raw.(*VerifyResult)
			warnings, _ := resultWarnings(t, result)
			if len(warnings) != 1 || warnings[0].Code != "possible_duplicate" {
				t.Fatalf("warnings = %+v, want one possible_duplicate", warnings)
			}
			if len(result.Results) != 2 || result.Summary != (VerifySummary{Total: 2, Pass: 2}) {
				t.Fatalf("verify changed by warning: results=%+v summary=%+v", result.Results, result.Summary)
			}
		})
	})
}

func TestRunVerify_ManualAndNonPackageEntriesDoNotWarn(t *testing.T) {
	winget := &laneTestDriver{name: "winget", installed: map[string]bool{"Vendor.Tool": true}}
	missingPath := filepath.Join(t.TempDir(), "missing-manual-tool")
	path := writeLaneManifest(t, `
		{"id":"package","displayName":"Example Tool","refs":{"windows":"Vendor.Tool"}},
		{"id":"manual","displayName":" example tool ","refs":{},"manual":{"verifyPath":`+strconv.Quote(missingPath)+`}},
		{"id":"metadata-only","displayName":"EXAMPLE TOOL","refs":{}}`)

	withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget}, nil, func() {
		raw, eerr := RunVerify(VerifyFlags{Manifest: path})
		if eerr != nil {
			t.Fatalf("RunVerify error = %v", eerr)
		}
		result := raw.(*VerifyResult)
		warnings, _ := resultWarnings(t, result)
		if len(warnings) != 0 {
			t.Fatalf("warnings = %+v, manual/non-package entries must not participate", warnings)
		}
		if len(result.Results) != 2 || result.Results[0].Driver != "winget" || result.Results[1].Driver != "manual" {
			t.Fatalf("verify results = %+v, want package and manual only", result.Results)
		}
	})
}

func TestRunVerify_EmptyWarningsOmittedFromJSON(t *testing.T) {
	winget := &laneTestDriver{name: "winget", installed: map[string]bool{"Vendor.Tool": true}}
	path := writeLaneManifest(t, `{"id":"tool","displayName":"Tool","refs":{"windows":"Vendor.Tool"}}`)

	withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget}, nil, func() {
		raw, eerr := RunVerify(VerifyFlags{Manifest: path})
		if eerr != nil {
			t.Fatalf("RunVerify error = %v", eerr)
		}
		_, encoded := resultWarnings(t, raw)
		if strings.Contains(encoded, `"warnings"`) {
			t.Fatalf("empty warnings field was not omitted: %s", encoded)
		}
	})
}
