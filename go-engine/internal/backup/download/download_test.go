// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package download

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"
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
