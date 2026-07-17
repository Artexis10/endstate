// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"reflect"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

func TestParseRestoreTargetMappings(t *testing.T) {
	known := map[string]struct{}{
		"capture-a": {},
		"capture-b": {},
	}

	tests := []struct {
		name string
		raw  []string
		want map[string]string
	}{
		{name: "none", raw: nil, want: map[string]string{}},
		{
			name: "repeatable mappings",
			raw:  []string{"capture-a=instance-2", "capture-b=instance-1"},
			want: map[string]string{"capture-a": "instance-2", "capture-b": "instance-1"},
		},
		{
			name: "surrounding whitespace",
			raw:  []string{"  capture-a = instance-2  "},
			want: map[string]string{"capture-a": "instance-2"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseRestoreTargetMappings(test.raw, known)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("mappings = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestParseRestoreTargetMappingsRejectsInvalidInput(t *testing.T) {
	known := map[string]struct{}{"capture-a": {}}

	tests := []struct {
		name       string
		raw        []string
		wantReason string
		wantIndex  int
		wantID     string
	}{
		{name: "empty mapping", raw: []string{""}, wantReason: "malformed_mapping", wantIndex: 0},
		{name: "missing separator", raw: []string{"capture-a"}, wantReason: "malformed_mapping", wantIndex: 0},
		{name: "extra separator", raw: []string{"capture-a=instance=two"}, wantReason: "malformed_mapping", wantIndex: 0},
		{name: "empty capture", raw: []string{"=instance-1"}, wantReason: "empty_capture_id", wantIndex: 0},
		{name: "empty target", raw: []string{"capture-a="}, wantReason: "empty_target_instance_id", wantIndex: 0, wantID: "capture-a"},
		{name: "unknown capture", raw: []string{"capture-missing=instance-1"}, wantReason: "unknown_capture_id", wantIndex: 0, wantID: "capture-missing"},
		{name: "duplicate capture", raw: []string{"capture-a=instance-1", "capture-a=instance-2"}, wantReason: "duplicate_capture_id", wantIndex: 1, wantID: "capture-a"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseRestoreTargetMappings(test.raw, known)
			if got != nil {
				t.Fatalf("mappings = %#v, want nil", got)
			}
			if err == nil || err.Code != envelope.ErrInvalidRestoreTarget {
				t.Fatalf("error = %#v, want %s", err, envelope.ErrInvalidRestoreTarget)
			}
			detail, ok := err.Detail.(RestoreTargetErrorDetail)
			if !ok {
				t.Fatalf("detail type = %T", err.Detail)
			}
			if detail.Reason != test.wantReason || detail.Index != test.wantIndex || detail.CaptureID != test.wantID {
				t.Fatalf("detail = %+v, want reason=%q index=%d capture=%q", detail, test.wantReason, test.wantIndex, test.wantID)
			}
			if err.Remediation == "" {
				t.Fatal("remediation is empty")
			}
		})
	}
}

func TestParseRestoreTargetMappingsRejectsLegacyCaptureIDs(t *testing.T) {
	_, err := parseRestoreTargetMappings(
		[]string{"legacy-apps.git=instance-1"},
		map[string]struct{}{},
	)
	if err == nil || err.Code != envelope.ErrInvalidRestoreTarget {
		t.Fatalf("error = %#v, want %s", err, envelope.ErrInvalidRestoreTarget)
	}
	detail := err.Detail.(RestoreTargetErrorDetail)
	if detail.Reason != "unknown_capture_id" {
		t.Fatalf("reason = %q", detail.Reason)
	}
}
