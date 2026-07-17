// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import "testing"

func TestNewVersionEvidence_PreservesRawAndNormalizesNumericDotted(t *testing.T) {
	tests := []struct {
		raw        string
		normalized string
		parseable  bool
	}{
		{" 027.004.0 ", "27.4", true},
		{"1.0.0", "1", true},
		{"0.0", "0", true},
		{"2026.07.16.12", "2026.7.16.12", true},
		{"v27.4", "", false},
		{"27..4", "", false},
		{"27.4-beta", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			evidence := NewVersionEvidence(tt.raw)
			if evidence.Raw != tt.raw {
				t.Errorf("Raw = %q, want exact %q", evidence.Raw, tt.raw)
			}
			if evidence.Normalized != tt.normalized || evidence.Numeric != tt.parseable {
				t.Errorf("evidence = %+v, want normalized=%q numeric=%v", evidence, tt.normalized, tt.parseable)
			}
		})
	}
}

func TestCompareNumericVersions(t *testing.T) {
	tests := []struct {
		left, right string
		want        int
		wantErr     bool
	}{
		{"1.2", "1.2.0", 0, false},
		{"1.10", "1.2", 1, false},
		{"001.0002", "1.2", 0, false},
		{"999999999999999999999", "2", 1, false},
		{"2.0.0.1", "2", 1, false},
		{"v2", "2", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.left+"_"+tt.right, func(t *testing.T) {
			got, err := CompareNumericVersions(tt.left, tt.right)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("CompareNumericVersions(%q, %q) = %d, want %d", tt.left, tt.right, got, tt.want)
			}
		})
	}
}

func TestMatchNumericVersionRange(t *testing.T) {
	tests := []struct {
		version, expression string
		want                bool
		wantErr             bool
	}{
		{"27.4", ">=25 <28", true, false},
		{"28", ">=25 <28", false, false},
		{"28.0", ">=28 <=29.5", true, false},
		{"27", "==27.0", true, false},
		{"27", "!=27", false, false},
		{"27", "27", true, false},
		{"27", ">=x", false, true},
		{"27", ">=25 || <20", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.expression, func(t *testing.T) {
			got, err := MatchNumericVersionRange(tt.version, tt.expression)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("match = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelectGeneration_ExactlyOneSemantics(t *testing.T) {
	set := ConfigSetDef{
		ID: "preferences",
		Generations: []GenerationDef{
			{ID: "g1", Order: 1, Matches: []VersionSelectorDef{{VersionRange: ">=25 <28"}}},
			{ID: "g2", Order: 2, Matches: []VersionSelectorDef{{VersionPattern: "^v28(?:\\.[0-9]+)*$"}}},
		},
	}

	got, err := SelectGeneration(&set, NewVersionEvidence("27.4"))
	if err != nil || got == nil || got.ID != "g1" {
		t.Fatalf("SelectGeneration(27.4) = %+v, %v; want g1", got, err)
	}
	got, err = SelectGeneration(&set, NewVersionEvidence("v28.2"))
	if err != nil || got == nil || got.ID != "g2" {
		t.Fatalf("SelectGeneration(v28.2) = %+v, %v; want g2", got, err)
	}

	_, err = SelectGeneration(&set, NewVersionEvidence("24"))
	if err == nil || GenerationMatchCode(err) != GenerationUnknown {
		t.Fatalf("unknown error = %v, want %s", err, GenerationUnknown)
	}

	set.Generations = append(set.Generations, GenerationDef{ID: "overlap", Order: 3, Matches: []VersionSelectorDef{{VersionRange: ">=27 <30"}}})
	_, err = SelectGeneration(&set, NewVersionEvidence("27.4"))
	if err == nil || GenerationMatchCode(err) != GenerationAmbiguous {
		t.Fatalf("ambiguous error = %v, want %s", err, GenerationAmbiguous)
	}
}

func TestSelectGeneration_SingleSelectorlessGenerationIsCatchAll(t *testing.T) {
	set := ConfigSetDef{ID: "stable", Generations: []GenerationDef{{ID: "g1", Order: 1}}}
	got, err := SelectGeneration(&set, NewVersionEvidence("vendor-edition"))
	if err != nil || got == nil || got.ID != "g1" {
		t.Fatalf("SelectGeneration = %+v, %v; want stable g1", got, err)
	}
}
