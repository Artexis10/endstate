// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/provision"
)

func TestRunGenerations_ListsNewestFirst(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	if err := provision.Write(&provision.Generation{Backend: "nix", RunID: "r1"}); err != nil {
		t.Fatal(err)
	}
	if err := provision.Write(&provision.Generation{Backend: "winget", RunID: "r2"}); err != nil {
		t.Fatal(err)
	}

	res, eerr := RunGenerations(GenerationsFlags{})
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	gr, ok := res.(*GenerationsResult)
	if !ok {
		t.Fatalf("want *GenerationsResult, got %T", res)
	}
	if len(gr.Generations) != 2 {
		t.Fatalf("want 2 generations, got %d", len(gr.Generations))
	}
	if gr.Generations[0].Number != 2 || gr.Generations[1].Number != 1 {
		t.Fatalf("not newest-first: %d, %d", gr.Generations[0].Number, gr.Generations[1].Number)
	}
}

func TestRunGenerations_EmptyIsNotAnError(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	res, eerr := RunGenerations(GenerationsFlags{})
	if eerr != nil {
		t.Fatalf("unexpected envelope error: %v", eerr)
	}
	gr := res.(*GenerationsResult)
	if gr.Generations == nil || len(gr.Generations) != 0 {
		t.Fatalf("want empty non-nil slice, got %#v", gr.Generations)
	}
}
