// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestSnapAdapterCapabilityContract(t *testing.T) {
	runner := &fixtureRunner{outputs: map[string][]byte{"snap": []byte(`{
  "type":"sync","status-code":200,"status":"OK","result":[
    {"name":"editor","version":"1.2","type":"app","status":"active","apps":[{"name":"editor"}]},
    {"name":"old-editor","version":"1.0","type":"app","status":"installed","apps":[{"name":"old-editor"}]},
    {"name":"platform","version":"2","type":"base","status":"active"},
    {"name":"content","version":"3","type":"app","status":"active","apps":[]}
  ]}`)}}
	inventory, err := (SnapAdapter{Runner: runner}).Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []InventoryItem{{Source: "snap", Kind: EvidencePackage, Ref: "editor", Version: "1.2", DisplayName: "editor", UserFacing: true}}
	if !reflect.DeepEqual(inventory.Items, want) || inventory.Ignored != 3 {
		t.Fatalf("Snap runnable-application evidence = %+v", inventory)
	}
	if len(inventory.Warnings) != 1 || inventory.Warnings[0].Code != "snap_command_evidence_unavailable" {
		t.Fatalf("missing command metadata was labeled as proven runtime: %+v", inventory.Warnings)
	}
	wantArgs := []string{"debug", "api", "--disable-auth", "--fail", "--request", "GET", "/v2/snaps?select=enabled"}
	if len(runner.commands) != 1 || runner.commands[0].Path != "snap" || !reflect.DeepEqual(runner.commands[0].Args, wantArgs) {
		t.Fatalf("inventory must use one credential-free local GET: %+v", runner.commands)
	}
}

func TestSnapAdapterRejectsFailedAndMalformedInventory(t *testing.T) {
	for name, response := range map[string]string{
		"non-JSON":         "private server response",
		"HTTP error":       `{"type":"error","status-code":500,"result":{"message":"private details"}}`,
		"missing result":   `{"type":"sync","status-code":200}`,
		"null row":         `{"type":"sync","status-code":200,"result":[null]}`,
		"missing row type": `{"type":"sync","status-code":200,"result":[{"name":"editor","version":"1","status":"active"}]}`,
		"invalid name":     `{"type":"sync","status-code":200,"result":[{"name":"","type":"app","status":"active","apps":[{}]}]}`,
		"private name":     `{"type":"sync","status-code":200,"result":[{"name":"/private/config","version":"1","type":"app","status":"active","apps":[{"name":"editor"}]}]}`,
		"private version":  `{"type":"sync","status-code":200,"result":[{"name":"editor","version":"/private/config","type":"app","status":"active","apps":[{"name":"editor"}]}]}`,
		"empty command":    `{"type":"sync","status-code":200,"result":[{"name":"editor","version":"1","type":"app","status":"active","apps":[{}]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fixtureRunner{outputs: map[string][]byte{"snap": []byte(response)}}
			if _, err := (SnapAdapter{Runner: runner}).Inventory(context.Background()); err == nil {
				t.Fatal("untrustworthy inventory was accepted")
			}
		})
	}
	runner := &fixtureRunner{errors: map[string]error{"snap": ErrNotAvailable}}
	if _, err := (SnapAdapter{Runner: runner}).Inventory(context.Background()); !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("missing client lost unavailable state: %v", err)
	}
}

func TestExperimentalSnapSelectionReportsUnavailableClient(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENDSTATE_ROOT", root)
	t.Setenv("PATH", root)
	// The empty PATH directory guarantees this check cannot reach snapd.
	_, err := Discover(context.Background(), Request{SelectedSources: []string{" " + SnapExperimentalSource + " "}})
	if err == nil {
		t.Fatal("selected missing Snap client was silently treated as no selected source")
	}
}
