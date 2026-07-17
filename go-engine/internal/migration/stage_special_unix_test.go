// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package migration

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestStageRejectsSpecialPayloadFiles(t *testing.T) {
	payload, tempParent := newStagePayload(t, nil)
	if err := syscall.Mkfifo(filepath.Join(payload, "pipe"), 0o600); err != nil {
		t.Skipf("cannot create FIFO on this filesystem: %v", err)
	}
	request := StageRequest{
		CaptureID: "capture-special", PayloadRoot: payload,
		PayloadManifest:  []manifest.PayloadManifestEntry{{RelativePath: "pipe", Size: 0, SHA256: testStageSHA256(nil)}},
		SourceGeneration: "g1",
		TargetGeneration: &modules.GenerationDef{ID: "g1", Validate: []modules.ValidationDef{{Type: "file-exists", Path: "pipe"}}},
		TempParent:       tempParent,
	}
	_, err := NewEngine().Stage(context.Background(), request)
	assertStageError(t, err, CodePayloadIntegrityFailed, PhaseSourceIntegrity, -1, bundle.IntegrityNotRegular, "")
	assertStageTempParentEmpty(t, tempParent)
}
