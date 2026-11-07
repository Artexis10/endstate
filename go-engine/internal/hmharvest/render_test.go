// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package hmharvest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/releaseinputs"
	"github.com/Artexis10/endstate/go-engine/internal/settingscodec"
)

func productionRegistry(t *testing.T) *hmregistry.Registry {
	t.Helper()
	registry, err := hmregistry.Load(filepath.Join("..", "..", "..", "catalog", "home-manager", "registry.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

// The reviewed fixture is decoded, emitted through the runtime generator, built by
// Home Manager and decoded again. Each hop must keep the same value.
func TestTypedRoundTripDecodeEmitRenderDecodeEquivalence(t *testing.T) {
	registry := productionRegistry(t)
	cases, err := typedCases(registry, settingscodec.TypedFixtures)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("the production registry has no typed fixture to prove")
	}
	for _, c := range cases {
		t.Run("emit/"+c.entry+"/"+c.fixture, func(t *testing.T) {
			if err := c.checkLeaf(c.setting.Canonical); err != nil {
				t.Fatalf("the canonical encoding does not decode to the fixture: %v", err)
			}
			if runtime.GOOS == "windows" {
				t.Skip("the Home Manager flake generator serves Linux and macOS")
			}
			flake, err := c.generate(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			directory, _, _ := strings.Cut(flake, "#")
			staged, err := filepath.Glob(filepath.Join(directory, "typed", "*.toml"))
			if err != nil || len(staged) != 1 {
				t.Fatalf("expected one staged canonical file: %v %v", staged, err)
			}
			data, err := os.ReadFile(staged[0])
			if err != nil {
				t.Fatal(err)
			}
			if err := c.checkLeaf(data); err != nil {
				t.Fatalf("the staged file does not decode to the fixture: %v", err)
			}
			module, err := os.ReadFile(filepath.Join(directory, "typed.nix"))
			if err != nil || !strings.Contains(string(module), "./typed/"+filepath.Base(staged[0])) {
				t.Fatalf("the typed module does not read its staged file: %v\n%s", err, module)
			}
		})
	}
	t.Run("render", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("Home Manager builds activations on Linux")
		}
		if _, err := exec.LookPath("nix"); err != nil {
			// The required Nix catalog job runs this step through endstate-hm-registry --render-typed.
			t.Skip("nix is not on PATH, so the built activation is not checked here")
		}
		built, err := RenderTypedFixtures(context.Background(), registry, 10*time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if built != len(cases) {
			t.Fatalf("built %d of %d fixtures", built, len(cases))
		}
	})
}

func TestRenderRefusesATypedEntryWithoutFixture(t *testing.T) {
	registry := productionRegistry(t)
	noFixtures := func(string) ([]settingscodec.TypedFixture, error) { return nil, nil }
	_, err := typedCases(registry, noFixtures)
	if err == nil || !strings.Contains(err.Error(), "has no fixture") {
		t.Fatalf("a typed entry without a fixture was accepted: %v", err)
	}
}

func TestRenderFailsWhenTheBuiltFileDiffersFromTheFixture(t *testing.T) {
	cases, err := typedCases(productionRegistry(t), settingscodec.TypedFixtures)
	if err != nil || len(cases) == 0 {
		t.Fatalf("no typed fixture case: %v", err)
	}
	c := cases[0]
	for name, built := range map[string]string{
		"a different value":        "[virtualenvs]\ncreate = true\nin-project = false\n",
		"a key outside the schema": "[virtualenvs]\ncreate = true\n\n[unreviewed]\nkey = 1\n",
		"an empty file":            "",
	} {
		if err := c.checkLeaf([]byte(built)); err == nil {
			t.Errorf("%s was accepted as the built file", name)
		}
	}
}

// lockGraph is a flake.lock with the generator's two root inputs at these revisions.
func lockGraph(nixpkgs, homeManager string) []byte {
	return []byte(`{"version":7,"root":"root","nodes":{
  "root":{"inputs":{"nixpkgs":"nixpkgs","home-manager":"home-manager"}},
  "nixpkgs":{"locked":{"type":"github","owner":"NixOS","repo":"nixpkgs","rev":"` + nixpkgs + `"}},
  "home-manager":{"inputs":{"nixpkgs":["nixpkgs"]},"locked":{"type":"github","owner":"nix-community","repo":"home-manager","rev":"` + homeManager + `"}}}}`)
}

func TestRenderRefusesAnActivationThatLockedAnotherHomeManager(t *testing.T) {
	inputs, err := releaseinputs.Load()
	if err != nil {
		t.Fatal(err)
	}
	release := &realizer.InputResolution{LockGraph: lockGraph(inputs.Nixpkgs.Revision, inputs.HomeManager.Revision)}
	if err := checkReleasePins(release, inputs); err != nil {
		t.Fatalf("the release pair was refused: %v", err)
	}
	// ENDSTATE_HOME_MANAGER_PIN changes the Home Manager revision that the evaluation locks.
	overridden := &realizer.InputResolution{LockGraph: lockGraph(inputs.Nixpkgs.Revision, "0000000000000000000000000000000000000000")}
	if err := checkReleasePins(overridden, inputs); err == nil {
		t.Fatal("the proof accepted an activation that locked another Home Manager revision")
	}
}
