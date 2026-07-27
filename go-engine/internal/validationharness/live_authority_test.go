// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDecodeLiveCampaignJSONRejectsAmbiguousInput(t *testing.T) {
	t.Parallel()

	valid := mustLiveCampaignJSON(t, liveTestCampaign())
	for _, raw := range [][]byte{
		append(valid, []byte("{}")...),
		[]byte(strings.Replace(string(valid), "\"schemaVersion\":1", "\"schemaVersion\":1,\"schemaVersion\":1", 1)),
		[]byte(strings.Replace(string(valid), "{", "{\"unknown\":true,", 1)),
		bytes.Repeat([]byte("x"), maxLiveAuthorityBodyBytes+1),
	} {
		if _, err := DecodeLiveCampaignJSON(raw); err == nil {
			t.Fatalf("DecodeLiveCampaignJSON accepted %q", raw[:min(len(raw), 120)])
		}
	}
}

func TestLiveWorkflowRunClientBindsGitHubIdentity(t *testing.T) {
	t.Parallel()

	campaign := liveTestCampaign()
	campaign.ExpiresAt = time.Date(2030, 1, 1, 1, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/repos/Artexis10/endstate/actions/runs/1234" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		_, _ = writer.Write(mustLiveWorkflowRunJSON(t, campaign))
	}))
	t.Cleanup(server.Close)
	client := NewLiveWorkflowRunClient(server.Client(), server.URL)
	if _, err := client.Fetch(context.Background(), campaign); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	for _, mutate := range []func(map[string]any){
		func(value map[string]any) { value["repository"].(map[string]any)["full_name"] = "foreign/endstate" },
		func(value map[string]any) { value["path"] = ".github/workflows/foreign.yml" },
		func(value map[string]any) { value["event"] = "workflow_dispatch" },
		func(value map[string]any) { value["head_branch"] = "foreign" },
		func(value map[string]any) { value["head_sha"] = strings.Repeat("0", 40) },
		func(value map[string]any) { value["id"] = float64(5678) },
		func(value map[string]any) { value["run_attempt"] = float64(2) },
		func(value map[string]any) { value["actor"].(map[string]any)["type"] = "Bot" },
	} {
		payload := liveWorkflowRunValue(t, campaign)
		mutate(payload)
		body := mustLiveJSON(t, payload)
		bad := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { _, _ = writer.Write(body) }))
		if _, err := NewLiveWorkflowRunClient(bad.Client(), bad.URL).Fetch(context.Background(), campaign); err == nil {
			t.Fatal("Fetch() accepted a mismatched run")
		}
		bad.Close()
	}
	for _, body := range [][]byte{
		[]byte(`{"message":"unavailable"}`),
		[]byte(`{"id":1234,"id":1234}`),
		[]byte(`{"id":1234,"unknown":true}`),
		append(mustLiveWorkflowRunJSON(t, campaign), []byte(`{}`)...),
		bytes.Repeat([]byte("x"), maxLiveAuthorityBodyBytes+1),
	} {
		bad := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { _, _ = writer.Write(body) }))
		if _, err := NewLiveWorkflowRunClient(bad.Client(), bad.URL).Fetch(context.Background(), campaign); err == nil {
			t.Fatal("Fetch() accepted malformed API JSON")
		}
		bad.Close()
	}
	apiFailure := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { writer.WriteHeader(http.StatusBadGateway) }))
	if _, err := NewLiveWorkflowRunClient(apiFailure.Client(), apiFailure.URL).Fetch(context.Background(), campaign); err == nil {
		t.Fatal("Fetch() accepted API failure")
	}
	apiFailure.Close()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewLiveWorkflowRunClient(server.Client(), server.URL).Fetch(canceled, campaign); err == nil {
		t.Fatal("Fetch() accepted a canceled request")
	}
}

func TestLiveAuthoritySessionMintsSingleBoundPermit(t *testing.T) {
	t.Parallel()

	campaign := liveTestCampaign()
	campaign.ExpiresAt = time.Date(2030, 1, 1, 1, 0, 0, 0, time.UTC)
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), campaign.ModuleID)
	if err != nil {
		t.Fatal(err)
	}
	definitionHash := canonicalLiveDefinitionSHA256(t, definition)
	campaign.ModuleRevision = definition.ModuleRevision
	campaign.ValidationSourceSHA256 = definition.ValidationSourceSHA256
	campaign.SeedSHA256 = definition.SeedSHA256
	campaign.PackageRef = definition.WingetRef
	campaign.ComparatorSHA256 = liveSHA256Hex(definition.Comparator)
	campaign.TargetsSHA256 = liveSHA256Hex(definition.Comparator.Mappings)
	campaign.ObserverSHA256 = liveSHA256Hex(definition.Observer)
	campaign.PackageArguments = []string{"install", campaign.PackageRef, "--exact"}
	campaign.EngineSHA256 = strings.Repeat("1", 64)
	campaign.ValidatorSHA256 = strings.Repeat("2", 64)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write(mustLiveWorkflowRunJSON(t, campaign))
	}))
	t.Cleanup(server.Close)
	session, err := NewLiveAuthoritySession(context.Background(), NewLiveWorkflowRunClient(server.Client(), server.URL), LiveAuthoritySessionRequest{
		Campaign: campaign, Definition: definition, DefinitionSHA256: definitionHash, EngineSHA256: campaign.EngineSHA256, ValidatorSHA256: campaign.ValidatorSHA256, Now: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	nonce := session.NonceFor(liveOperationWingetExactInstall, 1)
	permit, err := session.MintMutationPermit(liveOperationWingetExactInstall, 1, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.MintMutationPermit(liveOperationWingetExactInstall, 1, nonce); err == nil {
		t.Fatal("session minted a second permit for one operation")
	}
	if !permit.capability.consume() || permit.capability.consume() {
		t.Fatal("permit was not single-use")
	}
}

func TestLiveAuthoritySessionRejectsPolicyAndBinaryDrift(t *testing.T) {
	t.Parallel()

	campaign := liveTestCampaign()
	campaign.ExpiresAt = time.Date(2030, 1, 1, 1, 0, 0, 0, time.UTC)
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), campaign.ModuleID)
	if err != nil {
		t.Fatal(err)
	}
	campaign.ModuleRevision = definition.ModuleRevision
	campaign.ValidationSourceSHA256 = definition.ValidationSourceSHA256
	campaign.SeedSHA256 = definition.SeedSHA256
	campaign.PackageRef = definition.WingetRef
	campaign.PackageArguments = []string{"install", campaign.PackageRef, "--exact"}
	campaign.ComparatorSHA256 = liveSHA256Hex(definition.Comparator)
	campaign.TargetsSHA256 = liveSHA256Hex(definition.Comparator.Mappings)
	campaign.ObserverSHA256 = liveSHA256Hex(definition.Observer)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write(mustLiveWorkflowRunJSON(t, campaign))
	}))
	t.Cleanup(server.Close)
	request := LiveAuthoritySessionRequest{Campaign: campaign, Definition: definition, DefinitionSHA256: canonicalLiveDefinitionSHA256(t, definition), EngineSHA256: campaign.EngineSHA256, ValidatorSHA256: campaign.ValidatorSHA256, Now: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)}
	for _, mutate := range []func(*LiveAuthoritySessionRequest){
		func(value *LiveAuthoritySessionRequest) { value.EngineSHA256 = strings.Repeat("0", 64) },
		func(value *LiveAuthoritySessionRequest) { value.ValidatorSHA256 = strings.Repeat("0", 64) },
		func(value *LiveAuthoritySessionRequest) { value.Definition.Policy.ReasonCode = "caller-substitution" },
		func(value *LiveAuthoritySessionRequest) { value.Now = value.Campaign.ExpiresAt },
	} {
		candidate := request
		mutate(&candidate)
		if _, err := NewLiveAuthoritySession(context.Background(), NewLiveWorkflowRunClient(server.Client(), server.URL), candidate); err == nil {
			t.Fatal("NewLiveAuthoritySession accepted source, binary, policy, or expiry drift")
		}
	}
}

func mustLiveCampaignJSON(t *testing.T, campaign LiveCampaign) []byte {
	t.Helper()
	return mustLiveJSON(t, campaign)
}
func mustLiveJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func mustLiveWorkflowRunJSON(t *testing.T, campaign LiveCampaign) []byte {
	t.Helper()
	return mustLiveJSON(t, liveWorkflowRunValue(t, campaign))
}
func liveWorkflowRunValue(t *testing.T, campaign LiveCampaign) map[string]any {
	t.Helper()
	return map[string]any{"id": campaign.RunID, "run_attempt": campaign.RunAttempt, "event": campaign.Event, "path": campaign.WorkflowPath, "head_branch": "main", "head_sha": campaign.ControllerCommit, "repository": map[string]any{"full_name": campaign.Repository}, "actor": map[string]any{"type": campaign.TrustedActorClass}}
}
func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
