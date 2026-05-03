// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package upload

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/backup/crypto"
)

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
