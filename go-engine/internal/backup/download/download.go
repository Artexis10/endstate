// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package download orchestrates the chunked, encrypted download of a
// backup version from Endstate Hosted Backup.
//
// Pipeline (contract §3, §7, §8):
//
//	storage.DownloadURLs([-1])                  → manifest URL
//	     ↓
//	GET manifest URL → SHA-256 verify (vs API manifestSha256) → AES-256-GCM open (0xFFFFFFFF AAD) → manifest JSON
//	     ↓
//	storage.DownloadURLs([0..N-1])              → chunk URLs
//	     ↓
//	GET each chunk → SHA-256 verify (vs manifest) → AES-256-GCM open (chunkIndex AAD)
//	     ↓
//	concatenate plaintext → untar to disk at --to
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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"

	"github.com/Artexis10/endstate/go-engine/internal/backup"
	"github.com/Artexis10/endstate/go-engine/internal/backup/auth"
	"github.com/Artexis10/endstate/go-engine/internal/backup/crypto"
	"github.com/Artexis10/endstate/go-engine/internal/backup/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/backup/storage"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
)

const (
	maxManifestEncryptedBytes int64 = 16 << 20
	maxManifestChunks               = 1024
)

// PullResult is returned to the command handler on a successful pull.
type PullResult struct {
	BackupID  string
	VersionID string
	WrittenTo string
}

// Dependencies are the moving pieces a pull operation needs.
type Dependencies struct {
	Storage         *storage.Client
	Session         *auth.SessionStore
	Events          *events.Emitter
	HTTPClient      *http.Client  // for presigned GET from R2; nil → http.DefaultClient
	Concurrency     int           // bounded parallelism for chunk GETs
	TransferTimeout time.Duration // per-presigned-GET deadline; <=0 → env/default
}

// PullVersion executes the download pipeline.
func PullVersion(ctx context.Context, deps Dependencies, backupID, versionID, to string, overwrite bool) (*PullResult, *envelope.Error) {
	if strings.TrimSpace(backupID) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError, "download: backupID is empty")
	}
	if strings.TrimSpace(to) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError, "download: target path is empty")
	}
	canonicalTarget, err := canonicalPublicationPath(to)
	if err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup pull: canonicalize target path: "+err.Error())
	}
	to = canonicalTarget
	// Repair an interrupted local publication before anything that can fail for
	// unrelated reasons (authentication, network, or backend availability).
	// A crash must not leave the prior target hidden until Cloud is reachable.
	if err := recoverPublicationBeforePull(to); err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup pull: recover interrupted publication: "+err.Error())
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

	// The version listing is fetched unconditionally — not only when
	// resolving "latest" — because it is the sole source of the
	// `manifestSha256` the manifest blob is verified against below
	// (contract §7). Read-only, so a newer backend minor only warns.
	versions, vErr := deps.Storage.ListVersions(ctx, backupID)
	if vErr != nil {
		return nil, vErr
	}

	resolvedVersionID := strings.TrimSpace(versionID)
	if resolvedVersionID == "" {
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
	expectedManifestSHA := manifestSHAFor(versions, resolvedVersionID)

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

	encManifest, gerr := getObject(ctx, httpClient, manifestURL.PresignedURL, maxManifestEncryptedBytes, 0, deps.TransferTimeout)
	if gerr != nil {
		return nil, downloadTransportEnvelope("backup pull: download manifest", gerr)
	}

	// Integrity gate BEFORE decrypt, mirroring the per-chunk check in
	// getParallelChunks: a manifest whose bytes do not match the hash the
	// API advertised is refused outright and nothing is written to disk.
	// Without this the manifest's only protection is the AEAD tag.
	if ivErr := verifyManifestSHA256("backup pull", encManifest, expectedManifestSHA); ivErr != nil {
		return nil, ivErr
	}

	mfJSON, dmErr := crypto.DecryptManifest(encManifest, dek)
	if dmErr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"backup pull: decrypt manifest: "+dmErr.Error()).
			WithRemediation("Run `endstate backup login` again to refresh the cached DEK; if this persists, restore a known-good older generation explicitly with `endstate backup pull --version-id <older> --to <path>`.")
	}
	mf, mErr2 := manifest.Unmarshal(mfJSON)
	if mErr2 != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup pull: parse manifest: "+mErr2.Error()).
			WithRemediation("The selected generation is not safe to restore. Restore a known-good older generation explicitly with `endstate backup pull --version-id <older> --to <path>`.")
	}
	if structureErr := validateManifest(mf); structureErr != nil {
		return nil, structureErr
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
	if dlErr := getParallelChunks(ctx, httpClient, chunkURLs, mf.Chunks, dek, plaintextChunks, concurrency, deps.TransferTimeout, deps.Events); dlErr != nil {
		deps.Events.EmitSummary("backup-pull", mf.ChunkCount+1, 0, 0, 1)
		return nil, dlErr
	}

	// Step 4: untar into a sibling staging directory, then publish it in one
	// rename sequence. The selected generation has been fully downloaded and
	// verified before the existing destination is touched.
	plaintextLen := 0
	for _, c := range plaintextChunks {
		plaintextLen += len(c)
	}
	concat := make([]byte, 0, plaintextLen)
	for _, c := range plaintextChunks {
		concat = append(concat, c...)
	}

	parent := filepath.Dir(to)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup pull: create destination parent: "+err.Error())
	}
	stage, err := os.MkdirTemp(parent, "."+filepath.Base(to)+".endstate-stage-")
	if err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup pull: create staging directory: "+err.Error())
	}
	defer os.RemoveAll(stage)
	if err := untarTo(bytes.NewReader(concat), stage); err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup pull: untar: "+err.Error())
	}
	if err := publishStage(stage, to, overwrite); err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup pull: publish restored tree: "+err.Error())
	}

	deps.Events.EmitSummary("backup-pull", mf.ChunkCount+1, mf.ChunkCount+1, 0, 0)

	return &PullResult{
		BackupID:  backupID,
		VersionID: resolvedVersionID,
		WrittenTo: to,
	}, nil
}

// LatestManifest fetches and decrypts the manifest of the newest version of a
// backup, for read-only inspection. It downloads only the manifest blob (not the
// chunks) and emits no events. Used by `backup push --if-changed` to read the
// latest version's ContentSHA256 for content-hash dedup. Returns (nil, nil) when
// the backup has no versions yet.
func LatestManifest(ctx context.Context, deps Dependencies, backupID string) (*manifest.Manifest, *envelope.Error) {
	dek, lerr := deps.Session.LoadDEK()
	if lerr != nil {
		return nil, envelope.NewError(envelope.ErrAuthRequired,
			"backup: no DEK in keychain — sign in first").
			WithRemediation("Run `endstate backup login` to populate the session.")
	}
	defer wipe(dek)

	versions, vErr := deps.Storage.ListVersions(ctx, backupID)
	if vErr != nil {
		return nil, vErr
	}
	if len(versions) == 0 {
		return nil, nil
	}
	latest, sErr := manifest.SelectLatest(toManifestVersions(versions))
	if sErr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup: select latest version: "+sErr.Error())
	}

	httpClient := deps.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	urls, uErr := deps.Storage.DownloadURLs(ctx, backupID, latest.VersionID, []int{storage.ManifestChunkIndex})
	if uErr != nil {
		return nil, uErr
	}
	murl := storage.FindManifestURL(urls)
	if murl == nil {
		return nil, envelope.NewError(envelope.ErrBackendIncompatible,
			"backup: substrate did not return a manifest URL").
			WithRemediation("Update the engine; this typically means a substrate response shape changed.")
	}
	encManifest, gerr := getObject(ctx, httpClient, murl.PresignedURL, maxManifestEncryptedBytes, 0, deps.TransferTimeout)
	if gerr != nil {
		return nil, downloadTransportEnvelope("backup: download manifest", gerr)
	}
	if ivErr := verifyManifestSHA256("backup", encManifest, latest.ManifestSHA256); ivErr != nil {
		return nil, ivErr
	}
	mfJSON, dmErr := crypto.DecryptManifest(encManifest, dek)
	if dmErr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup: decrypt manifest: "+dmErr.Error()).
			WithRemediation("The selected generation is not safe to use. Select a known-good older version explicitly.")
	}
	mf, pErr := manifest.Unmarshal(mfJSON)
	if pErr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup: parse manifest: "+pErr.Error()).
			WithRemediation("The selected generation is not safe to use. Select a known-good older version explicitly.")
	}
	if structureErr := validateManifest(mf); structureErr != nil {
		return nil, structureErr
	}
	return mf, nil
}

// manifestSHAFor returns the API-advertised manifest SHA-256 for versionID,
// or "" when the listing does not carry one (older backend, soft-deleted
// version, or a version absent from the page). An empty value disables
// verification rather than failing the pull — the check hardens the
// transport when the backend supplies the hash and must not break restore
// against backends that do not.
func manifestSHAFor(versions []storage.VersionInfo, versionID string) string {
	for _, v := range versions {
		if v.VersionID == versionID {
			return v.ManifestSHA256
		}
	}
	return ""
}

// verifyManifestSHA256 compares the encrypted manifest blob against the
// `manifestSha256` value the API returns for the version (contract §7),
// BEFORE any decryption is attempted. This mirrors the per-chunk integrity
// gate in getParallelChunks: on mismatch the engine refuses to decrypt and
// writes nothing to disk.
//
// An empty expectation is a no-op (see manifestSHAFor).
func verifyManifestSHA256(prefix string, blob []byte, expected string) *envelope.Error {
	want := strings.ToLower(strings.TrimSpace(expected))
	if want == "" {
		return nil
	}
	sum := sha256.Sum256(blob)
	got := hex.EncodeToString(sum[:])
	if got == want {
		return nil
	}
	return envelope.NewError(envelope.ErrInternalError,
		prefix+": manifest SHA-256 mismatch — refusing to decrypt").
		WithDetail(map[string]string{"expected": want, "actual": got}).
		WithRemediation("Re-run; if it persists, the manifest blob is corrupt in storage or disagrees with the version metadata. Restore an earlier version with `endstate backup pull --version-id <id>`.")
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
func getParallelChunks(ctx context.Context, hc *http.Client, urls []storage.PresignedURL, chunks []manifest.ChunkMeta, dek []byte, out [][]byte, concurrency int, transferTimeout time.Duration, em *events.Emitter) *envelope.Error {
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

	totalChunks := len(chunks)
	emitChunk := func(idx uint32, status, message string, encSize int) {
		em.EmitBackupChunk(events.BackupChunkProgress{
			ChunkIndex:    int(idx),
			TotalChunks:   totalChunks,
			EncryptedSize: encSize,
			Status:        status,
			Message:       message,
		})
	}

	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range work {
				encSize := int(j.meta.EncryptedSize)
				em.EmitItem(fmt.Sprintf("chunk-%d", j.index), "hosted-backup", "downloading", "", "", "")
				emitChunk(j.index, "downloading", "", encSize)
				blob, gerr := getObject(ctx, hc, j.url, j.meta.EncryptedSize, j.meta.EncryptedSize, transferTimeout)
				if gerr != nil {
					em.EmitItem(fmt.Sprintf("chunk-%d", j.index), "hosted-backup", "failed", gerr.Error(), "", "")
					emitChunk(j.index, "failed", gerr.Error(), encSize)
					errCh <- downloadTransportEnvelope(fmt.Sprintf("backup pull: download chunk %d", j.index), gerr)
					cancel()
					return
				}
				sum := sha256.Sum256(blob)
				if hex.EncodeToString(sum[:]) != strings.ToLower(j.meta.SHA256) {
					em.EmitItem(fmt.Sprintf("chunk-%d", j.index), "hosted-backup", "failed", "sha256 mismatch", "", "")
					emitChunk(j.index, "failed", "sha256 mismatch", encSize)
					errCh <- envelope.NewError(envelope.ErrInternalError,
						fmt.Sprintf("backup pull: chunk %d SHA-256 mismatch — refusing to decrypt", j.index)).
						WithRemediation("Re-run; if it persists, restore a known-good older generation explicitly with `endstate backup pull --version-id <older> --to <path>`.")
					cancel()
					return
				}
				em.EmitItem(fmt.Sprintf("chunk-%d", j.index), "hosted-backup", "verified", "", "", "")
				emitChunk(j.index, "verified", "", encSize)
				plain, derr := crypto.DecryptChunk(blob, j.index, dek)
				if derr != nil {
					em.EmitItem(fmt.Sprintf("chunk-%d", j.index), "hosted-backup", "failed", "decrypt failed", "", "")
					emitChunk(j.index, "failed", "decrypt failed", encSize)
					errCh <- envelope.NewError(envelope.ErrInternalError,
						fmt.Sprintf("backup pull: decrypt chunk %d: %s", j.index, derr.Error())).
						WithRemediation("The selected generation is not safe to restore. Restore a known-good older generation explicitly with `endstate backup pull --version-id <older> --to <path>`.")
					cancel()
					return
				}
				out[j.index] = plain
				em.EmitItem(fmt.Sprintf("chunk-%d", j.index), "hosted-backup", "decrypted", "", "", "")
				emitChunk(j.index, "decrypted", "", encSize)
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

// getOnce is the manifest-read compatibility wrapper used by focused tests.
func getOnce(ctx context.Context, hc *http.Client, url string) ([]byte, error) {
	return getObject(ctx, hc, url, maxManifestEncryptedBytes, 0, 0)
}

// getObject performs one bounded GET against a presigned URL. expectedSize is
// exact when positive (chunks); a zero value makes limit an upper bound
// (manifest). Both Content-Length and streamed bodies are checked before hash
// verification or decryption.
func getObject(ctx context.Context, hc *http.Client, url string, limit, expectedSize int64, timeout time.Duration) ([]byte, error) {
	requestCtx, cancel := backup.WithTransferTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, backup.RedactTransportError(err)
	}
	resp, err := backup.PresignedClient(hc).Do(req)
	if err != nil {
		return nil, backup.RedactTransportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, errors.New("presigned GET returned HTTP " + httpStatus(resp.StatusCode))
	}
	if resp.ContentLength > limit {
		return nil, fmt.Errorf("presigned GET object exceeds maximum size of %d bytes", limit)
	}
	if expectedSize > 0 && resp.ContentLength >= 0 && resp.ContentLength != expectedSize {
		return nil, fmt.Errorf("presigned GET object size %d does not match expected size %d", resp.ContentLength, expectedSize)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, backup.RedactTransportError(err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("presigned GET object exceeds maximum size of %d bytes", limit)
	}
	if expectedSize > 0 && int64(len(body)) != expectedSize {
		return nil, fmt.Errorf("presigned GET object size %d does not match expected size %d", len(body), expectedSize)
	}
	return body, nil
}

func downloadTransportEnvelope(prefix string, err error) *envelope.Error {
	if backup.IsTransportError(err) || errors.Is(err, context.DeadlineExceeded) {
		return envelope.NewError(envelope.ErrBackendUnreachable, prefix+": "+err.Error())
	}
	return envelope.NewError(envelope.ErrBackendError, prefix+": "+err.Error()).
		WithRemediation("The selected generation could not be read safely. Restore a known-good older generation explicitly with `endstate backup pull --version-id <older> --to <path>`.")
}

func validateManifest(mf *manifest.Manifest) *envelope.Error {
	if mf.ChunkCount < 1 || mf.ChunkCount > maxManifestChunks || len(mf.Chunks) != mf.ChunkCount {
		return invalidManifest("invalid chunk count")
	}
	if mf.OriginalSize < 0 || mf.ChunkSize <= 0 || mf.ChunkSize > crypto.ChunkPlainSize {
		return invalidManifest("invalid plaintext size metadata")
	}
	if mf.OriginalSize > int64(mf.ChunkCount)*mf.ChunkSize {
		return invalidManifest("plaintext size exceeds declared chunks")
	}
	for want, chunk := range mf.Chunks {
		if chunk.Index != uint32(want) {
			return invalidManifest("chunk indices must be unique and contiguous")
		}
		if chunk.EncryptedSize < crypto.NonceSize+crypto.GCMTagSize || chunk.EncryptedSize > crypto.ChunkPlainSize+crypto.NonceSize+crypto.GCMTagSize {
			return invalidManifest("invalid encrypted chunk size")
		}
		if len(chunk.SHA256) != sha256.Size*2 {
			return invalidManifest("invalid chunk SHA-256")
		}
		if _, err := hex.DecodeString(chunk.SHA256); err != nil {
			return invalidManifest("invalid chunk SHA-256")
		}
	}
	return nil
}

func invalidManifest(reason string) *envelope.Error {
	return envelope.NewError(envelope.ErrBackendError, "backup pull: malformed encrypted manifest: "+reason).
		WithRemediation("The selected generation is not safe to restore. Restore a known-good older generation explicitly with `endstate backup pull --version-id <older> --to <path>`.")
}

func recoverPublicationBeforePull(target string) error {
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create destination parent: %w", err)
	}
	lock := flock.New(filepath.Join(parent, "."+filepath.Base(target)+".endstate-pull.lock"))
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("lock target publication: %w", err)
	}
	defer func() { _ = lock.Unlock() }()
	return recoverPublishJournal(target)
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
		if err := validateTarEntry(hdr); err != nil {
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
			return fmt.Errorf("unsupported tar entry type %d", e.hdr.Typeflag)
		}
	}
	return nil
}

func validateTarEntry(hdr *tar.Header) error {
	if hdr.Typeflag != tar.TypeDir && hdr.Typeflag != tar.TypeReg {
		return fmt.Errorf("unsafe tar entry %q: links and special files are not supported", hdr.Name)
	}
	name := filepath.FromSlash(hdr.Name)
	clean := filepath.Clean(name)
	if clean == "." || !filepath.IsLocal(name) || strings.HasPrefix(hdr.Name, "/") || strings.HasPrefix(hdr.Name, "\\") || filepath.IsAbs(name) || filepath.VolumeName(name) != "" || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || hasUnsafeWindowsPathComponent(name) {
		return fmt.Errorf("unsafe tar entry path %q", hdr.Name)
	}
	return nil
}

func hasUnsafeWindowsPathComponent(name string) bool {
	for _, component := range strings.FieldsFunc(filepath.ToSlash(name), func(r rune) bool { return r == '/' || r == '\\' }) {
		if strings.Contains(component, ":") {
			return true
		}
		base := strings.ToUpper(strings.TrimRight(strings.Split(component, ".")[0], " "))
		switch base {
		case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
			return true
		}
	}
	return false
}

// publishStage replaces target only after the stage is complete. If the final
// rename fails after the old tree was moved aside, it restores the old tree.
const (
	journalPrepared        = "prepared"
	journalRollbackReady   = "rollback-ready"
	journalTargetPublished = "target-published"
)

type publishJournal struct {
	Target   string `json:"target"`
	Rollback string `json:"rollback"`
	Phase    string `json:"phase"`
}

var (
	writePublishJournalFn = writePublishJournal
	removeAllFn           = os.RemoveAll
	removeFileFn          = os.Remove
)

func publishStage(stage, target string, overwrite bool) error {
	lock := flock.New(filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".endstate-pull.lock"))
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("lock target publication: %w", err)
	}
	defer func() { _ = lock.Unlock() }()
	if err := recoverPublishJournal(target); err != nil {
		return err
	}
	if _, err := os.Stat(target); err == nil {
		if !overwrite {
			return fmt.Errorf("target path already exists")
		}
		rollbackFile, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".endstate-rollback-")
		if err != nil {
			return fmt.Errorf("create rollback directory: %w", err)
		}
		rollback := rollbackFile.Name()
		if err := rollbackFile.Close(); err != nil {
			return err
		}
		if err := os.Remove(rollback); err != nil {
			return fmt.Errorf("prepare rollback path: %w", err)
		}
		journal := publishJournal{Target: target, Rollback: rollback, Phase: journalPrepared}
		if err := writePublishJournalFn(target, journal); err != nil {
			return fmt.Errorf("record publication intent: %w", err)
		}
		if err := os.Rename(target, rollback); err != nil {
			return fmt.Errorf("preserve existing destination: %w", err)
		}
		journal.Phase = journalRollbackReady
		if err := writePublishJournalFn(target, journal); err != nil {
			if restoreErr := os.Rename(rollback, target); restoreErr != nil {
				return fmt.Errorf("record rollback publication state: %v; restore prior target: %w", err, restoreErr)
			}
			_ = removeFileFn(publishJournalPath(target))
			return fmt.Errorf("record rollback publication state: %w", err)
		}
		if err := os.Rename(stage, target); err != nil {
			if restoreErr := os.Rename(rollback, target); restoreErr != nil {
				return fmt.Errorf("publish stage: %v; rollback existing destination: %w", err, restoreErr)
			}
			_ = os.Remove(publishJournalPath(target))
			return fmt.Errorf("publish stage: %w", err)
		}
		journal.Phase = journalTargetPublished
		_ = writePublishJournalFn(target, journal)
		// Publication already succeeded. A retained rollback directory is
		// untidy but must not turn a successful restore into a reported
		// failure or prompt callers to repeat it against the new target.
		if err := removeAllFn(rollback); err == nil {
			_ = removeFileFn(publishJournalPath(target))
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect target: %w", err)
	}
	return os.Rename(stage, target)
}

func publishJournalPath(target string) string {
	return filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".endstate-pull-journal.json")
}

func writePublishJournal(target string, journal publishJournal) error {
	if !validPublishJournal(target, journal) {
		return errors.New("invalid publication journal paths")
	}
	data, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	path := publishJournalPath(target)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// recoverPublishJournal completes a previously interrupted atomic target
// replacement before any new publication begins. Journal paths are accepted
// only when they are siblings of target, so a corrupted journal cannot move
// arbitrary paths or escape the restore parent.
func recoverPublishJournal(target string) error {
	path := publishJournalPath(target)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read publication journal: %w", err)
	}
	var journal publishJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		return fmt.Errorf("parse publication journal: %w", err)
	}
	if !validPublishJournal(target, journal) {
		return errors.New("publication journal has unsafe paths")
	}
	_, targetErr := os.Stat(target)
	_, rollbackErr := os.Stat(journal.Rollback)
	targetExists, rollbackExists := targetErr == nil, rollbackErr == nil
	if targetErr != nil && !os.IsNotExist(targetErr) {
		return targetErr
	}
	if rollbackErr != nil && !os.IsNotExist(rollbackErr) {
		return rollbackErr
	}
	switch {
	case !targetExists && rollbackExists:
		if err := os.Rename(journal.Rollback, target); err != nil {
			return fmt.Errorf("recover prior target: %w", err)
		}
	case targetExists && rollbackExists:
		if err := os.RemoveAll(journal.Rollback); err != nil {
			return fmt.Errorf("remove completed rollback tree: %w", err)
		}
	case !targetExists && !rollbackExists:
		return errors.New("publication journal cannot recover a missing target")
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func validPublishJournal(target string, journal publishJournal) bool {
	if journal.Rollback == "" || (journal.Phase != journalPrepared && journal.Phase != journalRollbackReady && journal.Phase != journalTargetPublished) {
		return false
	}
	targetBase := filepath.Base(filepath.Clean(target))
	journalTargetBase := filepath.Base(filepath.Clean(journal.Target))
	if targetBase != journalTargetBase {
		return false
	}
	targetParent, err := os.Stat(filepath.Dir(target))
	if err != nil {
		return false
	}
	journalTargetParent, err := os.Stat(filepath.Dir(journal.Target))
	if err != nil || !os.SameFile(targetParent, journalTargetParent) {
		return false
	}
	rollbackParent, err := os.Stat(filepath.Dir(journal.Rollback))
	if err != nil || !os.SameFile(targetParent, rollbackParent) {
		return false
	}
	base := filepath.Base(journal.Rollback)
	return strings.HasPrefix(base, "."+targetBase+".endstate-rollback-") && base == filepath.Clean(base)
}

// canonicalPublicationPath resolves aliases in the existing parent while
// allowing the target itself to be absent. macOS exposes temporary directories
// through both /var and /private/var; treating those spellings as different
// would strand a valid crash-recovery journal.
func canonicalPublicationPath(path string) (string, error) {
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		if os.IsNotExist(err) {
			return abs, nil
		}
		return "", err
	}
	return filepath.Join(parent, filepath.Base(abs)), nil
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
