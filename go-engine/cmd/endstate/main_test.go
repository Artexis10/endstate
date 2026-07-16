// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseArgsPreservesRepeatableRestoreTargets(t *testing.T) {
	parsed := parseArgs([]string{
		"rebuild",
		"--from", "profile.zip",
		"--restore-target", "capture-b=instance-2",
		"--restore-target", "capture-a=instance-1",
	})

	want := []string{"capture-b=instance-2", "capture-a=instance-1"}
	if !reflect.DeepEqual(parsed.restoreTargets, want) {
		t.Fatalf("restoreTargets = %#v, want %#v", parsed.restoreTargets, want)
	}
}

func TestParseArgsPreservesMissingRestoreTargetForValidation(t *testing.T) {
	parsed := parseArgs([]string{"restore", "--restore-target", "--dry-run"})
	if !reflect.DeepEqual(parsed.restoreTargets, []string{""}) {
		t.Fatalf("restoreTargets = %#v, want one empty value", parsed.restoreTargets)
	}
	if !parsed.dryRun {
		t.Fatal("following flag was consumed as a restore target")
	}
}

func TestRestoreCapableCommandUsageAdvertisesRestoreTarget(t *testing.T) {
	for _, command := range []string{"apply", "restore", "rebuild"} {
		usage := commandUsage(command)
		if !strings.Contains(usage, "--restore-target <captureId>=<targetInstanceId>") {
			t.Fatalf("%s usage does not advertise --restore-target: %s", command, usage)
		}
	}
	if !strings.Contains(usageText, "--restore-target <m>") {
		t.Fatalf("top-level usage does not advertise repeatable --restore-target: %s", usageText)
	}
}
