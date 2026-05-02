// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// EngineSchemaMajor is the API schema major version this engine speaks
// (contract §11). A backend advertising any other major triggers
// SCHEMA_INCOMPATIBLE on every request.
const EngineSchemaMajor = 1

// EngineSchemaMinor is the maximum minor version this engine knows about.
// A backend advertising a higher minor on a write request also triggers
// SCHEMA_INCOMPATIBLE; on a read-only request it logs a warning and lets
// the request proceed.
const EngineSchemaMinor = 0

// versionHeader is the response header substrate sets on every response
// (contract §11).
const versionHeader = "X-Endstate-API-Version"

// parsedVersion holds the major/minor extracted from the version header.
type parsedVersion struct {
	major int
	minor int
}

func parseVersionHeader(v string) (parsedVersion, error) {
	parts := strings.SplitN(strings.TrimSpace(v), ".", 2)
	if len(parts) != 2 {
		return parsedVersion{}, fmt.Errorf("expected MAJOR.MINOR, got %q", v)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil || major < 0 {
		return parsedVersion{}, fmt.Errorf("invalid major component %q", parts[0])
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil || minor < 0 {
		return parsedVersion{}, fmt.Errorf("invalid minor component %q", parts[1])
	}
	return parsedVersion{major: major, minor: minor}, nil
}

// versionMismatch returns nil if the backend version is acceptable. For a
// major mismatch it always returns an APIError with SCHEMA_INCOMPATIBLE.
// For a higher minor it returns an APIError on writes and a warning
// (boolean second return) on reads.
func versionMismatch(backend parsedVersion, readOnly bool) (*APIError, bool) {
	if backend.major != EngineSchemaMajor {
		return &APIError{
			Code: envelope.ErrSchemaIncompatible,
			BackendMessage: fmt.Sprintf(
				"Backend schema major %d does not match engine major %d.",
				backend.major, EngineSchemaMajor,
			),
		}, false
	}
	if backend.minor > EngineSchemaMinor {
		if readOnly {
			return nil, true // proceed but warn
		}
		return &APIError{
			Code: envelope.ErrSchemaIncompatible,
			BackendMessage: fmt.Sprintf(
				"Backend schema minor %d is newer than engine minor %d (write blocked).",
				backend.minor, EngineSchemaMinor,
			),
		}, false
	}
	return nil, false
}
