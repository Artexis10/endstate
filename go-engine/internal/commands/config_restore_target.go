// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// RestoreTargetErrorDetail is stable structured context for an invalid
// --restore-target mapping.
type RestoreTargetErrorDetail struct {
	Index     int    `json:"index"`
	Mapping   string `json:"mapping"`
	CaptureID string `json:"captureId,omitempty"`
	Reason    string `json:"reason"`
}

// parseRestoreTargetMappings validates the repeatable
// --restore-target <captureId>=<targetInstanceId> form. knownCaptures must
// contain only generation-aware captures; legacy lanes are never targetable.
func parseRestoreTargetMappings(
	raw []string,
	knownCaptures map[string]struct{},
) (map[string]string, *envelope.Error) {
	mappings := make(map[string]string, len(raw))
	for index, original := range raw {
		mapping := strings.TrimSpace(original)
		if strings.Count(mapping, "=") != 1 {
			return nil, invalidRestoreTarget(index, original, "", "malformed_mapping")
		}

		parts := strings.SplitN(mapping, "=", 2)
		captureID := strings.TrimSpace(parts[0])
		targetInstanceID := strings.TrimSpace(parts[1])
		if captureID == "" {
			return nil, invalidRestoreTarget(index, original, "", "empty_capture_id")
		}
		if targetInstanceID == "" {
			return nil, invalidRestoreTarget(index, original, captureID, "empty_target_instance_id")
		}
		if _, exists := knownCaptures[captureID]; !exists {
			return nil, invalidRestoreTarget(index, original, captureID, "unknown_capture_id")
		}
		if _, exists := mappings[captureID]; exists {
			return nil, invalidRestoreTarget(index, original, captureID, "duplicate_capture_id")
		}
		mappings[captureID] = targetInstanceID
	}
	return mappings, nil
}

func invalidRestoreTarget(index int, mapping, captureID, reason string) *envelope.Error {
	message := "Invalid --restore-target mapping."
	if captureID != "" {
		message = fmt.Sprintf("Invalid --restore-target mapping for capture %q.", captureID)
	}
	return envelope.NewError(envelope.ErrInvalidRestoreTarget, message).
		WithDetail(RestoreTargetErrorDetail{
			Index:     index,
			Mapping:   mapping,
			CaptureID: captureID,
			Reason:    reason,
		}).
		WithRemediation("Use one --restore-target <captureId>=<targetInstanceId> per generation-aware capture; capture IDs may appear only once.")
}
