// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/discovery"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/releaseinputs"
)

// discoverLinuxFn is the read-only ordinary-machine inventory boundary. Tests
// replace it with hermetic native-package/config evidence.
var discoverLinuxFn = func(ctx context.Context, request discovery.Request, r realizer.Realizer) (discovery.Result, error) {
	var extra []discovery.Adapter
	if r != nil {
		inputs, err := releaseinputs.Load()
		if err != nil {
			return discovery.Result{}, err
		}
		extra = append(extra, discovery.RealizerInventoryAdapter{Realizer: r, DefaultInput: inputs.Nixpkgs.FlakeRef})
	}
	return discovery.DiscoverWithAdapters(ctx, request, extra...)
}

// discoveryCaptureRealizer adapts already-resolved portable discovery intent to
// the established package-scoped capture finalizer. It never invokes Nix: the
// immutable installables were established by the reviewed package catalog.
type discoveryCaptureRealizer struct {
	current realizer.Set
}

func (d *discoveryCaptureRealizer) Name() string { return "nix" }
func (d *discoveryCaptureRealizer) Current() (realizer.Set, error) {
	return d.current, nil
}
func (d *discoveryCaptureRealizer) Plan([]realizer.Installable) (realizer.Diff, error) {
	return realizer.Diff{}, fmt.Errorf("discovery capture realizer cannot plan")
}
func (d *discoveryCaptureRealizer) Realize([]realizer.Installable) (realizer.Result, error) {
	return realizer.Result{}, fmt.Errorf("discovery capture realizer cannot realize")
}

// runCaptureLinux makes ordinary-machine discovery the Linux entry point. The
// existing Endstate profile remains the compatibility path until its evidence
// is reconciled into the same discovery result.
func runCaptureLinux(flags CaptureFlags, r realizer.Realizer, emitter *events.Emitter) (interface{}, *envelope.Error) {
	emitter.EmitPhase("capture")
	result, err := discoverLinuxFn(context.Background(), discovery.Request{
		Only:            parseOnlyIDs(flags.Only),
		IncludeRuntimes: flags.IncludeRuntimes,
	}, r)
	if err != nil {
		if r != nil {
			fallback := discovery.Result{
				SchemaVersion: discovery.SchemaVersion,
				Sources:       []discovery.SourceStatus{}, Resolved: []discovery.ResolvedIntent{},
				Settings: []discovery.SettingsCandidate{}, Unresolved: []discovery.UnresolvedItem{},
				Warnings: []discovery.Warning{{
					Code:    "ordinary_discovery_failed",
					Message: "Ordinary Linux application sources could not be read; the Endstate-managed profile was captured on its own.",
				}},
			}
			raw, captureErr := runCaptureRealizerSelected(flags, r, emitter, realizerCaptureSelection{nix: true})
			if captureErr == nil {
				raw.(*CaptureResult).Discovery = &fallback
			}
			return raw, captureErr
		}
		return nil, envelope.NewError(
			envelope.ErrCaptureFailed,
			fmt.Sprintf("Linux application discovery could not read any trustworthy source: %v", err),
		)
	}
	if result.SchemaVersion == "" {
		result.SchemaVersion = discovery.SchemaVersion
	}
	flags.linuxDiscoveryCounts = &result.Counts
	flags.linuxSettingsApps = make(map[string]bool)
	flags.linuxSettingsModules = make(map[string]bool)
	for _, candidate := range result.Settings {
		if candidate.PackageID != "" {
			flags.linuxSettingsApps[candidate.PackageID] = true
		}
		if candidate.ModuleID != "" {
			flags.linuxSettingsModules[candidate.ModuleID] = true
		}
		if strings.HasPrefix(candidate.ID, "apps.") {
			flags.linuxSettingsModules[candidate.ID] = true
		} else if candidate.ID != "" {
			flags.linuxSettingsApps[candidate.ID] = true
		}
	}
	if !flags.Sanitize {
		flags.linuxHomeManagerFiles = make([]bundle.HomeManagerFileCapturePlan, 0, len(result.SettingsFiles))
		for _, plan := range result.SettingsFiles {
			flags.linuxHomeManagerFiles = append(flags.linuxHomeManagerFiles, bundle.HomeManagerFileCapturePlan{
				CandidateID: plan.CandidateID, Target: plan.Target, Source: plan.Source,
				Optional: plan.Optional, ObservedSize: plan.ObservedSize, ObservedSHA256: plan.ObservedSHA256,
			})
		}
	}

	// Preserve the current Nix-profile behavior byte-for-byte when ordinary
	// discovery has not yet contributed a resolved intent.
	if len(result.Resolved) == 0 && r != nil {
		raw, captureErr := runCaptureRealizerSelected(flags, r, emitter, realizerCaptureSelection{nix: true})
		if captureErr == nil {
			raw.(*CaptureResult).Discovery = &result
		}
		return raw, captureErr
	}

	elements := make(map[string]realizer.Element, len(result.Resolved))
	for _, intent := range result.Resolved {
		elements[intent.ID] = realizer.Element{Name: intent.Installable}
	}
	raw, captureErr := runCaptureRealizerSelected(
		flags,
		&discoveryCaptureRealizer{current: realizer.Set{Elements: elements}},
		emitter,
		realizerCaptureSelection{nix: true},
	)
	if captureErr != nil {
		return nil, captureErr
	}
	raw.(*CaptureResult).Discovery = &result
	return raw, nil
}
