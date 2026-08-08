// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"encoding/json"
	"strings"

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
	fixture           bool
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
		presence.scenarios[index].timeoutSeconds = hasJSONField(scenario, "timeoutSeconds")
		presence.scenarios[index].minimumAssertions = hasJSONField(scenario, "minimumAssertions")
		_, exists := jsonField(scenario, "fixture")
		presence.scenarios[index].fixture = exists
		if !exists {
			continue
		}
	}
	if len(document.Live) != 0 {
		var live map[string]json.RawMessage
		if err := json.Unmarshal(document.Live, &live); err != nil {
			return defaultPresence{}, err
		}
		presence.live.reasonCode = hasJSONField(live, "reasonCode")
		presence.live.explanation = hasJSONField(live, "explanation")
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
		if !presence.fixture {
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

func hasJSONField(fields map[string]json.RawMessage, name string) bool {
	_, ok := jsonField(fields, name)
	return ok
}

func jsonField(fields map[string]json.RawMessage, name string) (json.RawMessage, bool) {
	for field, value := range fields {
		if strings.EqualFold(field, name) {
			return value, true
		}
	}
	return nil, false
}
