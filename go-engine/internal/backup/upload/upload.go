// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package upload orchestrates the chunked, encrypted upload of a profile
// to Endstate Hosted Backup. Inputs: a plaintext profile path on disk and
// the unwrapped DEK from the session. Outputs: a fresh versionId and a
// fully populated manifest stored on substrate.
//
// Pipeline (contract §3, §7, §8):
//
//	profile → tar → 4 MiB chunks → AES-256-GCM (chunkIndex AAD)
//	     ↓
//	manifest{versionId, chunks[], wrappedDEK, kdf} → AES-256-GCM (0xFFFFFFFF AAD)
//	     ↓
//	storage.CreateVersion → presigned PUT URLs (manifest at chunkIndex=-1)
//	     ↓
//	PUT each chunk + manifest in parallel, retry once on 5xx
//	     ↓
//	storage.CommitVersion → the generation becomes durable (contract §8)
//
// The commit is the last step, and it is what makes a generation a restore
// target. If any chunk or the manifest fails to upload, no commit is sent
// and the push fails — a partially uploaded generation is never reported
// as protected.
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
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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
	"github.com/Artexis10/endstate/go-engine/internal/backup/download"
	"github.com/Artexis10/endstate/go-engine/internal/backup/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/backup/storage"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
)

// PushResult is returned to the command handler on a successful push.
type PushResult struct {
	BackupID  string
	VersionID string
	// Skipped is true when --if-changed found the content unchanged and no new
	// version was created; VersionID then refers to the existing latest version.
	Skipped bool
}

// Dependencies are the moving pieces a push operation needs. Construct
// from a `*backup.Stack` in the command handler; the test suite injects a
// stack pointing at httptest servers.
type Dependencies struct {
	Storage         *storage.Client
	Session         *auth.SessionStore
	Events          *events.Emitter
	HTTPClient      *http.Client  // for presigned PUT to R2; nil → http.DefaultClient
	Concurrency     int           // bounded parallelism for chunk PUTs; <1 → backup.Concurrency()
	UploadRetry     int           // retries on 5xx per chunk; <0 → 1
	TransferTimeout time.Duration // per-presigned-PUT deadline; <=0 → env/default
	Now             func() time.Time
	// IfChanged enables content-hash dedup: skip the upload (mint no new version)
	// when the candidate's plaintext content matches the latest version's
	// ContentSHA256. Best-effort — a failed/absent peek falls through to upload.
	IfChanged bool
	// Scheduled holds durable replay state for an automatic push. It is nil for
	// interactive pushes, whose prepared bundle only needs to live in memory.
	Scheduled *ScheduledCreate
}

// ScheduledCreate is the queue-owned state required to make an automatic
// create-version mutation restart-safe. Persist must atomically update that
// queue before the first create POST is attempted.
type ScheduledCreate struct {
	StateDir            string
	BackupID            string
	OperationID         string
	CreateSpool         string
	LegacyCreateStarted bool
	Persist             func(ScheduledCreate) error
}

// PreparedCreate is the durable, byte-exact state needed to replay a
// capability-negotiated create-version request after a scheduled restart.
// It deliberately contains ciphertext only; plaintext and DEK never touch the
// spool.
type PreparedCreate struct {
	SchemaVersion     int                     `json:"schemaVersion"`
	BackupID          string                  `json:"backupId"`
	OperationID       string                  `json:"operationId"`
	EncryptedManifest []byte                  `json:"encryptedManifest"`
	ManifestSHA256    string                  `json:"manifestSha256"`
	Chunks            [][]byte                `json:"chunks"`
	ChunkMetadata     []storage.ChunkMetaWire `json:"chunkMetadata"`
}

func writePreparedCreate(path string, prepared PreparedCreate) error {
	if err := validatePreparedCreate(prepared); err != nil {
		return err
	}
	data, err := json.Marshal(prepared)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readPreparedCreate(path string) (PreparedCreate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PreparedCreate{}, err
	}
	var prepared PreparedCreate
	if err := json.Unmarshal(data, &prepared); err != nil {
		return PreparedCreate{}, err
	}
	return prepared, validatePreparedCreate(prepared)
}

func validatePreparedCreate(prepared PreparedCreate) error {
	manifestSum := sha256.Sum256(prepared.EncryptedManifest)
	if prepared.SchemaVersion != 1 || prepared.BackupID == "" || prepared.OperationID == "" || len(prepared.EncryptedManifest) == 0 || !strings.EqualFold(prepared.ManifestSHA256, hex.EncodeToString(manifestSum[:])) || len(prepared.Chunks) != len(prepared.ChunkMetadata) {
		return errors.New("invalid scheduled create spool")
	}
	for i, chunk := range prepared.Chunks {
		meta := prepared.ChunkMetadata[i]
		sum := sha256.Sum256(chunk)
		if meta.Index != uint32(i) || meta.EncryptedSize != int64(len(chunk)) || !strings.EqualFold(meta.SHA256, hex.EncodeToString(sum[:])) {
			return errors.New("scheduled create spool does not match chunk metadata")
		}
	}
	return nil
}

// PushVersion executes the upload pipeline. Inputs:
//   - profilePath: a file or directory on disk to back up
//   - backupID: existing backup id to add a version to; used verbatim when set
//   - name: when backupID is empty, a non-empty name CREATES a new backup with
//     that label (one backup per profile). Empty backupID + empty name keeps the
//     legacy convenience: append to the first existing backup, else create "default".
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

	scheduled := deps.Scheduled
	if scheduled != nil && scheduled.LegacyCreateStarted {
		return nil, envelope.NewError(envelope.ErrBackupUploadUncertain,
			"legacy-create-ambiguous: scheduled create response was not confirmed; it will not be retried automatically").
			WithRemediation("Cloud may have accepted the version. Check Cloud versions, then run `endstate schedule discard-upload --artifact-sha256 <sha> --confirm` to stop retrying this local queue item; the local capture is retained.")
	}

	resolvedBackupID := backupID
	operationID := ""
	var bundle *encBundle
	if scheduled != nil && scheduled.CreateSpool != "" {
		if !isScheduledSpool(scheduled.StateDir, scheduled.CreateSpool) {
			return nil, envelope.NewError(envelope.ErrInternalError, "scheduled create spool is outside the schedule state directory")
		}
		prepared, readErr := readPreparedCreate(scheduled.CreateSpool)
		if readErr != nil || prepared.BackupID != scheduled.BackupID || prepared.OperationID != scheduled.OperationID {
			return nil, envelope.NewError(envelope.ErrInternalError, "scheduled create spool is corrupt; refusing to regenerate under the same operation ID")
		}
		if !deps.Storage.SupportsVersionCreateOperationReplay(ctx) {
			return nil, envelope.NewError(envelope.ErrBackendIncompatible,
				"scheduled create spool requires version-create-operation-replay-v1; refusing to downgrade a persisted operation ID")
		}
		resolvedBackupID, operationID = prepared.BackupID, prepared.OperationID
		bundle = &encBundle{encrypted: prepared.Chunks, encManifest: prepared.EncryptedManifest, chunkMeta: prepared.ChunkMetadata, chunkCount: len(prepared.Chunks)}
	} else {
		var envErr *envelope.Error
		if scheduled != nil {
			resolvedBackupID, envErr = resolveScheduledBackupID(ctx, deps.Storage, scheduled.BackupID)
		} else {
			resolvedBackupID, envErr = resolveBackupID(ctx, deps.Storage, backupID, name, deviceLabel())
		}
		if envErr != nil {
			return nil, envErr
		}

		tarBytes, terr := tarProfile(profilePath)
		if terr != nil {
			return nil, envelope.NewError(envelope.ErrInternalError, "backup push: tar profile: "+terr.Error()).
				WithRemediation("Verify --profile points at a readable file or directory.")
		}
		contentSum := sha256.Sum256(tarBytes)
		contentSHA := hex.EncodeToString(contentSum[:])
		if deps.IfChanged {
			if latest, _ := download.LatestManifest(ctx, download.Dependencies{Storage: deps.Storage, Session: deps.Session, HTTPClient: deps.HTTPClient}, resolvedBackupID); latest != nil && latest.ContentSHA256 == contentSHA {
				return &PushResult{BackupID: resolvedBackupID, VersionID: latest.VersionID, Skipped: true}, nil
			}
		}
		var bErr *envelope.Error
		bundle, bErr = buildBundle(deps, dek, tarBytes, contentSHA)
		if bErr != nil {
			return nil, bErr
		}
		if deps.Storage.SupportsVersionCreateOperationReplay(ctx) {
			operationID = newUUID()
		}
		if scheduled != nil {
			if operationID != "" {
				spool := filepath.Join(scheduled.StateDir, "schedule", "create-spool", operationID+".json")
				manifestSum := sha256.Sum256(bundle.encManifest)
				prepared := PreparedCreate{SchemaVersion: 1, BackupID: resolvedBackupID, OperationID: operationID, EncryptedManifest: bundle.encManifest, ManifestSHA256: hex.EncodeToString(manifestSum[:]), Chunks: bundle.encrypted, ChunkMetadata: bundle.chunkMeta}
				if err := writePreparedCreate(spool, prepared); err != nil {
					return nil, envelope.NewError(envelope.ErrInternalError, "write scheduled create spool: "+err.Error())
				}
				scheduled.BackupID, scheduled.OperationID, scheduled.CreateSpool = resolvedBackupID, operationID, spool
			} else {
				scheduled.BackupID, scheduled.LegacyCreateStarted = resolvedBackupID, true
			}
			if scheduled.Persist == nil {
				return nil, envelope.NewError(envelope.ErrInternalError, "scheduled create state cannot be persisted")
			}
			if err := scheduled.Persist(*scheduled); err != nil {
				return nil, envelope.NewError(envelope.ErrInternalError, "persist scheduled create state: "+err.Error())
			}
		}
	}

	deps.Events.EmitPhase("backup-push")
	chunkCount := bundle.chunkCount
	encrypted := bundle.encrypted
	encManifest := bundle.encManifest
	chunkMeta := bundle.chunkMeta

	resp, cvErr := deps.Storage.CreateVersionWithReplay(ctx, resolvedBackupID, encManifest, chunkMeta, operationID)
	if cvErr != nil {
		if scheduled != nil && scheduled.LegacyCreateStarted && definiteCreatePreSendFailure(cvErr) {
			scheduled.LegacyCreateStarted = false
			if scheduled.Persist == nil || scheduled.Persist(*scheduled) != nil {
				return nil, envelope.NewError(envelope.ErrInternalError, "persist scheduled legacy create retry state")
			}
		}
		deps.Events.EmitSummary("backup-push", 0, 0, 0, 0)
		return nil, cvErr
	}
	// The replay protocol returns this terminal acknowledgement when a prior
	// create + upload + commit completed but its response was lost. There are
	// deliberately no upload URLs to consume in this state.
	if resp.AlreadyCommitted {
		deps.Events.EmitSummary("backup-push", 0, 0, 0, 0)
		return &PushResult{BackupID: resolvedBackupID, VersionID: resp.VersionID}, nil
	}

	manifestURL := storage.FindManifestURL(resp.UploadURLs)
	if manifestURL == nil {
		deps.Events.EmitSummary("backup-push", 0, 0, 0, 0)
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
	work = append(work, newUploadItem(storage.ManifestChunkIndex, manifestURL.PresignedURL, encManifest, resp.RequiresCommit, deps.TransferTimeout))
	for i, blob := range encrypted {
		u := storage.FindChunkURL(resp.UploadURLs, uint32(i))
		if u == nil {
			deps.Events.EmitSummary("backup-push", 0, 0, 0, 0)
			return nil, envelope.NewError(envelope.ErrBackendIncompatible,
				fmt.Sprintf("backup push: no presigned URL for chunk index %d", i)).
				WithRemediation("Update the engine; this typically means a substrate response shape changed.")
		}
		work = append(work, newUploadItem(i, u.PresignedURL, blob, resp.RequiresCommit, deps.TransferTimeout))
	}

	successCount, failedCount, perr := putParallel(ctx, httpClient, work, concurrency, retries, chunkCount, deps.Events)
	if perr != nil {
		total := chunkCount + 1
		_, skipped, failedCount := uploadSummary(total, successCount, failedCount)
		deps.Events.EmitSummary("backup-push", total, successCount, skipped, failedCount)
		return nil, envelope.NewError(uploadFailureCode(perr),
			"backup push: chunk upload failed: "+perr.Error()).
			WithRemediation(uncommittedRemediation)
	}

	// A pre-2.1 server omits requiresCommit (or sends false), which means
	// creation itself was the durable boundary. Do not probe a legacy server's
	// commit route: it would turn an already-durable generation into an
	// ambiguous extra network failure.
	if !resp.RequiresCommit {
		deps.Events.EmitSummary("backup-push", chunkCount+1, successCount, 0, 0)
		return &PushResult{BackupID: resolvedBackupID, VersionID: resp.VersionID}, nil
	}

	// Commit LAST — after every chunk and the manifest are durably PUT.
	// This is the only point at which the generation becomes a restore
	// target (contract §7, §8). A commit failure means the generation is
	// NOT protected, so the push fails; the uncommitted version is never
	// listed, never counted against quota, and never selected by
	// manifest.SelectLatest.
	//
	// Once the create response required a commit, every commit error —
	// including 404, authorization rejection, timeout, and cancellation —
	// means the generation is not durable. There is no legacy fallback in
	// this state.
	if err := ctx.Err(); err != nil {
		deps.Events.EmitSummary("backup-push", chunkCount+2, successCount, 0, 1)
		return nil, envelope.NewError(envelope.ErrBackendUnreachable,
			"backup push: cancelled before the required commit: "+err.Error()).
			WithRemediation(uncommittedRemediation)
	}
	if _, cErr := deps.Storage.CommitVersion(ctx, resolvedBackupID, resp.VersionID); cErr != nil {
		// The commit is the extra unit of work on this path: every blob
		// succeeded and the commit is the one that failed. Counting it
		// keeps the event contract's `total = success + skipped + failed`
		// guarantee exact (successCount is chunkCount+1 here).
		deps.Events.EmitSummary("backup-push", chunkCount+2, successCount, 0, 1)
		return nil, envelope.NewError(cErr.Code,
			"backup push: upload finished but the version could not be committed, so it is NOT protected: "+cErr.Message).
			WithDetail(map[string]string{"backupId": resolvedBackupID, "versionId": resp.VersionID}).
			WithRemediation(uncommittedRemediation)
	}

	deps.Events.EmitSummary("backup-push", chunkCount+1, successCount, 0, 0)

	return &PushResult{BackupID: resolvedBackupID, VersionID: resp.VersionID}, nil
}

// uploadSummary assigns every upload item to exactly one terminal event
// bucket, even when several workers observe an early cancellation together.
func uploadSummary(total, success, failed int) (int, int, int) {
	if total < 0 {
		total = 0
	}
	if success < 0 {
		success = 0
	}
	if success > total {
		success = total
	}
	if failed < 0 {
		failed = 0
	}
	if failed > total-success {
		failed = total - success
	}
	return success, total - success - failed, failed
}

// uncommittedRemediation describes what actually happens to a generation
// whose upload did not reach a successful commit.
//
// The previous text claimed "the half-uploaded version is garbage-collected
// by substrate", which was false: before the commit endpoint existed,
// CreateVersion made the row durable immediately, so a partial generation
// stayed listed, counted against quota, and could be picked as the restore
// target. This string states the real behaviour on both backend versions.
const uncommittedRemediation = "Re-run `endstate backup push`; a fresh versionId is minted. " +
	"This generation was never committed, so it is not protected and is not a restore target. " +
	"On a schema 2.1 backend an uncommitted version stays invisible to listing, quota, and restore, and the backend reclaims it. " +
	"On an older 2.0 backend the partial version may still be listed — remove it with `endstate backup delete-version --backup-id <id> --version-id <id> --confirm`."

// encBundle is the fully client-side, encrypted result of bundling a profile —
// the encrypted chunks plus the encrypted manifest — i.e. the exact set of
// bytes a push uploads. Both PushVersion and EstimateSize build it through this
// single path so a size estimate can never drift from a real push.
type encBundle struct {
	encrypted   [][]byte
	encManifest []byte
	chunkMeta   []storage.ChunkMetaWire
	chunkCount  int
}

// buildBundle runs the encrypt pipeline (4 MiB chunks → AES-256-GCM → manifest
// → encrypted manifest) over an already-tarred profile. No network I/O. The DEK
// is the caller's; the wrappedDEK is read from the session.
func buildBundle(deps Dependencies, dek, tarBytes []byte, contentSHA string) (*encBundle, *envelope.Error) {
	chunks := chunkBytes(tarBytes, crypto.ChunkPlainSize)
	chunkCount := len(chunks)

	encrypted := make([][]byte, chunkCount)
	chunkMeta := make([]storage.ChunkMetaWire, chunkCount)
	manifestChunks := make([]manifest.ChunkMeta, chunkCount)

	for i, plain := range chunks {
		blob, eerr := crypto.EncryptChunk(plain, uint32(i), dek)
		if eerr != nil {
			return nil, envelope.NewError(envelope.ErrInternalError, fmt.Sprintf("backup: encrypt chunk %d: %s", i, eerr.Error()))
		}
		sum := sha256.Sum256(blob)
		hexSum := hex.EncodeToString(sum[:])
		encrypted[i] = blob
		chunkMeta[i] = storage.ChunkMetaWire{Index: uint32(i), EncryptedSize: int64(len(blob)), SHA256: hexSum}
		manifestChunks[i] = manifest.ChunkMeta{Index: uint32(i), EncryptedSize: int64(len(blob)), SHA256: hexSum}
	}

	// The manifest's `wrappedDEK` is the masterKey-wrapped DEK substrate stored
	// at signup (contract §3), cached in the keychain at login/signup/recover.
	wrappedDEKB64, werr := deps.Session.LoadWrappedDEK()
	if werr != nil {
		return nil, envelope.NewError(envelope.ErrAuthRequired,
			"backup: no wrappedDEK in keychain — sign in first").
			WithRemediation("Run `endstate backup login` (or `endstate backup signup`) to populate the session.")
	}

	mf := &manifest.Manifest{
		EnvelopeVersion: crypto.EnvelopeVersion,
		VersionID:       newUUID(),
		CreatedAt:       deps.now().UTC().Format(time.RFC3339Nano),
		OriginalSize:    int64(len(tarBytes)),
		ChunkSize:       crypto.ChunkPlainSize,
		ChunkCount:      chunkCount,
		Chunks:          manifestChunks,
		KDF:             crypto.DefaultKDFParams(),
		WrappedDEK:      wrappedDEKB64,
		ContentSHA256:   contentSHA,
	}
	mfJSON, mfErr := manifest.Marshal(mf)
	if mfErr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup: marshal manifest: "+mfErr.Error())
	}
	encManifest, emErr := crypto.EncryptManifest(mfJSON, dek)
	if emErr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup: encrypt manifest: "+emErr.Error())
	}

	return &encBundle{encrypted: encrypted, encManifest: encManifest, chunkMeta: chunkMeta, chunkCount: chunkCount}, nil
}

// SizeEstimate is the result of EstimateSize: the byte counts a push of the
// same profile would produce, with no network I/O.
type SizeEstimate struct {
	EstimatedUploadBytes int64
	PlaintextBytes       int64
	ChunkCount           int
}

// EstimateSize computes the exact number of bytes a `backup push` of
// profilePath would upload (encrypted chunks + encrypted manifest) WITHOUT any
// network call. It runs the identical client-side bundling path push uses (via
// buildBundle), so the estimate can't drift from a real push. Requires a
// signed-in session — the DEK is needed to produce true ciphertext sizes.
func EstimateSize(deps Dependencies, profilePath string) (*SizeEstimate, *envelope.Error) {
	if strings.TrimSpace(profilePath) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup estimate: profile path is empty")
	}

	dek, err := deps.Session.LoadDEK()
	if err != nil {
		return nil, envelope.NewError(envelope.ErrAuthRequired,
			"backup estimate: no DEK in keychain — sign in first").
			WithRemediation("Run `endstate backup login` (or `endstate backup signup`) to populate the session.")
	}
	defer wipe(dek)

	tarBytes, terr := tarProfile(profilePath)
	if terr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, "backup estimate: tar profile: "+terr.Error()).
			WithRemediation("Verify --profile points at a readable file or directory.")
	}
	contentSum := sha256.Sum256(tarBytes)
	contentSHA := hex.EncodeToString(contentSum[:])

	bundle, bErr := buildBundle(deps, dek, tarBytes, contentSHA)
	if bErr != nil {
		return nil, bErr
	}

	var total int64
	for _, blob := range bundle.encrypted {
		total += int64(len(blob))
	}
	total += int64(len(bundle.encManifest))

	return &SizeEstimate{
		EstimatedUploadBytes: total,
		PlaintextBytes:       int64(len(tarBytes)),
		ChunkCount:           bundle.chunkCount,
	}, nil
}

// backupResolverStore is the minimal storage surface resolveBackupID needs.
// Satisfied by *storage.Client; lets the resolution logic be unit-tested with
// a fake instead of a live backend.
type backupResolverStore interface {
	ListBackups(ctx context.Context) ([]storage.Backup, *envelope.Error)
	CreateBackup(ctx context.Context, name string) (string, *envelope.Error)
}

// resolveScheduledBackupID never creates a backup row. CreateBackup is a
// separate non-idempotent mutation, so automatic replay is only safe once the
// queue has a resolved existing backup ID.
func resolveScheduledBackupID(ctx context.Context, store backupResolverStore, backupID string) (string, *envelope.Error) {
	if strings.TrimSpace(backupID) != "" {
		return backupID, nil
	}
	backups, err := store.ListBackups(ctx)
	if err != nil {
		return "", err
	}
	if len(backups) != 1 || strings.TrimSpace(backups[0].ID) == "" {
		return "", envelope.NewError(envelope.ErrBackupSetupRequired,
			"scheduled backup requires exactly one existing Cloud backup; configure --backup-id when multiple backups exist").
			WithRemediation("Save a version manually first, or configure schedule enable --backup-id <id>.")
	}
	return backups[0].ID, nil
}

func definiteCreatePreSendFailure(err *envelope.Error) bool {
	switch err.Code {
	case envelope.ErrAuthRequired, envelope.ErrSubscriptionRequired, envelope.ErrRateLimited, envelope.ErrNotFound, envelope.ErrStorageQuotaExceeded:
		return true
	default:
		return false
	}
}

func isScheduledSpool(stateDir, path string) bool {
	if strings.TrimSpace(stateDir) == "" || strings.TrimSpace(path) == "" {
		return false
	}
	root := filepath.Clean(filepath.Join(stateDir, "schedule", "create-spool"))
	rel, err := filepath.Rel(root, filepath.Clean(path))
	return err == nil && rel != "." && rel != "" && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

// resolveBackupID picks a backup id to write a version against:
//   - an explicit backupID is used verbatim;
//   - otherwise a non-empty name creates a NEW backup labeled `name` — so the
//     GUI's per-profile model gets one backup per profile, addressed by id on
//     later pushes. (Previously a named push fell through to "append to the
//     first existing backup", silently ignoring --name once any backup existed.)
//   - a push with neither id nor name keeps the legacy convenience: append to
//     the user's first backup, or create a backup labeled defaultName (the
//     device label — see deviceLabel) if they have none.
func resolveBackupID(ctx context.Context, store backupResolverStore, backupID, name, defaultName string) (string, *envelope.Error) {
	if strings.TrimSpace(backupID) != "" {
		return backupID, nil
	}
	if createName := strings.TrimSpace(name); createName != "" {
		id, cerr := store.CreateBackup(ctx, createName)
		if cerr != nil {
			return "", cerr
		}
		return id, nil
	}
	backups, err := store.ListBackups(ctx)
	if err != nil {
		return "", err
	}
	if len(backups) > 0 {
		return backups[0].ID, nil
	}
	id, cerr := store.CreateBackup(ctx, defaultName)
	if cerr != nil {
		return "", cerr
	}
	return id, nil
}

// deviceLabel returns a human label for this machine's default backup — the
// OS host name (COMPUTERNAME on Windows), trimmed. Falls back to "default"
// when the host name is empty or unavailable. The backup's identity is its
// backend id; this is only the display label, so a missing name is non-fatal
// and must never fail a push.
func deviceLabel() string {
	host, err := os.Hostname()
	return deviceLabelFrom(host, err)
}

// deviceLabelFrom is the pure core of deviceLabel, split out so the
// trim/fallback logic is unit-testable without depending on the host's name.
func deviceLabelFrom(host string, err error) string {
	if err != nil {
		return "default"
	}
	if h := strings.TrimSpace(host); h != "" {
		return h
	}
	return "default"
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
	index           int
	url             string
	data            []byte
	requireCreate   bool
	checksumSHA256  string
	checksumHex     string
	transferTimeout time.Duration
}

func newUploadItem(index int, url string, data []byte, requireCreate bool, transferTimeout time.Duration) uploadItem {
	item := uploadItem{index: index, url: url, data: data, requireCreate: requireCreate, transferTimeout: transferTimeout}
	if requireCreate {
		sum := sha256.Sum256(data)
		item.checksumSHA256 = base64.StdEncoding.EncodeToString(sum[:])
		item.checksumHex = hex.EncodeToString(sum[:])
	}
	return item
}

// putParallel uploads each item to its presigned URL with bounded
// concurrency and limited 5xx retries. Returns (successCount,
// failedCount, error). On any item failing past its retry budget, the
// returned error is non-nil and ctx propagation cancels remaining work.
// totalChunks is the count of data chunks (manifest excluded), forwarded
// to per-chunk progress events for GUI rendering.
func putParallel(ctx context.Context, hc *http.Client, items []uploadItem, concurrency, retries, totalChunks int, em *events.Emitter) (int, int, error) {
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
				if err := putWithRetry(ctx, hc, it, retries, totalChunks, em); err != nil {
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

// putWithRetry PUTs one upload item, retrying once on 5xx. Emits both
// item events (for log continuity) and richer backup-chunk events (for the
// GUI's per-chunk progress dialog, including retry visibility).
//
// totalChunks is the count of data chunks (manifest excluded). The
// manifest item carries chunkIndex == storage.ManifestChunkIndex (-1);
// data chunks carry their 0-based index.
func putWithRetry(ctx context.Context, hc *http.Client, it uploadItem, retries, totalChunks int, em *events.Emitter) error {
	em.EmitItem(itemID(it.index), "hosted-backup", "uploading", "", "", "")
	em.EmitBackupChunk(events.BackupChunkProgress{
		ChunkIndex:    it.index,
		TotalChunks:   totalChunks,
		EncryptedSize: len(it.data),
		Status:        "uploading",
	})
	attempt := 0
	maxAttempts := retries + 1
	for {
		err := putOnce(ctx, hc, it)
		if err == nil {
			em.EmitItem(itemID(it.index), "hosted-backup", "uploaded", "", "", "")
			em.EmitBackupChunk(events.BackupChunkProgress{
				ChunkIndex:    it.index,
				TotalChunks:   totalChunks,
				EncryptedSize: len(it.data),
				Status:        "uploaded",
			})
			return nil
		}
		if !isRetryable(err) || attempt >= retries {
			em.EmitItem(itemID(it.index), "hosted-backup", "failed", err.Error(), "", "")
			em.EmitBackupChunk(events.BackupChunkProgress{
				ChunkIndex:    it.index,
				TotalChunks:   totalChunks,
				EncryptedSize: len(it.data),
				Status:        "failed",
				Message:       err.Error(),
			})
			return err
		}
		attempt++
		// Emit the retry event BEFORE the backoff sleep so the GUI shows
		// "Retrying chunk N of M (attempt X of Y)" while the sleep runs,
		// not after.
		em.EmitBackupChunk(events.BackupChunkProgress{
			ChunkIndex:    it.index,
			TotalChunks:   totalChunks,
			EncryptedSize: len(it.data),
			Status:        "retrying",
			Message:       err.Error(),
			Attempt:       attempt + 1,
			MaxAttempts:   maxAttempts,
		})
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(jitter(time.Duration(attempt)*250*time.Millisecond, 0.25)):
		}
	}
}

func putOnce(ctx context.Context, hc *http.Client, it uploadItem) error {
	requestCtx, cancel := backup.WithTransferTimeout(ctx, it.transferTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPut, it.url, bytes.NewReader(it.data))
	if err != nil {
		return backup.RedactTransportError(err)
	}
	if it.requireCreate {
		req.Header.Set("If-None-Match", "*")
		if it.checksumSHA256 != "" {
			req.Header.Set("x-amz-checksum-sha256", it.checksumSHA256)
			req.Header.Set("x-amz-meta-endstate-sha256", it.checksumHex)
		}
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(it.data))
	resp, err := backup.PresignedClient(hc).Do(req)
	if err != nil {
		return backup.RedactTransportError(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode/100 == 2 {
		return nil
	}
	// A checksum-bound create-only retry that receives 412 has observed the
	// object created by its own earlier ambiguous attempt. Substrate signs and
	// verifies x-amz-checksum-sha256 on these URLs, so this is not a blind
	// precondition-success shortcut. Legacy PUTs have neither bound and retain
	// their established 412 failure behaviour.
	if resp.StatusCode == http.StatusPreconditionFailed && it.requireCreate && it.checksumSHA256 != "" && it.checksumHex != "" {
		return nil
	}
	return &putError{status: resp.StatusCode}
}

type putError struct{ status int }

func (e *putError) Error() string {
	return fmt.Sprintf("upload: presigned PUT returned HTTP %d", e.status)
}

func isRetryable(err error) bool {
	if backup.IsTransportError(err) {
		return true
	}
	var pe *putError
	if errors.As(err, &pe) {
		return pe.status >= 500 && pe.status < 600
	}
	return false
}

func uploadFailureCode(err error) envelope.ErrorCode {
	if backup.IsTransportError(err) || errors.Is(err, context.DeadlineExceeded) {
		return envelope.ErrBackendUnreachable
	}
	return envelope.ErrBackendError
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
