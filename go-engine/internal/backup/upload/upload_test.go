// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package upload

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/backup/crypto"
	"github.com/Artexis10/endstate/go-engine/internal/backup/storage"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestChunkBytes_DivisibleSize(t *testing.T) {
	in := bytes.Repeat([]byte{0x01}, 4*crypto.ChunkPlainSize)
	got := chunkBytes(in, crypto.ChunkPlainSize)
	if len(got) != 4 {
		t.Fatalf("chunk count = %d, want 4", len(got))
	}
	for i, c := range got {
		if len(c) != crypto.ChunkPlainSize {
			t.Errorf("chunk %d size = %d, want %d", i, len(c), crypto.ChunkPlainSize)
		}
	}
}

func TestChunkBytes_RemainderInLastChunk(t *testing.T) {
	in := bytes.Repeat([]byte{0xAA}, crypto.ChunkPlainSize+17)
	got := chunkBytes(in, crypto.ChunkPlainSize)
	if len(got) != 2 {
		t.Fatalf("chunk count = %d, want 2", len(got))
	}
	if len(got[1]) != 17 {
		t.Errorf("trailing chunk size = %d, want 17", len(got[1]))
	}
}

func TestChunkBytes_EmptyInputProducesOneEmptyChunk(t *testing.T) {
	got := chunkBytes(nil, crypto.ChunkPlainSize)
	if len(got) != 1 || len(got[0]) != 0 {
		t.Errorf("chunkBytes(nil) = %#v, want [[]byte{}]", got)
	}
}

func TestTarProfile_FilePreservesContents(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "f.txt")
	body := []byte("hello there")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	tarBytes, err := tarProfile(path)
	if err != nil {
		t.Fatalf("tarProfile: %v", err)
	}

	tr := tar.NewReader(bytes.NewReader(tarBytes))
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("tar.Next: %v", err)
	}
	if hdr.Name != "f.txt" {
		t.Errorf("entry name = %q, want f.txt", hdr.Name)
	}
	got, _ := io.ReadAll(tr)
	if !bytes.Equal(got, body) {
		t.Errorf("body bytes mismatch")
	}
}

func TestTarProfile_DirectoryWalksTree(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "configs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "manifest.jsonc"), []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "configs", "blob.bin"), []byte{0x01, 0x02, 0x03}, 0o644); err != nil {
		t.Fatal(err)
	}

	tarBytes, err := tarProfile(tmp)
	if err != nil {
		t.Fatalf("tarProfile: %v", err)
	}

	files := map[string][]byte{}
	tr := tar.NewReader(bytes.NewReader(tarBytes))
	for {
		hdr, terr := tr.Next()
		if terr == io.EOF {
			break
		}
		if terr != nil {
			t.Fatalf("tar.Next: %v", terr)
		}
		if hdr.Typeflag == tar.TypeReg {
			body, _ := io.ReadAll(tr)
			files[hdr.Name] = body
		}
	}
	if !bytes.Equal(files["manifest.jsonc"], []byte(`{"a":1}`)) {
		t.Errorf("manifest.jsonc content mismatch")
	}
	if !bytes.Equal(files["configs/blob.bin"], []byte{0x01, 0x02, 0x03}) {
		t.Errorf("configs/blob.bin content mismatch")
	}
}

func TestUploadSummary_PreservesTotalAfterConcurrentEarlyFailures(t *testing.T) {
	const total = 11
	// More workers than the number of successes can observe cancellation, but
	// every scheduled item still belongs to exactly one terminal bucket.
	success, skipped, failed := uploadSummary(total, 2, 3)
	if success+skipped+failed != total {
		t.Fatalf("summary total = %d, want %d (success=%d skipped=%d failed=%d)", success+skipped+failed, total, success, skipped, failed)
	}
}

func TestPutParallel_RequiresCommitUsesCreateOnlyPUTs(t *testing.T) {
	for _, tc := range []struct {
		name           string
		requiresCommit bool
		wantHeader     string
		wantChecksum   bool
	}{
		{name: "explicit commit", requiresCommit: true, wantHeader: "*", wantChecksum: true},
		{name: "legacy create is durable", requiresCommit: false, wantHeader: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headers := make(chan http.Header, 3)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPut {
					t.Errorf("method = %q, want PUT", r.Method)
				}
				headers <- r.Header.Clone()
				w.WriteHeader(http.StatusNoContent)
			}))
			defer server.Close()

			items := []uploadItem{
				newUploadItem(storage.ManifestChunkIndex, server.URL, []byte("manifest"), tc.requiresCommit, 0),
				newUploadItem(0, server.URL, []byte("first"), tc.requiresCommit, 0),
				newUploadItem(1, server.URL, []byte("second"), tc.requiresCommit, 0),
			}
			success, failed, err := putParallel(context.Background(), server.Client(), items, len(items), 0, 2, events.NewEmitter("test", false))
			if err != nil || success != len(items) || failed != 0 {
				t.Fatalf("putParallel = success:%d failed:%d err:%v", success, failed, err)
			}
			close(headers)
			for got := range headers {
				if got.Get("If-None-Match") != tc.wantHeader {
					t.Errorf("If-None-Match = %q, want %q", got.Get("If-None-Match"), tc.wantHeader)
				}
				if (got.Get("x-amz-checksum-sha256") != "") != tc.wantChecksum {
					t.Errorf("x-amz-checksum-sha256 present = %t, want %t", got.Get("x-amz-checksum-sha256") != "", tc.wantChecksum)
				}
				if (got.Get("x-amz-meta-endstate-sha256") != "") != tc.wantChecksum {
					t.Errorf("x-amz-meta-endstate-sha256 present = %t, want %t", got.Get("x-amz-meta-endstate-sha256") != "", tc.wantChecksum)
				}
			}
		})
	}
}

func TestPutWithRetry_ChecksumBoundCreateOnlyRetryTreatsPreconditionAsStored(t *testing.T) {
	body := []byte("ciphertext")
	sum := sha256.Sum256(body)
	checksum := base64.StdEncoding.EncodeToString(sum[:])
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if got := r.Header.Get("If-None-Match"); got != "*" {
			t.Errorf("If-None-Match = %q, want *", got)
		}
		if got := r.Header.Get("x-amz-checksum-sha256"); got != checksum {
			t.Errorf("x-amz-checksum-sha256 = %q, want %q", got, checksum)
		}
		if got := r.Header.Get("x-amz-meta-endstate-sha256"); got != fmt.Sprintf("%x", sum) {
			t.Errorf("x-amz-meta-endstate-sha256 = %q, want %x", got, sum)
		}
		if attempts == 1 {
			// The object reached storage, but the client did not receive a
			// success response. The second create-only PUT sees it.
			http.Error(w, "response lost after store", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusPreconditionFailed)
	}))
	defer server.Close()

	err := putWithRetry(context.Background(), server.Client(), uploadItem{
		index: 0, url: server.URL, data: body, requireCreate: true, checksumSHA256: checksum, checksumHex: fmt.Sprintf("%x", sum),
	}, 1, 1, events.NewEmitter("test", false))
	if err != nil {
		t.Fatalf("putWithRetry = %v, want checksum-bound retry success", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestPutOnce_PreconditionFailureWithoutChecksumBindingFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPreconditionFailed)
	}))
	defer server.Close()
	if err := putOnce(context.Background(), server.Client(), uploadItem{
		index: 0, url: server.URL, data: []byte("ciphertext"), requireCreate: true, checksumSHA256: "bound-only",
	}); err == nil {
		t.Fatal("unbound create-only precondition failure succeeded")
	}
	if err := putOnce(context.Background(), server.Client(), uploadItem{
		index: 0, url: server.URL, data: []byte("ciphertext"), requireCreate: false,
	}); err == nil {
		t.Fatal("legacy precondition failure succeeded")
	}
}

func TestPutOnce_RedactsPresignedURLFromTransportError(t *testing.T) {
	const sentinel = "SENTINEL-PRESIGNED-SIGNATURE"
	url := "https://storage.example/object?X-Amz-Signature=" + sentinel
	hc := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("Put \"" + url + "\": connection refused")
	})}

	err := putOnce(context.Background(), hc, uploadItem{url: url, data: []byte("ciphertext")})
	if err == nil {
		t.Fatal("putOnce returned nil")
	}
	if strings.Contains(err.Error(), sentinel) || strings.Contains(err.Error(), "X-Amz-Signature") {
		t.Fatalf("transport error leaked presigned URL secret: %v", err)
	}
}

func TestPutOnce_DeadlineBoundsStalledHeaders(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)

	started := time.Now()
	err := putOnce(context.Background(), server.Client(), uploadItem{url: server.URL, data: []byte("ciphertext"), transferTimeout: 20 * time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "presigned object transfer failed") {
		t.Fatalf("putOnce error = %v, want redacted deadline failure", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("stalled PUT took %v, want bounded deadline", elapsed)
	}
}

func TestUploadFailureCode_RejectsDefinitePresignedStatusAsBackendError(t *testing.T) {
	if got := uploadFailureCode(&putError{status: http.StatusForbidden}); got != envelope.ErrBackendError {
		t.Fatalf("uploadFailureCode(403) = %q, want %q", got, envelope.ErrBackendError)
	}
}

func TestPutOnce_RefusesPresignedRedirect(t *testing.T) {
	landings := 0
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/put", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, server.URL+"/landing?X-Amz-Signature=SENTINEL", http.StatusFound)
	})
	mux.HandleFunc("/landing", func(w http.ResponseWriter, r *http.Request) {
		landings++
		w.WriteHeader(http.StatusOK)
	})

	err := putOnce(context.Background(), server.Client(), uploadItem{url: server.URL + "/put?X-Amz-Signature=SENTINEL", data: []byte("ciphertext")})
	if err == nil {
		t.Fatal("putOnce followed a presigned redirect")
	}
	if landings != 0 {
		t.Fatalf("redirect landing requests = %d, want 0", landings)
	}
	if strings.Contains(err.Error(), "SENTINEL") {
		t.Fatalf("redirect error leaked URL secret: %v", err)
	}
}

func TestPreparedCreate_RoundTripAndCorruptionFailClosed(t *testing.T) {
	chunk := []byte("ciphertext")
	sum := sha256.Sum256(chunk)
	prepared := PreparedCreate{
		SchemaVersion:     1,
		BackupID:          "backup-1",
		OperationID:       "operation-1",
		EncryptedManifest: []byte("manifest"),
		ManifestSHA256:    fmt.Sprintf("%x", sha256.Sum256([]byte("manifest"))),
		Chunks:            [][]byte{chunk},
		ChunkMetadata:     []storage.ChunkMetaWire{{Index: 0, EncryptedSize: int64(len(chunk)), SHA256: fmt.Sprintf("%x", sum)}},
	}
	path := filepath.Join(t.TempDir(), "create.json")
	if err := writePreparedCreate(path, prepared); err != nil {
		t.Fatalf("writePreparedCreate: %v", err)
	}
	got, err := readPreparedCreate(path)
	if err != nil {
		t.Fatalf("readPreparedCreate: %v", err)
	}
	if !bytes.Equal(got.EncryptedManifest, prepared.EncryptedManifest) || !bytes.Equal(got.Chunks[0], chunk) || got.OperationID != prepared.OperationID {
		t.Fatalf("round trip = %#v, want byte-exact prepared create", got)
	}
	if err := os.WriteFile(path, []byte(`{"schemaVersion":1,"backupId":"b","operationId":"o"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPreparedCreate(path); err == nil {
		t.Fatal("corrupt prepared create was accepted")
	}
}
