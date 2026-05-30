// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
)

// GenerationsFlags holds the parsed CLI flags for the generations command.
type GenerationsFlags struct {
	// Events controls streaming event output. "jsonl" enables it; "" disables.
	Events string
}

// GenerationsResult is the data payload for the generations command JSON
// envelope.
type GenerationsResult struct {
	Generations []*provision.Generation `json:"generations"`
}

// RunGenerations lists the recorded Provisioning Generations, newest first. It
// is read-only: it inspects state and writes nothing.
func RunGenerations(flags GenerationsFlags) (interface{}, *envelope.Error) {
	_ = flags
	gens, err := provision.List()
	if err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, err.Error())
	}
	if gens == nil {
		gens = []*provision.Generation{}
	}
	return &GenerationsResult{Generations: gens}, nil
}
