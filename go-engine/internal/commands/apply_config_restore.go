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
) (*ConfigResultFields, *envelope.Error) {
	if session == nil || flags.configRestoreRuntime == nil {
		return nil, nil
	}
	inputs := flags.configRestoreRuntime.inputs
	execution, envErr := session.Execute(
		ctx,
		applyConfigRestoreExecutionOptions(flags, runID, flags.configRestoreRepoRoot, emitter),
	)
	if !inputs.hasConfigPayloads {
		return nil, envErr
	}
	return NewConfigResultFields(execution.Plan.Sets, execution.RestoreItems), envErr
}
