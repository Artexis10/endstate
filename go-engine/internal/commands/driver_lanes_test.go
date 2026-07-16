// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bootstrap"
	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

type laneTestDriver struct {
	name                  string
	installed             map[string]bool
	versions              map[string]string
	batchErr              error
	batchCalls            int
	detectCalls           []string
	installCalls          []string
	installVersionCalls   []string
	reinstallVersionCalls []string
	installResult         *driver.InstallResult
	leaveVersionUnchanged bool
}

func (d *laneTestDriver) Name() string { return d.name }

func (d *laneTestDriver) Detect(ref string) (bool, string, error) {
	d.detectCalls = append(d.detectCalls, ref)
	return d.installed[ref], ref + " name", nil
}

func (d *laneTestDriver) DetectBatch(refs []string) (map[string]driver.DetectResult, error) {
	d.batchCalls++
	if d.batchErr != nil {
		return nil, d.batchErr
	}
	results := make(map[string]driver.DetectResult, len(refs))
	for _, ref := range refs {
		version := ""
		if d.installed[ref] {
			version = d.versions[ref]
		}
		results[ref] = driver.DetectResult{
			Installed:   d.installed[ref],
			DisplayName: ref + " name",
			Version:     version,
		}
	}
	return results, nil
}

func (d *laneTestDriver) Install(ref string) (*driver.InstallResult, error) {
	d.installCalls = append(d.installCalls, ref)
	if d.installed == nil {
		d.installed = make(map[string]bool)
	}
	d.installed[ref] = true
	if d.installResult != nil {
		result := *d.installResult
		return &result, nil
	}
	return &driver.InstallResult{Status: driver.StatusInstalled, Message: "installed"}, nil
}

func (d *laneTestDriver) InstallVersion(ref, version string) (*driver.InstallResult, error) {
	d.installVersionCalls = append(d.installVersionCalls, ref+"@"+version)
	if d.installed == nil {
		d.installed = make(map[string]bool)
	}
	d.installed[ref] = true
	if d.versions == nil {
		d.versions = make(map[string]string)
	}
	if !d.leaveVersionUnchanged {
		d.versions[ref] = version
	}
	return &driver.InstallResult{Status: driver.StatusInstalled, Message: "installed version"}, nil
}

func (d *laneTestDriver) ReinstallVersion(ref, version string) (*driver.InstallResult, error) {
	d.reinstallVersionCalls = append(d.reinstallVersionCalls, ref+"@"+version)
	if d.versions == nil {
		d.versions = make(map[string]string)
	}
	if !d.leaveVersionUnchanged {
		d.versions[ref] = version
	}
	return &driver.InstallResult{Status: driver.StatusInstalled, Message: "reinstalled version"}, nil
}

type detectErrorDriver struct {
	name         string
	detectErr    error
	installCalls int
}

func (d *detectErrorDriver) Name() string { return d.name }
func (d *detectErrorDriver) Detect(string) (bool, string, error) {
	return false, "", d.detectErr
}
func (d *detectErrorDriver) Install(string) (*driver.InstallResult, error) {
	d.installCalls++
	return &driver.InstallResult{Status: driver.StatusInstalled}, nil
}

func writeLaneManifest(t *testing.T, apps string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest.jsonc")
	contents := `{"name":"driver-lanes","apps":[` + apps + `]}`
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func withNamedDriverLanes(t *testing.T, drivers map[string]driver.Driver, emitterBuf *bytes.Buffer, fn func()) {
	t.Helper()
	origDefault := newDriverFn
	origNamed := newNamedDriverFn
	origRealizer := newRealizerFn
	origBrew := newBrewDriverFn
	origEmitter := newApplyEmitterFn
	newDriverFn = func() (driver.Driver, error) {
		if d := drivers["winget"]; d != nil {
			return d, nil
		}
		return nil, ErrNoBackend
	}
	newNamedDriverFn = func(name string) (driver.Driver, error) {
		if d := drivers[strings.ToLower(name)]; d != nil {
			return d, nil
		}
		return nil, ErrUnsupportedDriver
	}
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	newBrewDriverFn = func() (driver.Driver, error) { return nil, ErrNoBrewDriver }
	if emitterBuf != nil {
		newApplyEmitterFn = func(runID string, enabled bool) *events.Emitter {
			return events.NewEmitterWithWriter(runID, enabled, emitterBuf)
		}
	}
	defer func() {
		newDriverFn = origDefault
		newNamedDriverFn = origNamed
		newRealizerFn = origRealizer
		newBrewDriverFn = origBrew
		newApplyEmitterFn = origEmitter
	}()
	fn()
}

func TestRunPlan_MixedDriverLanesPreserveManifestOrder(t *testing.T) {
	winget := &laneTestDriver{name: "winget", installed: map[string]bool{"Winget.A": true}}
	choco := &laneTestDriver{name: "chocolatey", installed: map[string]bool{"choco-b": true}}
	path := writeLaneManifest(t, `
		{"id":"a","refs":{"windows":"Winget.A"}},
		{"id":"b","driver":"chocolatey","refs":{"windows":"choco-b"}},
		{"id":"c","driver":"WINGET","refs":{"windows":"Winget.C"}}`)

	withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget, "chocolatey": choco}, nil, func() {
		raw, eerr := RunPlan(PlanFlags{Manifest: path})
		if eerr != nil {
			t.Fatalf("RunPlan error = %v", eerr)
		}
		result := raw.(*PlanResult)
		var ids, names []string
		for _, action := range result.Actions {
			ids = append(ids, action.ID)
			names = append(names, action.Driver)
		}
		if !reflect.DeepEqual(ids, []string{"a", "b", "c"}) {
			t.Fatalf("action ids = %v", ids)
		}
		if !reflect.DeepEqual(names, []string{"winget", "chocolatey", "winget"}) {
			t.Fatalf("action drivers = %v", names)
		}
	})
	if winget.batchCalls != 1 || choco.batchCalls != 1 {
		t.Fatalf("batch calls winget=%d chocolatey=%d, want one each", winget.batchCalls, choco.batchCalls)
	}
}

func TestRunVerify_MixedDriverLanesExposeDriver(t *testing.T) {
	winget := &laneTestDriver{name: "winget", installed: map[string]bool{"Winget.A": true}}
	choco := &laneTestDriver{name: "chocolatey", installed: map[string]bool{"choco-b": true}}
	path := writeLaneManifest(t, `
		{"id":"a","refs":{"windows":"Winget.A"}},
		{"id":"b","driver":"chocolatey","refs":{"windows":"choco-b"}}`)

	withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget, "chocolatey": choco}, nil, func() {
		raw, eerr := RunVerify(VerifyFlags{Manifest: path})
		if eerr != nil {
			t.Fatalf("RunVerify error = %v", eerr)
		}
		result := raw.(*VerifyResult)
		if got := []string{result.Results[0].Driver, result.Results[1].Driver}; !reflect.DeepEqual(got, []string{"winget", "chocolatey"}) {
			t.Fatalf("result drivers = %v", got)
		}
	})
	if winget.batchCalls != 1 || choco.batchCalls != 1 {
		t.Fatalf("batch calls winget=%d chocolatey=%d, want one each", winget.batchCalls, choco.batchCalls)
	}
}

func TestRunApply_MixedDriverLanesWriteScopedGenerationsAndRebootFact(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	winget := &laneTestDriver{name: "winget", installed: map[string]bool{}}
	choco := &laneTestDriver{
		name:      "chocolatey",
		installed: map[string]bool{},
		installResult: &driver.InstallResult{
			Status:         driver.StatusInstalled,
			Message:        "installed; restart required",
			RebootRequired: true,
		},
	}
	path := writeLaneManifest(t, `
		{"id":"a","refs":{"windows":"Winget.A"}},
		{"id":"b","driver":"chocolatey","refs":{"windows":"choco-b"}}`)
	var eventBuf bytes.Buffer

	withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget, "chocolatey": choco}, &eventBuf, func() {
		raw, eerr := RunApply(ApplyFlags{Manifest: path, Events: "jsonl"})
		if eerr != nil {
			t.Fatalf("RunApply error = %v", eerr)
		}
		result := raw.(*ApplyResult)
		if got := []string{result.Actions[0].Driver, result.Actions[1].Driver}; !reflect.DeepEqual(got, []string{"winget", "chocolatey"}) {
			t.Fatalf("action drivers = %v", got)
		}
		if !result.Actions[1].RebootRequired {
			t.Fatal("Chocolatey action did not preserve rebootRequired")
		}
	})

	gens, err := provision.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(gens) != 2 {
		t.Fatalf("generations = %d, want 2", len(gens))
	}
	if gens[0].RunID != gens[1].RunID {
		t.Fatalf("generation run ids differ: %q != %q", gens[0].RunID, gens[1].RunID)
	}
	// List returns newest generation first. Lanes are written in first-manifest
	// appearance order, so Chocolatey's later write is listed before Winget.
	if got := []string{gens[0].Backend, gens[1].Backend}; !reflect.DeepEqual(got, []string{"chocolatey", "winget"}) {
		t.Fatalf("generation backends = %v", got)
	}
	if !strings.Contains(eventBuf.String(), `"driver":"chocolatey"`) || !strings.Contains(eventBuf.String(), `"rebootRequired":true`) {
		t.Fatalf("event stream missing Chocolatey reboot fact:\n%s", eventBuf.String())
	}
	for _, phase := range []string{"plan", "apply", "verify"} {
		if got := strings.Count(eventBuf.String(), `"event":"phase","phase":"`+phase+`"`); got != 1 {
			t.Fatalf("%s phase events = %d, want 1", phase, got)
		}
		if got := strings.Count(eventBuf.String(), `"event":"summary","phase":"`+phase+`"`); got != 1 {
			t.Fatalf("%s summary events = %d, want 1", phase, got)
		}
	}
}

func TestRunApply_UnpinnedInstallsRecordDetectedVersionsPerDriver(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	winget := &laneTestDriver{name: "winget", installed: map[string]bool{}, versions: map[string]string{"Winget.A": "1.2.3"}}
	choco := &laneTestDriver{name: "chocolatey", installed: map[string]bool{}, versions: map[string]string{"choco-a": "4.5.6"}}
	path := writeLaneManifest(t, `
		{"id":"winget-a","refs":{"windows":"Winget.A"}},
		{"id":"choco-a","driver":"chocolatey","refs":{"windows":"choco-a"}}
	`)

	withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget, "chocolatey": choco}, nil, func() {
		if _, eerr := RunApply(ApplyFlags{Manifest: path}); eerr != nil {
			t.Fatalf("RunApply error = %v", eerr)
		}
	})

	gens, err := provision.List()
	if err != nil {
		t.Fatalf("List generations: %v", err)
	}
	if len(gens) != 2 {
		t.Fatalf("generations = %+v, want winget and chocolatey", gens)
	}
	versions := map[string]string{}
	for _, generation := range gens {
		if len(generation.Items) != 1 {
			t.Fatalf("generation %s items = %+v, want one", generation.Backend, generation.Items)
		}
		versions[generation.Backend] = generation.Items[0].Version
	}
	if versions["winget"] != "1.2.3" || versions["chocolatey"] != "4.5.6" {
		t.Fatalf("recorded versions = %+v, want winget=1.2.3 chocolatey=4.5.6", versions)
	}
}

func TestRunApply_BatchInfrastructureErrorDoesNotInstallFailedLane(t *testing.T) {
	winget := &laneTestDriver{name: "winget", installed: map[string]bool{}}
	choco := &laneTestDriver{name: "chocolatey", installed: map[string]bool{}, batchErr: errors.New("choco ledger unavailable")}
	path := writeLaneManifest(t, `
		{"id":"a","refs":{"windows":"Winget.A"}},
		{"id":"b","driver":"chocolatey","refs":{"windows":"choco-b"}}`)

	withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget, "chocolatey": choco}, nil, func() {
		raw, eerr := RunApply(ApplyFlags{Manifest: path})
		if eerr != nil {
			t.Fatalf("RunApply error = %v", eerr)
		}
		result := raw.(*ApplyResult)
		if result.Actions[1].Status != driver.StatusFailed || !strings.Contains(result.Actions[1].Message, "ledger unavailable") {
			t.Fatalf("Chocolatey action = %+v", result.Actions[1])
		}
	})
	if !reflect.DeepEqual(winget.installCalls, []string{"Winget.A"}) {
		t.Fatalf("winget installs = %v", winget.installCalls)
	}
	if len(choco.installCalls) != 0 {
		t.Fatalf("Chocolatey installs = %v, want none", choco.installCalls)
	}
}

func TestRunApply_PerRefDetectErrorDoesNotBecomeMissingInstall(t *testing.T) {
	winget := &detectErrorDriver{name: "winget", detectErr: errors.New("winget database locked")}
	path := writeLaneManifest(t, `{"id":"a","refs":{"windows":"Winget.A"}}`)

	withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget}, nil, func() {
		raw, eerr := RunApply(ApplyFlags{Manifest: path})
		if eerr != nil {
			t.Fatalf("RunApply error = %v", eerr)
		}
		action := raw.(*ApplyResult).Actions[0]
		if action.Status != driver.StatusFailed || !strings.Contains(action.Message, "database locked") {
			t.Fatalf("action = %+v", action)
		}
	})
	if winget.installCalls != 0 {
		t.Fatalf("install calls = %d, want 0", winget.installCalls)
	}
}

func TestRunApply_KnownUnsupportedDriverIsVisibleAndNeverFallsBack(t *testing.T) {
	winget := &laneTestDriver{name: "winget", installed: map[string]bool{}}
	path := writeLaneManifest(t, `{"id":"brew-app","driver":"brew","refs":{"darwin":"hello"}}`)

	withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget}, nil, func() {
		raw, eerr := RunApply(ApplyFlags{Manifest: path})
		if eerr != nil {
			t.Fatalf("RunApply error = %v", eerr)
		}
		action := raw.(*ApplyResult).Actions[0]
		if action.Driver != "brew" || action.Status != driver.StatusSkipped {
			t.Fatalf("unsupported action = %+v", action)
		}
	})
	if len(winget.installCalls) != 0 || winget.batchCalls != 0 {
		t.Fatalf("unsupported brew app fell back to winget: batch=%d installs=%v", winget.batchCalls, winget.installCalls)
	}
}

func TestRunApply_ChocolateyExactVersionUsesSelectedVersionedInstaller(t *testing.T) {
	winget := &laneTestDriver{name: "winget", installed: map[string]bool{}}
	choco := &laneTestDriver{name: "chocolatey", installed: map[string]bool{}}
	path := writeLaneManifest(t, `{"id":"git","driver":"chocolatey","version":"2.47.1","refs":{"windows":"git.install"}}`)

	withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget, "chocolatey": choco}, nil, func() {
		_, eerr := RunApply(ApplyFlags{Manifest: path})
		if eerr != nil {
			t.Fatalf("RunApply error = %v", eerr)
		}
	})
	if !reflect.DeepEqual(choco.installVersionCalls, []string{"git.install@2.47.1"}) {
		t.Fatalf("Chocolatey InstallVersion calls = %v", choco.installVersionCalls)
	}
	if len(choco.installCalls) != 0 || len(winget.installCalls) != 0 {
		t.Fatalf("versioned Chocolatey app crossed driver/method: choco latest=%v winget=%v", choco.installCalls, winget.installCalls)
	}
}

func TestRunApply_ChocolateyRepinUsesSelectedVersionedInstaller(t *testing.T) {
	winget := &laneTestDriver{name: "winget", installed: map[string]bool{}}
	choco := &laneTestDriver{
		name:      "chocolatey",
		installed: map[string]bool{"git.install": true},
		versions:  map[string]string{"git.install": "2.46.0"},
	}
	path := writeLaneManifest(t, `{"id":"git","driver":"chocolatey","version":"2.47.1","refs":{"windows":"git.install"}}`)

	withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget, "chocolatey": choco}, nil, func() {
		_, eerr := RunApply(ApplyFlags{Manifest: path, Repin: true, Confirm: true})
		if eerr != nil {
			t.Fatalf("RunApply error = %v", eerr)
		}
	})
	if !reflect.DeepEqual(choco.reinstallVersionCalls, []string{"git.install@2.47.1"}) {
		t.Fatalf("Chocolatey ReinstallVersion calls = %v", choco.reinstallVersionCalls)
	}
	if len(winget.installCalls) != 0 {
		t.Fatalf("Chocolatey repin fell back to Winget: %v", winget.installCalls)
	}
}

func TestRunApply_RepinPostVerifySurfacesVersionDrift(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	choco := &laneTestDriver{
		name:                  "chocolatey",
		installed:             map[string]bool{"git.install": true},
		versions:              map[string]string{"git.install": "2.46.0"},
		leaveVersionUnchanged: true,
	}
	path := writeLaneManifest(t, `{"id":"git","driver":"chocolatey","version":"2.47.1","refs":{"windows":"git.install"}}`)
	var eventStream bytes.Buffer

	withNamedDriverLanes(t, map[string]driver.Driver{"chocolatey": choco}, &eventStream, func() {
		raw, eerr := RunApply(ApplyFlags{Manifest: path, Repin: true, Confirm: true, Events: "jsonl"})
		if eerr != nil {
			t.Fatalf("RunApply error = %v", eerr)
		}
		action := raw.(*ApplyResult).Actions[0]
		if action.Reason != driver.ReasonVersionDrift || action.Version != "2.46.0" || !strings.Contains(action.Message, "want 2.47.1") {
			t.Fatalf("post-verify action = %+v, want surfaced version drift", action)
		}
	})
	if !strings.Contains(eventStream.String(), `"reason":"version_drift"`) {
		t.Fatalf("verify stream did not report version drift:\n%s", eventStream.String())
	}
	gens, err := provision.List()
	if err != nil || len(gens) != 1 {
		t.Fatalf("repin generations = %+v, err=%v, want one partial record", gens, err)
	}
	if len(gens[0].AddedRefs) != 0 || !gens[0].Partial {
		t.Fatalf("failed repin generation = %+v, want no added refs and partial=true", gens[0])
	}
}

func TestRunApply_RepinGenerationCannotUninstallPreexistingPackageOnRollback(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	winget := &laneTestDriver{
		name:      "winget",
		installed: map[string]bool{"Vendor.A": true},
		versions:  map[string]string{"Vendor.A": "1.0.0"},
	}
	path := writeLaneManifest(t, `{"id":"a","version":"2.0.0","refs":{"windows":"Vendor.A"}}`)
	withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget}, nil, func() {
		if _, eerr := RunApply(ApplyFlags{Manifest: path, Repin: true, Confirm: true}); eerr != nil {
			t.Fatalf("RunApply error = %v", eerr)
		}
	})

	uninstaller := &fakeUninstaller{}
	withDriverOnly(uninstaller, func() {
		raw, eerr := RunRollback(RollbackFlags{Confirm: true})
		if eerr != nil {
			t.Fatalf("RunRollback error = %v", eerr)
		}
		if removed := raw.(*RollbackResult).RemovedRefs; len(removed) != 0 {
			t.Fatalf("rollback removed refs = %v, want none for a repin-only generation", removed)
		}
	})
	if len(uninstaller.calls) != 0 {
		t.Fatalf("rollback uninstall calls = %v, preexisting repinned package must not be uninstalled", uninstaller.calls)
	}
}

func TestRunApply_ConsentedChocolateyBootstrapFailureFailsOnlyChocolateyLane(t *testing.T) {
	winget := &laneTestDriver{name: "winget", installed: map[string]bool{}}
	choco := &laneTestDriver{name: "chocolatey", installed: map[string]bool{}}
	path := writeLaneManifest(t, `
		{"id":"a","refs":{"windows":"Winget.A"}},
		{"id":"b","driver":"chocolatey","refs":{"windows":"choco-b"}}`)

	origBootstrap := bootstrapBackendsFn
	bootstrapBackendsFn = func(needed []bootstrap.Backend, mutating bool, consent Consent, _ *events.Emitter) (map[bootstrap.Backend]bool, *envelope.Error) {
		if !reflect.DeepEqual(needed, []bootstrap.Backend{bootstrap.BackendChocolatey}) || !mutating || !consent.Granted {
			t.Fatalf("bootstrap preflight args: needed=%v mutating=%v consent=%+v", needed, mutating, consent)
		}
		return map[bootstrap.Backend]bool{bootstrap.BackendChocolatey: false}, nil
	}
	defer func() { bootstrapBackendsFn = origBootstrap }()

	withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget, "chocolatey": choco}, nil, func() {
		raw, eerr := RunApply(ApplyFlags{Manifest: path, BootstrapBackends: true})
		if eerr != nil {
			t.Fatalf("RunApply error = %v", eerr)
		}
		result := raw.(*ApplyResult)
		if result.Actions[0].Status != driver.StatusInstalled {
			t.Fatalf("Winget action = %+v", result.Actions[0])
		}
		if result.Actions[1].Status != driver.StatusFailed || result.Actions[1].Driver != "chocolatey" {
			t.Fatalf("Chocolatey action = %+v", result.Actions[1])
		}
	})
	if !reflect.DeepEqual(winget.installCalls, []string{"Winget.A"}) || len(choco.installCalls) != 0 {
		t.Fatalf("lane isolation failed: winget=%v chocolatey=%v", winget.installCalls, choco.installCalls)
	}
}

func TestRunApply_UnavailableChocolateyAddsStructuredWarning(t *testing.T) {
	path := writeLaneManifest(t, `{"id":"git","driver":"chocolatey","refs":{"windows":"git.install"}}`)
	origBootstrap := bootstrapBackendsFn
	bootstrapBackendsFn = func([]bootstrap.Backend, bool, Consent, *events.Emitter) (map[bootstrap.Backend]bool, *envelope.Error) {
		return map[bootstrap.Backend]bool{bootstrap.BackendChocolatey: false}, nil
	}
	defer func() { bootstrapBackendsFn = origBootstrap }()

	withNamedDriverLanes(t, map[string]driver.Driver{}, nil, func() {
		raw, eerr := RunApply(ApplyFlags{Manifest: path})
		if eerr != nil {
			t.Fatalf("RunApply error = %v", eerr)
		}
		warnings := raw.(*ApplyResult).Warnings
		if len(warnings) != 1 || warnings[0].Code != "optional_driver_unavailable" || warnings[0].Driver != "chocolatey" {
			t.Fatalf("warnings = %+v", warnings)
		}
	})
}

func TestPackageDriverPreflight_ChocolateyUnavailableModes(t *testing.T) {
	mf := &manifest.Manifest{Apps: []manifest.App{{
		ID: "git", Driver: "chocolatey", Refs: map[string]string{"windows": "git.install"},
	}}}
	tests := []struct {
		name              string
		flags             ApplyFlags
		bootstrapErr      *envelope.Error
		wantMutating      bool
		wantConsentDenied bool
		wantFailed        bool
		wantMessage       string
	}{
		{name: "dry-run", flags: ApplyFlags{DryRun: true}, wantMutating: false, wantMessage: "dry-run"},
		{name: "no-bootstrap", flags: ApplyFlags{NoBootstrap: true}, wantMutating: true, wantConsentDenied: true, wantMessage: "declined"},
		{name: "probe-error", bootstrapErr: envelope.NewError(envelope.ErrInternalError, "probe boom"), wantMutating: true, wantFailed: true, wantMessage: "probe boom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := bootstrapBackendsFn
			bootstrapBackendsFn = func(needed []bootstrap.Backend, mutating bool, consent Consent, _ *events.Emitter) (map[bootstrap.Backend]bool, *envelope.Error) {
				if !reflect.DeepEqual(needed, []bootstrap.Backend{bootstrap.BackendChocolatey}) || mutating != tt.wantMutating || consent.Denied != tt.wantConsentDenied {
					t.Fatalf("preflight args: needed=%v mutating=%v consent=%+v", needed, mutating, consent)
				}
				return map[bootstrap.Backend]bool{bootstrap.BackendChocolatey: false}, tt.bootstrapErr
			}
			defer func() { bootstrapBackendsFn = orig }()

			overrides, warnings := packageDriverPreflight(tt.flags, mf, events.NewEmitter("test", false))
			override, ok := overrides["chocolatey"]
			if !ok || override.failed != tt.wantFailed || override.err == nil || !strings.Contains(override.err.Error(), tt.wantMessage) {
				t.Fatalf("override = %+v, want failed=%v message containing %q", override, tt.wantFailed, tt.wantMessage)
			}
			if len(warnings) != 1 || warnings[0].Code != "optional_driver_unavailable" || !strings.Contains(warnings[0].Message, tt.wantMessage) {
				t.Fatalf("warnings = %+v", warnings)
			}
		})
	}
}

func TestRunApply_ChocolateyConsentFollowsFirstPlanPhase(t *testing.T) {
	path := writeLaneManifest(t, `{"id":"git","driver":"chocolatey","refs":{"windows":"git.install"}}`)
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	origBootstrap := bootstrapBackendsFn
	origBootstrapper := newBootstrapperFn
	origGOOS := bootstrapGOOSFn
	bootstrapBackendsFn = realEnsureBackends
	newBootstrapperFn = func() *bootstrap.Bootstrapper {
		return &bootstrap.Bootstrapper{
			Detect: func(bootstrap.Backend) (bool, error) { return false, nil },
			Install: func(bootstrap.Backend) error {
				t.Fatal("consent was not granted; installer must not run")
				return nil
			},
			Verify: func(bootstrap.Backend) (bool, error) {
				t.Fatal("consent was not granted; verifier must not run")
				return false, nil
			},
		}
	}
	bootstrapGOOSFn = func() string { return "windows" }
	defer func() {
		bootstrapBackendsFn = origBootstrap
		newBootstrapperFn = origBootstrapper
		bootstrapGOOSFn = origGOOS
	}()

	var eventBuf bytes.Buffer
	withNamedDriverLanes(t, map[string]driver.Driver{}, &eventBuf, func() {
		if _, eerr := RunApply(ApplyFlags{Manifest: path, Events: "jsonl"}); eerr != nil {
			t.Fatalf("RunApply error = %v", eerr)
		}
	})

	events := parseEvents(&eventBuf)
	if len(events) < 2 {
		t.Fatalf("events = %v, want plan phase followed by consent", events)
	}
	if events[0]["event"] != "phase" || events[0]["phase"] != "plan" {
		t.Fatalf("first event = %v, want plan phase", events[0])
	}
	if events[1]["event"] != "consent" {
		t.Fatalf("second event = %v, want consent", events[1])
	}
	backends, ok := events[1]["backends"].([]interface{})
	if !ok || len(backends) != 1 || backends[0] != "chocolatey" {
		t.Fatalf("consent backends = %v, want [chocolatey]", events[1]["backends"])
	}
}

func TestRunVerify_BatchInfrastructureErrorIsNotMissing(t *testing.T) {
	winget := &laneTestDriver{name: "winget", installed: map[string]bool{}, batchErr: errors.New("winget database locked")}
	path := writeLaneManifest(t, `{"id":"a","refs":{"windows":"Winget.A"}}`)

	withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget}, nil, func() {
		raw, eerr := RunVerify(VerifyFlags{Manifest: path})
		if eerr != nil {
			t.Fatalf("RunVerify error = %v", eerr)
		}
		item := raw.(*VerifyResult).Results[0]
		if item.Reason != driver.ReasonInstallFailed || item.Reason == driver.ReasonMissing {
			t.Fatalf("infrastructure error reason = %q, want %q", item.Reason, driver.ReasonInstallFailed)
		}
	})
}

func TestRunPlanAndVerify_AbsentChocolateyAreVisibleSkippedWithoutDriverFallback(t *testing.T) {
	winget := &laneTestDriver{name: "winget", installed: map[string]bool{}}
	path := writeLaneManifest(t, `{"id":"git","driver":"chocolatey","refs":{"windows":"git.install"}}`)
	origBootstrap := bootstrapBackendsFn
	bootstrapBackendsFn = func([]bootstrap.Backend, bool, Consent, *events.Emitter) (map[bootstrap.Backend]bool, *envelope.Error) {
		return map[bootstrap.Backend]bool{bootstrap.BackendChocolatey: false}, nil
	}
	defer func() { bootstrapBackendsFn = origBootstrap }()

	withNamedDriverLanes(t, map[string]driver.Driver{"winget": winget}, nil, func() {
		planRaw, planErr := RunPlan(PlanFlags{Manifest: path})
		if planErr != nil {
			t.Fatalf("RunPlan error = %v", planErr)
		}
		planAction := planRaw.(*PlanResult).Actions[0]
		if planAction.Driver != "chocolatey" || planAction.PlannedAction != "skip" || planAction.CurrentStatus != driver.StatusSkipped {
			t.Fatalf("plan action = %+v", planAction)
		}

		verifyRaw, verifyErr := RunVerify(VerifyFlags{Manifest: path})
		if verifyErr != nil {
			t.Fatalf("RunVerify error = %v", verifyErr)
		}
		verifyItem := verifyRaw.(*VerifyResult).Results[0]
		if verifyItem.Driver != "chocolatey" || verifyItem.Status != driver.StatusSkipped {
			t.Fatalf("verify item = %+v", verifyItem)
		}
	})
	if winget.batchCalls != 0 || len(winget.installCalls) != 0 {
		t.Fatalf("absent Chocolatey fell back to Winget: batch=%d installs=%v", winget.batchCalls, winget.installCalls)
	}
}

func TestPackageCommandsReportUnknownDriverAsManifestValidationError(t *testing.T) {
	path := writeLaneManifest(t, `{"id":"bad","driver":"scoop","refs":{"windows":"bad"}}`)
	tests := []struct {
		name string
		run  func() *envelope.Error
	}{
		{name: "apply", run: func() *envelope.Error { _, err := RunApply(ApplyFlags{Manifest: path}); return err }},
		{name: "plan", run: func() *envelope.Error { _, err := RunPlan(PlanFlags{Manifest: path}); return err }},
		{name: "verify", run: func() *envelope.Error { _, err := RunVerify(VerifyFlags{Manifest: path}); return err }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.run()
			if err == nil || err.Code != envelope.ErrManifestValidationError {
				t.Fatalf("error = %+v, want %s", err, envelope.ErrManifestValidationError)
			}
		})
	}
}

func TestRunApply_PackageModuleMapIsDriverQualifiedAndLegacyMapStaysWingetOnly(t *testing.T) {
	choco := &laneTestDriver{name: "chocolatey", installed: map[string]bool{"Git.Install": true}}
	path := writeLaneManifest(t, `{"id":"git","driver":"chocolatey","refs":{"windows":"Git.Install"}}`)
	catalog := map[string]*modules.Module{
		"apps.git": {
			ID:      "apps.git",
			Matches: modules.MatchCriteria{Chocolatey: []string{"git.install"}},
			Capture: &modules.CaptureDef{Files: []modules.CaptureFile{{Source: "a", Dest: "b"}}},
		},
	}

	withNamedDriverLanes(t, map[string]driver.Driver{"chocolatey": choco}, nil, func() {
		withMockCatalog(catalog, nil, func() {
			raw, eerr := RunApply(ApplyFlags{Manifest: path, DryRun: true})
			if eerr != nil {
				t.Fatalf("RunApply error = %v", eerr)
			}
			result := raw.(*ApplyResult)
			if len(result.ConfigModuleMap) != 0 {
				t.Fatalf("legacy configModuleMap leaked Chocolatey refs: %v", result.ConfigModuleMap)
			}
			if got := result.PackageModuleMap["chocolatey:git.install"]; !reflect.DeepEqual(got, []string{"apps.git"}) {
				t.Fatalf("packageModuleMap = %v", result.PackageModuleMap)
			}
		})
	})
}
