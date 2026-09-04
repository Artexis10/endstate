// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type fixtureRunner struct {
	outputs  map[string][]byte
	errors   map[string]error
	commands []Command
}

func (runner *fixtureRunner) Run(_ context.Context, command Command) ([]byte, error) {
	runner.commands = append(runner.commands, command)
	if err := runner.errors[command.Path]; err != nil {
		return nil, err
	}
	return append([]byte(nil), runner.outputs[command.Path]...), nil
}

func debianFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "debian", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDebianAdapterDiscoversExplicitPackagesAndCountsDependencies(t *testing.T) {
	runner := &fixtureRunner{outputs: map[string][]byte{
		"apt-mark":   debianFixture(t, "apt-mark-showmanual.txt"),
		"dpkg-query": debianFixture(t, "dpkg-query.txt"),
	}, errors: map[string]error{}}
	adapter := DebianAdapter{Runner: runner}
	inventory, err := adapter.Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if adapter.ID() != "debian-explicit" {
		t.Fatalf("ID = %q", adapter.ID())
	}
	if inventory.Ignored != 4 {
		t.Fatalf("ignored dependency/system packages = %d, want 4", inventory.Ignored)
	}
	want := []InventoryItem{
		{Source: "debian", Kind: EvidencePackage, Ref: "fd-find", DisplayName: "fd-find", Version: "9.0.0-2", Explicit: true},
		{Source: "debian", Kind: EvidencePackage, Ref: "ripgrep", DisplayName: "ripgrep", Version: "14.1.0-1", Explicit: true},
	}
	if !reflect.DeepEqual(inventory.Items, want) {
		t.Fatalf("items = %#v, want %#v", inventory.Items, want)
	}
	if len(runner.commands) != 2 || runner.commands[0].Path != "apt-mark" || !reflect.DeepEqual(runner.commands[0].Args, []string{"showmanual"}) {
		t.Fatalf("commands = %+v", runner.commands)
	}
	if runner.commands[1].Path != "dpkg-query" || !reflect.DeepEqual(runner.commands[1].Args, []string{"--show", `--showformat=${binary:Package}\t${Version}\t${db:Status-Abbrev}\t${Essential}\t${Priority}\t${Section}\n`}) {
		t.Fatalf("dpkg command = %+v", runner.commands[1])
	}
}

func TestDebianAdapterMissingOrFailedSourceIsExplicit(t *testing.T) {
	for name, testCase := range map[string]struct {
		err          error
		notAvailable bool
	}{
		"missing apt": {err: ErrNotAvailable, notAvailable: true},
		"failed apt":  {err: errors.New("permission denied")},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fixtureRunner{outputs: map[string][]byte{}, errors: map[string]error{"apt-mark": testCase.err}}
			_, err := (DebianAdapter{Runner: runner}).Inventory(context.Background())
			if err == nil {
				t.Fatal("Inventory accepted unreadable apt source")
			}
			if errors.Is(err, ErrNotAvailable) != testCase.notAvailable {
				t.Fatalf("error = %v, notAvailable = %v", err, errors.Is(err, ErrNotAvailable))
			}
		})
	}
}

func TestDebianAdapterRejectsMalformedRows(t *testing.T) {
	runner := &fixtureRunner{outputs: map[string][]byte{
		"apt-mark":   []byte("ripgrep\nnot a package\n"),
		"dpkg-query": debianFixture(t, "dpkg-query.txt"),
	}, errors: map[string]error{}}
	if _, err := (DebianAdapter{Runner: runner}).Inventory(context.Background()); err == nil {
		t.Fatal("Inventory accepted malformed apt-mark output")
	}
}
