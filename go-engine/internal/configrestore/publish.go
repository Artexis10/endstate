// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"errors"
	"fmt"
	"os"
)

type publicationState uint8

const (
	publicationNotDurable publicationState = iota
	publicationDurable
	publicationAmbiguous
)

// ErrPublicationAmbiguous marks a fail-stop result: publication returned an
// error and the destination could not subsequently be proven absent or equal
// to the intended immutable record. Callers must not infer that rollback is
// safe from this error.
var ErrPublicationAmbiguous = errors.New("journal publication outcome is ambiguous")

func publicationFailure(state publicationState, publishErr, reconciliationErr error) error {
	if state != publicationAmbiguous && (reconciliationErr == nil || os.IsNotExist(reconciliationErr)) {
		return publishErr
	}
	return errors.Join(
		ErrPublicationAmbiguous,
		fmt.Errorf("publish: %w", publishErr),
		fmt.Errorf("destination reconciliation: %w", reconciliationErr),
	)
}
