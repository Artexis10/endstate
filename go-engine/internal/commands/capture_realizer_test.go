// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// ---------------------------------------------------------------------------
// Helpers for the realizer capture path
// ---------------------------------------------------------------------------

// nixSet builds a realizer.Set from element names. Each element's Name is the
// bare attr (its map key, as `nix profile list` reports a nixpkgs install) while
// AttrPath carries a realistic, system-qualified path that DIFFERS from Name —
// so a test asserting the emitted ref equals Name proves we emit the portable
// bare attr, not the arch-baked AttrPath. The store path uses a valid 32-char
// base32 hash and encodes version "1.0.0" so version-capture tests can assert
// the parsed version.
func nixSet(names ...string) realizer.Set {
	els := map[string]realizer.Element{}
	for _, n := range names {
		els[n] = realizer.Element{
			Name:     n,
			AttrPath: "legacyPackages.x86_64-linux." + n,
			// 32-char base32 hash (all zeros is valid base32) + name + version.
			StorePaths: []string{"/nix/store/00000000000000000000000000000000-" + n + "-1.0.0"},
		}
	}
	return realizer.Set{Generation: 1, Elements: els}
}

// capturedManifestFile is the on-disk capture manifest shape, for read-back.
type capturedManifestFile struct {
	Version  int    `json:"version"`
	Name     string `json:"name"`
	Captured string `json:"captured"`
	Apps     []struct {
		ID      string            `json:"id"`
		Refs    map[string]string `json:"refs"`
		Version string            `json:"version"`
	} `json:"apps"`
	HomeManager *struct {
		Flake    string          `json:"flake"`
		Config   string          `json:"config"`
		Settings json.RawMessage `json:"settings"`
	} `json:"homeManager"`
}

// withFakeGenerations replaces listGenerationsFn (the provisioning-history read
// used to recover the activated home-manager flake) with one returning the given
// generations and error, calls f, then restores the original. Generations are
// newest-first, matching provision.List.
func withFakeGenerations(gens []*provision.Generation, err error, f func()) {
	orig := listGenerationsFn
	listGenerationsFn = func() ([]*provision.Generation, error) { return gens, err }
	defer func() { listGenerationsFn = orig }()
	f()
}

// hmGen builds a provisioning generation that activated the given home-manager
// flake (genNum is the home-manager generation number, irrelevant to capture).
func hmGen(flake string, genNum int) *provision.Generation {
	return &provision.Generation{HomeManager: &provision.HomeGenRef{Flake: flake, Generation: genNum}}
}

// pkgGen builds a package-only provisioning generation (no home-manager config).
func pkgGen() *provision.Generation {
	return &provision.Generation{}
}

// hmGenConfig builds a generation from a homeManager.config apply: it records the
// user's declared config path AND the engine-generated (machine-local) flake the
// engine actually activated.
func hmGenConfig(config, generatedFlake string, genNum int) *provision.Generation {
	return &provision.Generation{HomeManager: &provision.HomeGenRef{Config: config, Flake: generatedFlake, Generation: genNum}}
}

// hmGenSettings builds a generation from a homeManager.settings (catalog) apply:
// it records the user's declared catalog settings AND the engine-compiled
// (machine-local) flake the engine actually activated.
func hmGenSettings(settings *manifest.HomeManagerSettings, generatedFlake string, genNum int) *provision.Generation {
	return &provision.Generation{HomeManager: &provision.HomeGenRef{Settings: settings, Flake: generatedFlake, Generation: genNum}}
}

func readCapturedManifest(t *testing.T, path string) capturedManifestFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest %s: %v", path, err)
	}
	var mf capturedManifestFile
	if err := json.Unmarshal(data, &mf); err != nil {
		t.Fatalf("manifest is not valid JSON: %v\n%s", err, data)
	}
	return mf
}

// ---------------------------------------------------------------------------
// runCaptureRealizer — core behavior
// ---------------------------------------------------------------------------

// Each element is emitted as a manifest app whose only ref is host-keyed
// (runtime.GOOS) and equal to the element's bare attr Name — NOT its AttrPath.
// Apps are sorted by id; the manifest is version 1; no version is recorded; and
// the result synthesizes no config modules (packages only).
func TestRunCaptureRealizer_EmitsBareAttrHostKeyedRefs(t *testing.T) {
	out := filepath.Join(t.TempDir(), "nix-capture.jsonc")
	fr := &fakeRealizer{currentSet: nixSet("ripgrep", "jq")}

	raw, eerr := runCaptureRealizer(CaptureFlags{Out: out, Name: "nixbox"}, fr, noopEmitter())
	if eerr != nil {
		t.Fatalf("runCaptureRealizer returned envelope error: %+v", eerr)
	}
	res, ok := raw.(*CaptureResult)
	if !ok {
		t.Fatalf("expected *CaptureResult, got %T", raw)
	}
	if res.Counts.Included != 2 {
		t.Errorf("Counts.Included = %d, want 2", res.Counts.Included)
	}
	if res.Counts.TotalFound != 2 {
		t.Errorf("Counts.TotalFound = %d, want 2", res.Counts.TotalFound)
	}
	if res.OutputFormat != "jsonc" {
		t.Errorf("OutputFormat = %q, want jsonc", res.OutputFormat)
	}
	if len(res.ConfigModules) != 0 {
		t.Errorf("realizer path must not synthesize config modules, got %d", len(res.ConfigModules))
	}

	mf := readCapturedManifest(t, out)
	if mf.Version != 1 {
		t.Errorf("manifest version = %d, want 1", mf.Version)
	}
	if mf.Name != "nixbox" {
		t.Errorf("manifest name = %q, want nixbox", mf.Name)
	}
	if len(mf.Apps) != 2 {
		t.Fatalf("manifest apps = %d, want 2", len(mf.Apps))
	}
	// Deterministic order: sorted by id (jq before ripgrep).
	if mf.Apps[0].ID != "jq" || mf.Apps[1].ID != "ripgrep" {
		t.Errorf("apps not sorted by id: %q, %q", mf.Apps[0].ID, mf.Apps[1].ID)
	}
	for _, a := range mf.Apps {
		ref, ok := a.Refs[runtime.GOOS]
		if !ok {
			t.Errorf("app %q missing host ref %q: %+v", a.ID, runtime.GOOS, a.Refs)
			continue
		}
		if ref != a.ID {
			t.Errorf("app %q: ref = %q, want bare attr %q (must not emit AttrPath)", a.ID, ref, a.ID)
		}
		if len(a.Refs) != 1 {
			t.Errorf("app %q: expected exactly the host ref, got %+v", a.ID, a.Refs)
		}
		// Version is now populated from the store path ("1.0.0" in the nixSet fixture).
		if a.Version != "1.0.0" {
			t.Errorf("app %q: version = %q, want 1.0.0 (parsed from store path)", a.ID, a.Version)
		}
	}
}

// An empty realizer set produces a valid manifest with an empty apps array (not
// null) and no error.
func TestRunCaptureRealizer_EmptyProfile_WritesValidEmptyManifest(t *testing.T) {
	out := filepath.Join(t.TempDir(), "empty.jsonc")
	fr := &fakeRealizer{currentSet: realizer.Set{Elements: map[string]realizer.Element{}}}

	raw, eerr := runCaptureRealizer(CaptureFlags{Out: out}, fr, noopEmitter())
	if eerr != nil {
		t.Fatalf("runCaptureRealizer returned envelope error: %+v", eerr)
	}
	res := raw.(*CaptureResult)
	if res.Counts.Included != 0 {
		t.Errorf("Counts.Included = %d, want 0", res.Counts.Included)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if !strings.Contains(string(data), `"apps": []`) {
		t.Errorf("expected empty apps array (not null), got:\n%s", data)
	}
	mf := readCapturedManifest(t, out)
	if mf.Version != 1 {
		t.Errorf("manifest version = %d, want 1", mf.Version)
	}
	if len(mf.Apps) != 0 {
		t.Errorf("manifest apps = %d, want 0", len(mf.Apps))
	}
}

// A systemic backend failure (unavailable / permission denied) surfaces as a
// top-level envelope error and writes no manifest.
func TestRunCaptureRealizer_SystemicError_ReturnsEnvelopeError(t *testing.T) {
	out := filepath.Join(t.TempDir(), "systemic.jsonc")
	fr := &fakeRealizer{currentErr: &realizer.Error{Code: envelope.ErrRealizerUnavailable, Raw: "daemon down"}}

	raw, eerr := runCaptureRealizer(CaptureFlags{Out: out}, fr, noopEmitter())
	if eerr == nil {
		t.Fatal("expected envelope error for systemic Current() failure, got nil")
	}
	if eerr.Code != envelope.ErrRealizerUnavailable {
		t.Errorf("error code = %q, want REALIZER_UNAVAILABLE", eerr.Code)
	}
	if raw != nil {
		t.Errorf("expected nil data on systemic error, got %T", raw)
	}
	if _, statErr := os.Stat(out); statErr == nil {
		t.Error("expected no manifest written on systemic error")
	}
}

// A non-systemic read issue is treated as an empty set (mirrors verify): capture
// writes a valid empty manifest rather than failing the whole command.
func TestRunCaptureRealizer_NonSystemicError_CapturesEmpty(t *testing.T) {
	out := filepath.Join(t.TempDir(), "nonsystemic.jsonc")
	fr := &fakeRealizer{currentErr: errors.New("transient read glitch")}

	raw, eerr := runCaptureRealizer(CaptureFlags{Out: out}, fr, noopEmitter())
	if eerr != nil {
		t.Fatalf("non-systemic error should not fail capture, got: %+v", eerr)
	}
	res := raw.(*CaptureResult)
	if res.Counts.Included != 0 {
		t.Errorf("Counts.Included = %d, want 0", res.Counts.Included)
	}
}

// --update merges with the existing manifest, host-keyed, without duplicating a
// package already present under the host key.
func TestRunCaptureRealizer_Update_HostKeyedDedup(t *testing.T) {
	tmp := t.TempDir()
	existing := filepath.Join(tmp, "existing.jsonc")
	existingJSON := fmt.Sprintf(`{"version":1,"name":"box","apps":[{"id":"ripgrep","refs":{"%s":"ripgrep"}}]}`, runtime.GOOS)
	if err := os.WriteFile(existing, []byte(existingJSON), 0644); err != nil {
		t.Fatalf("write existing manifest: %v", err)
	}
	out := filepath.Join(tmp, "merged.jsonc")
	fr := &fakeRealizer{currentSet: nixSet("ripgrep", "jq")}

	raw, eerr := runCaptureRealizer(CaptureFlags{Out: out, Update: true, Manifest: existing}, fr, noopEmitter())
	if eerr != nil {
		t.Fatalf("runCaptureRealizer returned envelope error: %+v", eerr)
	}
	_ = raw

	mf := readCapturedManifest(t, out)
	if len(mf.Apps) != 2 {
		t.Fatalf("merged apps = %d, want 2 (ripgrep deduped + jq added)", len(mf.Apps))
	}
	count := map[string]int{}
	for _, a := range mf.Apps {
		count[a.ID]++
	}
	if count["ripgrep"] != 1 {
		t.Errorf("ripgrep appears %d times, want 1 (no duplicate on merge)", count["ripgrep"])
	}
	if count["jq"] != 1 {
		t.Errorf("jq appears %d times, want 1", count["jq"])
	}
}

// ---------------------------------------------------------------------------
// RunCapture fork
// ---------------------------------------------------------------------------

// When a realizer is available (newRealizerFn succeeds), RunCapture takes the
// realizer capture path and emits host-keyed refs — not winget "windows" refs.
func TestRunCapture_ForksToRealizerWhenAvailable(t *testing.T) {
	out := filepath.Join(t.TempDir(), "fork.jsonc")
	fr := &fakeRealizer{currentSet: nixSet("ripgrep")}

	var raw interface{}
	var eerr *envelope.Error
	withFakeRealizer(fr, func() {
		raw, eerr = RunCapture(CaptureFlags{Out: out})
	})
	if eerr != nil {
		t.Fatalf("RunCapture returned envelope error: %+v", eerr)
	}
	res := raw.(*CaptureResult)
	if res.Counts.Included != 1 {
		t.Errorf("Counts.Included = %d, want 1", res.Counts.Included)
	}

	mf := readCapturedManifest(t, out)
	if len(mf.Apps) != 1 {
		t.Fatalf("apps = %d, want 1", len(mf.Apps))
	}
	if _, ok := mf.Apps[0].Refs[runtime.GOOS]; !ok {
		t.Errorf("fork did not take the realizer path; refs = %+v", mf.Apps[0].Refs)
	}
	if _, win := mf.Apps[0].Refs["windows"]; win && runtime.GOOS != "windows" {
		t.Errorf("unexpected windows ref — winget path was taken: %+v", mf.Apps[0].Refs)
	}
}

// When no realizer is available (Windows: newRealizerFn → ErrNoRealizer),
// RunCapture falls through to the winget path unchanged (windows-keyed refs).
func TestRunCapture_NoRealizer_UsesWingetPath(t *testing.T) {
	out := filepath.Join(t.TempDir(), "winget.jsonc")

	orig := newRealizerFn
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	defer func() { newRealizerFn = orig }()

	withMockSnapshot(sampleApps(), nil, func() {
		noopDisplayNames(func() {
			emptyCatalog(func() {
				_, eerr := RunCapture(CaptureFlags{Out: out})
				if eerr != nil {
					t.Fatalf("RunCapture returned envelope error: %+v", eerr)
				}
			})
		})
	})

	mf := readCapturedManifest(t, out)
	if len(mf.Apps) == 0 {
		t.Fatal("expected winget apps captured")
	}
	foundWin := false
	for _, a := range mf.Apps {
		if _, ok := a.Refs["windows"]; ok {
			foundWin = true
		}
	}
	if !foundWin {
		t.Errorf("expected winget path (windows refs) when no realizer, got %+v", mf.Apps)
	}
}

// ---------------------------------------------------------------------------
// Home-manager config capture (engine-provisioned flake passthrough)
// ---------------------------------------------------------------------------

// The activated home-manager flake is recovered from provisioning history and
// emitted into the manifest alongside the captured packages.
func TestRunCaptureRealizer_EmitsHomeManagerFlakeFromHistory(t *testing.T) {
	out := filepath.Join(t.TempDir(), "hm.jsonc")
	fr := &fakeRealizer{currentSet: nixSet("ripgrep")}

	withFakeGenerations([]*provision.Generation{hmGen("/home/me/dots#hugo", 3)}, nil, func() {
		if _, eerr := runCaptureRealizer(CaptureFlags{Out: out}, fr, noopEmitter()); eerr != nil {
			t.Fatalf("runCaptureRealizer returned envelope error: %+v", eerr)
		}
	})

	mf := readCapturedManifest(t, out)
	if mf.HomeManager == nil {
		t.Fatalf("expected homeManager block, got none")
	}
	if mf.HomeManager.Flake != "/home/me/dots#hugo" {
		t.Errorf("homeManager.flake = %q, want /home/me/dots#hugo", mf.HomeManager.Flake)
	}
	if len(mf.Apps) != 1 {
		t.Errorf("packages should still be captured: apps = %d, want 1", len(mf.Apps))
	}
}

// When a later generation activated no config (HomeManager=nil), capture uses the
// most-recent generation that DID — the config still in effect — not "latest".
func TestRunCaptureRealizer_HomeManager_MostRecentNonNil(t *testing.T) {
	out := filepath.Join(t.TempDir(), "hm2.jsonc")
	fr := &fakeRealizer{currentSet: nixSet("jq")}

	// newest-first: newest is package-only, older activated the flake.
	gens := []*provision.Generation{pkgGen(), hmGen("github:me/dots#box", 2)}
	withFakeGenerations(gens, nil, func() {
		if _, eerr := runCaptureRealizer(CaptureFlags{Out: out}, fr, noopEmitter()); eerr != nil {
			t.Fatalf("runCaptureRealizer returned envelope error: %+v", eerr)
		}
	})

	mf := readCapturedManifest(t, out)
	if mf.HomeManager == nil || mf.HomeManager.Flake != "github:me/dots#box" {
		t.Fatalf("expected the most-recent activated flake github:me/dots#box, got %+v", mf.HomeManager)
	}
}

// No generation activated a config → the homeManager field is omitted entirely.
func TestRunCaptureRealizer_NoHomeManagerHistory_OmitsField(t *testing.T) {
	out := filepath.Join(t.TempDir(), "hm3.jsonc")
	fr := &fakeRealizer{currentSet: nixSet("ripgrep")}

	withFakeGenerations([]*provision.Generation{pkgGen()}, nil, func() {
		if _, eerr := runCaptureRealizer(CaptureFlags{Out: out}, fr, noopEmitter()); eerr != nil {
			t.Fatalf("runCaptureRealizer returned envelope error: %+v", eerr)
		}
	})

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if strings.Contains(string(data), "homeManager") {
		t.Errorf("expected no homeManager field, got:\n%s", data)
	}
	if mf := readCapturedManifest(t, out); mf.HomeManager != nil {
		t.Errorf("expected nil homeManager, got %+v", mf.HomeManager)
	}
}

// A provisioning-history read error must not fail capture: the package manifest
// is still written and the homeManager field is simply omitted (best-effort).
func TestRunCaptureRealizer_GenerationsError_OmitsAndSucceeds(t *testing.T) {
	out := filepath.Join(t.TempDir(), "hm4.jsonc")
	fr := &fakeRealizer{currentSet: nixSet("ripgrep", "jq")}

	withFakeGenerations(nil, errors.New("state dir unreadable"), func() {
		raw, eerr := runCaptureRealizer(CaptureFlags{Out: out}, fr, noopEmitter())
		if eerr != nil {
			t.Fatalf("history read error must not fail capture: %+v", eerr)
		}
		res := raw.(*CaptureResult)
		if res.Counts.Included != 2 {
			t.Errorf("packages still captured: Included = %d, want 2", res.Counts.Included)
		}
	})

	mf := readCapturedManifest(t, out)
	if mf.HomeManager != nil {
		t.Errorf("expected nil homeManager on history error, got %+v", mf.HomeManager)
	}
	if len(mf.Apps) != 2 {
		t.Errorf("apps = %d, want 2", len(mf.Apps))
	}
}

// On --update with no config in history, an existing manifest's homeManager block
// is preserved rather than dropped.
func TestRunCaptureRealizer_Update_PreservesExistingHomeManager(t *testing.T) {
	tmp := t.TempDir()
	existing := filepath.Join(tmp, "existing.jsonc")
	existingJSON := fmt.Sprintf(
		`{"version":1,"name":"box","apps":[{"id":"ripgrep","refs":{"%s":"ripgrep"}}],"homeManager":{"flake":"github:me/dots#hugo"}}`,
		runtime.GOOS,
	)
	if err := os.WriteFile(existing, []byte(existingJSON), 0644); err != nil {
		t.Fatalf("write existing manifest: %v", err)
	}
	out := filepath.Join(tmp, "merged.jsonc")
	fr := &fakeRealizer{currentSet: nixSet("ripgrep", "jq")}

	withFakeGenerations([]*provision.Generation{pkgGen()}, nil, func() {
		if _, eerr := runCaptureRealizer(CaptureFlags{Out: out, Update: true, Manifest: existing}, fr, noopEmitter()); eerr != nil {
			t.Fatalf("runCaptureRealizer returned envelope error: %+v", eerr)
		}
	})

	mf := readCapturedManifest(t, out)
	if mf.HomeManager == nil || mf.HomeManager.Flake != "github:me/dots#hugo" {
		t.Fatalf("expected preserved homeManager github:me/dots#hugo, got %+v", mf.HomeManager)
	}
}

// A config-declared apply records both the user's config path and the engine-
// generated (machine-local) flake. Capture must emit the declared config path,
// NOT the generated flake — otherwise the manifest can't round-trip elsewhere.
func TestRunCaptureRealizer_EmitsConfigOverGeneratedFlake(t *testing.T) {
	out := filepath.Join(t.TempDir(), "hmcfg.jsonc")
	fr := &fakeRealizer{currentSet: nixSet("ripgrep")}

	gen := hmGenConfig("./home.nix", "/home/me/.local/state/endstate/state/home-manager/me#me", 4)
	withFakeGenerations([]*provision.Generation{gen}, nil, func() {
		if _, eerr := runCaptureRealizer(CaptureFlags{Out: out}, fr, noopEmitter()); eerr != nil {
			t.Fatalf("runCaptureRealizer returned envelope error: %+v", eerr)
		}
	})

	mf := readCapturedManifest(t, out)
	if mf.HomeManager == nil {
		t.Fatalf("expected homeManager block, got none")
	}
	if mf.HomeManager.Config != "./home.nix" {
		t.Errorf("homeManager.config = %q, want ./home.nix", mf.HomeManager.Config)
	}
	if mf.HomeManager.Flake != "" {
		t.Errorf("must NOT emit the generated flake for a config apply, got flake=%q", mf.HomeManager.Flake)
	}
}

// A flake-declared apply still captures as homeManager.flake (no regression).
func TestRunCaptureRealizer_FlakeDeclared_StillEmitsFlake(t *testing.T) {
	out := filepath.Join(t.TempDir(), "hmflake.jsonc")
	fr := &fakeRealizer{currentSet: nixSet("ripgrep")}

	withFakeGenerations([]*provision.Generation{hmGen("github:me/dots#hugo", 2)}, nil, func() {
		if _, eerr := runCaptureRealizer(CaptureFlags{Out: out}, fr, noopEmitter()); eerr != nil {
			t.Fatalf("runCaptureRealizer returned envelope error: %+v", eerr)
		}
	})

	mf := readCapturedManifest(t, out)
	if mf.HomeManager == nil || mf.HomeManager.Flake != "github:me/dots#hugo" {
		t.Fatalf("expected homeManager.flake github:me/dots#hugo, got %+v", mf.HomeManager)
	}
	if mf.HomeManager.Config != "" {
		t.Errorf("flake apply must not emit a config, got config=%q", mf.HomeManager.Config)
	}
}

// ---------------------------------------------------------------------------
// Version capture (store-path parsing)
// ---------------------------------------------------------------------------

// nixSetWithVersion builds a realizer.Set where each element has a store path
// encoding a specific version, for version-capture assertions.
func nixSetWithVersion(name, version string) realizer.Set {
	return realizer.Set{
		Generation: 1,
		Elements: map[string]realizer.Element{
			name: {
				Name:     name,
				AttrPath: "legacyPackages.x86_64-linux." + name,
				StorePaths: []string{
					"/nix/store/2rwsbbpn5p76jf35rv7cb9qlhpxnp83p-" + name + "-" + version,
				},
			},
		},
	}
}

// nixSetNoStorePaths builds a realizer.Set where the element has no store paths,
// simulating the case where store-path version parsing yields "".
func nixSetNoStorePaths(name string) realizer.Set {
	return realizer.Set{
		Generation: 1,
		Elements: map[string]realizer.Element{
			name: {
				Name:       name,
				AttrPath:   "legacyPackages.x86_64-linux." + name,
				StorePaths: []string{},
			},
		},
	}
}

// Version is parsed from the store path and emitted into the captured manifest
// App.Version field.
func TestRunCaptureRealizer_Version_PopulatedFromStorePath(t *testing.T) {
	out := filepath.Join(t.TempDir(), "ver.jsonc")
	fr := &fakeRealizer{currentSet: nixSetWithVersion("ripgrep", "14.1.0")}

	_, eerr := runCaptureRealizer(CaptureFlags{Out: out}, fr, noopEmitter())
	if eerr != nil {
		t.Fatalf("runCaptureRealizer returned envelope error: %+v", eerr)
	}

	mf := readCapturedManifest(t, out)
	if len(mf.Apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(mf.Apps))
	}
	if mf.Apps[0].Version != "14.1.0" {
		t.Errorf("app version = %q, want 14.1.0", mf.Apps[0].Version)
	}
}

// When the element has no store paths, the captured App.Version is empty and
// the run does NOT fail.
func TestRunCaptureRealizer_Version_EmptyWhenNoStorePaths(t *testing.T) {
	out := filepath.Join(t.TempDir(), "nover.jsonc")
	fr := &fakeRealizer{currentSet: nixSetNoStorePaths("ripgrep")}

	raw, eerr := runCaptureRealizer(CaptureFlags{Out: out}, fr, noopEmitter())
	if eerr != nil {
		t.Fatalf("runCaptureRealizer returned envelope error: %+v", eerr)
	}
	res := raw.(*CaptureResult)
	if res.Counts.Included != 1 {
		t.Errorf("Counts.Included = %d, want 1", res.Counts.Included)
	}

	mf := readCapturedManifest(t, out)
	if len(mf.Apps) != 1 {
		t.Fatalf("expected 1 app, got %d", len(mf.Apps))
	}
	if mf.Apps[0].Version != "" {
		t.Errorf("app version = %q, want empty when no store paths", mf.Apps[0].Version)
	}
}
