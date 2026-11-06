// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/discovery"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/realizer/nix"
)

// A machine with no nix executable and no Endstate state must still get the
// honest ordinary-discovery result. The package backend is the real Nix backend
// whose process runner reports the spawn failure a missing executable produces.
func TestLinuxCapture_NoNixNoState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux home, XDG state and PATH lookup of the nix executable")
	}
	for _, test := range []struct {
		name        string
		native      []discovery.Adapter
		wantMessage string
	}{
		{
			name: "installed applications have no reviewed mapping",
			native: []discovery.Adapter{reportingAdapter{id: "debian-explicit", inventory: discovery.AdapterInventory{
				Items: []discovery.InventoryItem{{Source: "debian", Kind: discovery.EvidencePackage, Ref: "unmapped", Explicit: true, UserFacing: true}},
			}}},
			wantMessage: "1 unresolved",
		},
		{
			name:        "no native source can be read",
			wantMessage: "trustworthy application source",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
			t.Setenv("PATH", t.TempDir()) // no nix on PATH
			oldGOOS, oldRealizer, oldDiscovery := captureGOOSFn, newRealizerFn, discoverLinuxFn
			t.Cleanup(func() { captureGOOSFn, newRealizerFn, discoverLinuxFn = oldGOOS, oldRealizer, oldDiscovery })
			captureGOOSFn = func() string { return "linux" }
			newRealizerFn = func() (realizer.Realizer, error) {
				return &nix.Backend{
					Profile: filepath.Join(home, ".local", "state", "endstate", "nix-profile"),
					Run: func(...string) ([]byte, []byte, int, error) {
						return nil, nil, -1, &exec.Error{Name: "nix", Err: exec.ErrNotFound}
					},
				}, nil
			}
			discoverLinuxFn = func(ctx context.Context, request discovery.Request, r realizer.Realizer) (discovery.Result, error) {
				adapters := append([]discovery.Adapter{
					discovery.NixUserProfileAdapter{},
					discovery.RealizerInventoryAdapter{Realizer: r},
				}, test.native...)
				return (discovery.Orchestrator{Adapters: adapters}).Discover(ctx, request)
			}

			var stream bytes.Buffer
			restore := events.ActivateDefaultWriter(&stream)
			defer restore()
			out := filepath.Join(t.TempDir(), "capture.endstate")
			withFakeGenerations(nil, errors.New("no Endstate history"), func() {
				emptyCatalog(func() {
					_, captureErr := RunCapture(CaptureFlags{Out: out, Events: "jsonl"})
					if captureErr == nil {
						t.Fatal("capture with nothing to save succeeded")
					}
					if captureErr.Code == envelope.ErrRealizerUnavailable {
						t.Fatalf("capture failed only because Nix is absent: %+v", captureErr)
					}
					if captureErr.Code != envelope.ErrCaptureFailed || !strings.Contains(captureErr.Message, test.wantMessage) {
						t.Fatalf("capture error = %+v, want CAPTURE_FAILED mentioning %q", captureErr, test.wantMessage)
					}
				})
			})
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatalf("capture with nothing to save wrote an artifact: %v", err)
			}

			states := map[string]discovery.SourceState{}
			decoder := json.NewDecoder(bytes.NewReader(stream.Bytes()))
			for decoder.More() {
				var row discoveryWireEvent
				if err := decoder.Decode(&row); err != nil {
					t.Fatal(err)
				}
				if row.Event == "discovery-source" && row.Source != nil {
					states[row.Source.ID] = row.Source.State
				}
			}
			for _, id := range []string{"nix-user-profile", "endstate-profile"} {
				if states[id] != discovery.SourceNotAvailable {
					t.Fatalf("source %s = %q, want not_available; all sources: %v", id, states[id], states)
				}
			}
		})
	}
}
