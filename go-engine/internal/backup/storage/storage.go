// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package storage wraps the substrate `/api/backups/*` API surface
// defined in docs/contracts/hosted-backup-contract.md §7.
//
// Every method returns the engine's domain error type (*envelope.Error)
// suitable for direct return from a command handler. The package never
// touches plaintext profile contents — that work happens in the
// upload/download packages, which in turn call into crypto.
//
// Manifest URL convention (contract §7): the manifest blob is addressed
// in `uploadUrls` / `urls` arrays by the sentinel `chunkIndex == -1`.
// This is a wire-protocol flag; it is independent of the cryptographic
// AAD sentinel `0xFFFFFFFF` used inside the encrypted manifest blob
// (contract §3). Implementations MUST treat the two as independent —
// the constants below carry comments to that effect.
package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/backup/client"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// ManifestChunkIndex is the wire-protocol sentinel used in
// uploadUrls/urls arrays to identify the manifest URL (contract §7).
// Distinct from `crypto.ManifestAAD` (`0xFFFFFFFF`) which is the
// cryptographic AAD inside the encrypted manifest blob (contract §3).
const ManifestChunkIndex int = -1

// Client wraps the storage API surface. Construct it with the same
// HTTP + OIDC clients the auth package uses so JWT and refresh-token
// state stays consistent across calls.
type Client struct {
	issuer string
	httpc  *client.Client
}

// New returns a Client. issuer is the OIDC issuer URL with no trailing
// slash; the storage endpoints live under `${issuer}/api/backups/...`.
func New(issuer string, hc *client.Client) *Client {
	return &Client{issuer: strings.TrimRight(issuer, "/"), httpc: hc}
}

// Backup is one row of the GET /api/backups response.
type Backup struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	LatestVersionID string `json:"latestVersionId,omitempty"`
	VersionCount    int    `json:"versionCount"`
	TotalSize       int64  `json:"totalSize"`
	UpdatedAt       string `json:"updatedAt"`
}

// ListBackups returns the user's backups. Read-only — minor version
// mismatches degrade to a warning.
func (c *Client) ListBackups(ctx context.Context) ([]Backup, *envelope.Error) {
	type listResp struct {
		Backups []Backup `json:"backups"`
	}
	var resp listResp
	if err := c.httpc.Do(ctx, client.Request{
		Method:   "GET",
		URL:      c.url(""),
		ReadOnly: true,
	}, &resp); err != nil {
		return nil, err
	}
	if resp.Backups == nil {
		resp.Backups = []Backup{}
	}
	return resp.Backups, nil
}

// CreateBackup creates a new backup metadata row and returns its id.
func (c *Client) CreateBackup(ctx context.Context, name string) (string, *envelope.Error) {
	type req struct {
		Name string `json:"name"`
	}
	type resp struct {
		BackupID string `json:"backupId"`
	}
	var out resp
	if err := c.httpc.Do(ctx, client.Request{
		Method:   "POST",
		URL:      c.url(""),
		Body:     req{Name: name},
		ReadOnly: false,
	}, &out); err != nil {
		return "", err
	}
	return out.BackupID, nil
}

// DeleteBackup permanently removes a backup and all its versions.
func (c *Client) DeleteBackup(ctx context.Context, backupID string) *envelope.Error {
	return c.httpc.Do(ctx, client.Request{
		Method:   "DELETE",
		URL:      c.url("/" + backupID),
		ReadOnly: false,
	}, nil)
}

// VersionInfo is one row of GET /api/backups/:id/versions.
type VersionInfo struct {
	VersionID      string `json:"versionId"`
	CreatedAt      string `json:"createdAt"`
	Size           int64  `json:"size"`
	ManifestSHA256 string `json:"manifestSha256"`
}

// ListVersions returns the versions of one backup.
func (c *Client) ListVersions(ctx context.Context, backupID string) ([]VersionInfo, *envelope.Error) {
	type vresp struct {
		Versions []VersionInfo `json:"versions"`
	}
	var resp vresp
	if err := c.httpc.Do(ctx, client.Request{
		Method:   "GET",
		URL:      c.url("/" + backupID + "/versions"),
		ReadOnly: true,
	}, &resp); err != nil {
		return nil, err
	}
	if resp.Versions == nil {
		resp.Versions = []VersionInfo{}
	}
	return resp.Versions, nil
}

// DeleteVersion soft-deletes one version. Substrate purges the blob
// from R2 after a 7-day retention window per contract §8.
func (c *Client) DeleteVersion(ctx context.Context, backupID, versionID string) *envelope.Error {
	return c.httpc.Do(ctx, client.Request{
		Method:   "DELETE",
		URL:      c.url("/" + backupID + "/versions/" + versionID),
		ReadOnly: false,
	}, nil)
}

// PresignedURL is one entry in an uploadUrls / urls array. Manifest URLs
// carry ChunkIndex == ManifestChunkIndex (-1).
type PresignedURL struct {
	ChunkIndex   int    `json:"chunkIndex"`
	PresignedURL string `json:"presignedUrl"`
	ExpiresAt    string `json:"expiresAt"`
}

// CreateVersionResponse is the substrate response from POST .../versions.
// `UploadURLs` includes the manifest URL with ChunkIndex == -1 as the
// first entry per contract §7.
type CreateVersionResponse struct {
	VersionID  string         `json:"versionId"`
	UploadURLs []PresignedURL `json:"uploadUrls"`
}

// CreateVersion creates a new version row and returns presigned upload
// URLs the engine PUTs the manifest + chunks to.
func (c *Client) CreateVersion(ctx context.Context, backupID string, encryptedManifest []byte, chunkMeta []ChunkMetaWire) (*CreateVersionResponse, *envelope.Error) {
	// Substrate accepts the encrypted manifest as a base64 string in the
	// JSON body; chunkMeta is the array of {index, encryptedSize, sha256}
	// triples used to mint upload URLs.
	type req struct {
		EncryptedManifest []byte           `json:"encryptedManifest"`
		ChunkMetadata     []ChunkMetaWire  `json:"chunkMetadata"`
	}
	var resp CreateVersionResponse
	if err := c.httpc.Do(ctx, client.Request{
		Method:   "POST",
		URL:      c.url("/" + backupID + "/versions"),
		Body:     req{EncryptedManifest: encryptedManifest, ChunkMetadata: chunkMeta},
		ReadOnly: false,
	}, &resp); err != nil {
		return nil, err
	}
	if !containsManifestURL(resp.UploadURLs) {
		return nil, envelope.NewError(envelope.ErrBackendIncompatible,
			fmt.Sprintf("CreateVersion response missing manifest URL (chunkIndex == %d)", ManifestChunkIndex)).
			WithRemediation("This backend appears to violate contract §7. Update the engine or contact support.")
	}
	return &resp, nil
}

// ChunkMetaWire is the on-the-wire shape of a chunk-metadata entry sent
// to substrate in CreateVersion. Distinct from manifest.ChunkMeta to
// keep wire and storage models loosely coupled.
type ChunkMetaWire struct {
	Index         uint32 `json:"index"`
	EncryptedSize int64  `json:"encryptedSize"`
	SHA256        string `json:"sha256"`
}

// DownloadURLs requests presigned GET URLs for a set of chunk indices.
// Callers MUST include `-1` to receive the manifest URL.
func (c *Client) DownloadURLs(ctx context.Context, backupID, versionID string, chunkIndices []int) ([]PresignedURL, *envelope.Error) {
	type req struct {
		ChunkIndices []int `json:"chunkIndices"`
	}
	type resp struct {
		URLs []PresignedURL `json:"urls"`
	}
	var out resp
	if err := c.httpc.Do(ctx, client.Request{
		Method:   "POST",
		URL:      c.url("/" + backupID + "/versions/" + versionID + "/download-urls"),
		Body:     req{ChunkIndices: chunkIndices},
		ReadOnly: true,
	}, &out); err != nil {
		return nil, err
	}
	if out.URLs == nil {
		out.URLs = []PresignedURL{}
	}
	return out.URLs, nil
}

// FindManifestURL returns the entry whose ChunkIndex == -1, or nil if
// the array does not contain one.
func FindManifestURL(urls []PresignedURL) *PresignedURL {
	for i := range urls {
		if urls[i].ChunkIndex == ManifestChunkIndex {
			return &urls[i]
		}
	}
	return nil
}

// FindChunkURL returns the entry whose ChunkIndex matches idx, or nil
// if not found.
func FindChunkURL(urls []PresignedURL, idx uint32) *PresignedURL {
	for i := range urls {
		if urls[i].ChunkIndex >= 0 && uint32(urls[i].ChunkIndex) == idx {
			return &urls[i]
		}
	}
	return nil
}

// DeleteAccount calls DELETE /api/account (contract §12).
func (c *Client) DeleteAccount(ctx context.Context) *envelope.Error {
	return c.httpc.Do(ctx, client.Request{
		Method:   "DELETE",
		URL:      c.issuer + "/api/account",
		ReadOnly: false,
	}, nil)
}

func (c *Client) url(suffix string) string {
	return c.issuer + "/api/backups" + suffix
}

func containsManifestURL(urls []PresignedURL) bool {
	for _, u := range urls {
		if u.ChunkIndex == ManifestChunkIndex {
			return true
		}
	}
	return false
}
