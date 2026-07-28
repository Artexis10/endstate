// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"fmt"
	"reflect"
	"sort"
)

type liveBoundaryTargetState struct {
	present bool
	kind    LiveDeclaredTargetKind
}

type liveBoundaryReader interface {
	Observe(context.Context, LiveObserverDefinition) LiveObservation
	Target(context.Context, LiveDeclaredTarget) (liveBoundaryTargetState, error)
	Services(context.Context) ([]string, error)
	Drivers(context.Context) ([]string, error)
	Tasks(context.Context) ([]string, error)
	PendingReboot(context.Context) ([]string, error)
}

// liveBoundarySnapshot deliberately represents only the declared Notepad++
// boundary. Equality says nothing about the rest of the host.
type liveBoundarySnapshot struct {
	observation LiveObservation
	targets     map[string]liveBoundaryTargetState
	services    []string
	drivers     []string
	tasks       []string
	reboot      []string
}

func snapshotLiveBoundary(ctx context.Context, definition LiveDefinition, reader liveBoundaryReader) (liveBoundarySnapshot, error) {
	if err := validateLiveDefinition(definition); err != nil || reader == nil {
		return liveBoundarySnapshot{}, fmt.Errorf("live boundary definition or reader is invalid")
	}
	snapshot := liveBoundarySnapshot{observation: reader.Observe(ctx, definition.Observer), targets: make(map[string]liveBoundaryTargetState, len(definition.DeclaredTargets))}
	if snapshot.observation.Status == LiveObservationFailed || snapshot.observation.Ref != definition.WingetRef {
		return liveBoundarySnapshot{}, fmt.Errorf("live package boundary observation failed")
	}
	for _, target := range definition.DeclaredTargets {
		state, err := reader.Target(ctx, target)
		if err != nil || state.kind != "" && state.kind != target.Kind {
			return liveBoundarySnapshot{}, fmt.Errorf("live declared target observation failed")
		}
		state.kind = target.Kind
		snapshot.targets[target.Identity] = state
	}
	var err error
	if snapshot.services, err = canonicalLiveBoundaryNames(reader.Services(ctx)); err != nil {
		return liveBoundarySnapshot{}, fmt.Errorf("live service boundary observation failed")
	}
	if snapshot.drivers, err = canonicalLiveBoundaryNames(reader.Drivers(ctx)); err != nil {
		return liveBoundarySnapshot{}, fmt.Errorf("live driver boundary observation failed")
	}
	if snapshot.tasks, err = canonicalLiveBoundaryNames(reader.Tasks(ctx)); err != nil {
		return liveBoundarySnapshot{}, fmt.Errorf("live task boundary observation failed")
	}
	if snapshot.reboot, err = canonicalLiveBoundaryNames(reader.PendingReboot(ctx)); err != nil {
		return liveBoundarySnapshot{}, fmt.Errorf("live reboot boundary observation failed")
	}
	return snapshot, nil
}

func canonicalLiveBoundaryNames(values []string, err error) ([]string, error) {
	if err != nil || len(values) > maxLiveObserverRecords {
		return nil, fmt.Errorf("boundary category is invalid")
	}
	result := append([]string(nil), values...)
	for _, value := range result {
		if !validLiveObserverValue(value) {
			return nil, fmt.Errorf("boundary category member is invalid")
		}
	}
	sort.Strings(result)
	for index := 1; index < len(result); index++ {
		if result[index-1] == result[index] {
			return nil, fmt.Errorf("boundary category contains duplicate")
		}
	}
	return result, nil
}

func (before liveBoundarySnapshot) Equal(after liveBoundarySnapshot) error {
	if before.observation != after.observation || !reflect.DeepEqual(before.targets, after.targets) || !reflect.DeepEqual(before.services, after.services) || !reflect.DeepEqual(before.drivers, after.drivers) || !reflect.DeepEqual(before.tasks, after.tasks) || !reflect.DeepEqual(before.reboot, after.reboot) {
		return fmt.Errorf("declared live boundary changed")
	}
	return nil
}

func (snapshot liveBoundarySnapshot) RequireAbsent() error {
	if snapshot.observation.Status != LiveObservationAbsent {
		return fmt.Errorf("package boundary is not absent")
	}
	for _, state := range snapshot.targets {
		if state.present {
			return fmt.Errorf("declared target is present")
		}
	}
	return nil
}
