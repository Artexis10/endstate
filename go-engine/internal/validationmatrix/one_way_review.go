// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"strings"
	"time"
)

func validateOneWayReview(record *ValidationRecord, scenario *Scenario, now time.Time) error {
	isOneWay := scenario.Mode == ScenarioCaptureContract || scenario.Mode == ScenarioRestoreContract
	if !isOneWay {
		if scenario.Review != nil {
			return validationError(CodeInvalidOneWayReview, record.ModuleID, record.FilePath, "scenario %q cannot declare one-way review metadata", scenario.ID)
		}
		return nil
	}
	if scenario.Review == nil {
		return validationError(CodeInvalidOneWayReview, record.ModuleID, record.FilePath, "scenario %q requires one-way review metadata", scenario.ID)
	}
	review := scenario.Review
	if review.Decision != "approved-one-way" || !stableKebabPattern.MatchString(review.ReasonCode) ||
		strings.TrimSpace(review.Reviewer) == "" || strings.TrimSpace(review.Evidence) == "" {
		return validationError(CodeInvalidOneWayReview, record.ModuleID, record.FilePath, "scenario %q review must be approved, reason-coded, owned, and evidenced", scenario.ID)
	}
	if !datePattern.MatchString(review.ReviewedOn) {
		return validationError(CodeInvalidOneWayReview, record.ModuleID, record.FilePath, "scenario %q reviewedOn must be strict YYYY-MM-DD", scenario.ID)
	}
	reviewedOn, err := time.Parse("2006-01-02", review.ReviewedOn)
	year, month, day := now.UTC().Date()
	today := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	if err != nil || reviewedOn.After(today) {
		return validationError(CodeInvalidOneWayReview, record.ModuleID, record.FilePath, "scenario %q reviewedOn is malformed or in the future", scenario.ID)
	}
	return nil
}
