// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import "fmt"

// bindHostedLiveStorageRoot accepts only Task 2's registered owned attempt
// root and rechecks its object identity before every storage inspection.
func bindHostedLiveStorageRoot(attempt windowsLiveAttemptRoot) (hostedLiveStorageRoot, error) {
	if !attempt.valid() {
		return hostedLiveStorageRoot{}, fmt.Errorf("hosted live storage root is not the owned attempt root")
	}
	return hostedLiveStorageRoot{path: attempt.path, validate: func(path string) error {
		if path != attempt.path || !attempt.valid() {
			return fmt.Errorf("hosted live storage root identity changed")
		}
		object, err := windowsLiveObjectIdentityForPath(path, true)
		if err != nil || object != attempt.object {
			return fmt.Errorf("hosted live storage root identity changed")
		}
		return nil
	}}, nil
}
