// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/desktopsettings"
	"github.com/Artexis10/endstate/go-engine/internal/desktopsettings/dconf"
	"github.com/Artexis10/endstate/go-engine/internal/desktopsettings/dconf/dconftest"
	"github.com/Artexis10/endstate/go-engine/internal/discovery"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

const (
	gnomeModuleID = "desktop.gnome-clock-show-date"
	gnomeClockKey = "/org/gnome/desktop/interface/clock-show-date"
)

// gnomeRepo installs the reviewed GNOME allowlist fixture into a fresh repository
// root, and points the capture seams at it. The shipped catalogue stays untouched.
func gnomeRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	fixture, err := os.ReadFile(filepath.Join("testdata", "gnome-allowlist", "module.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "modules", "desktop", "gnome-clock-show-date")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "module.jsonc"), fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	oldRoot, oldGOOS, oldRealizer, oldDiscovery, oldLoader, oldBackend := resolveRepoRootFn, captureGOOSFn, newRealizerFn, discoverLinuxFn, loadCaptureModuleCatalogFn, desktopSettingsBackend
	t.Cleanup(func() {
		resolveRepoRootFn, captureGOOSFn, newRealizerFn, discoverLinuxFn, loadCaptureModuleCatalogFn, desktopSettingsBackend = oldRoot, oldGOOS, oldRealizer, oldDiscovery, oldLoader, oldBackend
	})
	resolveRepoRootFn = func() string { return root }
	captureGOOSFn = func() string { return "linux" }
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	loadCaptureModuleCatalogFn = modules.GetCatalogWithDiagnostics
	discoverLinuxFn = func(context.Context, discovery.Request, realizer.Realizer) (discovery.Result, error) {
		return discovery.Result{Counts: discovery.Counts{Discovered: 1, Selected: 1, Settings: 1, SettingsOnly: 1}, Settings: []discovery.SettingsCandidate{{ID: gnomeModuleID, ModuleID: gnomeModuleID, DisplayName: "GNOME clock shows the date", Portability: discovery.PortabilityConfigOnly, Actions: []discovery.SettingsAction{discovery.SettingsCapture}}}}, nil
	}
	if catalog, diagnostics, err := modules.GetCatalogWithDiagnostics(root); err != nil || len(diagnostics) != 0 || len(catalog) != 1 {
		t.Fatalf("allowlist fixture is not a valid module: %v %+v %+v", err, diagnostics, catalog)
	}
	return root
}

// useDesktop makes session the desktop the engine talks to, through the real dconf backend.
func useDesktop(session *dconftest.Session) {
	desktopSettingsBackend = dconf.New(session.Host())
}

func clockValue(t *testing.T, session *dconftest.Session) string {
	t.Helper()
	value, set := session.Values[gnomeClockKey]
	if !set {
		return "absent"
	}
	return value
}

// Capture, apply, verify, drift, reconcile and revert over the dconf backend. It
// catches an inherited default being written as a pinned false, and a revert that
// pins instead of resetting. Each case captures on one session and applies on another.
func TestGnomeClockWorkflowOverDconf(t *testing.T) {
	for _, test := range []struct {
		name     string
		captured string // what the source session stores: "absent", "false" or "true"
		prior    string // the target session before apply
		drift    string // another owner's later value
	}{
		{"inherited default", "absent", "true", "true"},
		{"explicit false", "false", "absent", "true"},
		{"explicit true", "true", "false", "absent"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := gnomeRepo(t)
			set := func(session *dconftest.Session, state string) {
				delete(session.Values, gnomeClockKey)
				if state != "absent" {
					session.Values[gnomeClockKey] = state
				}
			}

			source := dconftest.New(t)
			set(source, test.captured)
			useDesktop(source)
			raw, err := RunCapture(CaptureFlags{Out: filepath.Join(root, "clock.endstate"), Only: gnomeModuleID})
			if err != nil {
				t.Fatalf("RunCapture: %+v", err)
			}
			result := raw.(*CaptureResult)
			if result.ConfigCapture == nil || len(result.ConfigCapture.ConfigSets) != 1 || result.ConfigCapture.ConfigSets[0].Status != "captured" {
				t.Fatalf("capture: %+v", result.ConfigCapture)
			}
			if source.Writes != 0 {
				t.Fatalf("capture wrote to the source desktop %d times", source.Writes)
			}
			extracted, extractErr := bundle.ExtractBundle(result.OutputPath)
			if extractErr != nil {
				t.Fatal(extractErr)
			}
			t.Cleanup(func() { os.RemoveAll(filepath.Dir(extracted)) })
			mf, loadErr := manifest.LoadManifest(extracted)
			if loadErr != nil || len(mf.Apps) != 0 || len(mf.ConfigCaptures) != 1 {
				t.Fatalf("manifest: %v %+v", loadErr, mf)
			}
			payload, readErr := os.ReadFile(filepath.Join(filepath.Dir(extracted), mf.ConfigCaptures[0].PayloadRoot, "clock-show-date.json"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			portable, decodeErr := desktopsettings.Decode(payload)
			if decodeErr != nil || portable.Present != (test.captured != "absent") {
				t.Fatalf("portable state %q: %v", payload, decodeErr)
			}

			target := dconftest.New(t)
			set(target, test.prior)
			useDesktop(target)
			restore := func(flags RestoreFlags) *RestoreData {
				t.Helper()
				raw, err := RunRestore(flags)
				if err != nil {
					t.Fatalf("RunRestore: %+v", err)
				}
				return raw.(*RestoreData)
			}
			preview := restore(RestoreFlags{Manifest: extracted, EnableRestore: true, DryRun: true})
			if len(preview.ConfigResolutions) != 1 || preview.ConfigResolutions[0].Status != planner.StatusPlanned {
				t.Fatalf("preview: %+v", preview.ConfigResolutions)
			}
			restore(RestoreFlags{Manifest: extracted})
			if target.Writes != 0 || clockValue(t, target) != test.prior {
				t.Fatalf("preview or restore without consent changed the desktop: writes=%d value=%s", target.Writes, clockValue(t, target))
			}

			apply := func() {
				t.Helper()
				raw, err := RunApply(ApplyFlags{Manifest: extracted, EnableRestore: true, RestoreFilter: gnomeModuleID, NoBootstrap: true})
				if err != nil {
					t.Fatalf("RunApply: %+v", err)
				}
				if got := raw.(*ApplyResult); len(got.ConfigResolutions) != 1 || got.ConfigResolutions[0].Status != planner.StatusRestored {
					t.Fatalf("apply: %+v", got.ConfigResolutions)
				}
				if clockValue(t, target) != test.captured {
					t.Fatalf("apply left %s, want %s", clockValue(t, target), test.captured)
				}
			}
			verify := func(want string) {
				t.Helper()
				raw, err := RunVerify(VerifyFlags{Manifest: extracted})
				if err != nil {
					t.Fatalf("RunVerify: %+v", err)
				}
				if items := raw.(*VerifyResult).Results; len(items) != 1 || items[0].Type != "config" || items[0].Status != want {
					t.Fatalf("verify want %s: %+v", want, items)
				}
			}
			revert := func(want string) {
				t.Helper()
				if _, err := RunRevert(RevertFlags{}); err != nil {
					t.Fatalf("RunRevert: %+v", err)
				}
				if clockValue(t, target) != want {
					t.Fatalf("revert left %s, want %s", clockValue(t, target), want)
				}
			}
			apply()
			verify("pass")
			revert(test.prior)
			apply()
			set(target, test.drift)
			verify("fail")
			apply()
			verify("pass")
			revert(test.drift)
			if target.Writes != 5 || len(source.Rejected)+len(target.Rejected) != 0 {
				t.Fatalf("writes=%d (want one per apply and revert), rejected=%q %q", target.Writes, source.Rejected, target.Rejected)
			}
		})
	}
}

// capturedClock captures the source session and returns the extracted manifest path.
func capturedClock(t *testing.T, root string, source *dconftest.Session) string {
	t.Helper()
	useDesktop(source)
	raw, err := RunCapture(CaptureFlags{Out: filepath.Join(root, "clock.endstate"), Only: gnomeModuleID})
	if err != nil {
		t.Fatalf("RunCapture: %+v", err)
	}
	extracted, extractErr := bundle.ExtractBundle(raw.(*CaptureResult).OutputPath)
	if extractErr != nil {
		t.Fatal(extractErr)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(extracted)) })
	return extracted
}

// Another owner's value written after apply is not ours to revert over.
func TestGnomeRevertDoesNotOverwriteAThirdValue(t *testing.T) {
	root := gnomeRepo(t)
	source := dconftest.New(t)
	source.Values[gnomeClockKey] = "false"
	extracted := capturedClock(t, root, source)
	target := dconftest.New(t)
	useDesktop(target)
	if _, err := RunApply(ApplyFlags{Manifest: extracted, EnableRestore: true, RestoreFilter: gnomeModuleID, NoBootstrap: true}); err != nil {
		t.Fatalf("RunApply: %+v", err)
	}
	target.Values[gnomeClockKey] = "true"
	_, err := RunRevert(RevertFlags{})
	if err == nil || err.Code != envelope.ErrRestoreFailed || !strings.Contains(fmt.Sprint(err.Detail), "ownership conflict") {
		t.Fatalf("revert over a third value: %+v", err)
	}
	if clockValue(t, target) != "true" || target.Writes != 1 {
		t.Fatalf("revert overwrote another owner: value=%s writes=%d", clockValue(t, target), target.Writes)
	}
}

// A desktop with no session bus must stop only the GNOME lane: package capture
// still succeeds, the failure names its fix, and apply and verify never write.
func TestDesktopSessionUnavailableIsScoped(t *testing.T) {
	root := gnomeRepo(t)
	source := dconftest.New(t)
	source.Values[gnomeClockKey] = "false"
	extracted := capturedClock(t, root, source)

	t.Run("capture", func(t *testing.T) {
		oldDiscovery := discoverLinuxFn
		t.Cleanup(func() { discoverLinuxFn = oldDiscovery })
		discoverLinuxFn = func(ctx context.Context, request discovery.Request, r realizer.Realizer) (discovery.Result, error) {
			result, err := oldDiscovery(ctx, request, r)
			result.SchemaVersion = discovery.SchemaVersion
			result.Resolved = []discovery.ResolvedIntent{{ID: "ripgrep", Installable: "github:example/packages#ripgrep"}}
			return result, err
		}
		offline := dconftest.New(t)
		offline.NoBus = true
		useDesktop(offline)
		raw, err := runCaptureLinux(CaptureFlags{Out: filepath.Join(root, "offline.jsonc")}, &fakeRealizer{currentSet: nixSet("ripgrep")}, noopEmitter())
		if err != nil {
			t.Fatalf("a missing desktop session failed the whole capture: %+v", err)
		}
		result := raw.(*CaptureResult)
		if len(result.AppsIncluded) != 1 || result.ConfigCapture == nil || len(result.ConfigCapture.ConfigSets) != 0 {
			t.Fatalf("package capture was affected: %+v", result)
		}
		diagnostics := result.ConfigCapture.Diagnostics
		if len(diagnostics) != 1 || diagnostics[0].Status != bundle.CaptureBundleStatusFailed || diagnostics[0].Code != CapturePlanningDiscoveryFailed ||
			!strings.Contains(diagnostics[0].Detail, "desktop_runtime_unavailable") || !strings.Contains(diagnostics[0].Detail, "logged-in desktop session") {
			t.Fatalf("desktop failure is not scoped, named and actionable: %+v", diagnostics)
		}
	})

	// Both refusals skip the lane; the output must also say which one it was.
	for _, test := range []struct {
		name   string
		change func(*dconftest.Session)
		cause  string
		other  string
	}{
		{"no session bus", func(s *dconftest.Session) { s.NoBus = true }, "desktop_runtime_unavailable", "desktop_value_unsupported"},
		{"stored value of another type", func(s *dconftest.Session) { s.Values[gnomeClockKey] = "'yes'" }, "desktop_value_unsupported", "desktop_runtime_unavailable"},
	} {
		t.Run("apply and verify with "+test.name, func(t *testing.T) {
			desktop := dconftest.New(t)
			test.change(desktop)
			useDesktop(desktop)
			applied, err := RunApply(ApplyFlags{Manifest: extracted, EnableRestore: true, RestoreFilter: gnomeModuleID, NoBootstrap: true})
			if err != nil {
				t.Fatalf("RunApply: %+v", err)
			}
			resolutions := applied.(*ApplyResult).ConfigResolutions
			if len(resolutions) != 1 || resolutions[0].Status != planner.StatusSkipped || resolutions[0].Remediation == nil {
				t.Fatalf("apply did not skip with remediation: %+v", resolutions)
			}
			if message := resolutions[0].Message; !strings.Contains(message, test.cause) || strings.Contains(message, test.other) {
				t.Fatalf("apply message does not name the cause %s: %q", test.cause, message)
			}
			verified, err := RunVerify(VerifyFlags{Manifest: extracted})
			if err != nil {
				t.Fatalf("RunVerify: %+v", err)
			}
			items := verified.(*VerifyResult).Results
			if len(items) != 1 || items[0].Status != "skipped" || items[0].Reason != "target_detection_failed" {
				t.Fatalf("verify did not skip: %+v", items)
			}
			if !strings.Contains(items[0].Message, test.cause) || strings.Contains(items[0].Message, test.other) {
				t.Fatalf("verify message does not name the cause %s: %q", test.cause, items[0].Message)
			}
			if desktop.Writes != 0 {
				t.Fatalf("wrote %d times without a usable session", desktop.Writes)
			}
		})
	}
}

// The execution-time check sees concrete targets after variable expansion. A file
// restore over the dconf user database must still collide with a GNOME value there.
func TestConcreteRestoreClaimsConflictDesktopValueWithDconfDatabase(t *testing.T) {
	t.Setenv("HOME", "/home/me")
	t.Setenv("XDG_CONFIG_HOME", "/home/me/.config")
	desktop := "desktop\x00" + desktopsettings.Target{Schema: "org.gnome.desktop.interface", Path: "/org/gnome/desktop/interface/", Key: "clock-show-date", Type: "boolean"}.Identity()
	for _, test := range []struct {
		file     string
		conflict bool
	}{
		{"/home/me/.config/dconf/user", true},
		{"/home/me/.config", true},
		{"/home/me/.config/gtk-3.0/settings.ini", false},
	} {
		if got := concreteConfigRestoreClaimsOverlap(desktop, "file\x00"+test.file); got != test.conflict {
			t.Fatalf("%s: conflict=%v, want %v", test.file, got, test.conflict)
		}
	}
}
