// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/state"
)

// ReportFlags holds the parsed CLI flags for the report command.
type ReportFlags struct {
	// Latest requests only the single most recent run.
	Latest bool
	// Last limits the number of recent runs returned.
	Last int
	// RunID requests a specific run by its ID.
	RunID string
	// Events controls streaming event output. "jsonl" enables it; "" disables.
	Events string
}

// ReportResult is the data payload for the report command JSON envelope.
// Shape matches docs/contracts/cli-json-contract.md section "Command: report".
type ReportResult struct {
	Reports []*state.RunRecord `json:"reports"`
}

// RunReport executes the report command with the provided flags.
//
// It reads run history from the state/runs/ directory. When no flags are
// provided it defaults to returning the single most recent run. A missing
// state directory is treated as a non-fatal condition (empty reports).
func RunReport(flags ReportFlags) (interface{}, *envelope.Error) {
	stateDir := state.StateDir()
	runDir := filepath.Join(stateDir, "runs")

	var records []*state.RunRecord
	var err error

	if flags.RunID != "" {
		record, e := state.GetRunHistory(runDir, flags.RunID)
		if e != nil {
			return nil, envelope.NewError(envelope.ErrInternalError, "Run not found: "+flags.RunID)
		}
		records = []*state.RunRecord{record}
	} else if flags.Last > 0 {
		records, err = state.ListRunHistory(runDir, flags.Last)
	} else {
		// Default: latest (1 most recent).
		records, err = state.ListRunHistory(runDir, 1)
	}

	if err != nil {
		// Non-fatal: return empty reports if state dir doesn't exist or is unreadable.
		records = []*state.RunRecord{}
	}

	if records == nil {
		records = []*state.RunRecord{}
	}

	return &ReportResult{Reports: records}, nil
}
