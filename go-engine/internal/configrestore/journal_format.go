// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
)

const (
	journalIntentFormat  = "endstate.config-restore-intent"
	journalMarkerFormat  = "endstate.config-restore-marker"
	journalFormatVersion = 1
)

type journalIntentIdentity struct {
	Format           string              `json:"format"`
	Version          int                 `json:"version"`
	State            JournalState        `json:"state"`
	Lineage          JournalLineage      `json:"lineage"`
	Actions          []JournalAction     `json:"actions"`
	Validations      []JournalValidation `json:"validations"`
	ValidationStatus ValidationStatus    `json:"validationStatus"`
	RollbackOutcome  RollbackOutcome     `json:"rollbackOutcome"`
}

type journalMarkerIdentity struct {
	Format           string           `json:"format"`
	Version          int              `json:"version"`
	IntentDigest     string           `json:"intentDigest"`
	State            JournalState     `json:"state"`
	ValidationStatus ValidationStatus `json:"validationStatus"`
	RollbackOutcome  RollbackOutcome  `json:"rollbackOutcome"`
}

type journalMarkerDisk struct {
	Format           string           `json:"format"`
	Version          int              `json:"version"`
	IntentDigest     string           `json:"intentDigest"`
	State            JournalState     `json:"state"`
	ValidationStatus ValidationStatus `json:"validationStatus"`
	RollbackOutcome  RollbackOutcome  `json:"rollbackOutcome"`
	MarkerDigest     string           `json:"markerDigest"`
}

type journalIntentDisk struct {
	Format           string              `json:"format"`
	Version          int                 `json:"version"`
	State            JournalState        `json:"state"`
	Lineage          JournalLineage      `json:"lineage"`
	Actions          []JournalAction     `json:"actions"`
	Validations      []JournalValidation `json:"validations"`
	ValidationStatus ValidationStatus    `json:"validationStatus"`
	RollbackOutcome  RollbackOutcome     `json:"rollbackOutcome"`
	IntentDigest     string              `json:"intentDigest"`
}

func newJournalIntentDisk(lineage JournalLineage, actions []JournalAction, validations []JournalValidation) (journalIntentDisk, []byte, error) {
	identity := journalIntentIdentity{
		Format: journalIntentFormat, Version: journalFormatVersion, State: JournalPending,
		Lineage: cloneJournalLineage(lineage), Actions: cloneJournalActions(actions),
		Validations:      append([]JournalValidation{}, validations...),
		ValidationStatus: ValidationPending, RollbackOutcome: RollbackNotAttempted,
	}
	identityBytes, err := json.Marshal(identity)
	if err != nil {
		return journalIntentDisk{}, nil, err
	}
	digestBytes := sha256.Sum256(identityBytes)
	disk := journalIntentDisk{
		Format: identity.Format, Version: identity.Version, State: identity.State,
		Lineage: identity.Lineage, Actions: identity.Actions, Validations: identity.Validations,
		ValidationStatus: identity.ValidationStatus, RollbackOutcome: identity.RollbackOutcome,
		IntentDigest: hex.EncodeToString(digestBytes[:]),
	}
	encoded, err := json.Marshal(disk)
	if err != nil {
		return journalIntentDisk{}, nil, err
	}
	return disk, append(encoded, '\n'), nil
}

func decodeJournalIntent(data []byte) (journalIntentDisk, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var disk journalIntentDisk
	if err := decoder.Decode(&disk); err != nil {
		return journalIntentDisk{}, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return journalIntentDisk{}, err
	}
	if disk.Format != journalIntentFormat || disk.Version != journalFormatVersion || disk.State != JournalPending ||
		disk.ValidationStatus != ValidationPending || disk.RollbackOutcome != RollbackNotAttempted {
		return journalIntentDisk{}, fmt.Errorf("unsupported or inconsistent journal intent header")
	}
	expected, canonical, err := newJournalIntentDisk(disk.Lineage, disk.Actions, disk.Validations)
	if err != nil {
		return journalIntentDisk{}, err
	}
	if disk.IntentDigest != expected.IntentDigest {
		return journalIntentDisk{}, fmt.Errorf("journal intent digest mismatch")
	}
	if !bytes.Equal(data, canonical) {
		return journalIntentDisk{}, fmt.Errorf("journal intent is not canonical")
	}
	return disk, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected trailing JSON value")
		}
		return err
	}
	return nil
}

func intentFromDisk(root, path string, disk journalIntentDisk) *JournalIntent {
	return &JournalIntent{
		transactionRoot: root, path: path, digest: disk.IntentDigest,
		lineage: cloneJournalLineage(disk.Lineage), actions: cloneJournalActions(disk.Actions),
		validations: append([]JournalValidation{}, disk.Validations...),
	}
}

func newJournalMarkerDisk(
	intentDigest string,
	state JournalState,
	validation ValidationStatus,
	rollback RollbackOutcome,
) (journalMarkerDisk, []byte, error) {
	identity := journalMarkerIdentity{
		Format: journalMarkerFormat, Version: journalFormatVersion, IntentDigest: intentDigest,
		State: state, ValidationStatus: validation, RollbackOutcome: rollback,
	}
	identityBytes, err := json.Marshal(identity)
	if err != nil {
		return journalMarkerDisk{}, nil, err
	}
	digestBytes := sha256.Sum256(identityBytes)
	disk := journalMarkerDisk{
		Format: identity.Format, Version: identity.Version, IntentDigest: identity.IntentDigest,
		State: identity.State, ValidationStatus: identity.ValidationStatus, RollbackOutcome: identity.RollbackOutcome,
		MarkerDigest: hex.EncodeToString(digestBytes[:]),
	}
	encoded, err := json.Marshal(disk)
	if err != nil {
		return journalMarkerDisk{}, nil, err
	}
	return disk, append(encoded, '\n'), nil
}

func decodeJournalMarker(data []byte) (journalMarkerDisk, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var disk journalMarkerDisk
	if err := decoder.Decode(&disk); err != nil {
		return journalMarkerDisk{}, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return journalMarkerDisk{}, err
	}
	if disk.Format != journalMarkerFormat || disk.Version != journalFormatVersion || !isLowerHexDigest(disk.IntentDigest) {
		return journalMarkerDisk{}, fmt.Errorf("unsupported or inconsistent journal marker header")
	}
	if err := validateMarkerOutcome(disk.State, disk.ValidationStatus, disk.RollbackOutcome); err != nil {
		return journalMarkerDisk{}, err
	}
	expected, canonical, err := newJournalMarkerDisk(
		disk.IntentDigest, disk.State, disk.ValidationStatus, disk.RollbackOutcome,
	)
	if err != nil {
		return journalMarkerDisk{}, err
	}
	if disk.MarkerDigest != expected.MarkerDigest {
		return journalMarkerDisk{}, fmt.Errorf("journal marker digest mismatch")
	}
	if !bytes.Equal(data, canonical) {
		return journalMarkerDisk{}, fmt.Errorf("journal marker is not canonical")
	}
	return disk, nil
}

func validateMarkerOutcome(state JournalState, validation ValidationStatus, rollback RollbackOutcome) error {
	switch state {
	case JournalCommitted:
		if validation != ValidationPassed || rollback != RollbackNotRequired {
			return fmt.Errorf("committed marker must record passed validation and no required rollback")
		}
	case JournalRolledBack:
		if validation != ValidationNotRun && validation != ValidationPassed && validation != ValidationFailed {
			return fmt.Errorf("rolled-back marker has invalid validation status %q", validation)
		}
		switch rollback {
		case RollbackSucceeded:
		case RollbackNotRequired:
			if validation != ValidationNotRun {
				return fmt.Errorf("no-mutation marker must record validation not run")
			}
		default:
			return fmt.Errorf("rolled-back marker must record proven or unnecessary rollback")
		}
	default:
		return fmt.Errorf("unsupported terminal journal state %q", state)
	}
	return nil
}

func markerFromDisk(root, path string, disk journalMarkerDisk, intent *JournalIntent) *JournalMarker {
	return &JournalMarker{
		transactionRoot: root, path: path, digest: disk.MarkerDigest, intentDigest: disk.IntentDigest,
		state: disk.State, validationStatus: disk.ValidationStatus, rollbackOutcome: disk.RollbackOutcome,
		intent: cloneJournalIntent(intent),
	}
}
