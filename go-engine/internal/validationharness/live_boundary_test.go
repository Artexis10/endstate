// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"testing"
)

func TestLiveBoundaryRejectsUnexpectedClosedCategoryDelta(t *testing.T) {
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	reader := fakeLiveBoundaryReader{observation: LiveObservation{Status: LiveObservationAbsent, Ref: definition.WingetRef}, targets: map[string]liveBoundaryTargetState{}, services: []string{"existing-service"}, drivers: []string{"existing-driver"}, tasks: []string{"existing-task"}}
	before, err := snapshotLiveBoundary(context.Background(), definition, &reader)
	if err != nil {
		t.Fatal(err)
	}
	reader.services = append(reader.services, "foreign-service")
	after, err := snapshotLiveBoundary(context.Background(), definition, &reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := before.Equal(after); err == nil {
		t.Fatal("closed boundary accepted an unexpected service delta")
	}
}

type fakeLiveBoundaryReader struct {
	observation LiveObservation
	targets     map[string]liveBoundaryTargetState
	services    []string
	drivers     []string
	tasks       []string
	reboot      []string
}

func (fake *fakeLiveBoundaryReader) Observe(context.Context, LiveObserverDefinition) LiveObservation {
	return fake.observation
}
func (fake *fakeLiveBoundaryReader) Target(context.Context, LiveDeclaredTarget) (liveBoundaryTargetState, error) {
	return fake.targets[""], nil
}
func (fake *fakeLiveBoundaryReader) Services(context.Context) ([]string, error) {
	return append([]string(nil), fake.services...), nil
}
func (fake *fakeLiveBoundaryReader) Drivers(context.Context) ([]string, error) {
	return append([]string(nil), fake.drivers...), nil
}
func (fake *fakeLiveBoundaryReader) Tasks(context.Context) ([]string, error) {
	return append([]string(nil), fake.tasks...), nil
}
func (fake *fakeLiveBoundaryReader) PendingReboot(context.Context) ([]string, error) {
	return append([]string(nil), fake.reboot...), nil
}
