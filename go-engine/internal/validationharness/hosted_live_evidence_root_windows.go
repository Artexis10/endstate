// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

type hostedLiveEvidenceResultRoot struct {
	parent     string
	path       string
	campaign   LiveCampaign
	id         string
	definition string
	nonce      [32]byte
	object     windowsLiveObjectIdentity
}

var windowsHostedLiveEvidenceRoots sync.Map
var windowsHostedLiveEvidenceRootBeforePersist func(string)

func newHostedLiveEvidenceResultRoot(campaign LiveCampaign, definition LiveDefinition) (hostedLiveEvidenceResultRoot, error) {
	id, err := CanonicalLiveCampaignIdentity(campaign)
	if err != nil {
		return hostedLiveEvidenceResultRoot{}, fmt.Errorf("hosted live result campaign is invalid")
	}
	definitionID, err := CanonicalLiveDefinitionSHA256(definition)
	if err != nil {
		return hostedLiveEvidenceResultRoot{}, fmt.Errorf("hosted live result definition is invalid")
	}
	parent, err := windowsLiveRunnerTemp()
	if err != nil || parent == "" || filepath.Clean(parent) != parent || safepath.ValidateRoot(parent) != nil {
		return hostedLiveEvidenceResultRoot{}, fmt.Errorf("hosted live result parent is unsafe")
	}
	path := filepath.Join(parent, hostedLiveEvidenceResultRootName(id, campaign.RunID, campaign.RunAttempt))
	if err := os.Mkdir(path, 0o700); err != nil {
		return hostedLiveEvidenceResultRoot{}, fmt.Errorf("create hosted live result root: %w", err)
	}
	root := hostedLiveEvidenceResultRoot{parent: parent, path: path, campaign: campaign, id: id, definition: definitionID}
	if _, err := rand.Read(root.nonce[:]); err != nil || root.nonce == ([32]byte{}) {
		_ = os.Remove(path)
		return hostedLiveEvidenceResultRoot{}, fmt.Errorf("hosted live result ownership receipt is unavailable")
	}
	object, err := windowsLiveObjectIdentityForPath(path, true)
	if err != nil {
		_ = os.Remove(path)
		return hostedLiveEvidenceResultRoot{}, fmt.Errorf("hosted live result root identity is unavailable")
	}
	root.object = object
	windowsHostedLiveEvidenceRoots.Store(root.nonce, root)
	if !root.valid() {
		windowsHostedLiveEvidenceRoots.Delete(root.nonce)
		_ = os.Remove(path)
		return hostedLiveEvidenceResultRoot{}, fmt.Errorf("hosted live result root is unsafe")
	}
	return root, nil
}

func (root hostedLiveEvidenceResultRoot) valid() bool {
	if !root.registered() || root.definition == "" || safepath.ValidateRoot(root.parent) != nil || !strings.EqualFold(filepath.Dir(root.path), root.parent) || filepath.Base(root.path) != hostedLiveEvidenceResultRootName(root.id, root.campaign.RunID, root.campaign.RunAttempt) {
		return false
	}
	canonical, err := CanonicalLiveCampaignIdentity(root.campaign)
	if err != nil || canonical != root.id {
		return false
	}
	object, err := windowsLiveObjectIdentityForPath(root.path, true)
	return err == nil && object == root.object
}

func (root hostedLiveEvidenceResultRoot) registered() bool {
	if root.parent == "" || root.path == "" || root.id == "" || root.nonce == ([32]byte{}) {
		return false
	}
	value, exists := windowsHostedLiveEvidenceRoots.Load(root.nonce)
	owned, ok := value.(hostedLiveEvidenceResultRoot)
	return exists && ok && owned.parent == root.parent && owned.path == root.path && owned.id == root.id && owned.definition == root.definition && owned.nonce == root.nonce && owned.object == root.object
}

// persistHostedLiveEvidence encodes typed evidence itself and writes it once to
// the fixed leaf beneath a registered, Windows-owned result-root capability.
func persistHostedLiveEvidence(root hostedLiveEvidenceResultRoot, evidence hostedLiveEvidence) error {
	if !root.valid() || evidence.Inputs.DefinitionSHA256 != root.definition || !hostedLiveEvidenceMatchesCampaign(evidence, root.campaign, root.id) {
		return fmt.Errorf("hosted live result root is invalid")
	}
	encoded, err := encodeHostedLiveEvidence(evidence)
	if err != nil {
		return err
	}
	if windowsHostedLiveEvidenceRootBeforePersist != nil {
		windowsHostedLiveEvidenceRootBeforePersist(root.path)
	}
	if !root.valid() {
		return fmt.Errorf("hosted live result root changed")
	}
	path, err := safepath.Resolve(root.path, hostedLiveEvidenceFilename)
	if err != nil {
		return fmt.Errorf("hosted live result path is invalid")
	}
	if existing, err := os.Lstat(path); err == nil {
		if safepath.IsLinkOrReparse(existing) || !existing.Mode().IsRegular() {
			return fmt.Errorf("hosted live result leaf is unsafe")
		}
		return fmt.Errorf("hosted live result already exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect hosted live result leaf: %w", err)
	}
	if err := safepath.AtomicWriteFile(path, encoded, 0o600); err != nil {
		return fmt.Errorf("persist hosted live evidence: %w", err)
	}
	if !root.valid() {
		return fmt.Errorf("hosted live result root changed")
	}
	return nil
}
