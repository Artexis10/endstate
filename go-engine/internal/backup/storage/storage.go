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
	"errors"
	"fmt"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/backup/client"
	"github.com/Artexis10/endstate/go-engine/internal/backup/oidc"
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
//
// The base URL for /api/backups/* endpoints is resolved per-request from
// the OIDC discovery document's `endstate_extensions.backup_api_base`
// field (contract §9). The OIDC client's 1-hour cache means there is no
// per-request fetch; resolution is effectively free after warmup.
//
// /api/account/* is NOT under backup_api_base — it lives off the
// issuer. We retain the issuer here for that reason.
type Client struct {
	issuer string
	oc     *oidc.Client
	httpc  *client.Client
}

// New returns a Client. issuer is the OIDC issuer URL (with or without
// trailing slash); oc is the discovery client used to resolve
// `backup_api_base`. Validation failures fail closed. A discovery transport
// failure falls back to `${issuer}/api/backups` only after this OIDC client has
// already accepted a discovery document for the configured issuer.
func New(issuer string, oc *oidc.Client, hc *client.Client) *Client {
	return &Client{
		issuer: strings.TrimRight(issuer, "/"),
		oc:     oc,
		httpc:  hc,
	}
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
	url, err := c.url(ctx, "")
	if err != nil {
		return nil, err
	}
	var resp listResp
	if err := c.httpc.Do(ctx, client.Request{
		Method:   "GET",
		URL:      url,
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
	url, err := c.url(ctx, "")
	if err != nil {
		return "", err
	}
	var out resp
	if err := c.httpc.Do(ctx, client.Request{
		Method:   "POST",
		URL:      url,
		Body:     req{Name: name},
		ReadOnly: false,
	}, &out); err != nil {
		return "", err
	}
	return out.BackupID, nil
}

// DeleteBackup permanently removes a backup and all its versions.
func (c *Client) DeleteBackup(ctx context.Context, backupID string) *envelope.Error {
	url, err := c.url(ctx, "/"+backupID)
	if err != nil {
		return err
	}
	return c.httpc.Do(ctx, client.Request{
		Method:   "DELETE",
		URL:      url,
		ReadOnly: false,
	}, nil)
}

// UpdatedBackup is the response of PATCH /api/backups/:id.
type UpdatedBackup struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	UpdatedAt string `json:"updatedAt"`
}

// UpdateBackup changes a backup's mutable metadata (today only its display
// name) via PATCH. The backend id is the durable identity and is unchanged;
// only the label moves.
func (c *Client) UpdateBackup(ctx context.Context, backupID, name string) (UpdatedBackup, *envelope.Error) {
	type req struct {
		Name string `json:"name"`
	}
	url, err := c.url(ctx, "/"+backupID)
	if err != nil {
		return UpdatedBackup{}, err
	}
	var out UpdatedBackup
	if err := c.httpc.Do(ctx, client.Request{
		Method:   "PATCH",
		URL:      url,
		Body:     req{Name: name},
		ReadOnly: false,
	}, &out); err != nil {
		return UpdatedBackup{}, err
	}
	return out, nil
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
	url, err := c.url(ctx, "/"+backupID+"/versions")
	if err != nil {
		return nil, err
	}
	var resp vresp
	if err := c.httpc.Do(ctx, client.Request{
		Method:   "GET",
		URL:      url,
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
	url, err := c.url(ctx, "/"+backupID+"/versions/"+versionID)
	if err != nil {
		return err
	}
	return c.httpc.Do(ctx, client.Request{
		Method:   "DELETE",
		URL:      url,
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
	VersionID        string         `json:"versionId"`
	UploadURLs       []PresignedURL `json:"uploadUrls"`
	RequiresCommit   bool           `json:"requiresCommit"`
	AlreadyCommitted bool           `json:"alreadyCommitted"`
}

// CreateVersion creates a new version row and returns presigned upload
// URLs the engine PUTs the manifest + chunks to.
func (c *Client) CreateVersion(ctx context.Context, backupID string, encryptedManifest []byte, chunkMeta []ChunkMetaWire) (*CreateVersionResponse, *envelope.Error) {
	return c.createVersion(ctx, backupID, encryptedManifest, chunkMeta, "", false)
}

// SupportsVersionCreateOperationReplay returns true only when the issuer has
// explicitly advertised the replay protocol. Discovery failures and malformed
// optional capability data are legacy-safe false.
func (c *Client) SupportsVersionCreateOperationReplay(ctx context.Context) bool {
	if c.oc == nil {
		return false
	}
	doc, err := c.oc.Discovery(ctx)
	return err == nil && doc.EndstateExtensions.SupportsVersionCreateOperationReplay()
}

// CreateVersionWithReplay uses a caller-owned stable operation ID only when
// discovery explicitly advertises the replay protocol. It otherwise remains a
// one-shot legacy mutation.
func (c *Client) CreateVersionWithReplay(ctx context.Context, backupID string, encryptedManifest []byte, chunkMeta []ChunkMetaWire, operationID string) (*CreateVersionResponse, *envelope.Error) {
	if operationID == "" {
		return c.createVersion(ctx, backupID, encryptedManifest, chunkMeta, "", false)
	}
	if !c.SupportsVersionCreateOperationReplay(ctx) {
		return nil, envelope.NewError(envelope.ErrBackendIncompatible,
			"create-version replay capability disappeared; refusing to send a persisted operation as a legacy create")
	}
	return c.createVersion(ctx, backupID, encryptedManifest, chunkMeta, operationID, true)
}

func (c *Client) createVersion(ctx context.Context, backupID string, encryptedManifest []byte, chunkMeta []ChunkMetaWire, operationID string, retrySafe bool) (*CreateVersionResponse, *envelope.Error) {
	// Substrate accepts the encrypted manifest as a base64 string in the
	// JSON body; chunkMeta is the array of {index, encryptedSize, sha256}
	// triples used to mint upload URLs.
	type req struct {
		EncryptedManifest []byte          `json:"encryptedManifest"`
		ChunkMetadata     []ChunkMetaWire `json:"chunkMetadata"`
	}
	url, err := c.url(ctx, "/"+backupID+"/versions")
	if err != nil {
		return nil, err
	}
	var resp CreateVersionResponse
	if err := c.httpc.Do(ctx, client.Request{
		Method:   "POST",
		URL:      url,
		Body:     req{EncryptedManifest: encryptedManifest, ChunkMetadata: chunkMeta},
		ReadOnly: false,
		// A legacy create endpoint may not deduplicate an operation ID after a
		// lost response. Do not replay this mutating POST until create itself
		// has an explicit server acknowledgement of idempotency.
		RetrySafe:   retrySafe,
		OperationID: operationID,
	}, &resp); err != nil {
		return nil, err
	}
	if strings.TrimSpace(resp.VersionID) == "" {
		return nil, envelope.NewError(envelope.ErrBackendIncompatible, "CreateVersion response missing versionId").
			WithRemediation("This backend appears to violate contract §7. Update the engine or contact support.")
	}
	if resp.AlreadyCommitted {
		return &resp, nil
	}
	if !containsManifestURL(resp.UploadURLs) {
		return nil, envelope.NewError(envelope.ErrBackendIncompatible,
			fmt.Sprintf("CreateVersion response missing manifest URL (chunkIndex == %d)", ManifestChunkIndex)).
			WithRemediation("This backend appears to violate contract §7. Update the engine or contact support.")
	}
	return &resp, nil
}

// CommitVersion finalises a version created by CreateVersion, calling
// `POST /api/backups/:backupId/versions/:versionId/commit` (contract §7).
//
// Until this call lands, a version created by a schema-2.1 client is not
// durable: substrate does not list it, does not count it against quota,
// and does not prune retention on its behalf (contract §8). Committing is
// therefore the moment a generation becomes a restore target — which is
// why upload.PushVersion only calls it after every chunk AND the manifest
// have been PUT successfully.
//
// The endpoint is idempotent server-side: a repeated commit of an
// already-committed version succeeds and changes nothing.
//
// Callers invoke this only when CreateVersionResponse.RequiresCommit is true.
// In that state every non-2xx result, including 404, is a failed durability
// transition. Legacy servers omit RequiresCommit, so upload does not call this
// route for them at all.
//
// Like every sibling call, the URL is resolved through backupBaseURL so
// `endstate_extensions.backup_api_base` (contract §9) is honoured verbatim.
func (c *Client) CommitVersion(ctx context.Context, backupID, versionID string) (bool, *envelope.Error) {
	url, resolveErr := c.url(ctx, "/"+backupID+"/versions/"+versionID+"/commit")
	if resolveErr != nil {
		return false, resolveErr
	}
	err := c.httpc.Do(ctx, client.Request{
		Method:    "POST",
		URL:       url,
		ReadOnly:  false,
		RetrySafe: true,
	}, nil)
	if err != nil {
		return false, err
	}
	return true, nil
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
	url, err := c.url(ctx, "/"+backupID+"/versions/"+versionID+"/download-urls")
	if err != nil {
		return nil, err
	}
	var out resp
	if err := c.httpc.Do(ctx, client.Request{
		Method:   "POST",
		URL:      url,
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

// backupBaseURL resolves the /api/backups base URL via discovery
// (contract §9, `endstate_extensions.backup_api_base`). Validation errors
// stop the call before it can fall back to an issuer-derived storage URL.
func (c *Client) backupBaseURL(ctx context.Context) (string, *envelope.Error) {
	doc, err := c.oc.Discovery(ctx)
	if err == nil && doc.EndstateExtensions.BackupAPIBase != "" {
		return strings.TrimRight(doc.EndstateExtensions.BackupAPIBase, "/"), nil
	}
	if errors.Is(err, oidc.ErrIssuerMismatch) {
		return "", envelope.NewError(envelope.ErrBackendIncompatible,
			"The configured backend issuer does not match its OIDC discovery document.").
			WithRemediation("Set ENDSTATE_OIDC_ISSUER_URL to the same value on both sides, or check that your substrate deployment has it set in its server-side env.")
	}
	if errors.Is(err, oidc.ErrIncompatibleIssuer) {
		return "", envelope.NewError(envelope.ErrBackendIncompatible,
			"The configured backend's discovery document is incompatible with Endstate Backup.").
			WithRemediation("Verify ENDSTATE_OIDC_ISSUER_URL points at a substrate-compatible backend.")
	}
	if errors.Is(err, oidc.ErrDiscoveryTransport) && c.oc.HasCachedDiscovery() {
		return c.issuer + "/api/backups", nil
	}
	return "", envelope.NewError(envelope.ErrBackendUnreachable,
		"Could not fetch OIDC discovery before contacting the Endstate backup service.").
		WithRemediation("Check your network connection or override ENDSTATE_OIDC_ISSUER_URL.")
}

func (c *Client) url(ctx context.Context, suffix string) (string, *envelope.Error) {
	base, err := c.backupBaseURL(ctx)
	if err != nil {
		return "", err
	}
	return base + suffix, nil
}

func containsManifestURL(urls []PresignedURL) bool {
	for _, u := range urls {
		if u.ChunkIndex == ManifestChunkIndex {
			return true
		}
	}
	return false
}
