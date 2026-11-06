// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/discovery"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/realizer/nix"
)

type nativeHomeOwnershipRealizer struct {
	*fakeRealizer
	native *nix.Backend
}

func (r *nativeHomeOwnershipRealizer) ObserveHome() (realizer.HomeObservation, error) {
	return r.native.ObserveHome()
}

func TestGeneratedHomeOwnershipUnknownStopsBeforePackageMutation(t *testing.T) {
	for _, shape := range []string{"missing", "regular file", "malformed link", "native inspection failure"} {
		t.Run(shape, func(t *testing.T) {
			t.Setenv("ENDSTATE_ROOT", t.TempDir())
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			profile := filepath.Join(os.Getenv("XDG_STATE_HOME"), "nix", "profiles", "home-manager")
			if err := os.MkdirAll(filepath.Dir(profile), 0700); err != nil {
				t.Fatal(err)
			}
			switch shape {
			case "regular file":
				if err := os.WriteFile(profile, []byte("unknown profile"), 0600); err != nil {
					t.Fatal(err)
				}
			case "malformed link", "native inspection failure":
				target := "unknown-profile"
				if shape == "native inspection failure" {
					target = "home-manager-9-link"
				}
				if err := os.Symlink(target, profile); err != nil {
					t.Fatal(err)
				}
			}
			backend := &nativeHomeOwnershipRealizer{fakeRealizer: &fakeRealizer{homeGenNum: 1}, native: &nix.Backend{Run: func(args ...string) ([]byte, []byte, int, error) {
				if len(args) != 2 || args[0] != "path-info" || args[1] != profile+"-9-link" {
					t.Fatalf("inspection did not select the native generation: %q", args)
				}
				return nil, []byte("inspection unavailable"), 1, nil
			}}}
			app := nixApp("package", "nixpkgs#package")
			backend.planDiff.ToAdd = []realizer.Installable{{ID: app.ID, Ref: hostRef(app)}}
			manifestPath, cfgRel := writeHomeNix(t)
			mf := nixManifest(app)
			mf.HomeManager = &manifest.HomeManagerConfig{Config: cfgRel}
			_, eerr := runApplyRealizer(ApplyFlags{Manifest: manifestPath, EnableRestore: true}, mf, backend, noopEmitter(), "home-ownership", nil, nil, nil, nil, nil)
			if shape == "missing" {
				if eerr != nil || backend.activateCalls != 1 || backend.realizeCalls != 1 {
					t.Fatalf("proven absence did not permit apply: %d, %d, %+v", backend.activateCalls, backend.realizeCalls, eerr)
				}
				return
			}
			if eerr == nil || eerr.Code != realizer.ErrInputProvenanceUnresolved || backend.activateCalls != 0 || backend.realizeCalls != 0 {
				t.Fatalf("unknown ownership mutated the target: %d, %d, %+v", backend.activateCalls, backend.realizeCalls, eerr)
			}
			if detail, ok := eerr.Detail.(map[string]string); !ok || detail["subcode"] != "home-ownership" {
				t.Fatalf("ownership failure lost its classification: %+v", eerr)
			}
		})
	}
}

type changingHomeOwnershipRealizer struct {
	*fakeRealizer
	after    realizer.HomeObservation
	afterErr error
}

func (r *changingHomeOwnershipRealizer) ObserveHome() (realizer.HomeObservation, error) {
	if r.realizeCalls > 0 {
		return r.after, r.afterErr
	}
	return realizer.HomeObservation{}, nil
}

func TestGeneratedHomeOwnershipRefreshRetainsCommittedPackages(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		name := "external"
		if unknown {
			name = "unknown"
		}
		t.Run(name, func(t *testing.T) {
			t.Setenv("ENDSTATE_ROOT", t.TempDir())
			app := nixApp("package", "nixpkgs#package")
			backend := &changingHomeOwnershipRealizer{fakeRealizer: &fakeRealizer{
				planDiff:      realizer.Diff{ToAdd: []realizer.Installable{{ID: app.ID, Ref: hostRef(app)}}},
				currentSet:    nixSet("package"),
				realizeResult: realizer.Result{Advanced: true, ToGeneration: 2},
			}, after: realizer.HomeObservation{Present: true, Generation: 9, ActivationOutput: "/nix/store/external-home"}}
			if unknown {
				backend.afterErr = errors.New("profile became unreadable")
			}
			manifestPath, cfgRel := writeHomeNix(t)
			mf := nixManifest(app)
			mf.HomeManager = &manifest.HomeManagerConfig{Config: cfgRel}
			raw, eerr := runApplyRealizer(ApplyFlags{Manifest: manifestPath, EnableRestore: true}, mf, backend, noopEmitter(), "late-home-ownership", nil, nil, nil, nil, nil)
			if backend.activateCalls != 0 || backend.realizeCalls != 1 || (eerr != nil) != unknown {
				t.Fatalf("late ownership change was ignored: %+v, activation=%d", eerr, backend.activateCalls)
			}
			if !unknown {
				result := raw.(*ApplyResult)
				if result.HomeManager == nil || result.HomeManager.OwnershipAction == nil || result.HomeManager.OwnershipAction.ActiveGeneration != 9 {
					t.Fatalf("late external ownership lost the import action: %+v", result.HomeManager)
				}
			} else if eerr.Code != realizer.ErrInputProvenanceUnresolved {
				t.Fatalf("late unknown ownership lost its error: %+v", eerr)
			}
			history, err := provision.List()
			if err != nil || len(history) != 1 || history[0].Native != "2" || history[0].HomeManager != nil || history[0].Partial != unknown || len(history[0].AddedRefs) != 1 {
				t.Fatalf("late ownership change lost committed package provenance: %+v, %v", history, err)
			}
		})
	}
}

func TestHomeOwnershipUsesExactOutputWithoutLegacyDowngrade(t *testing.T) {
	for _, tc := range []struct {
		name     string
		history  []*provision.Generation
		external bool
	}{
		{name: "exact output", history: []*provision.Generation{{HomeManager: &provision.HomeGenRef{Generation: 3, ActivationOutput: "/nix/store/active-home"}}}},
		{name: "same generation different output", history: []*provision.Generation{{HomeManager: &provision.HomeGenRef{Generation: 9, ActivationOutput: "/nix/store/other-home"}}}, external: true},
		{name: "wholly legacy", history: []*provision.Generation{{HomeManager: &provision.HomeGenRef{Generation: 9}}}},
		{name: "mixed history", history: []*provision.Generation{{HomeManager: &provision.HomeGenRef{Generation: 9}}, {HomeManager: &provision.HomeGenRef{Generation: 3, ActivationOutput: "/nix/store/other-home"}}}, external: true},
		{name: "failed activation still owns output", history: []*provision.Generation{{HomeManager: &provision.HomeGenRef{Generation: 9, ActivationOutput: "/nix/store/active-home", ActivationFailed: true}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &fakeRealizer{homeObservation: realizer.HomeObservation{Present: true, Generation: 9, ActivationOutput: "/nix/store/active-home"}}
			withFakeGenerations(tc.history, nil, func() {
				action, err := externalHomeManagerAction(backend, "path:/generated#profile", true)
				if err != nil || (action != nil) != tc.external {
					t.Fatalf("ownership did not follow native output: %+v, %v", action, err)
				}
			})
		})
	}
}

func TestHomeOwnershipUnreadableHistoryIsUnknown(t *testing.T) {
	backend := &fakeRealizer{homeObservation: realizer.HomeObservation{Present: true, Generation: 9, ActivationOutput: "/nix/store/active-home"}}
	withFakeGenerations(nil, errors.New("history unavailable"), func() {
		if action, err := externalHomeManagerAction(backend, "path:/generated#profile", true); action != nil || err == nil {
			t.Fatalf("unreadable history claimed an external owner: %+v, %v", action, err)
		}
		if item := checkHomeManagerGeneration(backend, noopEmitter()); item.Reason != "unreadable" {
			t.Fatalf("unreadable history became drift: %+v", item)
		}
	})
}

func TestHomeOwnershipUnknownKeepsIndependentDiscoveryCapture(t *testing.T) {
	backend := &fakeRealizer{homeObserveErr: errors.New("profile is unreadable")}
	if item := checkHomeManagerGeneration(backend, noopEmitter()); item.Reason != "unreadable" {
		t.Fatalf("unknown native state became missing: %+v", item)
	}
	oldDiscovery := discoverLinuxFn
	discoverLinuxFn = func(context.Context, discovery.Request, realizer.Realizer) (discovery.Result, error) {
		return discovery.Result{SchemaVersion: discovery.SchemaVersion, Resolved: []discovery.ResolvedIntent{{ID: "ripgrep", Installable: "github:example/packages#ripgrep"}}}, nil
	}
	t.Cleanup(func() { discoverLinuxFn = oldDiscovery })
	withFakeGenerations([]*provision.Generation{{HomeManager: &provision.HomeGenRef{Generation: 9, Settings: &manifest.HomeManagerSettings{Git: &manifest.GitSettings{DefaultBranch: "main"}}}}}, nil, func() {
		raw, eerr := runCaptureLinux(CaptureFlags{Out: filepath.Join(t.TempDir(), "capture.jsonc")}, backend, noopEmitter())
		if eerr != nil {
			t.Fatal(eerr)
		}
		result := raw.(*CaptureResult)
		if len(result.AppsIncluded) != 1 || len(result.Warnings) != 1 || result.Warnings[0].Code != "home_manager_provenance_unavailable" {
			t.Fatalf("unknown home provenance lost independent capture: %+v", result)
		}
		if mf := readCapturedManifest(t, result.OutputPath); mf.HomeManager != nil || len(mf.Apps) != 1 {
			t.Fatalf("capture manufactured active settings provenance: %+v", mf)
		}
	})
}

type unobservedHomeRealizer struct {
	minimalRealizer
	activations int
}

func (r *unobservedHomeRealizer) ActivateHome(string) (int, error) {
	r.activations++
	return 1, nil
}

func TestGeneratedHomeOwnershipRequiresObservationCapability(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	manifestPath, cfgRel := writeHomeNix(t)
	mf := nixManifest()
	mf.HomeManager = &manifest.HomeManagerConfig{Config: cfgRel}
	backend := &unobservedHomeRealizer{}
	_, eerr := runApplyRealizer(ApplyFlags{Manifest: manifestPath, EnableRestore: true}, mf, backend, noopEmitter(), "unobserved-home", nil, nil, nil, nil, nil)
	if eerr == nil || eerr.Code != realizer.ErrInputProvenanceUnresolved || backend.activations != 0 {
		t.Fatalf("missing observation capability authorized generated activation: %+v, %d", eerr, backend.activations)
	}
}

func TestHomeOwnershipUncertainActivationRetainsPackageCommit(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		name := "apply"
		if rollback {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			f := newNativeFreezeFixture(t)
			activate := f.backend.Script
			f.backend.Script = func(path string, args ...string) ([]byte, []byte, int, error) {
				out, stderr, exit, err := activate(path, args...)
				profile := filepath.Join(os.Getenv("XDG_STATE_HOME"), "nix", "profiles", "home-manager")
				if err := os.Remove(profile); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("unknown-profile", profile); err != nil {
					t.Fatal(err)
				}
				return out, stderr, exit, err
			}
			var eerr *envelope.Error
			expectedNative, expectedRecords := "1", 1
			if rollback {
				resolution := &realizer.InputResolution{Outputs: []string{"out"}, OutputPaths: map[string]string{"out": "/nix/store/home-output"}}
				if err := provision.Write(&provision.Generation{Backend: "nix", Native: "1", HomeManager: &provision.HomeGenRef{Kind: "settings", Generated: true, Generation: 3, ActivationOutput: "/nix/store/home-output", Resolution: resolution}}); err != nil {
					t.Fatal(err)
				}
				native := f.backend.Run
				f.backend.Run = func(args ...string) ([]byte, []byte, int, error) {
					if len(args) > 1 && args[0] == "profile" && args[1] == "rollback" {
						if err := os.Symlink("profile-2-link", f.backend.Profile); err != nil {
							t.Fatal(err)
						}
						return nil, nil, 0, nil
					}
					return native(args...)
				}
				_, eerr = runRealizerRollback(RollbackFlags{To: "1", Confirm: true, EnableRestore: true}, f.backend)
				expectedNative, expectedRecords = "2", 2
			} else {
				mf, flags := freezeManifest(t)
				_, eerr = runApplyRealizer(flags, mf, f.backend, noopEmitter(), "uncertain-home", nil, nil, nil, nil, nil)
			}
			if eerr == nil {
				t.Fatal("unreadable committed Home Manager identity was reported as successful")
			}
			if detail, ok := eerr.Detail.(map[string]string); ok {
				if detail["stage"] != "commit" {
					t.Fatalf("uncertain outcome lost its commit stage: %+v", eerr)
				}
			} else if detail, ok := eerr.Detail.(map[string]interface{}); !ok || detail["committed"] != true {
				t.Fatalf("uncertain outcome lost its commit evidence: %+v", eerr)
			}
			history, err := provision.List()
			if err != nil || len(history) != expectedRecords || history[0].Native != expectedNative || !history[0].Partial || history[0].Rollback != rollback {
				t.Fatalf("uncertain Home Manager identity erased committed packages or fabricated provenance: %+v, %v", history, err)
			}
			if rollback {
				// Retain the failed historical target, without claiming a new native generation.
				if home := history[0].HomeManager; home == nil || home.Generation != 3 || home.ActivationOutput != "/nix/store/home-output" || !home.ActivationFailed {
					t.Fatalf("the uncertain rollback lost its historical failure evidence: %+v", home)
				}
			} else if history[0].HomeManager != nil {
				t.Fatalf("the first uncertain apply invented a native home identity: %+v", history[0].HomeManager)
			}
		})
	}
}

func TestHomeOwnershipFailedRetryWithUnreadableResultInvalidatesCompletion(t *testing.T) {
	for _, rollback := range []bool{false, true} {
		name := "apply"
		if rollback {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			f := newNativeFreezeFixture(t)
			mf, flags := freezeManifest(t)
			mf.Apps = nil
			flags.Prune = false
			if _, eerr := runApplyRealizer(flags, mf, f.backend, noopEmitter(), "initial-home", nil, nil, nil, nil, nil); eerr != nil {
				t.Fatal(eerr)
			}
			f.backend.Script = func(string, ...string) ([]byte, []byte, int, error) {
				f.activeHome = "unavailable"
				return nil, []byte("settings step failed"), 1, nil
			}
			native := f.backend.Run
			f.backend.Run = func(args ...string) ([]byte, []byte, int, error) {
				if args[0] == "path-info" && f.activeHome == "unavailable" {
					return nil, nil, -1, errors.New("native observation unavailable")
				}
				return native(args...)
			}
			var eerr *envelope.Error
			if rollback {
				_, eerr = runRealizerRollback(RollbackFlags{To: "1", Confirm: true, EnableRestore: true}, f.backend)
			} else {
				_, eerr = runApplyRealizer(flags, mf, f.backend, noopEmitter(), "failed-home-retry", nil, nil, nil, nil, nil)
			}
			history, err := provision.List()
			if eerr == nil || err != nil || len(history) != 2 || !history[0].Partial || history[0].HomeManager == nil || !history[0].HomeManager.ActivationFailed {
				t.Fatalf("the failed config-only attempt left an older success active: %+v, %v, %+v", history, err, eerr)
			}
			if history[0].HomeManager.Generation != history[1].HomeManager.Generation || history[0].HomeManager.ActivationOutput != history[1].HomeManager.ActivationOutput || history[0].Native != "" || f.additions != 0 || history[0].Rollback != rollback {
				t.Fatalf("the failed attempt invented a native commit: %+v", history[0])
			}
			f.activeHome = "/nix/store/home-output"
			if item := checkHomeManagerGeneration(f.backend, noopEmitter()); item.Status != "fail" {
				t.Fatalf("recovered native reads revived an older completed activation: %+v", item)
			}
			if cfg, warnings := recoverHomeManagerWithWarnings(CaptureFlags{}, f.backend); cfg != nil || len(warnings) != 1 {
				t.Fatalf("capture revived the failed settings declaration: %+v, %v", cfg, warnings)
			}
			if action, err := externalHomeManagerAction(f.backend, "path:/generated#profile", true); action != nil || err != nil {
				t.Fatalf("failed completion erased established native ownership: %+v, %v", action, err)
			}
		})
	}
}

func TestHomeOwnershipLegacyRollbackRetainsCommitWhenEnrichmentFails(t *testing.T) {
	f := newNativeFreezeFixture(t)
	profile := filepath.Join(os.Getenv("XDG_STATE_HOME"), "nix", "profiles", "home-manager")
	if err := os.MkdirAll(filepath.Dir(profile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(os.Getenv("ENDSTATE_ROOT"), "snapshot"), profile+"-3-link"); err != nil {
		t.Fatal(err)
	}
	f.backend.Script = func(string, ...string) ([]byte, []byte, int, error) {
		f.activeHome = "/nix/store/home-output"
		if err := os.Symlink("home-manager-4-link", profile); err != nil {
			t.Fatal(err)
		}
		return nil, nil, 0, nil
	}
	native := f.backend.Run
	unavailable := false
	f.backend.Run = func(args ...string) ([]byte, []byte, int, error) {
		if args[0] == "path-info" {
			if args[1] == profile+"-3-link" {
				return []byte("/nix/store/home-output\n"), nil, 0, nil
			}
			if unavailable {
				return nil, nil, -1, errors.New("native observation unavailable after known activation")
			}
			unavailable = true
		}
		return native(args...)
	}
	if err := provision.Write(&provision.Generation{Backend: "nix", HomeManager: &provision.HomeGenRef{Generation: 3, ActivationOutput: "/nix/store/home-output", Flake: "github:example/config#profile", FileExpectations: &provision.HomeFileExpectations{Files: []provision.HomeFileExpectation{}}}}); err != nil {
		t.Fatal(err)
	}
	_, eerr := runRealizerRollback(RollbackFlags{To: "1", Confirm: true, EnableRestore: true}, f.backend)
	history, err := provision.List()
	if eerr == nil || err != nil || len(history) != 2 || !history[0].Partial || !history[0].Rollback || history[0].HomeManager == nil {
		t.Fatalf("the enrichment read erased the proved config-only rollback: %+v, %v, %+v", history, err, eerr)
	}
	if home := history[0].HomeManager; home.Generation != 4 || home.ActivationOutput != "" || !home.ActivationFailed {
		t.Fatalf("the uncertain rollback lost its proved generation or copied an unobserved output: %+v", home)
	}
	unavailable = false
	if cfg, warnings := recoverHomeManagerWithWarnings(CaptureFlags{}, f.backend); cfg != nil || len(warnings) != 1 {
		t.Fatalf("capture skipped the failed rollback in favor of older output provenance: %+v, %v", cfg, warnings)
	}
}

func TestHomeOwnershipSpawnFailureReleasesUncommittedPreparation(t *testing.T) {
	f := newNativeFreezeFixture(t)
	profile := filepath.Join(os.Getenv("XDG_STATE_HOME"), "nix", "profiles", "home-manager")
	if err := os.MkdirAll(filepath.Dir(profile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("home-manager-7-link", profile); err != nil {
		t.Fatal(err)
	}
	f.activeHome = "/nix/store/home-output"
	spawnFailed := false
	f.backend.Script = func(string, ...string) ([]byte, []byte, int, error) {
		spawnFailed = true
		return nil, nil, -1, &os.PathError{Op: "fork/exec", Path: "/nix/store/home-output/activate", Err: os.ErrNotExist}
	}
	native := f.backend.Run
	f.backend.Run = func(args ...string) ([]byte, []byte, int, error) {
		if spawnFailed && args[0] == "path-info" {
			return nil, nil, -1, errors.New("native observation unavailable")
		}
		return native(args...)
	}
	mf := &manifest.Manifest{HomeManager: &manifest.HomeManagerConfig{Flake: "github:example/config#profile"}}
	_, eerr := runApplyRealizer(ApplyFlags{EnableRestore: true}, mf, f.backend, noopEmitter(), "unstarted-home", nil, nil, nil, nil, nil)
	if eerr == nil || eerr.Code != envelope.ErrRealizerUnavailable {
		t.Fatalf("the activation spawn failure disappeared: %+v", eerr)
	}
	if detail, ok := eerr.Detail.(map[string]string); !ok || detail["stage"] != "spawn" {
		t.Fatalf("the unstarted activation was reported as committed: %+v", eerr)
	}
	history, err := provision.List()
	roots, globErr := filepath.Glob(filepath.Join(filepath.Dir(f.backend.Profile), "activation-root-*"))
	if err != nil || len(history) != 0 || globErr != nil || len(roots) != 0 {
		t.Fatalf("the unstarted config-only activation kept commit evidence or roots: %+v, %v, %v, %v", history, roots, err, globErr)
	}
}
