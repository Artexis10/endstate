// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package download

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/backup/auth"
	"github.com/Artexis10/endstate/go-engine/internal/backup/crypto"
	"github.com/Artexis10/endstate/go-engine/internal/backup/keychain"
	"github.com/Artexis10/endstate/go-engine/internal/backup/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/backup/storage"
	"github.com/Artexis10/endstate/go-engine/internal/events"
)

// TestUntarTo_ExtractsTreePreservingContents builds a small tar in
// memory and asserts untarTo writes byte-equal contents into the target
// directory.
func TestUntarTo_ExtractsTreePreservingContents(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range []struct {
		name string
		typ  byte
		mode int64
		body []byte
	}{
		{name: "configs/", typ: tar.TypeDir, mode: 0o755},
		{name: "manifest.jsonc", typ: tar.TypeReg, mode: 0o644, body: []byte(`{"name":"x"}`)},
		{name: "configs/blob.bin", typ: tar.TypeReg, mode: 0o644, body: []byte{0xDE, 0xAD, 0xBE, 0xEF}},
	} {
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     e.mode,
			Size:     int64(len(e.body)),
			Typeflag: e.typ,
			Format:   tar.FormatPAX,
		}
		if e.typ == tar.TypeDir {
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader: %v", err)
		}
		if e.typ == tar.TypeReg {
			if _, err := tw.Write(e.body); err != nil {
				t.Fatalf("Write: %v", err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	tmp := t.TempDir()
	if err := untarTo(bytes.NewReader(buf.Bytes()), tmp); err != nil {
		t.Fatalf("untarTo: %v", err)
	}

	got, _ := os.ReadFile(filepath.Join(tmp, "manifest.jsonc"))
	if !bytes.Equal(got, []byte(`{"name":"x"}`)) {
		t.Errorf("manifest.jsonc bytes mismatch: got %q", got)
	}
	gotBlob, _ := os.ReadFile(filepath.Join(tmp, "configs", "blob.bin"))
	if !bytes.Equal(gotBlob, []byte{0xDE, 0xAD, 0xBE, 0xEF}) {
		t.Errorf("blob.bin bytes mismatch: got %x", gotBlob)
	}
	info, _ := os.Stat(filepath.Join(tmp, "configs"))
	if !info.IsDir() {
		t.Error("configs/ should be a directory")
	}
}

func TestUntarTo_RejectsTraversalAndLinks(t *testing.T) {
	for _, tc := range []struct {
		name string
		hdr  *tar.Header
	}{
		{name: "parent traversal", hdr: &tar.Header{Name: "../outside.txt", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}},
		{name: "absolute path", hdr: &tar.Header{Name: "/outside.txt", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}},
		{name: "symlink", hdr: &tar.Header{Name: "link", Linkname: "outside.txt", Typeflag: tar.TypeSymlink}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			tw := tar.NewWriter(&buf)
			if err := tw.WriteHeader(tc.hdr); err != nil {
				t.Fatal(err)
			}
			if tc.hdr.Typeflag == tar.TypeReg {
				if _, err := tw.Write([]byte("x")); err != nil {
					t.Fatal(err)
				}
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			if err := untarTo(bytes.NewReader(buf.Bytes()), filepath.Join(root, "stage")); err == nil {
				t.Fatal("untarTo accepted unsafe tar entry")
			}
			if _, err := os.Stat(filepath.Join(root, "outside.txt")); !os.IsNotExist(err) {
				t.Errorf("unsafe entry escaped target: stat err = %v", err)
			}
		})
	}
}

func TestUntarTo_RejectsWindowsUnsafePaths(t *testing.T) {
	for _, name := range []string{"file.txt:stream", "NUL.txt", "COM1", "C:drive-relative.txt", `\\server\share\file.txt`} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			tw := tar.NewWriter(&buf)
			if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write([]byte("x")); err != nil {
				t.Fatal(err)
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(t.TempDir(), "stage")
			if err := untarTo(bytes.NewReader(buf.Bytes()), target); err == nil {
				t.Fatal("untarTo accepted unsafe Windows path")
			}
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatalf("unsafe archive created target: %v", err)
			}
		})
	}
}

func TestRecoverPublishJournal_RestoresRollbackWhenTargetMissing(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "restored")
	rollback := filepath.Join(parent, ".restored.endstate-rollback-test")
	if err := os.Mkdir(rollback, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rollback, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writePublishJournal(target, publishJournal{Target: target, Rollback: rollback, Phase: journalRollbackReady}); err != nil {
		t.Fatal(err)
	}
	if err := recoverPublishJournal(target); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(target, "old.txt"))
	if err != nil || string(got) != "old" {
		t.Fatalf("rollback was not restored: %q, %v", got, err)
	}
	if _, err := os.Stat(publishJournalPath(target)); !os.IsNotExist(err) {
		t.Fatalf("journal remains: %v", err)
	}
}

func TestRecoverPublishJournal_RemovesRollbackAfterTargetPublished(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "restored")
	rollback := filepath.Join(parent, ".restored.endstate-rollback-test")
	for path, body := range map[string]string{target: "new", rollback: "old"} {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "value.txt"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := writePublishJournal(target, publishJournal{Target: target, Rollback: rollback, Phase: journalTargetPublished}); err != nil {
		t.Fatal(err)
	}
	if err := recoverPublishJournal(target); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(target, "value.txt"))
	if err != nil || string(got) != "new" {
		t.Fatalf("new target changed: %q, %v", got, err)
	}
	if _, err := os.Stat(rollback); !os.IsNotExist(err) {
		t.Fatalf("rollback remains: %v", err)
	}
}

func TestRecoverPublishJournal_RemovesStaleJournalWhenTargetSurvived(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "restored")
	rollback := filepath.Join(parent, ".restored.endstate-rollback-stale")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writePublishJournal(target, publishJournal{Target: target, Rollback: rollback, Phase: journalTargetPublished}); err != nil {
		t.Fatal(err)
	}
	if err := recoverPublishJournal(target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(publishJournalPath(target)); !os.IsNotExist(err) {
		t.Fatalf("stale journal remains: %v", err)
	}
}

func TestPublishStage_SecondJournalWriteFailureRestoresOriginalTarget(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "restored")
	stage := filepath.Join(parent, "stage")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "value.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "value.txt"), []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}

	originalWrite := writePublishJournalFn
	t.Cleanup(func() { writePublishJournalFn = originalWrite })
	writes := 0
	writePublishJournalFn = func(path string, journal publishJournal) error {
		writes++
		if writes == 2 {
			return os.ErrPermission
		}
		return originalWrite(path, journal)
	}

	if err := publishStage(stage, target, true); err == nil {
		t.Fatal("publishStage returned nil after second journal write failed")
	}
	got, err := os.ReadFile(filepath.Join(target, "value.txt"))
	if err != nil || string(got) != "old" {
		t.Fatalf("target after journal failure = %q, %v; want original contents", got, err)
	}
	if _, err := os.Stat(publishJournalPath(target)); !os.IsNotExist(err) {
		t.Fatalf("journal remains after rollback: %v", err)
	}
}

func TestPublishStage_RollbackCleanupFailureRetainsRecoverableJournal(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "restored")
	stage := filepath.Join(parent, "stage")
	for path, body := range map[string]string{target: "old", stage: "new"} {
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "value.txt"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	originalRemoveAll := removeAllFn
	t.Cleanup(func() { removeAllFn = originalRemoveAll })
	removeAllFn = func(string) error { return os.ErrPermission }
	if err := publishStage(stage, target, true); err != nil {
		t.Fatalf("publishStage returned failure after the new target was published: %v", err)
	}
	if _, err := os.Stat(publishJournalPath(target)); err != nil {
		t.Fatalf("recoverable journal missing: %v", err)
	}
	rollbacks, err := filepath.Glob(filepath.Join(parent, ".restored.endstate-rollback-*"))
	if err != nil || len(rollbacks) != 1 {
		t.Fatalf("rollback trees = %v, %v; want one", rollbacks, err)
	}
	if err := recoverPublishJournal(target); err != nil {
		t.Fatalf("recoverPublishJournal: %v", err)
	}
	if _, err := os.Stat(rollbacks[0]); !os.IsNotExist(err) {
		t.Fatalf("rollback remains after recovery: %v", err)
	}
	if _, err := os.Stat(publishJournalPath(target)); !os.IsNotExist(err) {
		t.Fatalf("journal remains after recovery: %v", err)
	}
}

func TestGetOnce_RedactsPresignedURLFromTransportError(t *testing.T) {
	const sentinel = "SENTINEL-PRESIGNED-SIGNATURE"
	url := "https://storage.example/object?X-Amz-Signature=" + sentinel
	hc := &http.Client{Transport: downloadRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("Get \"" + url + "\": connection refused")
	})}

	_, err := getOnce(context.Background(), hc, url)
	if err == nil {
		t.Fatal("getOnce returned nil")
	}
	if strings.Contains(err.Error(), sentinel) || strings.Contains(err.Error(), "X-Amz-Signature") {
		t.Fatalf("transport error leaked presigned URL secret: %v", err)
	}
}

type downloadRoundTripFunc func(*http.Request) (*http.Response, error)

func (f downloadRoundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGetParallelChunks_RejectsObjectLargerThanManifestSize(t *testing.T) {
	dek := bytes.Repeat([]byte{0x41}, 32)
	blob, err := crypto.EncryptChunk([]byte("plaintext"), 0, dek)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(blob)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(blob)
	}))
	defer server.Close()

	out := make([][]byte, 1)
	errOut := getParallelChunks(context.Background(), server.Client(), []storage.PresignedURL{{ChunkIndex: 0, PresignedURL: server.URL}}, []manifest.ChunkMeta{{Index: 0, EncryptedSize: int64(len(blob) - 1), SHA256: fmt.Sprintf("%x", sum)}}, dek, out, 1, 0, events.NewEmitter("test", false))
	if errOut == nil || !strings.Contains(errOut.Message, "size") {
		t.Fatalf("getParallelChunks error = %#v, want encrypted-size rejection", errOut)
	}
}

func TestGetOnce_RejectsManifestLargerThanMaximum(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "16777217")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, err := getOnce(context.Background(), server.Client(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "maximum") {
		t.Fatalf("getOnce error = %v, want manifest size-limit rejection", err)
	}
}

func TestGetObject_DeadlineBoundsStalledBody(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		<-release
	}))
	defer server.Close()
	defer close(release)

	started := time.Now()
	_, err := getObject(context.Background(), server.Client(), server.URL, 1024, 0, 20*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "presigned object transfer failed") {
		t.Fatalf("getObject error = %v, want redacted deadline failure", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("stalled GET took %v, want bounded deadline", elapsed)
	}
}

func TestValidateManifest_RejectsMalformedChunkMetadataBeforeWork(t *testing.T) {
	validChunk := manifest.ChunkMeta{Index: 0, EncryptedSize: crypto.NonceSize + crypto.GCMTagSize + 1, SHA256: strings.Repeat("a", sha256.Size*2)}
	for _, tc := range []struct {
		name string
		mf   *manifest.Manifest
	}{
		{name: "negative count", mf: &manifest.Manifest{ChunkCount: -1}},
		{name: "huge count", mf: &manifest.Manifest{ChunkCount: maxManifestChunks + 1}},
		{name: "count mismatch", mf: &manifest.Manifest{ChunkCount: 2, OriginalSize: 1, ChunkSize: 1, Chunks: []manifest.ChunkMeta{validChunk}}},
		{name: "duplicate index", mf: &manifest.Manifest{ChunkCount: 2, OriginalSize: 1, ChunkSize: 1, Chunks: []manifest.ChunkMeta{validChunk, validChunk}}},
		{name: "invalid encrypted size", mf: &manifest.Manifest{ChunkCount: 1, OriginalSize: 1, ChunkSize: 1, Chunks: []manifest.ChunkMeta{{Index: 0, SHA256: validChunk.SHA256}}}},
		{name: "invalid hash", mf: &manifest.Manifest{ChunkCount: 1, OriginalSize: 1, ChunkSize: 1, Chunks: []manifest.ChunkMeta{{Index: 0, EncryptedSize: validChunk.EncryptedSize, SHA256: "not-a-hash"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateManifest(tc.mf); err == nil {
				t.Fatal("validateManifest returned nil")
			}
		})
	}
}

func TestPullVersion_RecoversPriorTargetBeforeOfflineAuthFailure(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "restored")
	rollback := filepath.Join(parent, ".restored.endstate-rollback-crash")
	if err := os.Mkdir(rollback, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rollback, "prior.txt"), []byte("must return"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writePublishJournal(target, publishJournal{Target: target, Rollback: rollback, Phase: journalRollbackReady}); err != nil {
		t.Fatal(err)
	}

	_, pullErr := PullVersion(context.Background(), Dependencies{Session: auth.NewSessionStore(keychain.NewMemory())}, "backup-id", "version-id", target, true)
	if pullErr == nil || pullErr.Code != "AUTH_REQUIRED" {
		t.Fatalf("PullVersion error = %#v, want offline/auth failure after recovery", pullErr)
	}
	got, err := os.ReadFile(filepath.Join(target, "prior.txt"))
	if err != nil || string(got) != "must return" {
		t.Fatalf("prior target was not restored: %q, %v", got, err)
	}
}

func TestPullVersion_RecoversAbsoluteJournalWhenRetryUsesRelativeTarget(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "restored")
	rollback := filepath.Join(parent, ".restored.endstate-rollback-crash")
	if err := os.Mkdir(rollback, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rollback, "prior.txt"), []byte("must return"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writePublishJournal(target, publishJournal{Target: target, Rollback: rollback, Phase: journalRollbackReady}); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(parent); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	_, pullErr := PullVersion(context.Background(), Dependencies{Session: auth.NewSessionStore(keychain.NewMemory())}, "backup-id", "version-id", "restored", true)
	if pullErr == nil || pullErr.Code != "AUTH_REQUIRED" {
		t.Fatalf("PullVersion error = %#v, want auth failure after journal recovery", pullErr)
	}
	got, err := os.ReadFile(filepath.Join(target, "prior.txt"))
	if err != nil || string(got) != "must return" {
		t.Fatalf("prior target was not restored: %q, %v", got, err)
	}
}

func TestDownloadTransportEnvelope_DefiniteStatusIsActionableBackendError(t *testing.T) {
	err := downloadTransportEnvelope("backup pull: download chunk 0", errors.New("presigned GET returned HTTP 403"))
	if err.Code != "BACKEND_ERROR" || !strings.Contains(err.Remediation, "--version-id <older>") {
		t.Fatalf("downloadTransportEnvelope = %#v, want actionable backend error", err)
	}
}

func TestGetOnce_RefusesPresignedRedirect(t *testing.T) {
	landings := 0
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/get", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, server.URL+"/landing?X-Amz-Signature=SENTINEL", http.StatusFound)
	})
	mux.HandleFunc("/landing", func(w http.ResponseWriter, r *http.Request) {
		landings++
		w.WriteHeader(http.StatusOK)
	})

	_, err := getOnce(context.Background(), server.Client(), server.URL+"/get?X-Amz-Signature=SENTINEL")
	if err == nil {
		t.Fatal("getOnce followed a presigned redirect")
	}
	if landings != 0 {
		t.Fatalf("redirect landing requests = %d, want 0", landings)
	}
	if strings.Contains(err.Error(), "SENTINEL") {
		t.Fatalf("redirect error leaked URL secret: %v", err)
	}
}
