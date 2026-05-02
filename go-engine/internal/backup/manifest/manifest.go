// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package manifest models the encrypted-version manifest defined in
// docs/contracts/hosted-backup-contract.md §3 and the version-selection
// helpers used by `endstate backup pull`.
package manifest

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/Artexis10/endstate/go-engine/internal/backup/crypto"
)

// ChunkMeta describes one chunk in the encrypted manifest. Index 0..n-1
// are the data chunks; the manifest itself is stored separately at the
// transport-layer sentinel `chunkIndex == -1` (contract §7) and is
// cryptographically bound to the AAD sentinel `0xFFFFFFFF` (contract §3).
type ChunkMeta struct {
	Index         uint32 `json:"index"`
	EncryptedSize int64  `json:"encryptedSize"`
	SHA256        string `json:"sha256"`
}

// Manifest is the on-the-wire JSON shape of the encrypted manifest.
// `WrappedDEK` is base64-encoded; `KDF` lets the recipient verify the
// envelope was produced under acceptable parameters before decrypting.
type Manifest struct {
	EnvelopeVersion int               `json:"envelopeVersion"`
	VersionID       string            `json:"versionId"`
	CreatedAt       string            `json:"createdAt"`
	OriginalSize    int64             `json:"originalSize"`
	ChunkSize       int64             `json:"chunkSize"`
	ChunkCount      int               `json:"chunkCount"`
	Chunks          []ChunkMeta       `json:"chunks"`
	KDF             crypto.KDFParams  `json:"kdf"`
	WrappedDEK      string            `json:"wrappedDEK"`
}

// Marshal serialises the manifest to compact JSON ready for encryption.
func Marshal(m *Manifest) ([]byte, error) {
	if m.EnvelopeVersion == 0 {
		m.EnvelopeVersion = crypto.EnvelopeVersion
	}
	return json.Marshal(m)
}

// Unmarshal parses a decrypted manifest blob and rejects unknown
// envelope versions. The engine refuses to load a manifest with a higher
// envelope version than it understands; older versions are accepted.
func Unmarshal(b []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("manifest: decode: %w", err)
	}
	if m.EnvelopeVersion == 0 {
		m.EnvelopeVersion = 1
	}
	if m.EnvelopeVersion > crypto.EnvelopeVersion {
		return nil, fmt.Errorf("manifest: envelopeVersion %d exceeds engine support %d", m.EnvelopeVersion, crypto.EnvelopeVersion)
	}
	return &m, nil
}

// Version is one row of `GET /api/backups/:id/versions`.
type Version struct {
	VersionID      string `json:"versionId"`
	CreatedAt      string `json:"createdAt"`
	Size           int64  `json:"size"`
	ManifestSHA256 string `json:"manifestSha256"`
}

// SelectLatest returns the newest version by `createdAt` (ISO 8601;
// lexicographic compare is correct for fixed-width UTC values). Tie-break
// by `versionId` lex order so the result is deterministic.
//
// Returns an error if the slice is empty.
func SelectLatest(versions []Version) (*Version, error) {
	if len(versions) == 0 {
		return nil, errors.New("manifest: no versions to select from")
	}
	cp := make([]Version, len(versions))
	copy(cp, versions)
	sort.Slice(cp, func(i, j int) bool {
		if cp[i].CreatedAt != cp[j].CreatedAt {
			return cp[i].CreatedAt > cp[j].CreatedAt
		}
		return cp[i].VersionID > cp[j].VersionID
	})
	return &cp[0], nil
}
