// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package upload orchestrates the chunked, encrypted upload of a profile
// to Endstate Hosted Backup. Inputs: a plaintext profile path on disk and
// the unwrapped DEK from the session. Outputs: a fresh versionId and a
// fully populated manifest stored on substrate.
//
// Pipeline (contract §3, §7, §8):
//
//   profile → tar → 4 MiB chunks → AES-256-GCM (chunkIndex AAD)
//        ↓
//   manifest{versionId, chunks[], wrappedDEK, kdf} → AES-256-GCM (0xFFFFFFFF AAD)
//        ↓
//   storage.CreateVersion → presigned PUT URLs (manifest at chunkIndex=-1)
//        ↓
//   PUT each chunk + manifest in parallel, retry once on 5xx
//
// The package never sees plaintext outside this process: chunks are
// encrypted client-side before they hit any presigned URL. The DEK is
// loaded from the session and zeroed on the way out.
package upload

import (
	"archive/tar"
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	mrand "math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/backup"
	"github.com/Artexis10/endstate/go-engine/internal/backup/auth"
	"github.com/Artexis10/endstate/go-engine/internal/backup/crypto"
	"github.com/Artexis10/endstate/go-engine/internal/backup/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/backup/storage"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
)

// PushResult is returned to the command handler on a successful push.
type PushResult struct {
	BackupID  string
	VersionID string
}

// Dependencies are the moving pieces a push operation needs. Construct
// from a `*backup.Stack` in the command handler; the test suite injects a
// stack pointing at httptest servers.
type Dependencies struct {
	Storage     *storage.Client
	Session     *auth.SessionStore
	Events      *events.Emitter
	HTTPClient  *http.Client // for presigned PUT to R2; nil → http.DefaultClient
	Concurrency int          // bounded parallelism for chunk PUTs; <1 → backup.Concurrency()
	UploadRetry int          // retries on 5xx per chunk; <0 → 1
	Now         func() time.Time
}

// PushVersion executes the upload pipeline. Inputs:
//   - profilePath: a file or directory on disk to back up
//   - backupID: existing backup id; if empty, the engine looks up the user's
//     first backup and creates a new one named `name` if none exists
//   - name: human-readable label used iff a fresh backup is created
//
// Returns the new versionId on success. Streaming progress is emitted on
// deps.Events when --events jsonl is active.
func PushVersion(ctx context.Context, deps Dependencies, backupID, profilePath, name string) (*PushResult, *envelope.Error) {
	if strings.TrimSpace(profilePath) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError, "upload: profile path is empty")
	}

	dek, err := deps.Session.LoadDEK()
	if err != nil {
		return nil, envelope.NewError(envelope.ErrAuthRequired,
			"backup push: no DEK in keychain — sign in first").
			WithRemediation("Run `endstate backup login` (or `endstate backup signup`) to populate the session.")
	}
	defer wipe(dek)

	resolvedBackupID, envErr := resolveBackupID(ctx, deps, backupID, name)
	if envErr != nil {
		return nil, envErr
	}

	tarBytes, terr := tarProfile(profilePath)
	if terr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup push: tar profile: "+terr.Error()).
			WithRemediation("Verify --profile points at a readable file or directory.")
	}

	chunks := chunkBytes(tarBytes, crypto.ChunkPlainSize)
	chunkCount := len(chunks)

	deps.Events.EmitPhase("backup-push")

	encrypted := make([][]byte, chunkCount)
	chunkMeta := make([]storage.ChunkMetaWire, chunkCount)
	manifestChunks := make([]manifest.ChunkMeta, chunkCount)

	for i, plain := range chunks {
		blob, eerr := crypto.EncryptChunk(plain, uint32(i), dek)
		if eerr != nil {
			return nil, envelope.NewError(envelope.ErrInternalError, fmt.Sprintf("backup push: encrypt chunk %d: %s", i, eerr.Error()))
		}
		sum := sha256.Sum256(blob)
		hexSum := hex.EncodeToString(sum[:])
		encrypted[i] = blob
		chunkMeta[i] = storage.ChunkMetaWire{Index: uint32(i), EncryptedSize: int64(len(blob)), SHA256: hexSum}
		manifestChunks[i] = manifest.ChunkMeta{Index: uint32(i), EncryptedSize: int64(len(blob)), SHA256: hexSum}
	}

	now := deps.now().UTC().Format(time.RFC3339Nano)
	versionID := newUUID()

	// The manifest's `wrappedDEK` field is the masterKey-wrapped DEK
	// substrate stored at signup (contract §3). We cache it in the
	// keychain at login/signup/recover-finalize so push can read it
	// without re-deriving the masterKey on every call.
	wrappedDEKB64, werr := deps.Session.LoadWrappedDEK()
	if werr != nil {
		return nil, envelope.NewError(envelope.ErrAuthRequired,
			"backup push: no wrappedDEK in keychain — sign in first").
			WithRemediation("Run `endstate backup login` (or `endstate backup signup`) to populate the session.")
	}

	mf := &manifest.Manifest{
		EnvelopeVersion: crypto.EnvelopeVersion,
		VersionID:       versionID,
		CreatedAt:       now,
		OriginalSize:    int64(len(tarBytes)),
		ChunkSize:       crypto.ChunkPlainSize,
		ChunkCount:      chunkCount,
		Chunks:          manifestChunks,
		KDF:             crypto.DefaultKDFParams(),
		WrappedDEK:      wrappedDEKB64,
	}
	mfJSON, mfErr := manifest.Marshal(mf)
	if mfErr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup push: marshal manifest: "+mfErr.Error())
	}

	encManifest, emErr := crypto.EncryptManifest(mfJSON, dek)
	if emErr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup push: encrypt manifest: "+emErr.Error())
	}

	resp, cvErr := deps.Storage.CreateVersion(ctx, resolvedBackupID, encManifest, chunkMeta)
	if cvErr != nil {
		return nil, cvErr
	}

	manifestURL := storage.FindManifestURL(resp.UploadURLs)
	if manifestURL == nil {
		return nil, envelope.NewError(envelope.ErrBackendIncompatible,
			fmt.Sprintf("backup push: substrate response missing manifest URL (chunkIndex == %d)", storage.ManifestChunkIndex)).
			WithRemediation("Update the engine; this typically means a substrate response shape changed.")
	}

	httpClient := deps.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	concurrency := deps.Concurrency
	if concurrency <= 0 {
		concurrency = backup.Concurrency()
	}
	retries := deps.UploadRetry
	if retries <= 0 {
		retries = 1
	}

	work := make([]uploadItem, 0, chunkCount+1)
	work = append(work, uploadItem{index: storage.ManifestChunkIndex, url: manifestURL.PresignedURL, data: encManifest})
	for i, blob := range encrypted {
		u := storage.FindChunkURL(resp.UploadURLs, uint32(i))
		if u == nil {
			return nil, envelope.NewError(envelope.ErrBackendIncompatible,
				fmt.Sprintf("backup push: no presigned URL for chunk index %d", i)).
				WithRemediation("Update the engine; this typically means a substrate response shape changed.")
		}
		work = append(work, uploadItem{index: i, url: u.PresignedURL, data: blob})
	}

	successCount, failedCount, perr := putParallel(ctx, httpClient, work, concurrency, retries, deps.Events)
	if perr != nil {
		deps.Events.EmitSummary("backup-push", chunkCount+1, successCount, 0, failedCount)
		return nil, envelope.NewError(envelope.ErrBackendUnreachable,
			"backup push: chunk upload failed: "+perr.Error()).
			WithRemediation("Re-run `endstate backup push`; a fresh versionId will be minted. The half-uploaded version is garbage-collected by substrate.")
	}

	deps.Events.EmitSummary("backup-push", chunkCount+1, successCount, 0, 0)

	return &PushResult{BackupID: resolvedBackupID, VersionID: resp.VersionID}, nil
}

// resolveBackupID picks a backup id to write a version against. If the
// caller specified one, it is used verbatim. Otherwise the user's
// existing backups are listed: if there's at least one, the first is
// used; otherwise a new backup is created with `name` (default: "default").
func resolveBackupID(ctx context.Context, deps Dependencies, backupID, name string) (string, *envelope.Error) {
	if strings.TrimSpace(backupID) != "" {
		return backupID, nil
	}
	backups, err := deps.Storage.ListBackups(ctx)
	if err != nil {
		return "", err
	}
	if len(backups) > 0 {
		return backups[0].ID, nil
	}
	createName := strings.TrimSpace(name)
	if createName == "" {
		createName = "default"
	}
	id, cerr := deps.Storage.CreateBackup(ctx, createName)
	if cerr != nil {
		return "", cerr
	}
	return id, nil
}

// tarProfile returns the tar archive of the profile's contents. If
// profilePath is a regular file, the archive contains exactly that file
// at its base name. If profilePath is a directory, the archive walks the
// tree and stores entries relative to profilePath. Format: uncompressed
// POSIX tar via stdlib archive/tar. Modification times are zeroed so
// repeated push of an unchanged profile produces byte-identical bytes.
func tarProfile(profilePath string) ([]byte, error) {
	info, err := os.Stat(profilePath)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	if !info.IsDir() {
		if err := writeTarFile(tw, profilePath, filepath.Base(profilePath), info); err != nil {
			return nil, err
		}
	} else {
		walkErr := filepath.Walk(profilePath, func(p string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, rerr := filepath.Rel(profilePath, p)
			if rerr != nil {
				return rerr
			}
			rel = filepath.ToSlash(rel)
			if rel == "." {
				return nil
			}
			if fi.IsDir() {
				return writeTarDir(tw, rel, fi)
			}
			return writeTarFile(tw, p, rel, fi)
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeTarFile(tw *tar.Writer, fsPath, archiveName string, info os.FileInfo) error {
	f, err := os.Open(fsPath)
	if err != nil {
		return err
	}
	defer f.Close()
	hdr := &tar.Header{
		Name:     archiveName,
		Mode:     int64(info.Mode().Perm()),
		Size:     info.Size(),
		Typeflag: tar.TypeReg,
		Format:   tar.FormatPAX,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := io.Copy(tw, f); err != nil {
		return err
	}
	return nil
}

func writeTarDir(tw *tar.Writer, archiveName string, info os.FileInfo) error {
	hdr := &tar.Header{
		Name:     archiveName + "/",
		Mode:     int64(info.Mode().Perm()),
		Typeflag: tar.TypeDir,
		Format:   tar.FormatPAX,
	}
	return tw.WriteHeader(hdr)
}

// chunkBytes splits b into successive blocks of size n. The last block
// may be shorter. Empty input yields no chunks (caller must handle the
// zero-chunk case).
func chunkBytes(b []byte, n int) [][]byte {
	if n <= 0 {
		return [][]byte{b}
	}
	if len(b) == 0 {
		return [][]byte{{}}
	}
	out := make([][]byte, 0, (len(b)+n-1)/n)
	for i := 0; i < len(b); i += n {
		end := i + n
		if end > len(b) {
			end = len(b)
		}
		cp := make([]byte, end-i)
		copy(cp, b[i:end])
		out = append(out, cp)
	}
	return out
}

type uploadItem struct {
	index int
	url   string
	data  []byte
}

// putParallel uploads each item to its presigned URL with bounded
// concurrency and limited 5xx retries. Returns (successCount,
// failedCount, error). On any item failing past its retry budget, the
// returned error is non-nil and ctx propagation cancels remaining work.
func putParallel(ctx context.Context, hc *http.Client, items []uploadItem, concurrency, retries int, em *events.Emitter) (int, int, error) {
	if concurrency < 1 {
		concurrency = 1
	}
	if concurrency > len(items) {
		concurrency = len(items)
	}

	work := make(chan uploadItem)
	errCh := make(chan error, len(items))
	var success, failed int
	var counterMu sync.Mutex

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for it := range work {
				if err := putWithRetry(ctx, hc, it, retries, em); err != nil {
					counterMu.Lock()
					failed++
					counterMu.Unlock()
					errCh <- err
					cancel()
					return
				}
				counterMu.Lock()
				success++
				counterMu.Unlock()
			}
		}()
	}

	go func() {
		defer close(work)
		for _, it := range items {
			select {
			case <-ctx.Done():
				return
			case work <- it:
			}
		}
	}()

	wg.Wait()
	close(errCh)

	var firstErr error
	for e := range errCh {
		if firstErr == nil {
			firstErr = e
		}
	}
	return success, failed, firstErr
}

// putWithRetry PUTs one upload item, retrying once on 5xx. Emits item
// events `uploading` → `uploaded` (or `failed`) for the GUI progress UI.
func putWithRetry(ctx context.Context, hc *http.Client, it uploadItem, retries int, em *events.Emitter) error {
	em.EmitItem(itemID(it.index), "hosted-backup", "uploading", "", "", "")
	attempt := 0
	for {
		err := putOnce(ctx, hc, it)
		if err == nil {
			em.EmitItem(itemID(it.index), "hosted-backup", "uploaded", "", "", "")
			return nil
		}
		if !isRetryable(err) || attempt >= retries {
			em.EmitItem(itemID(it.index), "hosted-backup", "failed", err.Error(), "", "")
			return err
		}
		attempt++
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(jitter(time.Duration(attempt)*250*time.Millisecond, 0.25)):
		}
	}
}

func putOnce(ctx context.Context, hc *http.Client, it uploadItem) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, it.url, bytes.NewReader(it.data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(it.data))
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode/100 == 2 {
		return nil
	}
	return &putError{status: resp.StatusCode}
}

type putError struct{ status int }

func (e *putError) Error() string { return fmt.Sprintf("upload: presigned PUT returned HTTP %d", e.status) }

func isRetryable(err error) bool {
	var pe *putError
	if errors.As(err, &pe) {
		return pe.status >= 500 && pe.status < 600
	}
	return false
}

func itemID(idx int) string {
	if idx == storage.ManifestChunkIndex {
		return "manifest"
	}
	return fmt.Sprintf("chunk-%d", idx)
}

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func (d Dependencies) now() time.Time {
	if d.Now == nil {
		return time.Now()
	}
	return d.Now()
}

func jitter(d time.Duration, frac float64) time.Duration {
	if frac <= 0 {
		return d
	}
	delta := float64(d) * frac
	r := mrand.Float64()*2 - 1
	return d + time.Duration(r*delta)
}

func newUUID() string {
	var b [16]byte
	if _, err := io.ReadFull(cryptorand.Reader, b[:]); err != nil {
		return fmt.Sprintf("v-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
