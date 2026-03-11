// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package envelope provides the standard JSON output envelope for Endstate CLI
// commands as defined in the CLI JSON Contract v1.0.
package envelope

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Envelope is the top-level JSON output structure emitted by every --json command.
// Field order matches the contract document for readability.
type Envelope struct {
	SchemaVersion string      `json:"schemaVersion"`
	CLIVersion    string      `json:"cliVersion"`
	Command       string      `json:"command"`
	RunID         string      `json:"runId"`
	TimestampUTC  string      `json:"timestampUtc"`
	Success       bool        `json:"success"`
	Data          interface{} `json:"data"`
	Error         *Error      `json:"error"`
}

// BuildRunID returns a run identifier in the format:
//
//	<command>-YYYYMMDD-HHMMSS-<HOSTNAME>
//
// If os.Hostname() fails, "unknown" is used for the hostname segment.
func BuildRunID(command string, t time.Time) string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown"
	}
	return fmt.Sprintf("%s-%s-%s", command, t.UTC().Format("20060102-150405"), hostname)
}

// NewSuccess builds an Envelope representing a successful command result.
// schemaVersion and cliVersion are read from the config package by the caller and
// passed in so this package stays import-free of higher-level packages.
func NewSuccess(command, runID, schemaVersion, cliVersion string, data interface{}) *Envelope {
	return &Envelope{
		SchemaVersion: schemaVersion,
		CLIVersion:    cliVersion,
		Command:       command,
		RunID:         runID,
		TimestampUTC:  time.Now().UTC().Format(time.RFC3339),
		Success:       true,
		Data:          data,
		Error:         nil,
	}
}

// NewFailure builds an Envelope representing a failed command result.
// data is set to an empty object so consumers always have a consistent shape.
func NewFailure(command, runID, schemaVersion, cliVersion string, err *Error) *Envelope {
	return &Envelope{
		SchemaVersion: schemaVersion,
		CLIVersion:    cliVersion,
		Command:       command,
		RunID:         runID,
		TimestampUTC:  time.Now().UTC().Format(time.RFC3339),
		Success:       false,
		Data:          map[string]interface{}{},
		Error:         err,
	}
}

// Marshal serializes the envelope to a single-line compact JSON byte slice.
// Uses json.Marshal (not MarshalIndent) per the design rules.
func Marshal(env *Envelope) ([]byte, error) {
	return json.Marshal(env)
}
