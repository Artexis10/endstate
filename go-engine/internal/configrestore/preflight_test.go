// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/migration"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

type recordingObserver struct {
	names []string
	err   error
	calls int
}

func (o *recordingObserver) RunningProcessBasenames(context.Context) ([]string, error) {
	o.calls++
	return append([]string(nil), o.names...), o.err
}

func TestMaterializeReturnsTypedAppRunningFromDeclaredBasenameGlob(t *testing.T) {
	stageRoot := t.TempDir()
	hostRoot := t.TempDir()
	generation := modules.GenerationDef{ID: "g2", RequiresAppClosed: true}
	observer := &recordingObserver{names: []string{"explorer.exe", "PHOTOSHOP.EXE"}}

	_, err := Materialize(context.Background(), Request{
		Stage:           &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"},
		Plan:            testPlan(hostRoot, generation),
		ProcessPatterns: []string{"Photoshop*.exe"},
		ProcessObserver: observer,
	})
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != CodeAppRunning || typed.ProcessBasename != "PHOTOSHOP.EXE" {
		t.Fatalf("error = %#v, want typed app_running for Photoshop", err)
	}
	if observer.calls != 1 {
		t.Fatalf("observer calls = %d, want 1", observer.calls)
	}
}

func TestMaterializeFailsClosedWhenClosureDeclarationCannotBeObserved(t *testing.T) {
	for _, test := range []struct {
		name     string
		patterns []string
		observer ProcessObserver
		code     Code
	}{
		{name: "missing patterns", observer: &recordingObserver{}, code: CodeAppClosureConfig},
		{name: "missing observer", patterns: []string{"photoshop.exe"}, code: CodeAppClosureConfig},
		{name: "path not basename", patterns: []string{`C:\Program Files\Adobe\Photoshop.exe`}, observer: &recordingObserver{}, code: CodeAppClosureConfig},
		{name: "observer failure", patterns: []string{"photoshop.exe"}, observer: &recordingObserver{err: fmt.Errorf("process snapshot failed")}, code: CodeProcessObservation},
	} {
		t.Run(test.name, func(t *testing.T) {
			stageRoot := t.TempDir()
			hostRoot := t.TempDir()
			generation := modules.GenerationDef{ID: "g2", RequiresAppClosed: true}
			_, err := Materialize(context.Background(), Request{
				Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"}, Plan: testPlan(hostRoot, generation),
				ProcessPatterns: test.patterns, ProcessObserver: test.observer,
			})
			if CodeOf(err) != test.code {
				t.Fatalf("error = %v, code = %q, want %q", err, CodeOf(err), test.code)
			}
			if CodeOf(err) == CodeAppRunning {
				t.Fatal("configuration/observation failure must not be fabricated as app_running")
			}
		})
	}
}

func TestMaterializeDoesNotObserveProcessesWhenClosureIsNotRequired(t *testing.T) {
	stageRoot := t.TempDir()
	hostRoot := t.TempDir()
	observer := &recordingObserver{names: []string{"photoshop.exe"}}
	generation := modules.GenerationDef{ID: "g2"}

	if _, err := Materialize(context.Background(), Request{
		Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"}, Plan: testPlan(hostRoot, generation),
		ProcessPatterns: []string{"photoshop.exe"}, ProcessObserver: observer,
	}); err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if observer.calls != 0 {
		t.Fatalf("observer calls = %d, want 0", observer.calls)
	}
}

func TestAppClosurePreflightMatchesOnlyDeclaredPatterns(t *testing.T) {
	observer := &recordingObserver{names: []string{"photoshop.exe", "notepad.exe"}}
	err := PreflightAppClosure(context.Background(), true, []string{"illustrator.exe"}, observer)
	if err != nil {
		t.Fatalf("PreflightAppClosure() error = %v", err)
	}
	if observer.calls != 1 {
		t.Fatalf("observer calls = %d, want 1", observer.calls)
	}
}
