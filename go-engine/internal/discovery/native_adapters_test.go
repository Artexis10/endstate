// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestRPMAdapterUsesUserInstalledLedgerAndDNFFallback(t *testing.T) {
	runner := &fixtureRunner{outputs: map[string][]byte{
		"dnf": []byte("ripgrep\t14.1.1-2.fc42\tx86_64\nfd-find\t10.2.0-1.fc42\tx86_64\nripgrep\t14.1.1-2.fc42\ti686\n"),
	}, errors: map[string]error{"dnf5": ErrNotAvailable}}
	inventory, err := (RPMAdapter{Runner: runner}).Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []InventoryItem{
		{Source: "rpm", Kind: EvidencePackage, Ref: "fd-find", DisplayName: "fd-find", Version: "10.2.0-1.fc42", Explicit: true},
		{Source: "rpm", Kind: EvidencePackage, Ref: "ripgrep", DisplayName: "ripgrep", Version: "14.1.1-2.fc42", Explicit: true},
	}
	if !reflect.DeepEqual(inventory.Items, want) || inventory.Ignored != 1 {
		t.Fatalf("inventory = %#v ignored=%d, want %#v ignored=1", inventory.Items, inventory.Ignored, want)
	}
	if len(runner.commands) != 2 || runner.commands[0].Path != "dnf5" || runner.commands[1].Path != "dnf" {
		t.Fatalf("fallback commands = %+v", runner.commands)
	}
}

func TestRPMAdapterUnavailableOnlyWhenNoSafeLedgerExists(t *testing.T) {
	runner := &fixtureRunner{outputs: map[string][]byte{}, errors: map[string]error{"dnf5": ErrNotAvailable, "dnf": ErrNotAvailable}}
	_, err := (RPMAdapter{Runner: runner}).Inventory(context.Background())
	if !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("error = %v, want ErrNotAvailable", err)
	}
}

func TestRPMAdapterReportsFailureAfterBoundedFallback(t *testing.T) {
	runner := &fixtureRunner{errors: map[string]error{
		"dnf5": &CommandError{Path: "dnf5", ExitCode: 1},
		"dnf":  &CommandError{Path: "dnf", ExitCode: 1},
	}}
	_, err := (RPMAdapter{Runner: runner}).Inventory(context.Background())
	if err == nil || errors.Is(err, ErrNotAvailable) {
		t.Fatalf("failed inventory was reported as empty or unavailable: %v", err)
	}
	if len(runner.commands) != 2 {
		t.Fatalf("inventory exceeded its two available queries: %+v", runner.commands)
	}
}

// This catches fallback attempts disappearing from both successful and failed source reports.
func TestRPMSourceReportsBoundedFallbackAttempts(t *testing.T) {
	for _, test := range []struct {
		name    string
		results []sequenceResult
		state   SourceState
		retries int
		queries int
	}{
		{name: "first success", results: []sequenceResult{{output: []byte("ripgrep\t14\tx86_64\n")}}, state: SourceSucceeded, queries: 1},
		{name: "absent then success", results: []sequenceResult{{err: ErrNotAvailable}, {output: []byte("ripgrep\t14\tx86_64\n")}}, state: SourceSucceeded, retries: 1, queries: 2},
		{name: "failed then success", results: []sequenceResult{{err: &CommandError{Path: "dnf5", ExitCode: 1}}, {}}, state: SourceSucceeded, retries: 1, queries: 2},
		{name: "exhausted", results: []sequenceResult{{err: &CommandError{Path: "dnf5", ExitCode: 1}}, {err: &CommandError{Path: "dnf", ExitCode: 2}}}, state: SourceFailed, retries: 1, queries: 2},
		{name: "both unavailable", results: []sequenceResult{{err: ErrNotAvailable}, {err: ErrNotAvailable}}, state: SourceNotAvailable, retries: 1, queries: 2},
		{name: "output bound", results: []sequenceResult{{err: &OutputLimitError{Path: "dnf5", Limit: MaxCommandOutputBytes}}}, state: SourceFailed, queries: 1},
		{name: "malformed", results: []sequenceResult{{output: []byte("malformed\n")}}, state: SourceFailed, queries: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &sequenceRunner{results: test.results}
			result, err := (Orchestrator{Adapters: []Adapter{RPMAdapter{Runner: runner}}}).Discover(context.Background(), Request{})
			if (err != nil) != (test.state != SourceSucceeded) {
				t.Fatalf("source state %s: error=%v", test.state, err)
			}
			if len(result.Sources) != 1 || result.Sources[0].State != test.state || result.Sources[0].Retries != test.retries {
				t.Errorf("source report = %+v, want state=%s retries=%d", result.Sources, test.state, test.retries)
			}
			want := []Command{
				{Path: "dnf5", Args: []string{"repoquery", "--installed", "--userinstalled", `--queryformat=%{name}\t%{evr}\t%{arch}\n`}, MaxOutputBytes: MaxCommandOutputBytes},
				{Path: "dnf", Args: []string{"repoquery", "--installed", "--userinstalled", `--qf=%{name}\t%{evr}\t%{arch}\n`}, MaxOutputBytes: MaxCommandOutputBytes},
			}
			if !reflect.DeepEqual(runner.commands, want[:test.queries]) {
				t.Fatalf("query sequence = %+v, want %+v", runner.commands, want[:test.queries])
			}
		})
	}
}

func TestRPMCancellationStopsBeforeFallback(t *testing.T) {
	for _, canceled := range []error{context.Canceled, context.DeadlineExceeded} {
		runner := &sequenceRunner{results: []sequenceResult{{err: canceled}, {}}}
		inventory, err := (RPMAdapter{Runner: runner}).Inventory(context.Background())
		if !errors.Is(err, canceled) || len(runner.commands) != 1 || inventory.Retries != 0 || len(inventory.Items) != 0 {
			t.Fatalf("cancellation tried another query or returned inventory: commands=%+v inventory=%+v err=%v", runner.commands, inventory, err)
		}
	}
}

func TestArchAdapterDiscoversExplicitPackages(t *testing.T) {
	runner := &fixtureRunner{outputs: map[string][]byte{
		"pacman": []byte("ripgrep 14.1.1-1\nfd 10.2.0-1\n"),
	}, errors: map[string]error{}}
	inventory, err := (ArchAdapter{Runner: runner}).Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []InventoryItem{
		{Source: "arch", Kind: EvidencePackage, Ref: "fd", DisplayName: "fd", Version: "10.2.0-1", Explicit: true},
		{Source: "arch", Kind: EvidencePackage, Ref: "ripgrep", DisplayName: "ripgrep", Version: "14.1.1-1", Explicit: true},
	}
	if !reflect.DeepEqual(inventory.Items, want) {
		t.Fatalf("items = %#v, want %#v", inventory.Items, want)
	}
}

func TestFlatpakAdapterFiltersAndCountsRuntimes(t *testing.T) {
	runnerWithSequence := &sequenceRunner{results: []sequenceResult{
		{output: []byte("org.mozilla.firefox\t142.0\ncom.visualstudio.code\t1.103\n")},
		{output: []byte("org.freedesktop.Platform\norg.gnome.Platform\n")},
	}}
	inventory, err := (FlatpakAdapter{Runner: runnerWithSequence}).Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Ignored != 2 || len(inventory.Items) != 2 {
		t.Fatalf("inventory = %#v ignored=%d", inventory.Items, inventory.Ignored)
	}
	if inventory.Items[0].Ref != "com.visualstudio.code" || inventory.Items[1].Ref != "org.mozilla.firefox" {
		t.Fatalf("items not stable: %#v", inventory.Items)
	}
	if len(runnerWithSequence.commands) != 2 || !reflect.DeepEqual(runnerWithSequence.commands[0].Args, []string{"list", "--app", "--columns=application,version"}) || !reflect.DeepEqual(runnerWithSequence.commands[1].Args, []string{"list", "--runtime", "--columns=application"}) {
		t.Fatalf("commands = %+v", runnerWithSequence.commands)
	}
}

func TestNativeAdaptersRejectMalformedRows(t *testing.T) {
	if _, _, err := parseRPMExplicit([]byte("bad row\n")); err == nil {
		t.Fatal("RPM parser accepted malformed output")
	}
	if _, err := parseArchExplicit([]byte("bad row with spaces\n")); err == nil {
		t.Fatal("Arch parser accepted malformed output")
	}
	if _, err := parseFlatpakApplications([]byte("not-an-app\t1\n")); err == nil {
		t.Fatal("Flatpak parser accepted malformed output")
	}
}

type sequenceResult struct {
	output []byte
	err    error
}

type sequenceRunner struct {
	results  []sequenceResult
	commands []Command
}

func (runner *sequenceRunner) Run(_ context.Context, command Command) ([]byte, error) {
	runner.commands = append(runner.commands, command)
	if len(runner.results) == 0 {
		return nil, errors.New("unexpected command")
	}
	result := runner.results[0]
	runner.results = runner.results[1:]
	return result.output, result.err
}
