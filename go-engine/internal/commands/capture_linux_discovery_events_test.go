// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/discovery"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

type reportingAdapter struct {
	id        string
	inventory discovery.AdapterInventory
	err       error
}

func (a reportingAdapter) ID() string { return a.id }
func (a reportingAdapter) Inventory(context.Context) (discovery.AdapterInventory, error) {
	return a.inventory, a.err
}

type discoveryWireEvent struct {
	Version    int                          `json:"version"`
	RunID      string                       `json:"runId"`
	Timestamp  string                       `json:"timestamp"`
	Event      string                       `json:"event"`
	Phase      string                       `json:"phase"`
	Kind       string                       `json:"kind"`
	Source     *discovery.SourceStatus      `json:"source"`
	Resolved   *discovery.ResolvedIntent    `json:"resolved"`
	Settings   *discovery.SettingsCandidate `json:"settings"`
	Unresolved *discovery.UnresolvedItem    `json:"unresolved"`
}

// This proves the public stream observes all reconciled rows, even when capture selects one package.
func TestLinuxCaptureDiscoveryEventsMatchAuthoritativeInventory(t *testing.T) {
	oldGOOS, oldRealizer, oldDiscovery := captureGOOSFn, newRealizerFn, discoverLinuxFn
	t.Cleanup(func() { captureGOOSFn, newRealizerFn, discoverLinuxFn = oldGOOS, oldRealizer, oldDiscovery })
	captureGOOSFn = func() string { return "linux" }
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	const privateDiagnostic = "backend-raw /private/host/root"
	discoverLinuxFn = func(ctx context.Context, request discovery.Request, _ realizer.Realizer) (discovery.Result, error) {
		return (discovery.Orchestrator{Adapters: []discovery.Adapter{
			reportingAdapter{id: "z-unavailable", err: discovery.ErrNotAvailable},
			reportingAdapter{id: "a-failed", err: errors.New(privateDiagnostic)},
			reportingAdapter{id: "m-readable", inventory: discovery.AdapterInventory{
				Resolved: []discovery.ResolvedIntent{
					{ID: "selected", Attribute: "selected", Input: "github:example/packages/revision", Installable: "github:example/packages/revision#selected"},
					{ID: "unselected", Attribute: "unselected", Input: "github:example/packages/revision", Installable: "github:example/packages/revision#unselected"},
				},
				Settings: []discovery.SettingsCandidate{{ID: "settings", DisplayName: "Application settings", Portability: discovery.PortabilityConfigOnly, Actions: []discovery.SettingsAction{discovery.SettingsRestore}}},
				Items:    []discovery.InventoryItem{{Source: "rpm", Kind: discovery.EvidencePackage, Ref: "unmapped", Explicit: true}},
			}},
		}}).Discover(ctx, request)
	}
	var stream bytes.Buffer
	restore := events.ActivateDefaultWriter(&stream)
	defer restore()
	out := filepath.Join(t.TempDir(), "selected.jsonc")
	withFakeGenerations(nil, errors.New("no history"), func() {
		emptyCatalog(func() {
			raw, err := RunCapture(CaptureFlags{Out: out, Only: "selected", Events: "jsonl"})
			if err != nil {
				t.Fatalf("capture: %+v", err)
			}
			result := raw.(*CaptureResult)
			if len(result.AppsIncluded) != 1 || result.Discovery.Counts.Selected != 1 {
				t.Fatalf("selection = %+v", result)
			}
			var sources []discovery.SourceStatus
			var resolved []discovery.ResolvedIntent
			var settings []discovery.SettingsCandidate
			var unresolved []discovery.UnresolvedItem
			var order []string
			decoder := json.NewDecoder(bytes.NewReader(stream.Bytes()))
			runID := ""
			var rows []discoveryWireEvent
			for decoder.More() {
				var row discoveryWireEvent
				if err := decoder.Decode(&row); err != nil {
					t.Fatal(err)
				}
				if row.Version != 1 || row.RunID == "" {
					t.Fatalf("invalid event identity: %+v", row)
				}
				if runID == "" {
					runID = row.RunID
				}
				if row.RunID != runID {
					t.Fatalf("event changed run: %+v", row)
				}
				if _, err := time.Parse(time.RFC3339Nano, row.Timestamp); err != nil {
					t.Fatal(err)
				}
				rows = append(rows, row)
				switch row.Event {
				case "discovery-source":
					if row.Source == nil {
						t.Fatal("source event lost normalized source")
					}
					sources = append(sources, *row.Source)
					order = append(order, row.Event)
				case "discovery-item":
					order = append(order, row.Kind)
					switch row.Kind {
					case "resolved":
						if row.Resolved == nil || row.Settings != nil || row.Unresolved != nil {
							t.Fatalf("invalid row: %+v", row)
						}
						resolved = append(resolved, *row.Resolved)
					case "settings":
						if row.Settings == nil || row.Resolved != nil || row.Unresolved != nil {
							t.Fatalf("invalid row: %+v", row)
						}
						settings = append(settings, *row.Settings)
					case "unresolved":
						if row.Unresolved == nil || row.Resolved != nil || row.Settings != nil {
							t.Fatalf("invalid row: %+v", row)
						}
						unresolved = append(unresolved, *row.Unresolved)
					default:
						t.Fatalf("unknown discovery kind: %+v", row)
					}
				}
			}
			if len(rows) == 0 || rows[0].Event != "phase" || rows[0].Phase != "capture" || rows[len(rows)-1].Event != "summary" {
				t.Fatalf("capture lifecycle = %+v", rows)
			}
			projected, _ := json.Marshal([]any{sources, resolved, settings, unresolved})
			expected, _ := json.Marshal([]any{result.Discovery.Sources, result.Discovery.Resolved, result.Discovery.Settings, result.Discovery.Unresolved})
			if !bytes.Equal(projected, expected) {
				t.Fatalf("stream lost or changed discovery: %s", stream.String())
			}
			if !reflect.DeepEqual(order, []string{"discovery-source", "discovery-source", "discovery-source", "resolved", "resolved", "settings", "unresolved"}) {
				t.Fatalf("projection order = %v", order)
			}
			data, _ := json.Marshal(result)
			artifact := readCaptureManifestBytes(t, result.OutputPath)
			if strings.Contains(stream.String()+string(data)+string(artifact), privateDiagnostic) {
				t.Fatal("raw source diagnostic escaped into public output")
			}
			stream.Reset()
			if _, err := RunCapture(CaptureFlags{Out: out, Only: "selected"}); err != nil {
				t.Fatal(err)
			}
			if stream.Len() != 0 {
				t.Fatalf("disabled events emitted: %s", stream.String())
			}
		})
	})
}

// This catches a fallback or fatal error erasing source health and leaking command paths.
func TestLinuxCaptureFailedDiscoveryRetainsSourceHealth(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(map[bool]string{false: "fatal", true: "managed fallback"}[fallback], func(t *testing.T) {
			oldGOOS, oldRealizer, oldDiscovery := captureGOOSFn, newRealizerFn, discoverLinuxFn
			t.Cleanup(func() { captureGOOSFn, newRealizerFn, discoverLinuxFn = oldGOOS, oldRealizer, oldDiscovery })
			captureGOOSFn = func() string { return "linux" }
			newRealizerFn = func() (realizer.Realizer, error) {
				if fallback {
					return &fakeRealizer{currentSet: nixSetWithVersion("ripgrep", "14")}, nil
				}
				return nil, ErrNoRealizer
			}
			var authoritative discovery.Result
			discoverLinuxFn = func(ctx context.Context, request discovery.Request, _ realizer.Realizer) (discovery.Result, error) {
				result, err := (discovery.Orchestrator{Adapters: []discovery.Adapter{
					reportingAdapter{id: "failed", err: &discovery.CommandError{Path: "/private/command/root", ExitCode: 17}},
					reportingAdapter{id: "unavailable", err: discovery.ErrNotAvailable},
				}}).Discover(ctx, request)
				authoritative = result
				return result, err
			}
			var stream bytes.Buffer
			restore := events.ActivateDefaultWriter(&stream)
			defer restore()
			out := filepath.Join(t.TempDir(), "failed.jsonc")
			withFakeGenerations(nil, errors.New("no history"), func() {
				emptyCatalog(func() {
					raw, captureErr := RunCapture(CaptureFlags{Out: out, Events: "jsonl"})
					if fallback {
						if captureErr != nil {
							t.Fatalf("fallback: %+v", captureErr)
						}
						result := raw.(*CaptureResult).Discovery
						if result == nil || len(result.Sources) != 2 || !reflect.DeepEqual(result.Sources, authoritative.Sources) || len(result.Warnings) != 2 {
							t.Fatalf("fallback erased health: %+v", result)
						}
					} else {
						if captureErr == nil {
							t.Fatal("all failed sources succeeded")
						}
						data, _ := json.Marshal(captureErr)
						var body struct {
							Detail discovery.Result `json:"detail"`
						}
						if err := json.Unmarshal(data, &body); err != nil {
							t.Fatal(err)
						}
						if len(body.Detail.Sources) != 2 || !reflect.DeepEqual(body.Detail.Sources, authoritative.Sources) {
							t.Fatalf("fatal error erased health: %s", data)
						}
						if strings.Contains(captureErr.Message, "/private/") {
							t.Fatalf("primary error leaked raw diagnostic: %s", captureErr.Message)
						}
						if _, err := os.Stat(out); !os.IsNotExist(err) {
							t.Fatalf("failed capture published output: %v", err)
						}
					}
					var sources []discovery.SourceStatus
					decoder := json.NewDecoder(bytes.NewReader(stream.Bytes()))
					for decoder.More() {
						var row discoveryWireEvent
						if err := decoder.Decode(&row); err != nil {
							t.Fatal(err)
						}
						if row.Event == "discovery-source" && row.Source != nil {
							sources = append(sources, *row.Source)
						}
						if row.Event == "discovery-item" {
							t.Fatal("failed discovery streamed incomplete candidates")
						}
					}
					if len(sources) != 2 || !reflect.DeepEqual(sources, authoritative.Sources) {
						t.Fatalf("failed-source stream = %+v; authoritative=%+v", sources, authoritative.Sources)
					}
					report, _ := json.Marshal(sources)
					var typed []struct {
						Diagnostic *struct {
							Code     string `json:"code"`
							ExitCode int    `json:"exitCode"`
						} `json:"diagnostic"`
					}
					if err := json.Unmarshal(report, &typed); err != nil {
						t.Fatal(err)
					}
					if typed[0].Diagnostic == nil || typed[0].Diagnostic.Code != "command_failed" || typed[0].Diagnostic.ExitCode != 17 || strings.Contains(string(report)+stream.String(), "/private/") {
						t.Fatalf("source diagnostic lost typed detail or leaked path: %s", report)
					}
				})
			})
		})
	}
}

func TestLinuxCaptureDiscoveryFailureKeepsRawErrorsPrivate(t *testing.T) {
	oldGOOS, oldRealizer, oldDiscovery := captureGOOSFn, newRealizerFn, discoverLinuxFn
	t.Cleanup(func() { captureGOOSFn, newRealizerFn, discoverLinuxFn = oldGOOS, oldRealizer, oldDiscovery })
	captureGOOSFn = func() string { return "linux" }
	for _, test := range []struct {
		name    string
		failure error
		managed bool
	}{
		{name: "raw failure", failure: errors.New("backend-raw /private/host/root")},
		{name: "cancellation", failure: context.Canceled, managed: true},
		{name: "deadline", failure: context.DeadlineExceeded, managed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			newRealizerFn = func() (realizer.Realizer, error) {
				if test.managed {
					return &fakeRealizer{currentSet: nixSetWithVersion("ripgrep", "14")}, nil
				}
				return nil, ErrNoRealizer
			}
			discoverLinuxFn = func(context.Context, discovery.Request, realizer.Realizer) (discovery.Result, error) {
				return discovery.Result{}, test.failure
			}
			out := filepath.Join(t.TempDir(), "untouched.endstate")
			raw, err := RunCapture(CaptureFlags{Out: out})
			if err == nil || raw != nil {
				t.Fatalf("failed discovery became a managed capture: result=%+v err=%+v", raw, err)
			}
			data, _ := json.Marshal(err)
			if strings.Contains(string(data), "backend-raw") || strings.Contains(string(data), "/private/") {
				t.Fatalf("raw error escaped: %s", data)
			}
			if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
				t.Fatalf("failed discovery published: %v", statErr)
			}
		})
	}
}
