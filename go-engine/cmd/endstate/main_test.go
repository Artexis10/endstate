// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/commands"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

func TestParseArgs_CaptureRepeatableDriver(t *testing.T) {
	got := parseArgs([]string{"capture", "--driver", "winget", "--driver", "chocolatey", "--json"})
	if want := []string{"winget", "chocolatey"}; !reflect.DeepEqual(got.drivers, want) {
		t.Fatalf("drivers = %v, want %v", got.drivers, want)
	}
}

func TestParseArgs_CaptureDriverRequiresValue(t *testing.T) {
	for _, args := range [][]string{
		{"capture", "--driver"},
		{"capture", "--driver", "--json"},
		{"capture", "--driver", "-h"},
	} {
		parsed := parseArgs(args)
		if !parsed.driverMissingValue {
			t.Fatalf("parseArgs(%v) did not record missing --driver value", args)
		}
		_, err := dispatch(parsed)
		if err == nil || err.Code != envelope.ErrManifestValidationError {
			t.Fatalf("dispatch(%v) error = %+v, want %s", args, err, envelope.ErrManifestValidationError)
		}
	}
}

func TestParseArgs_RebuildBootstrapFlags(t *testing.T) {
	got := parseArgs([]string{"rebuild", "--from", "machine.zip", "--bootstrap-backends", "--no-bootstrap"})
	if !got.bootstrapBackends || !got.noBootstrap {
		t.Fatalf("bootstrap flags = (%v, %v), want both parsed", got.bootstrapBackends, got.noBootstrap)
	}
}

func TestCommandUsage_MultiDriverFlags(t *testing.T) {
	for _, tc := range []struct {
		command string
		flags   []string
	}{
		{command: "capture", flags: []string{"--driver <name>"}},
		{command: "rebuild", flags: []string{"--bootstrap-backends", "--no-bootstrap"}},
	} {
		usage := commandUsage(tc.command)
		for _, flag := range tc.flags {
			if !strings.Contains(usage, flag) {
				t.Errorf("%s usage missing %q: %s", tc.command, flag, usage)
			}
		}
	}
}

func TestDispatch_ForwardsCaptureDrivers(t *testing.T) {
	orig := runCaptureFn
	defer func() { runCaptureFn = orig }()
	var captured commands.CaptureFlags
	runCaptureFn = func(flags commands.CaptureFlags) (interface{}, *envelope.Error) {
		captured = flags
		return struct{}{}, nil
	}

	parsed := parseArgs([]string{"capture", "--out", "capture.jsonc", "--driver", "winget", "--driver", "chocolatey", "--pin", "--events", "jsonl"})
	if _, eerr := dispatch(parsed); eerr != nil {
		t.Fatalf("dispatch error: %v", eerr)
	}
	if !reflect.DeepEqual(captured.Drivers, []string{"winget", "chocolatey"}) || !captured.Pin || captured.Events != "jsonl" || captured.Out != "capture.jsonc" {
		t.Fatalf("forwarded capture flags = %+v", captured)
	}
}

func TestDispatch_ForwardsRebuildBootstrapFlags(t *testing.T) {
	orig := runRebuildFn
	defer func() { runRebuildFn = orig }()
	var captured commands.RebuildFlags
	runRebuildFn = func(flags commands.RebuildFlags) (interface{}, *envelope.Error) {
		captured = flags
		return struct{}{}, nil
	}

	parsed := parseArgs([]string{"rebuild", "--from", "machine.zip", "--bootstrap-backends", "--no-bootstrap", "--dry-run"})
	if _, eerr := dispatch(parsed); eerr != nil {
		t.Fatalf("dispatch error: %v", eerr)
	}
	if captured.From != "machine.zip" || !captured.BootstrapBackends || !captured.NoBootstrap || !captured.DryRun {
		t.Fatalf("forwarded rebuild flags = %+v", captured)
	}
}
