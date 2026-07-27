// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const liveGitHubAPIEndpoint = "https://api.github.com"

type liveWorkflowRun struct {
	ID         uint64 `json:"id"`
	RunAttempt int    `json:"run_attempt"`
	Event      string `json:"event"`
	Path       string `json:"path"`
	HeadBranch string `json:"head_branch"`
	HeadSHA    string `json:"head_sha"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Actor struct {
		Type string `json:"type"`
	} `json:"actor"`
}

// LiveWorkflowRunClient has a fixed production endpoint. Tests may inject an
// HTTP client and endpoint; no token is retained in a campaign or permit.
type LiveWorkflowRunClient struct {
	client   *http.Client
	endpoint string
}

func NewLiveWorkflowRunClient(client *http.Client, endpoint string) LiveWorkflowRunClient {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	if endpoint == "" {
		endpoint = liveGitHubAPIEndpoint
	}
	return LiveWorkflowRunClient{client: client, endpoint: strings.TrimRight(endpoint, "/")}
}

func (client LiveWorkflowRunClient) Fetch(ctx context.Context, campaign LiveCampaign) (liveWorkflowRun, error) {
	if err := ValidateLiveCampaign(campaign); err != nil || campaign.RunID == 0 || campaign.RunAttempt != 1 {
		return liveWorkflowRun{}, fmt.Errorf("live workflow run request is invalid")
	}
	base, err := url.Parse(client.endpoint)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return liveWorkflowRun{}, fmt.Errorf("live workflow endpoint is invalid")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	requestURL := client.endpoint + "/repos/Artexis10/endstate/actions/runs/" + fmt.Sprint(campaign.RunID)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return liveWorkflowRun{}, fmt.Errorf("create live workflow request: %w", err)
	}
	response, err := client.client.Do(request)
	if err != nil {
		return liveWorkflowRun{}, fmt.Errorf("fetch live workflow run: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxLiveAuthorityBodyBytes))
		return liveWorkflowRun{}, fmt.Errorf("live workflow API returned %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxLiveAuthorityBodyBytes+1))
	if err != nil || len(raw) > maxLiveAuthorityBodyBytes {
		return liveWorkflowRun{}, fmt.Errorf("live workflow API body is invalid")
	}
	var run liveWorkflowRun
	if err := strictLiveAuthorityJSON(raw, &run); err != nil {
		return liveWorkflowRun{}, fmt.Errorf("decode live workflow run: %w", err)
	}
	if run.Repository.FullName != campaign.Repository || run.Path != campaign.WorkflowPath || run.Event != campaign.Event || run.HeadBranch != "main" || run.HeadSHA != campaign.ControllerCommit || run.ID != campaign.RunID || run.RunAttempt != campaign.RunAttempt || run.Actor.Type != campaign.TrustedActorClass {
		return liveWorkflowRun{}, fmt.Errorf("live workflow run identity differs from campaign")
	}
	return run, nil
}

type LiveAuthoritySessionRequest struct {
	Campaign         LiveCampaign
	Definition       LiveDefinition
	DefinitionSHA256 string
	EngineSHA256     string
	ValidatorSHA256  string
	Now              time.Time
}

type LiveAuthoritySession struct {
	campaignID [32]byte
	campaign   LiveCampaign
	definition liveAuthorityDefinition
	now        time.Time
	mu         sync.Mutex
	minted     map[liveAuthorityPermitKey]struct{}
}

type liveAuthorityDefinition struct {
	definition, engine, seed, packageRef    [32]byte
	comparator, targets, observer, workflow [32]byte
	packageArguments                        []string
}

type liveAuthorityPermitKey struct {
	operation liveOperation
	sequence  uint64
}

func NewLiveAuthoritySession(ctx context.Context, client LiveWorkflowRunClient, request LiveAuthoritySessionRequest) (*LiveAuthoritySession, error) {
	if err := ValidateLiveCampaign(request.Campaign); err != nil || request.Campaign.Mode != LiveCampaignScheduledQualification || request.Campaign.RunID == 0 || request.Campaign.RunAttempt != 1 {
		return nil, fmt.Errorf("live authority campaign is invalid")
	}
	if request.Now.IsZero() || request.Now.Location() != time.UTC || !request.Campaign.ExpiresAt.After(request.Now) || request.Campaign.ExpiresAt.Sub(request.Now) > 24*time.Hour {
		return nil, fmt.Errorf("live authority campaign expiry is invalid")
	}
	if _, err := client.Fetch(ctx, request.Campaign); err != nil {
		return nil, err
	}
	if err := validateLiveDefinition(request.Definition); err != nil {
		return nil, fmt.Errorf("live authority definition is invalid")
	}
	definitionDigest, err := CanonicalLiveDefinitionSHA256(request.Definition)
	if err != nil || definitionDigest != request.DefinitionSHA256 || request.Definition.ModuleID != request.Campaign.ModuleID || request.Definition.ModuleRevision != request.Campaign.ModuleRevision || request.Definition.ValidationSourceSHA256 != request.Campaign.ValidationSourceSHA256 || request.Definition.SeedSHA256 != request.Campaign.SeedSHA256 || request.Definition.WingetRef != request.Campaign.PackageRef || request.EngineSHA256 != request.Campaign.EngineSHA256 || request.ValidatorSHA256 != request.Campaign.ValidatorSHA256 {
		return nil, fmt.Errorf("live authority source or binary binding differs from campaign")
	}
	if liveSHA256Hex(request.Definition.Comparator) != request.Campaign.ComparatorSHA256 || liveSHA256Hex(request.Definition.Comparator.Mappings) != request.Campaign.TargetsSHA256 || liveSHA256Hex(request.Definition.Observer) != request.Campaign.ObserverSHA256 {
		return nil, fmt.Errorf("live authority comparator or observer binding differs from campaign")
	}
	identity, err := CanonicalLiveCampaignIdentity(request.Campaign)
	if err != nil {
		return nil, err
	}
	return &LiveAuthoritySession{campaignID: liveSHA256Bytes(identity), campaign: request.Campaign, now: request.Now, minted: make(map[liveAuthorityPermitKey]struct{}), definition: liveAuthorityDefinition{
		definition: liveSHA256Bytes(request.DefinitionSHA256), engine: liveSHA256Bytes(request.EngineSHA256), seed: liveSHA256Bytes(request.Definition.SeedSHA256), packageRef: liveSHA256Bytes(request.Campaign.PackageRef),
		comparator: liveSHA256Bytes(request.Campaign.ComparatorSHA256), targets: liveSHA256Bytes(request.Campaign.TargetsSHA256), observer: liveSHA256Bytes(request.Campaign.ObserverSHA256), workflow: liveSHA256Bytes(request.Campaign.WorkflowPolicySHA256), packageArguments: append([]string(nil), request.Campaign.PackageArguments...),
	}}, nil
}

func (session *LiveAuthoritySession) NonceFor(operation liveOperation, sequence uint64) [32]byte {
	if session == nil {
		return [32]byte{}
	}
	return liveAuthorityNonce(session.campaign.PhaseNonce, operation, sequence)
}

func (session *LiveAuthoritySession) MintMutationPermit(operation liveOperation, sequence uint64, nonce [32]byte) (trustedLiveMutationPermit, error) {
	if session == nil || !liveAuthorityOperationSequence(operation, sequence) || nonce != session.NonceFor(operation, sequence) {
		return trustedLiveMutationPermit{}, fmt.Errorf("live mutation operation is not predeclared")
	}
	key := liveAuthorityPermitKey{operation: operation, sequence: sequence}
	session.mu.Lock()
	defer session.mu.Unlock()
	if _, exists := session.minted[key]; exists {
		return trustedLiveMutationPermit{}, fmt.Errorf("live mutation permit already minted")
	}
	session.minted[key] = struct{}{}
	capability := &liveMutationCapability{campaign: session.campaignID, operation: operation, sequence: sequence, nonce: nonce, issuedAt: session.now, expiresAt: session.campaign.ExpiresAt, definition: session.definition.definition, engine: session.definition.engine, seed: session.definition.seed, packageRef: session.definition.packageRef, comparator: session.definition.comparator, targets: session.definition.targets, observer: session.definition.observer, workflow: session.definition.workflow, packageArguments: append([]string(nil), session.definition.packageArguments...)}
	return trustedLiveMutationPermit{capability: capability}, nil
}

func liveAuthorityOperationSequence(operation liveOperation, sequence uint64) bool {
	return map[uint64]liveOperation{1: liveOperationWingetExactInstall, 2: liveOperationHashBoundSeed, 3: liveOperationEngineApply, 4: liveOperationEngineVerify, 5: liveOperationEngineCapture, 6: liveOperationEngineRebuild, 7: liveOperationEngineRevert, 8: liveOperationEngineRebuild, 9: liveOperationEngineRebuild}[sequence] == operation
}

func liveAuthorityNonce(phaseNonce string, operation liveOperation, sequence uint64) [32]byte {
	return sha256.Sum256([]byte(phaseNonce + "\x00" + string(operation) + "\x00" + fmt.Sprint(sequence)))
}

func liveSHA256Hex(value any) string {
	encoded, _ := json.Marshal(value)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
func liveSHA256Bytes(value string) [32]byte {
	decoded, _ := hex.DecodeString(value)
	var result [32]byte
	copy(result[:], decoded)
	return result
}
