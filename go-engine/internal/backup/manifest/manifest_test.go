// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package manifest_test

import (
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/backup/crypto"
	"github.com/Artexis10/endstate/go-engine/internal/backup/manifest"
)

func TestMarshalUnmarshalRoundTrip(t *testing.T) {
	in := &manifest.Manifest{
		VersionID:    "v-1",
		CreatedAt:    "2026-05-02T00:00:00Z",
		OriginalSize: 12_345_678,
		ChunkSize:    crypto.ChunkPlainSize,
		ChunkCount:   3,
		Chunks: []manifest.ChunkMeta{
			{Index: 0, EncryptedSize: 4194316, SHA256: "aa"},
			{Index: 1, EncryptedSize: 4194316, SHA256: "bb"},
			{Index: 2, EncryptedSize: 4194316, SHA256: "cc"},
		},
		KDF:        crypto.DefaultKDFParams(),
		WrappedDEK: "AAAAAAAA",
	}
	b, err := manifest.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	out, err := manifest.Unmarshal(b)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if out.VersionID != in.VersionID {
		t.Errorf("VersionID = %q, want %q", out.VersionID, in.VersionID)
	}
	if out.EnvelopeVersion != crypto.EnvelopeVersion {
		t.Errorf("EnvelopeVersion = %d, want %d", out.EnvelopeVersion, crypto.EnvelopeVersion)
	}
	if len(out.Chunks) != 3 {
		t.Errorf("Chunks: len = %d, want 3", len(out.Chunks))
	}
}

func TestUnmarshal_RejectsFutureEnvelopeVersion(t *testing.T) {
	b := []byte(`{"envelopeVersion":99,"versionId":"v"}`)
	_, err := manifest.Unmarshal(b)
	if err == nil || !strings.Contains(err.Error(), "envelopeVersion") {
		t.Errorf("got %v, expected envelopeVersion rejection", err)
	}
}

func TestSelectLatest_ByCreatedAt(t *testing.T) {
	versions := []manifest.Version{
		{VersionID: "v-1", CreatedAt: "2026-05-01T00:00:00Z"},
		{VersionID: "v-2", CreatedAt: "2026-05-03T00:00:00Z"},
		{VersionID: "v-3", CreatedAt: "2026-05-02T00:00:00Z"},
	}
	got, err := manifest.SelectLatest(versions)
	if err != nil {
		t.Fatalf("SelectLatest: %v", err)
	}
	if got.VersionID != "v-2" {
		t.Errorf("got VersionID = %q, want v-2", got.VersionID)
	}
}

func TestSelectLatest_TieBreakByID(t *testing.T) {
	versions := []manifest.Version{
		{VersionID: "v-a", CreatedAt: "2026-05-02T00:00:00Z"},
		{VersionID: "v-b", CreatedAt: "2026-05-02T00:00:00Z"},
	}
	got, _ := manifest.SelectLatest(versions)
	if got.VersionID != "v-b" {
		t.Errorf("tie-break: got %q, want v-b (lex-greater)", got.VersionID)
	}
}

func TestSelectLatest_EmptyError(t *testing.T) {
	if _, err := manifest.SelectLatest(nil); err == nil {
		t.Error("expected error on empty slice")
	}
}
