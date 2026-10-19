// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/registryfile"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
	"github.com/Artexis10/endstate/go-engine/internal/verifier"
)

// TestRunVerifyChecksCapturedSettingsOnlyWhenThisRunRestoredThem catches
// verify reporting an OS setting captured on the old PC as a failure when the
// user did not restore it, for example rebuild with restore off or filtered
// to other modules. The new PC keeps its own value there, so the captured
// value is not expected.
func TestRunVerifyChecksCapturedSettingsOnlyWhenThisRunRestoredThem(t *testing.T) {
	const advanced = `HKCU\Software\Microsoft\Windows\CurrentVersion\Explorer\Advanced`
	manifestPath := filepath.Join(t.TempDir(), "manifest.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{
  "version": 1,
  "name": "captured",
  "apps": [],
  "verify": [
    {"type": "registry-value-equals", "path": "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Explorer\\Advanced",
     "valueName": "HideFileExt", "valueType": "REG_DWORD", "data": "0", "fromModule": "windows-settings.explorer"},
    {"type": "file-exists", "path": "C:\\Example\\settings.json"}
  ]
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var evaluated []manifest.VerifyEntry
	original := runVerifierFn
	runVerifierFn = func(entries []manifest.VerifyEntry, _ *validationmode.Context) ([]verifier.VerifyResult, error) {
		evaluated = append([]manifest.VerifyEntry(nil), entries...)
		results := make([]verifier.VerifyResult, len(entries))
		for index, entry := range entries {
			results[index] = verifier.VerifyResult{Type: entry.Type, Pass: false, Message: "differs on this PC"}
		}
		return results, nil
	}
	t.Cleanup(func() { runVerifierFn = original })

	for _, test := range []struct {
		name         string
		restored     map[string]struct{}
		wantFail     int
		wantSkipped  int
		wantCaptured bool
	}{
		{name: "restore not selected", restored: nil, wantFail: 1, wantSkipped: 1, wantCaptured: false},
		{name: "restored in this run", restored: map[string]struct{}{registryfile.MatchKey(advanced + `\HideFileExt`): {}}, wantFail: 2, wantSkipped: 0, wantCaptured: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			evaluated = nil
			var raw interface{}
			withMockDriver(&mockDriver{}, func() {
				out, eerr := RunVerify(VerifyFlags{Manifest: manifestPath, restoredRegistryValues: test.restored})
				if eerr != nil {
					t.Fatalf("RunVerify: %v", eerr)
				}
				raw = out
			})
			result := raw.(*VerifyResult)
			if result.Summary.Fail != test.wantFail || result.Summary.Skipped != test.wantSkipped {
				t.Fatalf("summary = %+v, want fail=%d skipped=%d; results=%+v", result.Summary, test.wantFail, test.wantSkipped, result.Results)
			}
			capturedEvaluated := false
			for _, entry := range evaluated {
				if entry.FromModule != "" {
					capturedEvaluated = true
				}
			}
			if capturedEvaluated != test.wantCaptured {
				t.Fatalf("captured setting evaluated = %v, want %v", capturedEvaluated, test.wantCaptured)
			}
			if test.wantSkipped == 1 {
				var skipped *VerifyItem
				for index := range result.Results {
					if result.Results[index].Status == "skipped" {
						skipped = &result.Results[index]
					}
				}
				if skipped == nil || skipped.Type != "registry-value-equals" || skipped.Reason != verifyReasonNotRestored {
					t.Fatalf("results = %+v, want one visible skipped captured setting", result.Results)
				}
			}
		})
	}
}
