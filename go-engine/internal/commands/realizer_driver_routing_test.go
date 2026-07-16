// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bootstrap"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

func unsupportedHostDriverApp(driverName string) manifest.App {
	return manifest.App{
		ID:          driverName + "-app",
		DisplayName: driverName + " app",
		Driver:      driverName,
		Refs:        map[string]string{runtime.GOOS: driverName + ".package"},
	}
}

func TestPartitionRealizerLanes_ExplicitPackageDriversNeverReachNix(t *testing.T) {
	nixApp := nixApp("ripgrep", "nixpkgs#ripgrep")
	wingetApp := unsupportedHostDriverApp("winget")
	chocoApp := unsupportedHostDriverApp("chocolatey")

	brew, unsupported, realizerApps := partitionRealizerLanes([]manifest.App{nixApp, wingetApp, chocoApp})

	if len(brew) != 0 {
		t.Fatalf("brew apps = %+v, want none", brew)
	}
	if len(realizerApps) != 1 || realizerApps[0].ID != nixApp.ID {
		t.Fatalf("realizer apps = %+v, want only %q", realizerApps, nixApp.ID)
	}
	if len(unsupported) != 2 || unsupported[0].Driver != "winget" || unsupported[1].Driver != "chocolatey" {
		t.Fatalf("unsupported apps = %+v, want winget then chocolatey", unsupported)
	}
}

func TestRunApplyRealizer_UnsupportedDriverIsVisibleSkip(t *testing.T) {
	fr := &fakeRealizer{}
	unsupported := []manifest.App{unsupportedHostDriverApp("chocolatey")}

	raw, envelopeErr := runApplyRealizer(
		ApplyFlags{Manifest: "test", DryRun: true},
		nixManifest(), fr, noopEmitter(), "run", nil, nil, nil, nil, unsupported,
	)
	if envelopeErr != nil {
		t.Fatalf("runApplyRealizer returned error: %v", envelopeErr)
	}
	if len(fr.lastPlanArgs) != 0 {
		t.Fatalf("Nix plan received unsupported packages: %+v", fr.lastPlanArgs)
	}

	result := raw.(*ApplyResult)
	if result.Summary.Total != 1 || result.Summary.Skipped != 1 {
		t.Fatalf("summary = %+v, want total=1 skipped=1", result.Summary)
	}
	if len(result.Actions) != 1 || result.Actions[0].Driver != "chocolatey" || result.Actions[0].Status != "skipped" {
		t.Fatalf("actions = %+v, want visible chocolatey skip", result.Actions)
	}
}

func TestRunPlanRealizer_UnsupportedDriverIsVisibleSkip(t *testing.T) {
	fr := &fakeRealizer{}
	unsupported := []manifest.App{unsupportedHostDriverApp("winget")}

	raw, envelopeErr := runPlanRealizer(
		PlanFlags{Manifest: "test"}, nixManifest(), fr, noopEmitter(), nil, nil, unsupported,
	)
	if envelopeErr != nil {
		t.Fatalf("runPlanRealizer returned error: %v", envelopeErr)
	}
	if len(fr.lastPlanArgs) != 0 {
		t.Fatalf("Nix plan received unsupported packages: %+v", fr.lastPlanArgs)
	}

	result := raw.(*PlanResult)
	if result.Plan.Total != 1 || result.Plan.Skipped != 1 {
		t.Fatalf("summary = %+v, want total=1 skipped=1", result.Plan)
	}
	if len(result.Actions) != 1 || result.Actions[0].Driver != "winget" || result.Actions[0].CurrentStatus != "skipped" {
		t.Fatalf("actions = %+v, want visible winget skip", result.Actions)
	}
}

func TestRunVerifyRealizer_UnsupportedDriverIsVisibleSkip(t *testing.T) {
	fr := &fakeRealizer{}
	unsupported := []manifest.App{unsupportedHostDriverApp("chocolatey")}

	raw, envelopeErr := runVerifyRealizer(
		VerifyFlags{Manifest: "test"}, nixManifest(), fr, noopEmitter(), nil, nil, unsupported,
	)
	if envelopeErr != nil {
		t.Fatalf("runVerifyRealizer returned error: %v", envelopeErr)
	}

	result := raw.(*VerifyResult)
	if result.Summary.Total != 1 || result.Summary.Skipped != 1 || result.Summary.Fail != 0 {
		t.Fatalf("summary = %+v, want total=1 skipped=1 fail=0", result.Summary)
	}
	if len(result.Results) != 1 || result.Results[0].Driver != "chocolatey" || result.Results[0].Status != "skipped" {
		t.Fatalf("results = %+v, want visible chocolatey skip", result.Results)
	}
}

func TestRunApply_RealizerConsentFollowsOpeningPlanPhase(t *testing.T) {
	path := writeLaneManifest(t, fmt.Sprintf(`{"id":"ripgrep","refs":{"%s":"nixpkgs#ripgrep"}}`, runtime.GOOS))
	fr := &fakeRealizer{}
	origBootstrap := bootstrapBackendsFn
	origEmitter := newApplyEmitterFn
	var stream bytes.Buffer
	bootstrapBackendsFn = func(_ []bootstrap.Backend, mutating bool, _ Consent, emitter *events.Emitter) (map[bootstrap.Backend]bool, *envelope.Error) {
		if !mutating {
			t.Fatal("ordering regression must exercise a live preflight")
		}
		emitter.EmitConsent([]string{"nix"}, "consent", []string{"installer"})
		return map[bootstrap.Backend]bool{bootstrap.BackendNix: false}, nil
	}
	newApplyEmitterFn = func(runID string, enabled bool) *events.Emitter {
		return events.NewEmitterWithWriter(runID, enabled, &stream)
	}
	defer func() {
		bootstrapBackendsFn = origBootstrap
		newApplyEmitterFn = origEmitter
	}()

	withFakeRealizer(fr, func() {
		if _, err := RunApply(ApplyFlags{Manifest: path, Events: "jsonl"}); err != nil {
			t.Fatalf("RunApply error = %v", err)
		}
	})

	lines := strings.Split(strings.TrimSpace(stream.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("event stream = %q, want phase then consent", stream.String())
	}
	var first, second map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatal(err)
	}
	if first["event"] != "phase" || first["phase"] != "plan" || second["event"] != "consent" {
		t.Fatalf("first events = %+v then %+v, want plan phase then consent", first, second)
	}
	planPhases := 0
	for _, line := range lines {
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event["event"] == "phase" && event["phase"] == "plan" {
			planPhases++
		}
	}
	if planPhases != 1 {
		t.Fatalf("plan phase count = %d, want 1; stream=%s", planPhases, stream.String())
	}
}

func TestRunApply_RealizerDryRunNeverMutatesBackendBootstrap(t *testing.T) {
	for _, tc := range []struct {
		name      string
		authorize bool
	}{
		{name: "unanswered consent"},
		{name: "explicit bootstrap authorization", authorize: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeLaneManifest(t, fmt.Sprintf(`{"id":"ripgrep","refs":{"%s":"nixpkgs#ripgrep"}}`, runtime.GOOS))
			fr := &fakeRealizer{}
			installCalls, verifyCalls := 0, 0
			bs := &bootstrap.Bootstrapper{
				Detect: func(bootstrap.Backend) (bool, error) { return false, nil },
				Install: func(bootstrap.Backend) error {
					installCalls++
					return nil
				},
				Verify: func(bootstrap.Backend) (bool, error) {
					verifyCalls++
					return true, nil
				},
			}

			origBootstrap := bootstrapBackendsFn
			origBootstrapper := newBootstrapperFn
			origGOOS := bootstrapGOOSFn
			origEmitter := newApplyEmitterFn
			var stream bytes.Buffer
			bootstrapBackendsFn = realEnsureBackends
			newBootstrapperFn = func() *bootstrap.Bootstrapper { return bs }
			bootstrapGOOSFn = func() string { return "linux" }
			newApplyEmitterFn = func(runID string, enabled bool) *events.Emitter {
				return events.NewEmitterWithWriter(runID, enabled, &stream)
			}
			defer func() {
				bootstrapBackendsFn = origBootstrap
				newBootstrapperFn = origBootstrapper
				bootstrapGOOSFn = origGOOS
				newApplyEmitterFn = origEmitter
			}()

			withFakeRealizer(fr, func() {
				if _, err := RunApply(ApplyFlags{
					Manifest:          path,
					DryRun:            true,
					BootstrapBackends: tc.authorize,
					Events:            "jsonl",
				}); err != nil {
					t.Fatalf("RunApply error = %v", err)
				}
			})

			if installCalls != 0 || verifyCalls != 0 {
				t.Fatalf("dry-run bootstrap calls: install=%d verify=%d, want zero", installCalls, verifyCalls)
			}
			for _, line := range strings.Split(strings.TrimSpace(stream.String()), "\n") {
				var event map[string]interface{}
				if err := json.Unmarshal([]byte(line), &event); err != nil {
					t.Fatalf("invalid JSONL event %q: %v", line, err)
				}
				if event["event"] == "consent" {
					t.Fatalf("dry-run emitted consent: %s", stream.String())
				}
			}
		})
	}
}
