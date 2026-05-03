// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package download orchestrates the chunked, encrypted download of a
// backup version from Endstate Hosted Backup.
//
// Pipeline (contract §3, §7, §8):
//
//   storage.DownloadURLs([-1])                  → manifest URL
//        ↓
//   GET manifest URL → AES-256-GCM open (0xFFFFFFFF AAD) → manifest JSON
//        ↓
//   storage.DownloadURLs([0..N-1])              → chunk URLs
//        ↓
//   GET each chunk → SHA-256 verify (vs manifest) → AES-256-GCM open (chunkIndex AAD)
//        ↓
//   concatenate plaintext → untar to disk at --to
//
// SHA-256 is verified BEFORE any decrypt attempt; mismatch returns an
// integrity error and writes nothing to disk. The DEK is loaded from the
// session and zeroed on the way out.
package download

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/Artexis10/endstate/go-engine/internal/backup"
	"github.com/Artexis10/endstate/go-engine/internal/backup/auth"
	"github.com/Artexis10/endstate/go-engine/internal/backup/crypto"
	"github.com/Artexis10/endstate/go-engine/internal/backup/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/backup/storage"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
)

// PullResult is returned to the command handler on a successful pull.
type PullResult struct {
	BackupID  string
	VersionID string
	WrittenTo string
}

// Dependencies are the moving pieces a pull operation needs.
type Dependencies struct {
	Storage     *storage.Client
	Session     *auth.SessionStore
	Events      *events.Emitter
	HTTPClient  *http.Client // for presigned GET from R2; nil → http.DefaultClient
	Concurrency int          // bounded parallelism for chunk GETs
}

// PullVersion executes the download pipeline.
func PullVersion(ctx context.Context, deps Dependencies, backupID, versionID, to string, overwrite bool) (*PullResult, *envelope.Error) {
	if strings.TrimSpace(backupID) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError, "download: backupID is empty")
	}
	if strings.TrimSpace(to) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError, "download: target path is empty")
	}

	if _, statErr := os.Stat(to); statErr == nil && !overwrite {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup pull: target path already exists").
			WithDetail(map[string]string{"path": to}).
			WithRemediation("Pass --overwrite to replace the contents, or choose a different --to path.")
	}

	dek, lerr := deps.Session.LoadDEK()
	if lerr != nil {
		return nil, envelope.NewError(envelope.ErrAuthRequired,
			"backup pull: no DEK in keychain — sign in first").
			WithRemediation("Run `endstate backup login` to populate the session.")
	}
	defer wipe(dek)

	resolvedVersionID := strings.TrimSpace(versionID)
	if resolvedVersionID == "" {
		versions, err := deps.Storage.ListVersions(ctx, backupID)
		if err != nil {
			return nil, err
		}
		if len(versions) == 0 {
			return nil, envelope.NewError(envelope.ErrNotFound,
				"backup pull: backup has no versions to restore").
				WithRemediation("Push a profile first via `endstate backup push --profile <path>`.")
		}
		latest, lerr := manifest.SelectLatest(toManifestVersions(versions))
		if lerr != nil {
			return nil, envelope.NewError(envelope.ErrInternalError, "backup pull: select latest version: "+lerr.Error())
		}
		resolvedVersionID = latest.VersionID
	}

	deps.Events.EmitPhase("backup-pull")

	httpClient := deps.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	concurrency := deps.Concurrency
	if concurrency <= 0 {
		concurrency = backup.Concurrency()
	}

	// Step 1: fetch manifest URL.
	manifestURLs, mErr := deps.Storage.DownloadURLs(ctx, backupID, resolvedVersionID, []int{storage.ManifestChunkIndex})
	if mErr != nil {
		return nil, mErr
	}
	manifestURL := storage.FindManifestURL(manifestURLs)
	if manifestURL == nil {
		return nil, envelope.NewError(envelope.ErrBackendIncompatible,
			"backup pull: substrate did not return a manifest URL").
			WithRemediation("Update the engine; this typically means a substrate response shape changed.")
	}

	encManifest, gerr := getOnce(ctx, httpClient, manifestURL.PresignedURL)
	if gerr != nil {
		return nil, envelope.NewError(envelope.ErrBackendUnreachable,
			"backup pull: download manifest: "+gerr.Error())
	}

	mfJSON, dmErr := crypto.DecryptManifest(encManifest, dek)
	if dmErr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup pull: decrypt manifest: "+dmErr.Error()).
			WithRemediation("Run `endstate backup login` again to refresh the cached DEK; if this persists, the manifest may be corrupt.")
	}
	mf, mErr2 := manifest.Unmarshal(mfJSON)
	if mErr2 != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup pull: parse manifest: "+mErr2.Error())
	}

	// Step 2: fetch chunk URLs.
	indices := make([]int, mf.ChunkCount)
	for i := 0; i < mf.ChunkCount; i++ {
		indices[i] = i
	}
	chunkURLs, cErr := deps.Storage.DownloadURLs(ctx, backupID, resolvedVersionID, indices)
	if cErr != nil {
		return nil, cErr
	}

	// Step 3: download chunks in parallel, verify SHA-256, decrypt.
	plaintextChunks := make([][]byte, mf.ChunkCount)
	if dlErr := getParallelChunks(ctx, httpClient, chunkURLs, mf.Chunks, dek, plaintextChunks, concurrency, deps.Events); dlErr != nil {
		deps.Events.EmitSummary("backup-pull", mf.ChunkCount+1, 0, 0, 1)
		return nil, dlErr
	}

	// Step 4: untar to disk.
	if overwrite {
		_ = os.RemoveAll(to)
	}
	if err := os.MkdirAll(to, 0o755); err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup pull: create target directory: "+err.Error())
	}

	plaintextLen := 0
	for _, c := range plaintextChunks {
		plaintextLen += len(c)
	}
	concat := make([]byte, 0, plaintextLen)
	for _, c := range plaintextChunks {
		concat = append(concat, c...)
	}

	if err := untarTo(bytes.NewReader(concat), to); err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup pull: untar: "+err.Error())
	}

	deps.Events.EmitSummary("backup-pull", mf.ChunkCount+1, mf.ChunkCount+1, 0, 0)

	return &PullResult{
		BackupID:  backupID,
		VersionID: resolvedVersionID,
		WrittenTo: to,
	}, nil
}

// toManifestVersions adapts storage.VersionInfo (from substrate) to the
// manifest.Version shape SelectLatest expects. They have identical fields
// today; the bridge keeps the manifest package free of storage's wire types.
func toManifestVersions(in []storage.VersionInfo) []manifest.Version {
	out := make([]manifest.Version, len(in))
	for i, v := range in {
		out[i] = manifest.Version{
			VersionID:      v.VersionID,
			CreatedAt:      v.CreatedAt,
			Size:           v.Size,
			ManifestSHA256: v.ManifestSHA256,
		}
	}
	return out
}

// getParallelChunks downloads each chunk URL, verifies SHA-256 against
// the manifest entry, decrypts via DEK + chunkIndex AAD, and writes the
// plaintext into out[i]. Bounded by concurrency. Any chunk failure (HTTP,
// SHA-256 mismatch, AEAD failure) cancels remaining work and returns.
func getParallelChunks(ctx context.Context, hc *http.Client, urls []storage.PresignedURL, chunks []manifest.ChunkMeta, dek []byte, out [][]byte, concurrency int, em *events.Emitter) *envelope.Error {
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > len(chunks) {
		concurrency = len(chunks)
	}

	type job struct {
		index uint32
		url   string
		meta  manifest.ChunkMeta
	}
	jobs := make([]job, len(chunks))
	for i, c := range chunks {
		u := storage.FindChunkURL(urls, c.Index)
		if u == nil {
			return envelope.NewError(envelope.ErrBackendIncompatible,
				fmt.Sprintf("backup pull: no presigned URL for chunk index %d", c.Index)).
				WithRemediation("Update the engine; this typically means a substrate response shape changed.")
		}
		jobs[i] = job{index: c.Index, url: u.PresignedURL, meta: c}
	}

	work := make(chan job)
	errCh := make(chan *envelope.Error, len(chunks))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range work {
				em.EmitItem(fmt.Sprintf("chunk-%d", j.index), "hosted-backup", "downloading", "", "", "")
				blob, gerr := getOnce(ctx, hc, j.url)
				if gerr != nil {
					em.EmitItem(fmt.Sprintf("chunk-%d", j.index), "hosted-backup", "failed", gerr.Error(), "", "")
					errCh <- envelope.NewError(envelope.ErrBackendUnreachable,
						fmt.Sprintf("backup pull: download chunk %d: %s", j.index, gerr.Error()))
					cancel()
					return
				}
				sum := sha256.Sum256(blob)
				if hex.EncodeToString(sum[:]) != strings.ToLower(j.meta.SHA256) {
					em.EmitItem(fmt.Sprintf("chunk-%d", j.index), "hosted-backup", "failed", "sha256 mismatch", "", "")
					errCh <- envelope.NewError(envelope.ErrInternalError,
						fmt.Sprintf("backup pull: chunk %d SHA-256 mismatch — refusing to decrypt", j.index)).
						WithRemediation("Re-run; if it persists, the chunk may be corrupted in storage.")
					cancel()
					return
				}
				em.EmitItem(fmt.Sprintf("chunk-%d", j.index), "hosted-backup", "verified", "", "", "")
				plain, derr := crypto.DecryptChunk(blob, j.index, dek)
				if derr != nil {
					em.EmitItem(fmt.Sprintf("chunk-%d", j.index), "hosted-backup", "failed", "decrypt failed", "", "")
					errCh <- envelope.NewError(envelope.ErrInternalError,
						fmt.Sprintf("backup pull: decrypt chunk %d: %s", j.index, derr.Error()))
					cancel()
					return
				}
				out[j.index] = plain
				em.EmitItem(fmt.Sprintf("chunk-%d", j.index), "hosted-backup", "decrypted", "", "", "")
			}
		}()
	}

	go func() {
		defer close(work)
		for _, j := range jobs {
			select {
			case <-ctx.Done():
				return
			case work <- j:
			}
		}
	}()

	wg.Wait()
	close(errCh)

	for e := range errCh {
		if e != nil {
			return e
		}
	}
	return nil
}

// getOnce performs one GET against a presigned URL and returns the body.
func getOnce(ctx context.Context, hc *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, errors.New("presigned GET returned HTTP " + httpStatus(resp.StatusCode))
	}
	return io.ReadAll(resp.Body)
}

func httpStatus(code int) string {
	return fmt.Sprintf("%d", code)
}

// untarTo expands a tar stream into target. Existing files are
// overwritten; missing parent directories are created with mode 0o755.
func untarTo(r io.Reader, target string) error {
	tr := tar.NewReader(r)

	// Collect header order so directories are created before files (the
	// tar writer in upload may have walked in any order).
	type entry struct {
		hdr  *tar.Header
		body []byte
	}
	var entries []entry
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		var body []byte
		if hdr.Typeflag == tar.TypeReg {
			b, rerr := io.ReadAll(tr)
			if rerr != nil {
				return rerr
			}
			body = b
		}
		entries = append(entries, entry{hdr: hdr, body: body})
	}
	// Sort: directories first, then files; within each group preserve
	// path order for determinism.
	sort.SliceStable(entries, func(i, j int) bool {
		di := entries[i].hdr.Typeflag == tar.TypeDir
		dj := entries[j].hdr.Typeflag == tar.TypeDir
		if di != dj {
			return di
		}
		return entries[i].hdr.Name < entries[j].hdr.Name
	})

	for _, e := range entries {
		full := filepath.Join(target, filepath.FromSlash(e.hdr.Name))
		switch e.hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(full, mode(e.hdr.Mode, 0o755)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(full, e.body, mode(e.hdr.Mode, 0o644)); err != nil {
				return err
			}
		default:
			// Unsupported typeflag (symlink, device, etc.) — skip in v1.
		}
	}
	return nil
}

func mode(headerMode int64, fallback os.FileMode) os.FileMode {
	if headerMode == 0 {
		return fallback
	}
	return os.FileMode(headerMode) & os.ModePerm
}

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
