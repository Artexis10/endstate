// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package realizer defines the whole-set, declarative package backend interface
// used on platforms whose package manager converges a desired set as one atomic
// generation switch (e.g. Nix). It sits BESIDE driver.Driver — the per-package
// adapter winget implements — and is never shoehorned into Driver.Install.
//
// The engine selects a realizer.Realizer on hosts that have one (linux/darwin via
// Nix) and drives a whole-set apply/plan/verify path that fans the single
// generation result back into the existing per-item event stream.
package realizer

import "github.com/Artexis10/endstate/go-engine/internal/envelope"

// Installable is one resolved package to realize: the manifest App.ID plus the
// concrete reference passed to the backend (e.g. a pinned nixpkgs flake
// installable such as "nixpkgs#ripgrep").
type Installable struct {
	ID  string
	Ref string
}

// Element is a single entry in the currently-installed set.
type Element struct {
	Name       string
	AttrPath   string
	StorePaths []string
}

// Set is the currently-installed package set plus the active generation number.
type Set struct {
	Generation int
	Elements   map[string]Element // keyed by element name
}

// Diff is the result of Plan: which desired installables are missing (ToAdd)
// versus already present in the current generation (Present).
type Diff struct {
	ToAdd   []Installable
	Present []Installable
}

// Result is the outcome of Realize.
type Result struct {
	// Advanced is true iff a new generation became active (full success).
	Advanced       bool
	FromGeneration int // -1 if unknown
	ToGeneration   int // -1 if unknown
	After          Set // installed set after the operation
	// Err is non-nil on failure, carrying the already-classified engine code.
	Err *Error
}

// Error is a classified realizer failure. Raw carries the backend's raw text and
// is destined ONLY for the envelope's error.detail — never a user-facing
// message (the moat: the user never has to read Nix output).
type Error struct {
	Code    envelope.ErrorCode
	Subcode string // eval | network | collision | store | permission | daemon | spawn
	Stage   string // eval | fetch | build | commit | spawn (pipeline stage, informational)
	Raw     string
}

// Error implements the error interface, returning the stable engine code.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return string(e.Code)
}

// Realizer is a whole-set, declarative package backend. It owns PACKAGES ONLY;
// configuration restore/verify remain separate pipeline stages.
type Realizer interface {
	// Name returns the stable backend identifier (e.g. "nix").
	Name() string
	// Current reads the installed set and active generation.
	Current() (Set, error)
	// Plan computes the diff between the desired installables and Current,
	// without mutating any state.
	Plan(desired []Installable) (Diff, error)
	// Realize adds the given installables in one atomic generation switch. It
	// receives ONLY the to-add set (already-present installables are excluded by
	// Plan, because the underlying verb appends rather than reconciles). On any
	// failure the prior generation is left intact.
	Realize(toAdd []Installable) (Result, error)
}

// Pruner is an OPTIONAL realizer capability: whole-set convergence by removing
// installed-but-undeclared elements ("drift") from the engine-managed set. It is
// discovered by type-assertion on a Realizer (like the rollback capability), so
// backends that cannot safely remove (e.g. shared-system package managers) simply
// do not implement it and the engine refuses `apply --prune` with
// CONVERGENCE_UNSUPPORTED. The Nix backend implements it via `nix profile remove`,
// which advances a profile generation atomically.
type Pruner interface {
	// Remove uninstalls the named elements in one atomic operation. names are
	// element names as they appear in Current().Elements. Remove(nil) is a no-op.
	Remove(names []string) (Result, error)
}

// HomeActivator is an OPTIONAL realizer capability: activating a declared
// home-manager configuration as the config stage of apply. Like Pruner it is
// discovered by type-assertion on a Realizer, so only a backend that owns a
// home-manager lifecycle (the Nix realizer) implements it; other backends (the
// winget driver path) simply do not, and the config stage no-ops. The engine
// owns the invocation (an engine-owned `home-manager switch`), so the user never
// installs home-manager.
type HomeActivator interface {
	// ActivateHome activates the home-manager configuration named by the given
	// flakeref and returns the resulting home-manager generation number. On
	// failure it returns a classified *Error (raw backend text in Error.Raw only).
	ActivateHome(flake string) (generation int, err error)
}
