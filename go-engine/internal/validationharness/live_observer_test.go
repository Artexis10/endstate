// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"testing"
)

type fakeLiveProcess struct {
	result LiveProcessResult
	err    error
	name   string
	args   []string
}

func (f *fakeLiveProcess) Run(_ context.Context, name string, args ...string) (LiveProcessResult, error) {
	f.name, f.args = name, append([]string(nil), args...)
	return f.result, f.err
}

type fakeLiveRegistry struct {
	records []LiveUninstallRecord
	err     error
}

func (f fakeLiveRegistry) UninstallRecords(context.Context) ([]LiveUninstallRecord, error) {
	return f.records, f.err
}

type fakeLivePath struct {
	entries []string
	err     error
	calls   *int
}

func (f fakeLivePath) MachineAndUserPath(context.Context) ([]string, error) {
	if f.calls != nil {
		*f.calls++
	}
	return f.entries, f.err
}

type fakeLiveFiles struct {
	files    map[string]LiveFileInfo
	versions map[string]string
}

func (f fakeLiveFiles) Stat(path string) (LiveFileInfo, error) {
	value, ok := f.files[path]
	if !ok {
		return LiveFileInfo{}, fs.ErrNotExist
	}
	return value, nil
}

func (f fakeLiveFiles) FileVersion(path string) (string, error) {
	value, ok := f.versions[path]
	if !ok {
		return "", errors.New("version absent")
	}
	return value, nil
}

func observerDefinition() LiveObserverDefinition {
	return LiveObserverDefinition{
		WingetRef:            "Vendor.Fixture",
		UninstallDisplayName: []string{"^Fixture$"},
		ExecutableNames:      []string{"fixture.exe"},
	}
}

func wingetRow(id, version, source string) string {
	return fmt.Sprintf("%-32s %-31s %-14s %s\n", "Fixture", id, version, source)
}

const wingetTable = "Name                            Id                                Version        Source\n" +
	"-------------------------------- ------------------------------- -------------- ------\n"

func TestObserveLiveExactWingetCommandAndTable(t *testing.T) {
	process := &fakeLiveProcess{result: LiveProcessResult{ExitCode: 0, Stdout: []byte(wingetTable + wingetRow("Vendor.Fixture", "v1.02.0", "winget"))}}
	observer := LiveObserver{Process: process, Registry: fakeLiveRegistry{}, Path: fakeLivePath{}, Files: fakeLiveFiles{}}
	result := observer.Observe(context.Background(), observerDefinition())
	if result.WingetPresent != true || result.WingetVersion != "1.2" || result.Status != LiveObservationMixed {
		t.Fatalf("result = %+v", result)
	}
	if process.name != "winget" || !reflect.DeepEqual(process.args, []string{"list", "--id", "Vendor.Fixture", "--exact", "--source", "winget", "--accept-source-agreements", "--disable-interactivity"}) {
		t.Fatalf("command = %q %q", process.name, process.args)
	}
}

func TestParseLiveWingetTableRejectsAmbiguousSimilarAndDrift(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"zero", wingetTable},
		{"multiple", wingetTable + wingetRow("Vendor.Fixture", "1.2", "winget") + wingetRow("Vendor.Fixture", "1.2", "winget")},
		{"similar prefix", wingetTable + wingetRow("Vendor.Fixture.Other", "1.2", "winget")},
		{"localized headers", "Nombre                          Identificador                     Versión        Origen\n" + wingetTable[strings.Index(wingetTable, "-"):]},
		{"truncated", wingetTable + "Fixture                         Vendor.Fixture\n"},
		{"control", wingetTable + wingetRow("Vendor.Fixture", "1.2\x01", "winget")},
		{"id shifted from second column", wingetTable + fmt.Sprintf("%-32s %-31s %-14s %s\n", "Fixture", "not-the-id", "Vendor.Fixture", "winget")},
		{"source not final column", wingetTable + fmt.Sprintf("%-32s %-31s %-14s %s\n", "winget", "Vendor.Fixture", "1.2", "not-source")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseLiveWingetTable([]byte(test.body), "Vendor.Fixture"); err == nil {
				t.Fatal("ParseLiveWingetTable() error = nil")
			}
		})
	}
}

func TestObserveLiveRegistryExecutableAndVersionReconcile(t *testing.T) {
	path := `C:\Program Files\Fixture\fixture.exe`
	observer := LiveObserver{
		Process:  &fakeLiveProcess{result: LiveProcessResult{ExitCode: 0, Stdout: []byte(wingetTable + wingetRow("Vendor.Fixture", "1.2.0", "winget"))}},
		Registry: fakeLiveRegistry{records: []LiveUninstallRecord{{View: LiveRegistryHKLM64, DisplayName: "Fixture", DisplayVersion: "v1.02", InstallLocation: `C:\Program Files\Fixture`, DisplayIcon: `"C:\Program Files\Fixture\fixture.exe"`}}},
		Path:     fakeLivePath{entries: []string{`C:\stale`}},
		Files:    fakeLiveFiles{files: map[string]LiveFileInfo{path: {Regular: true}}, versions: map[string]string{path: "1.2.0.0"}},
	}
	result := observer.Observe(context.Background(), observerDefinition())
	if result.Status != LiveObservationPresent || !result.RegistryPresent || !result.ExecutablePresent || result.RegistryVersion != "1.2" || result.ExecutableVersion != "1.2" {
		t.Fatalf("result = %+v", result)
	}
}

func TestObserveLiveFailsClosedForAmbiguityStalePathAndUnsafeExecutable(t *testing.T) {
	definition := observerDefinition()
	process := &fakeLiveProcess{result: LiveProcessResult{ExitCode: 0, Stdout: []byte(wingetTable + wingetRow("Vendor.Fixture", "1.2", "winget"))}}
	observer := LiveObserver{Process: process, Registry: fakeLiveRegistry{}, Path: fakeLivePath{}, Files: fakeLiveFiles{}}
	tests := []struct {
		name     string
		registry []LiveUninstallRecord
		path     []string
		files    map[string]LiveFileInfo
		want     LiveObservationStatus
	}{
		{"ambiguous registry", []LiveUninstallRecord{{DisplayName: "Fixture", DisplayVersion: "1.2"}, {DisplayName: "Fixture", DisplayVersion: "2.0"}}, nil, nil, LiveObservationAmbiguous},
		{"stale process path is ignored", []LiveUninstallRecord{{DisplayName: "Fixture", DisplayVersion: "1.2"}}, []string{`C:\fresh`}, map[string]LiveFileInfo{`C:\stale\fixture.exe`: {Regular: true}}, LiveObservationMixed},
		{"reparse executable is rejected", []LiveUninstallRecord{{DisplayName: "Fixture", DisplayVersion: "1.2", InstallLocation: `C:\Fixture`}}, nil, map[string]LiveFileInfo{`C:\Fixture\fixture.exe`: {Regular: true, ReparsePoint: true}}, LiveObservationMixed},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			observer.Registry = fakeLiveRegistry{records: test.registry}
			observer.Path = fakeLivePath{entries: test.path}
			observer.Files = fakeLiveFiles{files: test.files, versions: map[string]string{}}
			if result := observer.Observe(context.Background(), definition); result.Status != test.want {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}

func TestObserveLiveRejectsMatchingRecordsWithDistinctRegistryIdentity(t *testing.T) {
	observer := LiveObserver{
		Process: &fakeLiveProcess{result: LiveProcessResult{ExitCode: 0, Stdout: []byte(wingetTable + wingetRow("Vendor.Fixture", "1.2", "winget"))}},
		Registry: fakeLiveRegistry{records: []LiveUninstallRecord{
			{View: LiveRegistryHKLM64, KeyIdentity: `{fixture-64}`, DisplayName: "Fixture", DisplayVersion: "1.2", InstallLocation: `C:\Fixture`},
			{View: LiveRegistryHKLM32, KeyIdentity: `{fixture-32}`, DisplayName: "Fixture", DisplayVersion: "1.2", InstallLocation: `C:\Fixture`},
		}},
		Path:  fakeLivePath{},
		Files: fakeLiveFiles{},
	}
	if result := observer.Observe(context.Background(), observerDefinition()); result.Status != LiveObservationAmbiguous {
		t.Fatalf("result = %+v", result)
	}
}

func TestObserveLiveAbsentMixedAndVersionMismatch(t *testing.T) {
	definition := observerDefinition()
	allAbsent := LiveObserver{Process: &fakeLiveProcess{result: LiveProcessResult{ExitCode: 2, Classification: LiveProcessNoInstalled}}, Registry: fakeLiveRegistry{}, Path: fakeLivePath{}, Files: fakeLiveFiles{}}
	if result := allAbsent.Observe(context.Background(), definition); result.Status != LiveObservationAbsent {
		t.Fatalf("absent result = %+v", result)
	}

	path := `C:\Fixture\fixture.exe`
	mismatch := LiveObserver{
		Process:  &fakeLiveProcess{result: LiveProcessResult{ExitCode: 0, Stdout: []byte(wingetTable + wingetRow("Vendor.Fixture", "1.2", "winget"))}},
		Registry: fakeLiveRegistry{records: []LiveUninstallRecord{{DisplayName: "Fixture", DisplayVersion: "1.2", InstallLocation: `C:\Fixture`}}},
		Path:     fakeLivePath{}, Files: fakeLiveFiles{files: map[string]LiveFileInfo{path: {Regular: true}}, versions: map[string]string{path: "1.3"}},
	}
	if result := mismatch.Observe(context.Background(), definition); result.Status != LiveObservationVersionMismatch {
		t.Fatalf("mismatch result = %+v", result)
	}
}

func TestObserveLiveFailsClosedWhenAbsentRegistryStillHasPathError(t *testing.T) {
	pathCalls := 0
	observer := LiveObserver{
		Process:  &fakeLiveProcess{result: LiveProcessResult{ExitCode: 2, Classification: LiveProcessNoInstalled}},
		Registry: fakeLiveRegistry{},
		Path:     fakeLivePath{err: errors.New("fresh path unavailable"), calls: &pathCalls},
		Files:    fakeLiveFiles{},
	}
	if result := observer.Observe(context.Background(), observerDefinition()); result.Status != LiveObservationFailed {
		t.Fatalf("result = %+v", result)
	}
	if pathCalls != 1 {
		t.Fatalf("PATH calls = %d, want 1", pathCalls)
	}
}

func TestObserveLiveMarksUnboundPathExecutableAsMixedNotAbsent(t *testing.T) {
	shadow := `C:\Shadow\fixture.exe`
	observer := LiveObserver{
		Process:  &fakeLiveProcess{result: LiveProcessResult{ExitCode: 2316632084, Classification: LiveProcessNoInstalled}},
		Registry: fakeLiveRegistry{},
		Path:     fakeLivePath{entries: []string{`C:\Shadow`}},
		Files:    fakeLiveFiles{files: map[string]LiveFileInfo{shadow: {Regular: false}}},
	}
	result := observer.Observe(context.Background(), observerDefinition())
	if result.Status != LiveObservationMixed || result.ExecutablePresent {
		t.Fatalf("result = %+v", result)
	}
}

func TestObserveLiveFailsClosedForUnsafeUnboundPathEntries(t *testing.T) {
	for _, entry := range []string{"", ".", `relative\\bin`, `C:relative`} {
		t.Run(fmt.Sprintf("%q", entry), func(t *testing.T) {
			observer := LiveObserver{
				Process:  &fakeLiveProcess{result: LiveProcessResult{ExitCode: 2316632084, Classification: LiveProcessNoInstalled}},
				Registry: fakeLiveRegistry{},
				Path:     fakeLivePath{entries: []string{entry}},
				Files:    fakeLiveFiles{},
			}
			if result := observer.Observe(context.Background(), observerDefinition()); result.Status != LiveObservationFailed {
				t.Fatalf("result = %+v", result)
			}
		})
	}
}

func TestObserveLiveRejectsExecutableOutsideTrustedUninstallRoots(t *testing.T) {
	outside := `C:\Elsewhere\fixture.exe`
	observer := LiveObserver{
		Process:  &fakeLiveProcess{result: LiveProcessResult{ExitCode: 0, Stdout: []byte(wingetTable + wingetRow("Vendor.Fixture", "1.2", "winget"))}},
		Registry: fakeLiveRegistry{records: []LiveUninstallRecord{{DisplayName: "Fixture", DisplayVersion: "1.2", InstallLocation: `C:\Fixture`}}},
		Path:     fakeLivePath{entries: []string{`C:\Elsewhere`}},
		Files:    fakeLiveFiles{files: map[string]LiveFileInfo{outside: {Regular: true}}, versions: map[string]string{outside: "1.2"}},
	}
	if result := observer.Observe(context.Background(), observerDefinition()); result.Status != LiveObservationMixed || result.ExecutablePresent {
		t.Fatalf("result = %+v", result)
	}
}

func TestNormalizeLiveVersionRejectsNonNumericAndBounds(t *testing.T) {
	for raw, want := range map[string]string{"v001.020.000": "1.20", "1.2.0": "1.2"} {
		if got, err := NormalizeLiveVersion(raw); err != nil || got != want {
			t.Fatalf("NormalizeLiveVersion(%q) = %q, %v", raw, got, err)
		}
	}
	for _, raw := range []string{"", "v", "1.2-beta", "1..2", string(make([]byte, maxLiveStringBytes+1))} {
		if _, err := NormalizeLiveVersion(raw); err == nil {
			t.Fatalf("NormalizeLiveVersion(%q) error = nil", raw)
		}
	}
}
