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
		{envelope.ErrAuthRequired, "AUTH_REQUIRED"},
		{envelope.ErrSubscriptionRequired, "SUBSCRIPTION_REQUIRED"},
		{envelope.ErrNotFound, "NOT_FOUND"},
		{envelope.ErrRateLimited, "RATE_LIMITED"},
		{envelope.ErrBackendError, "BACKEND_ERROR"},
		{envelope.ErrBackendUnreachable, "BACKEND_UNREACHABLE"},
		{envelope.ErrBackendIncompatible, "BACKEND_INCOMPATIBLE"},
		{envelope.ErrStorageQuotaExceeded, "STORAGE_QUOTA_EXCEEDED"},
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

// ---------------------------------------------------------------------------
// Gap tests ported from Pester: JSON envelope and error structure
// (JsonSchema.Tests.ps1, VersionInjection.Tests.ps1)
// ---------------------------------------------------------------------------

// TestNewSuccess_SuccessIsTrue verifies the success field on a success envelope.
// Pester: "Should set success to true"
func TestNewSuccess_SuccessIsTrue(t *testing.T) {
	runID := envelope.BuildRunID("test", time.Now())
	env := envelope.NewSuccess("test", runID, testSchema, testVersion, nil)
	if !env.Success {
		t.Error("expected success=true on NewSuccess envelope")
	}
}

// TestNewFailure_SuccessIsFalse verifies the success field on a failure envelope.
// Pester: "Should set success to false when specified"
func TestNewFailure_SuccessIsFalse(t *testing.T) {
	runID := envelope.BuildRunID("test", time.Now())
	err := envelope.NewError(envelope.ErrInternalError, "test error")
	env := envelope.NewFailure("test", runID, testSchema, testVersion, err)
	if env.Success {
		t.Error("expected success=false on NewFailure envelope")
	}
}

// TestErrorWithoutOptionalFields_OmitsThemFromJSON verifies that optional Error
// fields (detail, remediation, docsKey) are omitted when not set.
// Pester: "Should not include optional fields when not provided"
func TestErrorWithoutOptionalFields_OmitsThemFromJSON(t *testing.T) {
	err := envelope.NewError(envelope.ErrManifestNotFound, "File not found")
	b, jsonErr := json.Marshal(err)
	if jsonErr != nil {
		t.Fatalf("json.Marshal: %v", jsonErr)
	}

	var decoded map[string]interface{}
	if jsonErr := json.Unmarshal(b, &decoded); jsonErr != nil {
		t.Fatalf("json.Unmarshal: %v", jsonErr)
	}

	if _, ok := decoded["detail"]; ok {
		t.Error("expected 'detail' to be omitted from JSON when not set")
	}
	if _, ok := decoded["remediation"]; ok {
		t.Error("expected 'remediation' to be omitted from JSON when not set")
	}
	if _, ok := decoded["docsKey"]; ok {
		t.Error("expected 'docsKey' to be omitted from JSON when not set")
	}
}

// TestErrorWithDetail verifies the detail field is present when provided.
// Pester: "Should include optional detail when provided"
func TestErrorWithDetail(t *testing.T) {
	err := envelope.NewError(envelope.ErrInternalError, "Test").
		WithDetail(map[string]string{"path": `C:\test`})

	b, jsonErr := json.Marshal(err)
	if jsonErr != nil {
		t.Fatalf("json.Marshal: %v", jsonErr)
	}

	var decoded map[string]interface{}
	if jsonErr := json.Unmarshal(b, &decoded); jsonErr != nil {
		t.Fatalf("json.Unmarshal: %v", jsonErr)
	}

	detail, ok := decoded["detail"]
	if !ok {
		t.Fatal("expected 'detail' to be present")
	}
	detailMap, ok := detail.(map[string]interface{})
	if !ok {
		t.Fatalf("detail is not a map: %T", detail)
	}
	if detailMap["path"] != `C:\test` {
		t.Errorf("detail.path = %v, want %q", detailMap["path"], `C:\test`)
	}
}

// TestErrorWithRemediation verifies the remediation field is present when provided.
// Pester: "Should include optional remediation when provided"
func TestErrorWithRemediation(t *testing.T) {
	err := envelope.NewError(envelope.ErrInternalError, "Test").
		WithRemediation("Try this fix")
	if err.Remediation != "Try this fix" {
		t.Errorf("Remediation = %q, want %q", err.Remediation, "Try this fix")
	}
}

// TestErrorWithDocsKey verifies the docsKey field is present when provided.
// Pester: "Should include optional docsKey when provided"
func TestErrorWithDocsKey(t *testing.T) {
	err := envelope.NewError(envelope.ErrInternalError, "Test").
		WithDocsKey("errors/test")
	if err.DocsKey != "errors/test" {
		t.Errorf("DocsKey = %q, want %q", err.DocsKey, "errors/test")
	}
}

// TestNewFailure_DataIsEmptyObject verifies that failure envelopes always have
// a non-nil data field serialized as an empty JSON object.
// Pester: Envelope data must be present even on failures.
func TestNewFailure_DataIsEmptyObject(t *testing.T) {
	runID := envelope.BuildRunID("test", time.Now())
	errObj := envelope.NewError(envelope.ErrInternalError, "test")
	env := envelope.NewFailure("test", runID, testSchema, testVersion, errObj)

	b, err := envelope.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	data, ok := decoded["data"]
	if !ok {
		t.Fatal("required field 'data' missing from failure envelope")
	}
	dataMap, ok := data.(map[string]interface{})
	if !ok {
		t.Fatalf("data is not an object: %T", data)
	}
	if len(dataMap) != 0 {
		t.Errorf("expected empty data object, got %v", dataMap)
	}
}

// TestEnvelopeFieldOrder verifies that when marshalled, the JSON keys are in the
// expected contract order.
// Pester: "Should maintain consistent field order for stable output"
func TestEnvelopeFieldOrder(t *testing.T) {
	runID := envelope.BuildRunID("test", time.Now())
	env := envelope.NewSuccess("test", runID, testSchema, testVersion, map[string]interface{}{})

	b, err := envelope.Marshal(env)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	// The expected order from the contract:
	expectedOrder := []string{
		"schemaVersion", "cliVersion", "command", "runId",
		"timestampUtc", "success", "data", "error",
	}

	jsonStr := string(b)
	lastIdx := -1
	for _, field := range expectedOrder {
		idx := strings.Index(jsonStr, `"`+field+`"`)
		if idx == -1 {
			t.Errorf("field %q not found in JSON output", field)
			continue
		}
		if idx <= lastIdx {
			t.Errorf("field %q appears before expected position in JSON output", field)
		}
		lastIdx = idx
	}
}

// TestVersionInjection_SchemaAndCLIVersion verifies that the version strings
// provided to NewSuccess are correctly injected.
// Pester: VersionInjection.Tests.ps1 - "Populates cliVersion" / "Populates schemaVersion"
func TestVersionInjection_SchemaAndCLIVersion(t *testing.T) {
	schemaV := "2.0"
	cliV := "3.5.1"
	runID := envelope.BuildRunID("test", time.Now())
	env := envelope.NewSuccess("test", runID, schemaV, cliV, nil)

	if env.SchemaVersion != schemaV {
		t.Errorf("SchemaVersion = %q, want %q", env.SchemaVersion, schemaV)
	}
	if env.CLIVersion != cliV {
		t.Errorf("CLIVersion = %q, want %q", env.CLIVersion, cliV)
	}
}

// TestTimestampUTC_EndsWithZ verifies the timestamp ends with Z (UTC).
// Pester: "Should include timestampUtc in ISO 8601 format"
func TestTimestampUTC_EndsWithZ(t *testing.T) {
	runID := envelope.BuildRunID("test", time.Now())
	env := envelope.NewSuccess("test", runID, testSchema, testVersion, nil)
	if !strings.HasSuffix(env.TimestampUTC, "Z") {
		t.Errorf("timestampUtc %q does not end with Z", env.TimestampUTC)
	}
}

// TestTimestampUTC_MatchesISO8601Pattern verifies the timestamp matches the ISO 8601 pattern.
// Pester: timestampUtc Should -Match "^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$"
func TestTimestampUTC_MatchesISO8601Pattern(t *testing.T) {
	runID := envelope.BuildRunID("test", time.Now())
	env := envelope.NewSuccess("test", runID, testSchema, testVersion, nil)

	pattern := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)
	if !pattern.MatchString(env.TimestampUTC) {
		t.Errorf("timestampUtc %q does not match ISO 8601 pattern", env.TimestampUTC)
	}
}
