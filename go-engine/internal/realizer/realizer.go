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

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// ErrHomeSnapshotMissing is returned by HomeRollbacker.RollbackHome when the
// target generation's built home-manager snapshot is no longer available (for
// example, garbage-collected from the Nix store), so the caller can fall back to
// re-activating the recorded configuration source instead of failing outright.
var ErrHomeSnapshotMissing = errors.New("home-manager generation snapshot is unavailable")

// Installable retains a package's portable ID and requested reference separately
// from the native resolution prepared for mutation.
type Installable struct {
	ID         string
	Ref        string
	Resolution *InputResolution `json:"resolution,omitempty"`
}

// ErrInputProvenanceUnresolved refuses an operation whose exact native target
// cannot be established before user-state mutation.
const ErrInputProvenanceUnresolved envelope.ErrorCode = "INPUT_PROVENANCE_UNRESOLVED"

// InputResolution separates requested intent from the native evaluation used to
// mutate state. Source may remain mutable even when Derivation is immutable.
type InputResolution struct {
	ExpandedRef string            `json:"expandedRef"`
	Source      InputSource       `json:"source"`
	LockGraph   json.RawMessage   `json:"lockGraph"`
	Derivation  string            `json:"drvPath"`
	Outputs     []string          `json:"outputs"`
	OutputPaths map[string]string `json:"outputPaths,omitempty"`
	Priority    int               `json:"priority"`
	MutationRef string            `json:"mutationRef"`
	WrapperURL  string            `json:"wrapperUrl,omitempty"`
	AttrPath    string            `json:"attrPath,omitempty"`
}

type InputSource struct {
	URL      string          `json:"url"`
	Path     string          `json:"path,omitempty"`
	Locked   json.RawMessage `json:"locked,omitempty"`
	Strength string          `json:"strength"` // immutable | mutable | unknown (native source metadata)
}

// InputResolver preflights every requested package before user-state mutation.
type InputResolver interface {
	Prepare([]Installable) ([]Installable, error)
}

// PreparationFinalizer releases temporary native roots after a non-committing
// run. Committed roots remain available to provisioning history and rollback.
type PreparationFinalizer interface{ FinalizePreparation(committed bool) }

// HomePreparation keeps ephemeral flat targets beside frozen activation provenance.
// FlatTargets is nil when optional parent configuration metadata is unavailable.
// A non-nil empty slice means evaluation found no enabled nonrecursive targets.
type HomePreparation struct {
	Resolution  *InputResolution
	FlatTargets []string
}

// HomePreparer freezes and optionally builds activation before package mutation.
type HomePreparer interface {
	PrepareHome(flake string, build bool) (*HomePreparation, error)
	// A positive generation with an error identifies the active intended output
	// after failed settings activation. The profile may already have been active.
	ActivatePreparedHome(*InputResolution) (int, error)
}

// Matches preserves portable identity without changing native removal keys.
func Matches(element Element, desired Installable) bool {
	if element.ID != "" {
		// An unchanged bare request can keep its installed pin. A changed request
		// must not use the portable ID to verify a different native target.
		return element.ID == desired.ID && element.RequestedRef == desired.Ref
	}
	return false
}

// ResolutionMatches requires both native identity and observed selected outputs.
func ResolutionMatches(element Element, resolution *InputResolution) bool {
	if resolution == nil || resolution.WrapperURL == "" || element.AttrPath != resolution.AttrPath {
		return false
	}
	if element.URL != resolution.WrapperURL && element.OriginalURL != resolution.WrapperURL {
		return false
	}
	expected := make([]string, 0, len(resolution.Outputs))
	seen := map[string]bool{}
	for _, output := range resolution.Outputs {
		path := resolution.OutputPaths[output]
		if path == "" {
			return false
		}
		if !seen[path] {
			expected = append(expected, path)
			seen[path] = true
		}
	}
	actual := append([]string(nil), element.StorePaths...)
	sort.Strings(expected)
	sort.Strings(actual)
	if len(expected) != len(actual) {
		return false
	}
	for i := range actual {
		if actual[i] != expected[i] {
			return false
		}
	}
	return len(expected) > 0
}

// Element is a single entry in the currently-installed set.
type Element struct {
	ID           string
	RequestedRef string
	Resolution   *InputResolution
	Name         string
	AttrPath     string
	StorePaths   []string
	// ObservedVersion retains a parsed Nix version when an inventory bridge
	// carries portable references without machine-local store paths.
	ObservedVersion string
	// OriginalURL is the flakeref requested when the profile element was added;
	// URL is Nix's resolved/locked source when present. Capture preserves this
	// provenance instead of reducing an immutable profile entry to a bare attr.
	OriginalURL string
	URL         string
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
	Stage   string // eval | fetch | build | commit | activation | spawn (pipeline stage, informational)
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

// HomeObservation joins the native generation and its exact activation output.
// Present is false only when the native profile is missing.
type HomeObservation struct {
	Present          bool
	Generation       int
	ActivationOutput string
}

// HomeObserver reads one native profile identity. Unreadable, incomplete or
// changing state returns an error instead of establishing absence.
type HomeObserver interface {
	ObserveHome() (HomeObservation, error)
}

// HomeRollbacker is an OPTIONAL realizer capability: reverting the home-manager
// configuration to a prior provisioning generation by re-activating the
// home-manager generation that generation recorded. Like Pruner/HomeActivator it
// is discovered by type-assertion on a Realizer, so only a backend that owns a
// home-manager lifecycle (the Nix realizer) implements it. The backend has no
// arbitrary version pointer-move, so re-activation mints a NEW forward
// home-manager generation that reproduces the recorded config — append-only,
// matching native package rollback ("newest == active").
type HomeRollbacker interface {
	// RollbackHome reverts the home-manager configuration by re-activating the
	// snapshot of the given home-manager generation number and returns the
	// resulting (new, forward) generation number. It returns ErrHomeSnapshotMissing
	// when that snapshot is no longer available (so the caller can fall back to the
	// recorded configuration source), or a classified *Error on activation failure
	// (raw backend text in Error.Raw only).
	RollbackHome(generation int) (newGen int, err error)
}

// PortableRef exports the original native target only when its source has
// immutable provenance. The metadata flake remains machine-local runtime state.
func PortableRef(resolution *InputResolution) string {
	if resolution == nil || resolution.Source.Strength != "immutable" {
		return ""
	}
	root := resolution.Source.URL
	if root == "" || strings.HasPrefix(root, "path:") || strings.HasPrefix(root, "/") {
		return ""
	}
	_, target, ok := strings.Cut(resolution.ExpandedRef, "#")
	if ok {
		return root + "#" + target
	}
	if i := strings.LastIndexByte(resolution.ExpandedRef, '^'); i >= 0 {
		return root + resolution.ExpandedRef[i:]
	}
	return root
}
