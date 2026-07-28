// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type liveRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip liveRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestLiveWorkflowRunClientPinsProductionOriginAndAcceptsFullGitHubResponse(t *testing.T) {
	t.Parallel()

	campaign := liveTestCampaign()
	body := liveWorkflowRunValue(t, campaign)
	body["path"] = campaign.WorkflowPath + "@main"
	body["name"] = "Hosted live"
	body["status"] = "completed"
	body["conclusion"] = "success"
	body["head_commit"] = map[string]any{"id": campaign.ControllerCommit, "message": "controller"}
	body["actor"].(map[string]any)["login"] = "maintainer"
	client := NewLiveWorkflowRunClient(&http.Client{Transport: liveRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.URL.Scheme != "https" || request.URL.Host != "api.github.com" || request.URL.Path != "/repos/Artexis10/endstate/actions/runs/1234" {
			t.Fatalf("authority request URL = %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(mustLiveJSON(t, body))), Request: request}, nil
	})})
	if _, err := client.Fetch(context.Background(), campaign); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
}

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
	client := newLiveWorkflowRunClient(server.Client(), server.URL)
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
		if _, err := newLiveWorkflowRunClient(bad.Client(), bad.URL).Fetch(context.Background(), campaign); err == nil {
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
		if _, err := newLiveWorkflowRunClient(bad.Client(), bad.URL).Fetch(context.Background(), campaign); err == nil {
			t.Fatal("Fetch() accepted malformed API JSON")
		}
		bad.Close()
	}
	apiFailure := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { writer.WriteHeader(http.StatusBadGateway) }))
	if _, err := newLiveWorkflowRunClient(apiFailure.Client(), apiFailure.URL).Fetch(context.Background(), campaign); err == nil {
		t.Fatal("Fetch() accepted API failure")
	}
	apiFailure.Close()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newLiveWorkflowRunClient(server.Client(), server.URL).Fetch(canceled, campaign); err == nil {
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
	campaign.TargetsSHA256 = liveSHA256Hex(definition.DeclaredTargets)
	campaign.ObserverSHA256 = liveSHA256Hex(definition.Observer)
	campaign.PackageArguments = []string{"uninstall", campaign.PackageRef, "--exact"}
	campaign.EngineSHA256 = strings.Repeat("1", 64)
	campaign.ValidatorSHA256 = strings.Repeat("2", 64)
	liveTestBindEngineOperations(&campaign)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write(mustLiveWorkflowRunJSON(t, campaign))
	}))
	t.Cleanup(server.Close)
	session, err := NewLiveAuthoritySession(context.Background(), newLiveWorkflowRunClient(server.Client(), server.URL), LiveAuthoritySessionRequest{
		Campaign: campaign, Definition: definition, DefinitionSHA256: definitionHash, EngineSHA256: campaign.EngineSHA256, ValidatorSHA256: campaign.ValidatorSHA256, ControllerCheckoutCommit: campaign.ControllerCommit, TestedCheckoutCommit: campaign.TestedCheckoutCommit, Now: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	nonce := session.NonceFor(liveOperationEngineApply, 3)
	issuer := session.NewReceiptIssuer()
	if err := issuer.skipDeclaredPreflight(); err != nil {
		t.Fatal(err)
	}
	admission, err := issuer.admit(liveOperationEngineApply, 3, nonce)
	if err != nil {
		t.Fatal(err)
	}
	foreignIssuer := newLiveReceiptIssuer()
	foreign, err := foreignIssuer.admit(liveOperationEngineApply, 1, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.MintMutationPermit(foreign); err == nil {
		t.Fatal("session minted a permit for a raw issuer admission")
	}
	tampered := admission
	tampered.token[0]++
	if _, err := session.MintMutationPermit(tampered); err == nil {
		t.Fatal("session minted a permit with a substituted admission token")
	}
	otherSession := &LiveAuthoritySession{campaignID: session.campaignID, campaign: session.campaign, definition: session.definition, now: session.now, minted: make(map[liveAuthorityPermitKey]struct{}), issuerID: issuer.id}
	otherSession.campaignID[0]++
	if _, err := otherSession.MintMutationPermit(admission); err == nil {
		t.Fatal("different authority session minted a permit")
	}
	permit, err := session.MintMutationPermit(admission)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.MintMutationPermit(admission); err == nil {
		t.Fatal("session minted a second permit for one operation")
	}
	if permit.capability.consumed.Load() {
		t.Fatal("minted permit was pre-consumed")
	}
	request := LiveProcessRequest{operation: admission.operation, admission: admission, expected: liveReceiptExpectedIdentity{
		definition: permit.capability.definition, engine: permit.capability.engine, seed: permit.capability.seed, packageRef: permit.capability.packageRef,
		comparator: permit.capability.comparator, targets: permit.capability.targets, observer: permit.capability.observer, workflow: permit.capability.workflow,
	}, executable: permit.capability.executable, args: append([]string(nil), permit.capability.arguments...), dir: permit.capability.directory, environment: cloneLiveEnvironment(permit.capability.environment)}
	if !permit.capability.validFor(request, session.now) {
		t.Fatal("permit rejected its exact admission")
	}
	request.admission.token[0]++
	if permit.capability.validFor(request, session.now) {
		t.Fatal("permit accepted a substituted admission token")
	}
}

func TestLiveAuthoritySessionIssuesOnlyOneReceiptIssuer(t *testing.T) {
	session := &LiveAuthoritySession{campaignID: sha256.Sum256([]byte("campaign")), campaign: LiveCampaign{PhaseNonce: "phase"}, definition: liveAuthorityDefinition{operations: map[uint64]LiveCampaignOperation{1: {Sequence: 1, Operation: string(liveOperationEngineApply)}}}}
	first := session.NewReceiptIssuer()
	if first == nil {
		t.Fatal("first receipt issuer rejected")
	}
	if second := session.NewReceiptIssuer(); second != nil {
		t.Fatal("session issued a second receipt issuer")
	}
}

func TestLiveAuthoritySessionIssuesOneReceiptIssuerConcurrently(t *testing.T) {
	session := &LiveAuthoritySession{campaignID: sha256.Sum256([]byte("campaign")), campaign: LiveCampaign{PhaseNonce: "phase"}, definition: liveAuthorityDefinition{operations: map[uint64]LiveCampaignOperation{1: {Sequence: 1, Operation: string(liveOperationEngineApply)}}}}
	issuers := make(chan *liveReceiptIssuer, 2)
	go func() { issuers <- session.NewReceiptIssuer() }()
	go func() { issuers <- session.NewReceiptIssuer() }()
	first, second := <-issuers, <-issuers
	if (first == nil) == (second == nil) {
		t.Fatal("concurrent issuer allocation did not produce exactly one issuer")
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
	campaign.PackageArguments = []string{"uninstall", campaign.PackageRef, "--exact"}
	campaign.ComparatorSHA256 = liveSHA256Hex(definition.Comparator)
	campaign.TargetsSHA256 = liveSHA256Hex(definition.DeclaredTargets)
	campaign.ObserverSHA256 = liveSHA256Hex(definition.Observer)
	liveTestBindEngineOperations(&campaign)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write(mustLiveWorkflowRunJSON(t, campaign))
	}))
	t.Cleanup(server.Close)
	request := LiveAuthoritySessionRequest{Campaign: campaign, Definition: definition, DefinitionSHA256: canonicalLiveDefinitionSHA256(t, definition), EngineSHA256: campaign.EngineSHA256, ValidatorSHA256: campaign.ValidatorSHA256, ControllerCheckoutCommit: campaign.ControllerCommit, TestedCheckoutCommit: campaign.TestedCheckoutCommit, Now: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)}
	for _, mutate := range []func(*LiveAuthoritySessionRequest){
		func(value *LiveAuthoritySessionRequest) { value.EngineSHA256 = strings.Repeat("0", 64) },
		func(value *LiveAuthoritySessionRequest) { value.ValidatorSHA256 = strings.Repeat("0", 64) },
		func(value *LiveAuthoritySessionRequest) { value.Definition.Policy.ReasonCode = "caller-substitution" },
		func(value *LiveAuthoritySessionRequest) { value.Now = value.Campaign.ExpiresAt },
		func(value *LiveAuthoritySessionRequest) { value.TestedCheckoutCommit = strings.Repeat("0", 40) },
		func(value *LiveAuthoritySessionRequest) { value.ControllerCheckoutCommit = strings.Repeat("0", 40) },
	} {
		candidate := request
		mutate(&candidate)
		if _, err := NewLiveAuthoritySession(context.Background(), newLiveWorkflowRunClient(server.Client(), server.URL), candidate); err == nil {
			t.Fatal("NewLiveAuthoritySession accepted source, binary, policy, or expiry drift")
		}
	}
}

func liveTestBindEngineOperations(campaign *LiveCampaign) {
	for index := range campaign.Operations {
		if liveCampaignEngineOperation(liveOperation(campaign.Operations[index].Operation)) {
			campaign.Operations[index].ExecutableSHA256 = campaign.EngineSHA256
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
