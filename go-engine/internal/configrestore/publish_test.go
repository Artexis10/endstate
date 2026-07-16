// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"errors"
	"os"
	"testing"
)

func TestPublicationFailureDistinguishesProvenAbsenceFromAmbiguity(t *testing.T) {
	publishErr := errors.New("publish failed")
	if got := publicationFailure(publishErr, os.ErrNotExist); !errors.Is(got, publishErr) ||
		errors.Is(got, ErrPublicationAmbiguous) {
		t.Fatalf("proven-absent publication error = %v", got)
	}
	reconcileErr := errors.New("reconciliation failed")
	if got := publicationFailure(publishErr, reconcileErr); !errors.Is(got, publishErr) ||
		!errors.Is(got, reconcileErr) || !errors.Is(got, ErrPublicationAmbiguous) {
		t.Fatalf("ambiguous publication error = %v", got)
	}
}
