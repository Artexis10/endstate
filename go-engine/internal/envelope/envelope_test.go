// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package envelope_test

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

const (
	testSchema  = "1.0"
	testVersion = "1.3.0"
)

// TestNewSuccess_AllFieldsPresent verifies that a success envelope includes every
// required contract field with the expected values.
func TestNewSuccess_AllFieldsPresent(t *testing.T) {
	runID := envelope.BuildRunID("capabilities", time.Now())
	data := map[string]interface{}{"foo": "bar"}

	env := envelope.NewSuccess("capabilities", runID, testSchema, testVersion, data)

	if env.SchemaVersion != testSchema {
		t.Errorf("schemaVersion: got %q, want %q", env.SchemaVersion, testSchema)
	}
	if env.CLIVersion != testVersion {
		t.Errorf("cliVersion: got %q, want %q", env.CLIVersion, testVersion)
	}
	if env.Command != "capabilities" {
		t.Errorf("command: got %q, want %q", env.Command, "capabilities")
	}
	if env.RunID != runID {
		t.Errorf("runId: got %q, want %q", env.RunID, runID)
	}
	if !env.Success {
		t.Error("success: expected true")
	}
	if env.Error != nil {
		t.Errorf("error: expected nil, got %+v", env.Error)
	}
	if env.Data == nil {
		t.Error("data: expected non-nil")
	}
}

// TestNewFailure_AllFieldsPresent verifies that a failure envelope includes the error
// object and sets success to false.
func TestNewFailure_AllFieldsPresent(t *testing.T) {
	runID := envelope.BuildRunID("apply", time.Now())
	err := envelope.NewError(envelope.ErrManifestNotFound, "Manifest file does not exist.").
		WithRemediation("Check the file path and ensure the manifest exists.").
		WithDocsKey("errors/manifest-not-found")

	env := envelope.NewFailure("apply", runID, testSchema, testVersion, err)

	if env.Success {
		t.Error("success: expected false")
	}
	if env.Error == nil {
		t.Fatal("error: expected non-nil Error object")
	}
	if env.Error.Code != envelope.ErrManifestNotFound {
		t.Errorf("error.code: got %q, want %q", env.Error.Code, envelope.ErrManifestNotFound)
	}
	if env.Error.Message == "" {
		t.Error("error.message: expected non-empty string")
	}
	if env.Error.Remediation == "" {
		t.Error("error.remediation: expected non-empty string")
	}
	if env.Error.DocsKey == "" {
		t.Error("error.docsKey: expected non-empty string")
	}
	// data must still be present (empty object)
	if env.Data == nil {
		t.Error("data: expected non-nil empty object")
	}
}

// TestErrorCodeSerialization verifies that ErrorCode values round-trip through JSON
// as their string representations.
func TestErrorCodeSerialization(t *testing.T) {
	cases := []struct {
		code ErrorCode
		want string
	}{
		{envelope.ErrManifestNotFound, "MANIFEST_NOT_FOUND"},
		{envelope.ErrManifestParseError, "MANIFEST_PARSE_ERROR"},
		{envelope.ErrManifestValidationError, "MANIFEST_VALIDATION_ERROR"},
		{envelope.ErrManifestWriteFailed, "MANIFEST_WRITE_FAILED"},
		{envelope.ErrPlanNotFound, "PLAN_NOT_FOUND"},
		{envelope.ErrPlanParseError, "PLAN_PARSE_ERROR"},
		{envelope.ErrWingetNotAvailable, "WINGET_NOT_AVAILABLE"},
		{envelope.ErrEngineCLINotFound, "ENGINE_CLI_NOT_FOUND"},
		{envelope.ErrCaptureFailed, "CAPTURE_FAILED"},
		{envelope.ErrCaptureBlocked, "CAPTURE_BLOCKED"},
		{envelope.ErrInstallFailed, "INSTALL_FAILED"},
		{envelope.ErrRestoreFailed, "RESTORE_FAILED"},
		{envelope.ErrVerifyFailed, "VERIFY_FAILED"},
		{envelope.ErrPermissionDenied, "PERMISSION_DENIED"},
		{envelope.ErrInternalError, "INTERNAL_ERROR"},
		{envelope.ErrSchemaIncompatible, "SCHEMA_INCOMPATIBLE"},
	}

	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			b, err := json.Marshal(tc.code)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			got := strings.Trim(string(b), `"`)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRunIDFormat validates that BuildRunID produces a string matching the expected
// pattern: <command>-YYYYMMDD-HHMMSS-<hostname>.
func TestRunIDFormat(t *testing.T) {
	t.Run("format_matches_pattern", func(t *testing.T) {
		// Pattern: word chars and hyphens for command, date, time, hostname.
		// Example: capabilities-20260311-143052-DESKTOP-ABC
		pattern := regexp.MustCompile(`^[a-z]+-\d{8}-\d{6}-.+$`)
		runID := envelope.BuildRunID("capabilities", time.Date(2026, 3, 11, 14, 30, 52, 0, time.UTC))
		if !pattern.MatchString(runID) {
			t.Errorf("runId %q does not match expected pattern %s", runID, pattern)
		}
	})

	t.Run("command_is_prefix", func(t *testing.T) {
		runID := envelope.BuildRunID("apply", time.Now())
		if !strings.HasPrefix(runID, "apply-") {
			t.Errorf("runId %q does not start with %q", runID, "apply-")
		}
	})

	t.Run("date_segment_correct", func(t *testing.T) {
		fixed := time.Date(2026, 3, 11, 14, 30, 52, 0, time.UTC)
		runID := envelope.BuildRunID("verify", fixed)
		if !strings.Contains(runID, "20260311-143052") {
			t.Errorf("runId %q does not contain expected date/time segment", runID)
		}
	})
}

// TestTimestampUTC_ISO8601 verifies that the timestampUtc field is a valid ISO 8601
// UTC timestamp that Go's time.RFC3339 can round-trip.
func TestTimestampUTC_ISO8601(t *testing.T) {
	runID := envelope.BuildRunID("capabilities", time.Now())
	env := envelope.NewSuccess("capabilities", runID, testSchema, testVersion, nil)

	parsed, err := time.Parse(time.RFC3339, env.TimestampUTC)
	if err != nil {
		t.Fatalf("TimestampUTC %q is not a valid RFC3339 timestamp: %v", env.TimestampUTC, err)
	}
	if parsed.Location() != time.UTC {
		t.Errorf("TimestampUTC %q is not in UTC", env.TimestampUTC)
	}
}

// TestMarshal_SingleLine verifies that Marshal produces compact (single-line) JSON
// with no newlines.
func TestMarshal_SingleLine(t *testing.T) {
	runID := envelope.BuildRunID("capabilities", time.Now())
	env := envelope.NewSuccess("capabilities", runID, testSchema, testVersion, map[string]interface{}{})

	b, err := envelope.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(b), "\n") {
		t.Error("Marshal output contains newlines; expected single-line compact JSON")
	}
}

// TestMarshal_ValidJSON verifies that the marshalled output is valid JSON containing
// all required envelope fields.
func TestMarshal_ValidJSON(t *testing.T) {
	runID := envelope.BuildRunID("capabilities", time.Now())
	env := envelope.NewSuccess("capabilities", runID, testSchema, testVersion, map[string]interface{}{"key": "value"})

	b, err := envelope.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	requiredFields := []string{
		"schemaVersion", "cliVersion", "command", "runId",
		"timestampUtc", "success", "data", "error",
	}
	for _, field := range requiredFields {
		if _, ok := decoded[field]; !ok {
			t.Errorf("required field %q missing from marshalled JSON", field)
		}
	}
}

// ErrorCode is a local type alias used in table tests to reference the exported type
// without repeating the package prefix throughout the test cases slice.
type ErrorCode = envelope.ErrorCode
