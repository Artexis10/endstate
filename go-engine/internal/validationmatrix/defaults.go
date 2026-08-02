// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"encoding/json"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

const (
	defaultTimeoutSeconds       = 120
	defaultCandidateReason      = "unproven-hosted-baseline"
	defaultCandidateExplanation = "Install metadata exists, but no trusted GitHub-hosted baseline has passed for this module."
	defaultManualReason         = "no-supported-package-reference"
	defaultManualExplanation    = "No supported unattended Winget or Chocolatey package reference is declared by this module."
)

type defaultPresence struct {
	scenarios []scenarioDefaultPresence
	live      liveDefaultPresence
}

type scenarioDefaultPresence struct {
	fixtureObject     bool
	fixtureType       bool
	fixturePath       bool
	fixtureSHA256     bool
	timeoutSeconds    bool
	minimumAssertions bool
}

type liveDefaultPresence struct {
	reasonCode  bool
	explanation bool
}

func collectDefaultPresence(data []byte) (defaultPresence, error) {
	var document struct {
		Synthetic struct {
			Scenarios []json.RawMessage `json:"scenarios"`
		} `json:"synthetic"`
		Live json.RawMessage `json:"live"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return defaultPresence{}, err
	}

	presence := defaultPresence{scenarios: make([]scenarioDefaultPresence, len(document.Synthetic.Scenarios))}
	for index, rawScenario := range document.Synthetic.Scenarios {
		var scenario map[string]json.RawMessage
		if err := json.Unmarshal(rawScenario, &scenario); err != nil {
			return defaultPresence{}, err
		}
		_, presence.scenarios[index].timeoutSeconds = scenario["timeoutSeconds"]
		_, presence.scenarios[index].minimumAssertions = scenario["minimumAssertions"]
		fixture, exists := scenario["fixture"]
		if !exists {
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(fixture, &fields); err != nil {
			return defaultPresence{}, err
		}
		presence.scenarios[index].fixtureObject = fields != nil
		_, presence.scenarios[index].fixtureType = fields["type"]
		_, presence.scenarios[index].fixturePath = fields["path"]
		_, presence.scenarios[index].fixtureSHA256 = fields["sha256"]
	}
	if len(document.Live) != 0 {
		var live map[string]json.RawMessage
		if err := json.Unmarshal(document.Live, &live); err != nil {
			return defaultPresence{}, err
		}
		_, presence.live.reasonCode = live["reasonCode"]
		_, presence.live.explanation = live["explanation"]
	}
	return presence, nil
}

func resolveDefaults(record *ValidationRecord, mod *modules.Module) {
	for index := range record.Synthetic.Scenarios {
		scenario := &record.Synthetic.Scenarios[index]
		if index >= len(record.defaultPresence.scenarios) {
			continue
		}
		presence := record.defaultPresence.scenarios[index]
		if presence.fixtureObject && !presence.fixtureType && !presence.fixturePath && !presence.fixtureSHA256 {
			scenario.Fixture.Type = FixtureAuto
		}
		if !presence.timeoutSeconds {
			scenario.TimeoutSeconds = defaultTimeoutSeconds
		}
		if !presence.minimumAssertions {
			scenario.MinimumAssertions = make(map[string]int)
			for _, assertion := range requiredAssertions(scenario.Mode, len(mod.Verify) > 0) {
				scenario.MinimumAssertions[assertion] = 1
			}
		}
	}

	switch record.Live.Mode {
	case LiveCandidate:
		if !record.defaultPresence.live.reasonCode {
			record.Live.ReasonCode = defaultCandidateReason
		}
		if !record.defaultPresence.live.explanation {
			record.Live.Explanation = defaultCandidateExplanation
		}
	case LiveManual:
		if !record.defaultPresence.live.reasonCode {
			record.Live.ReasonCode = defaultManualReason
		}
		if !record.defaultPresence.live.explanation {
			record.Live.Explanation = defaultManualExplanation
		}
	}
}
