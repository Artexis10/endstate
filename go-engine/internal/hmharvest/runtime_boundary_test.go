// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package hmharvest

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRuntimeCaptureDoesNotDependOnHarvester(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "go", "list", "-deps", "../../cmd/endstate").CombinedOutput()
	if err != nil {
		t.Fatalf("read actual runtime dependency graph: %v\n%s", err, output)
	}
	// This exact module package is a release-only boundary, not an inferred name.
	for _, dependency := range strings.Fields(string(output)) {
		if dependency == "github.com/Artexis10/endstate/go-engine/internal/hmharvest" {
			t.Fatal("runtime CLI imports release-time Home Manager evaluation")
		}
	}
}
