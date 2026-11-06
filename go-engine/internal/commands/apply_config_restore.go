// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
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
) (*ConfigResultFields, bool, *envelope.Error) {
	if session == nil || flags.configRestoreRuntime == nil {
		return nil, false, nil
	}
	inputs := flags.configRestoreRuntime.inputs
	options := applyConfigRestoreExecutionOptions(flags, runID, flags.configRestoreRepoRoot, emitter)
	if home != nil && home.ownershipAction == nil {
		options.HomeManagerTargets = home.flatTargets
	}
	execution, envErr := session.Execute(ctx, options)
	if !inputs.hasConfigPayloads {
		return nil, execution.HomeManagerBlocked, envErr
	}
	return NewConfigResultFields(execution.Plan.Sets, execution.RestoreItems), execution.HomeManagerBlocked, envErr
}
