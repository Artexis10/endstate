// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

func prepareApplyConfigRestore(
	ctx context.Context,
	flags ApplyFlags,
	evidence configRestoreEvidenceSource,
) (*configRestoreExecutionSession, *envelope.Error) {
	if flags.configRestoreRuntime == nil {
		return nil, nil
	}
	session := newConfigRestoreExecutionSession(flags.configRestoreRuntime, evidence)
	if _, err := session.Preview(ctx); err != nil {
		return nil, configRestoreInternalError(err.Error())
	}
	return session, nil
}

func executePreparedApplyConfigRestore(
	ctx context.Context,
	flags ApplyFlags,
	runID string,
	emitter *events.Emitter,
	session *configRestoreExecutionSession,
	home *preparedHomeManager,
) (*ConfigResultFields, planner.ResolutionReason, *envelope.Error) {
	if session == nil || flags.configRestoreRuntime == nil {
		return nil, "", nil
	}
	inputs := flags.configRestoreRuntime.inputs
	options := applyConfigRestoreExecutionOptions(flags, runID, flags.configRestoreRepoRoot, emitter)
	if home != nil {
		options.PrepareHomeManager = home.prepareCaptured
		if home.ownershipAction == nil && flags.DryRun {
			options.HomeManagerTargets = home.flatTargets
		} else if home.ownershipAction == nil {
			// Preview does not build; only a real apply can list the exact leaves, and they take precedence.
			options.HomeManagerLeaves = home.builtLeafClaims
		}
	}
	execution, envErr := session.Execute(ctx, options)
	if home != nil && execution.HomeManagerBlock == "" {
		execution.HomeManagerBlock = home.forceBlock
	}
	if !inputs.hasConfigPayloads {
		return nil, execution.HomeManagerBlock, envErr
	}
	return NewConfigResultFields(execution.Plan.Sets, execution.RestoreItems), execution.HomeManagerBlock, envErr
}
