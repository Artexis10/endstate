// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package hmharvest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
	"github.com/Artexis10/endstate/go-engine/internal/releaseinputs"
)

func TestIncompleteRefreshRetainsOnlyConfirmedObservationsAndPreservesInput(t *testing.T) {
	inputs, err := releaseinputs.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range []struct {
		name  string
		field Field
	}{
		{name: "missing tracked option", field: OptionField},
		{name: "unreadable source", field: SourceHashField},
		{name: "failed probe", field: ProbeTargetField},
	} {
		t.Run(failure.name, func(t *testing.T) {
			root := t.TempDir()
			declaration := filepath.Join(root, "modules", "programs", "sample.nix")
			if err := os.MkdirAll(filepath.Dir(declaration), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(declaration, []byte("{ }\n"), 0600); err != nil {
				t.Fatal(err)
			}
			registry := &hmregistry.Registry{Entries: []hmregistry.Entry{
				{ID: "unread", Program: "unread", Options: []hmregistry.Option{{Name: "programs.unread.enable"}}},
				{ID: "sample", Program: "sample", Disposition: hmregistry.FileRoundTrip,
					Options: []hmregistry.Option{
						{Name: "programs.sample.settings", Type: "old settings", Declarations: []string{"modules/programs/old.nix"}},
						{Name: "programs.sample.arguments", Type: "old arguments", Declarations: []string{"modules/programs/sample.nix"}},
					},
					SourceHashes: map[string]string{"modules/programs/sample.nix": "old hash", "modules/programs/old.nix": "unobserved hash"},
					Targets:      []hmregistry.Target{{Coordinate: "${home}/old"}},
					Probe: &hmregistry.Probe{Targets: []string{"${home}/old"}, Values: map[string]any{
						"programs.sample.settings": map[string]any{"nested": []any{map[string]any{"value": "preserve"}}},
					}},
					ReviewFingerprint: "old review",
				},
			}}
			options := map[string]any{
				"programs.sample.arguments": map[string]any{"type": "new arguments", "declarations": []map[string]string{{"name": "<home-manager/modules/programs/sample.nix>"}}},
				"programs.sample.settings":  map[string]any{"type": "new settings", "declarations": []map[string]string{{"name": "<home-manager/modules/programs/sample.nix>"}}},
			}
			if failure.field == OptionField {
				delete(options, "programs.sample.settings")
			}
			if failure.field == SourceHashField {
				options["programs.sample.settings"] = map[string]any{"type": "new settings", "declarations": []map[string]string{{"name": "<home-manager/modules/programs/unavailable.nix>"}}}
			}
			docs, err := json.Marshal(options)
			if err != nil {
				t.Fatal(err)
			}
			before, err := json.Marshal(registry)
			if err != nil {
				t.Fatal(err)
			}
			var priorReport Report
			for attempt := 0; attempt < 2; attempt++ {
				result, err := Refresh(context.Background(), registry, docs, root, inputs, mutatingFailedProber{})
				var stopped *ObservationError
				if !errors.As(err, &stopped) || result.Candidate != nil || result.Report.Complete || result.Report.Stopped == nil {
					t.Fatalf("incomplete refresh returned a candidate or lost its typed stop: %+v; %v", result, err)
				}
				if stopped.Observation.Adapter != "sample" || stopped.Observation.Field != failure.field {
					t.Fatalf("wrong stop: %+v", stopped.Observation)
				}
				knownType, knownHash, removed := false, false, false
				for _, change := range result.Report.Changes {
					if change.Adapter == "unread" {
						t.Fatalf("invented observation for an unread adapter: %+v", change)
					}
					if change.Field == OptionTypeField && change.Subject == "programs.sample.arguments" {
						knownType = change.Before.Value == "old arguments" && change.After.Value == "new arguments"
					}
					if change.Field == SourceHashField && change.Subject == "modules/programs/sample.nix" {
						knownHash = change.Before.Present && change.After.Present && change.After.Value != "old hash"
					}
					if change.Field == OptionField && change.Kind == Removed && change.Subject == "programs.sample.settings" {
						removed = change.Before.Present && !change.After.Present
					}
					if failure.field == SourceHashField && change.Field == SourceHashField && change.Subject != "modules/programs/sample.nix" {
						t.Fatalf("failed source read fabricated a change: %+v", change)
					}
					if change.Field == TargetField || change.Field == ProbeTargetField {
						t.Fatalf("unavailable probe fabricated targets: %+v", change)
					}
				}
				if !knownType || !knownHash || removed != (failure.field == OptionField) {
					t.Fatalf("lost known changes or fabricated removal: %+v", result.Report)
				}
				if failure.field == ProbeTargetField && (!errors.Is(err, context.DeadlineExceeded) || len(err.Error()) > 1100 || !utf8.ValidString(err.Error()) || !strings.Contains(err.Error(), "shortened")) {
					t.Fatalf("probe error lost identity or display bounds: %v", err)
				}
				if attempt != 0 && !reflect.DeepEqual(priorReport, result.Report) {
					t.Fatal("same observations produced different typed reports")
				}
				priorReport = result.Report
				after, err := json.Marshal(registry)
				if err != nil || string(before) != string(after) {
					t.Fatalf("failed refresh mutated nested caller data: %v", err)
				}
			}
		})
	}
}

type mutatingFailedProber struct{}

func TestNullDocsCannotConfirmOptionRemoval(t *testing.T) {
	registry := &hmregistry.Registry{Entries: []hmregistry.Entry{{ID: "sample", Options: []hmregistry.Option{{Name: "programs.sample.settings"}}}}}
	result, err := Refresh(context.Background(), registry, []byte(`null`), t.TempDir(), releaseinputs.Inputs{}, nil)
	if err == nil || result.Candidate != nil || result.Report.Complete || result.Report.Stopped == nil || result.Report.Stopped.Field != DocsHashField {
		t.Fatalf("null docs became an observed option inventory: %+v; %v", result, err)
	}
	for _, change := range result.Report.Changes {
		if change.Field == OptionField && change.Kind == Removed {
			t.Fatal("null docs fabricated a confirmed option removal")
		}
	}
}

func (mutatingFailedProber) ProbeTargets(_ context.Context, _ string, values map[string]any) ([]string, error) {
	values["programs.sample.settings"].(map[string]any)["nested"].([]any)[0].(map[string]any)["value"] = "mutated copy"
	return nil, fmt.Errorf("%s: %w", strings.Repeat("界", 1000), context.DeadlineExceeded)
}
