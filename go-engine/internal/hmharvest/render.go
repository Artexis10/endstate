// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package hmharvest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/realizer/nix"
	"github.com/Artexis10/endstate/go-engine/internal/releaseinputs"
	"github.com/Artexis10/endstate/go-engine/internal/settingscodec"
)

// A typed entry ships only when the pinned Home Manager builds each reviewed
// fixture of its codec to a file that decodes to the fixture's value. The proof
// runs the generator that apply uses, with the release pins and the default
// identity, and it builds the activation without activating it.

// typedCase is one reviewed fixture of one typed registry entry.
type typedCase struct {
	entry   string
	program string
	fixture string
	codec   *settingscodec.TypedCodec
	value   map[string]any
	setting nix.TypedSetting
	// leaf is the home-relative path of the file that Home Manager must build.
	leaf string
}

// typedCases lists every fixture of every typed entry. A typed entry without a
// fixture is an error: nothing would prove that Home Manager renders it.
func typedCases(registry *hmregistry.Registry, fixturesOf func(codec string) ([]settingscodec.TypedFixture, error)) ([]typedCase, error) {
	var cases []typedCase
	for _, entry := range registry.Entries {
		if entry.Disposition != hmregistry.TypedRoundTrip {
			continue
		}
		codec, ok := settingscodec.Typed(entry.Codec)
		if !ok {
			return nil, fmt.Errorf("typed entry %s names codec %q, which is not loaded", entry.ID, entry.Codec)
		}
		fixtures, err := fixturesOf(entry.Codec)
		if err != nil {
			return nil, fmt.Errorf("typed entry %s: %w", entry.ID, err)
		}
		if len(fixtures) == 0 {
			return nil, fmt.Errorf("typed entry %s has no fixture for codec %s", entry.ID, entry.Codec)
		}
		leaf, err := typedLeaf(codec)
		if err != nil {
			return nil, fmt.Errorf("typed entry %s: %w", entry.ID, err)
		}
		for _, fixture := range fixtures {
			value, err := codec.Decode(fixture.Data)
			if err != nil {
				return nil, fmt.Errorf("typed entry %s fixture %s does not decode: %w", entry.ID, fixture.Name, err)
			}
			canonical, err := codec.Encode(value)
			if err != nil {
				return nil, fmt.Errorf("typed entry %s fixture %s does not encode: %w", entry.ID, fixture.Name, err)
			}
			cases = append(cases, typedCase{
				entry: entry.ID, program: entry.Program, fixture: fixture.Name, codec: codec, value: value, leaf: leaf,
				setting: nix.TypedSetting{Program: entry.Program, Option: codec.Option(), NullPackage: entry.PackageAcceptsNull(), Canonical: canonical},
			})
		}
	}
	return cases, nil
}

// typedLeaf resolves the codec's reviewed coordinate below the default identity's
// home, with no XDG override, because the generated configuration uses Home
// Manager's own XDG defaults.
func typedLeaf(codec *settingscodec.TypedCodec) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	absolute, err := config.ResolveEnginePath(codec.Target(), config.PathEnvironment{GOOS: "linux", Home: home})
	if err != nil {
		return "", err
	}
	return nix.HomeRelativeTarget(absolute)
}

// generate writes the flake that apply would write for this case alone.
func (c typedCase) generate(stateDir string) (string, error) {
	return nix.GenerateHomeFlakeWithPlacements(stateDir, nil, stateDir, nil, "", nil, []nix.TypedSetting{c.setting})
}

// checkLeaf compares the built file with the fixture by the codec's own decoding.
func (c typedCase) checkLeaf(built []byte) error {
	value, err := c.codec.Decode(built)
	if err != nil {
		return fmt.Errorf("typed entry %s fixture %s: the built %s does not decode: %w", c.entry, c.fixture, c.leaf, err)
	}
	if !c.codec.Equal(value, c.value) {
		return fmt.Errorf("typed entry %s fixture %s: the built %s decodes to a different value", c.entry, c.fixture, c.leaf)
	}
	return nil
}

// RenderTypedFixtures builds every fixture of every typed entry and reports how
// many it built. Each build also proves the decision 30 projection against the
// real evaluator: the generated typed module lies outside Home Manager's own
// modules, so the projection must list its program, while a configuration
// without that module must not list it.
func RenderTypedFixtures(ctx context.Context, registry *hmregistry.Registry, timeout time.Duration) (int, error) {
	if timeout <= 0 {
		return 0, fmt.Errorf("typed render timeout must be positive")
	}
	cases, err := typedCases(registry, settingscodec.TypedFixtures)
	if err != nil {
		return 0, err
	}
	inputs, err := releaseinputs.Load()
	if err != nil {
		return 0, err
	}
	work, err := os.MkdirTemp("", "endstate-typed-render-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(work)
	backend := nix.New()
	backend.Profile = filepath.Join(work, "profile")
	backend.Run = boundedNixRunner(ctx, timeout, runBoundedCommand)
	defer backend.FinalizePreparation(false)

	controlled := map[string]bool{}
	for _, c := range cases {
		if !controlled[c.program] {
			controlled[c.program] = true
			if err := checkProjectionControl(backend, work, c.program); err != nil {
				return 0, err
			}
		}
		flake, err := c.generate(work)
		if err != nil {
			return 0, fmt.Errorf("typed entry %s fixture %s: %w", c.entry, c.fixture, err)
		}
		if err := checkReleasePins(flake, inputs); err != nil {
			return 0, err
		}
		prepared, err := backend.PrepareHome(flake, true)
		if err != nil {
			return 0, fmt.Errorf("typed entry %s fixture %s: build the activation: %w", c.entry, c.fixture, backendFailure(err))
		}
		if !slices.Contains(prepared.DefinedOutside, c.program) {
			return 0, fmt.Errorf("typed entry %s fixture %s: the projection does not report %s as defined outside Home Manager's modules", c.entry, c.fixture, c.program)
		}
		resolution := prepared.Resolution
		if resolution == nil || len(resolution.Outputs) != 1 {
			return 0, fmt.Errorf("typed entry %s fixture %s: the activation has no single output", c.entry, c.fixture)
		}
		built, err := nix.ReadHomeFile(resolution.OutputPaths[resolution.Outputs[0]], nix.StoreRoot, c.leaf, settingscodec.MaxContentSize)
		if err != nil {
			return 0, fmt.Errorf("typed entry %s fixture %s: %w", c.entry, c.fixture, err)
		}
		if err := c.checkLeaf(built); err != nil {
			return 0, err
		}
	}
	return len(cases), nil
}

// checkProjectionControl evaluates a configuration without the typed module. Its
// projection must exist and must not list the program.
func checkProjectionControl(backend *nix.Backend, work, program string) error {
	flake, err := nix.GenerateHomeFlakeWithPlacements(work, nil, work, nil, "", nil, nil)
	if err != nil {
		return err
	}
	prepared, err := backend.PrepareHome(flake, false)
	if err != nil {
		return fmt.Errorf("evaluate the configuration without typed settings: %w", backendFailure(err))
	}
	if prepared.DefinedOutside == nil {
		return fmt.Errorf("the projection of a configuration without typed settings is unavailable")
	}
	if slices.Contains(prepared.DefinedOutside, program) {
		return fmt.Errorf("the projection reports %s as defined outside Home Manager's modules without any typed settings", program)
	}
	return nil
}

// backendFailure appends the Nix output that a realizer error keeps in Raw, so the
// release job log shows why Nix refused.
func backendFailure(err error) error {
	var failure *realizer.Error
	if errors.As(err, &failure) && failure.Raw != "" {
		return fmt.Errorf("%w: %s", err, failure.Raw)
	}
	return err
}

// checkReleasePins fails when an environment override changed the pins that the
// generator wrote, because the proof must hold for the pair that ships.
func checkReleasePins(flake string, inputs releaseinputs.Inputs) error {
	directory, _, _ := strings.Cut(flake, "#")
	data, err := os.ReadFile(filepath.Join(directory, "flake.nix"))
	if err != nil {
		return err
	}
	for _, ref := range []string{inputs.Nixpkgs.FlakeRef, inputs.HomeManager.FlakeRef} {
		if !strings.Contains(string(data), ref) {
			return fmt.Errorf("the generated flake does not use the release input %s", ref)
		}
	}
	return nil
}

// boundedNixRunner runs nix with the release tooling's output bound and a
// deadline per call. The experimental-features option is what the runtime
// backend passes for the same verbs.
func boundedNixRunner(ctx context.Context, timeout time.Duration, run commandRunner) nix.Runner {
	return func(args ...string) ([]byte, []byte, int, error) {
		callCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		full := append([]string{"--extra-experimental-features", "nix-command flakes"}, args...)
		stdout, stderr, err := run(callCtx, "nix", full...)
		var exit *exec.ExitError
		switch {
		case err == nil:
			return stdout, stderr, 0, nil
		case errors.As(err, &exit):
			return stdout, stderr, exit.ExitCode(), nil
		default:
			return stdout, stderr, -1, err
		}
	}
}
