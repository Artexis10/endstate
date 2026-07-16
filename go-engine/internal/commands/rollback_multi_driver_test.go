// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

type namedFakeUninstaller struct {
	name    string
	calls   []string
	results map[string]*driver.UninstallResult
	uerr    error
}

func (f *namedFakeUninstaller) Name() string { return f.name }
func (f *namedFakeUninstaller) Detect(string) (bool, string, error) {
	return false, "", nil
}
func (f *namedFakeUninstaller) Install(string) (*driver.InstallResult, error) {
	return &driver.InstallResult{}, nil
}
func (f *namedFakeUninstaller) Uninstall(ref string) (*driver.UninstallResult, error) {
	f.calls = append(f.calls, ref)
	if f.uerr != nil {
		return nil, f.uerr
	}
	if result, ok := f.results[ref]; ok {
		return result, nil
	}
	return &driver.UninstallResult{Status: driver.StatusUninstalled}, nil
}

func seedDriverGeneration(t *testing.T, backend, runID string, added ...string) {
	t.Helper()
	if err := provision.Write(&provision.Generation{Backend: backend, RunID: runID, AddedRefs: added}); err != nil {
		t.Fatalf("seed generation: %v", err)
	}
}

func withRollbackDrivers(t *testing.T, defaultDriver driver.Driver, named map[string]driver.Driver, fn func()) {
	t.Helper()
	origResolve := rollbackDriverFn
	rollbackDriverFn = func(name string) (driver.Driver, error) {
		d, ok := named[strings.ToLower(name)]
		if !ok {
			return nil, errors.New("recorded backend unavailable: " + name)
		}
		return d, nil
	}
	defer func() { rollbackDriverFn = origResolve }()
	withDriverOnly(defaultDriver, fn)
}

func TestRollback_Driver_ExplicitTargetRoutesEachRecordedBackend(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	seedDriverGeneration(t, "winget", "old", "Old.App")         // gen 1, retained
	seedDriverGeneration(t, "winget", "apply-mixed", "Git.Git") // gen 2
	seedDriverGeneration(t, "chocolatey", "apply-mixed", "jq")  // gen 3

	winget := &namedFakeUninstaller{name: "winget"}
	choco := &namedFakeUninstaller{name: "chocolatey"}
	var result *RollbackResult
	withRollbackDrivers(t, winget, map[string]driver.Driver{"chocolatey": choco}, func() {
		raw, env := RunRollback(RollbackFlags{To: "1", Confirm: true})
		if env != nil {
			t.Fatalf("unexpected envelope error: %+v", env)
		}
		result = raw.(*RollbackResult)
	})

	if got := strings.Join(winget.calls, ","); got != "Git.Git" {
		t.Errorf("winget calls = %q, want Git.Git", got)
	}
	if got := strings.Join(choco.calls, ","); got != "jq" {
		t.Errorf("chocolatey calls = %q, want jq", got)
	}
	if result.Partial || !sameSet(result.RemovedRefs, []string{"Git.Git", "jq"}) {
		t.Errorf("mixed result = %+v, want both refs removed", result)
	}

	gens, err := provision.List()
	if err != nil {
		t.Fatal(err)
	}
	var rollbackGens []*provision.Generation
	for _, g := range gens {
		if g.Rollback {
			rollbackGens = append(rollbackGens, g)
		}
	}
	if len(rollbackGens) != 2 {
		t.Fatalf("rollback generations = %d, want 2: %+v", len(rollbackGens), rollbackGens)
	}
	if rollbackGens[0].RunID == "" || rollbackGens[0].RunID != rollbackGens[1].RunID {
		t.Errorf("rollback run IDs = %q, %q, want one shared non-empty run", rollbackGens[0].RunID, rollbackGens[1].RunID)
	}
	removedByBackend := map[string][]string{}
	for _, g := range rollbackGens {
		removedByBackend[g.Backend] = g.RemovedRefs
	}
	if got := strings.Join(removedByBackend["winget"], ","); got != "Git.Git" {
		t.Errorf("winget rollback generation refs = %q", got)
	}
	if got := strings.Join(removedByBackend["chocolatey"], ","); got != "jq" {
		t.Errorf("chocolatey rollback generation refs = %q", got)
	}
}

func TestRollback_Driver_BareTargetsNewestMixedRun(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	seedDriverGeneration(t, "winget", "apply-old", "Old.App")
	seedDriverGeneration(t, "winget", "apply-new", "Git.Git")
	seedDriverGeneration(t, "chocolatey", "apply-new", "jq")

	winget := &namedFakeUninstaller{name: "winget"}
	choco := &namedFakeUninstaller{name: "chocolatey"}
	withRollbackDrivers(t, winget, map[string]driver.Driver{"chocolatey": choco}, func() {
		_, env := RunRollback(RollbackFlags{Confirm: true})
		if env != nil {
			t.Fatalf("unexpected envelope error: %+v", env)
		}
	})

	if got := strings.Join(winget.calls, ","); got != "Git.Git" {
		t.Errorf("winget calls = %q, want only newest run's Git.Git", got)
	}
	if got := strings.Join(choco.calls, ","); got != "jq" {
		t.Errorf("chocolatey calls = %q, want newest run's jq", got)
	}
}

func TestRollback_Driver_ChocolateyOnlyDoesNotRequireDefaultDriver(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	seedDriverGeneration(t, "winget", "old", "Old.App")
	seedDriverGeneration(t, "chocolatey", "new", "jq")

	choco := &namedFakeUninstaller{name: "chocolatey"}
	defaultConstructed := false
	origR, origD, origResolve := newRealizerFn, newDriverFn, rollbackDriverFn
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	newDriverFn = func() (driver.Driver, error) {
		defaultConstructed = true
		return nil, ErrNoBackend
	}
	rollbackDriverFn = func(name string) (driver.Driver, error) {
		if name != "chocolatey" {
			return nil, errors.New("unexpected recorded backend: " + name)
		}
		return choco, nil
	}
	defer func() { newRealizerFn, newDriverFn, rollbackDriverFn = origR, origD, origResolve }()

	raw, env := RunRollback(RollbackFlags{To: "1", Confirm: true})
	if env != nil {
		t.Fatalf("unexpected envelope error: %+v", env)
	}
	if defaultConstructed {
		t.Fatal("Chocolatey-only rollback must not construct the default Winget driver")
	}
	if got := strings.Join(choco.calls, ","); got != "jq" {
		t.Fatalf("Chocolatey calls = %q, want jq", got)
	}
	result := raw.(*RollbackResult)
	if result.Backend != "chocolatey" || strings.Join(result.RemovedRefs, ",") != "jq" {
		t.Fatalf("result = %+v, want Chocolatey-only rollback", result)
	}
}

func TestRollback_Driver_UnavailableBackendIsPartialWithoutFallback(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	seedDriverGeneration(t, "winget", "old", "Old.App")
	seedDriverGeneration(t, "winget", "mixed", "Git.Git")
	seedDriverGeneration(t, "chocolatey", "mixed", "jq")

	winget := &namedFakeUninstaller{name: "winget"}
	var result *RollbackResult
	withRollbackDrivers(t, winget, nil, func() {
		raw, env := RunRollback(RollbackFlags{To: "1", Confirm: true})
		if env != nil {
			t.Fatalf("one unavailable lane must aggregate, got %+v", env)
		}
		result = raw.(*RollbackResult)
	})

	if got := strings.Join(winget.calls, ","); got != "Git.Git" {
		t.Errorf("default winget received %q, want only its own ref (no fallback)", got)
	}
	if !result.Partial || strings.Join(result.RemovedRefs, ",") != "Git.Git" || strings.Join(result.FailedRefs, ",") != "jq" {
		t.Errorf("result = %+v, want removed Git.Git and failed jq", result)
	}
}

func TestRollback_Driver_DryRunDoesNotResolveNamedDrivers(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	seedDriverGeneration(t, "winget", "old", "Old.App")
	seedDriverGeneration(t, "winget", "mixed", "Git.Git")
	seedDriverGeneration(t, "chocolatey", "mixed", "jq")

	origR, origD, origResolve := newRealizerFn, newDriverFn, rollbackDriverFn
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	newDriverFn = func() (driver.Driver, error) { t.Fatal("dry-run constructed the default driver"); return nil, nil }
	rollbackDriverFn = func(string) (driver.Driver, error) { t.Fatal("dry-run resolved a named driver"); return nil, nil }
	defer func() { newRealizerFn, newDriverFn, rollbackDriverFn = origR, origD, origResolve }()

	raw, env := RunRollback(RollbackFlags{To: "1", DryRun: true})
	if env != nil {
		t.Fatalf("unexpected envelope error: %+v", env)
	}
	result := raw.(*RollbackResult)
	if got := strings.Join(result.RemovedRefs, ","); got != "jq,Git.Git" {
		t.Errorf("dry-run refs = %q, want newest-backend-first jq,Git.Git", got)
	}
}

func TestRollback_Driver_AllUnavailableReturnsRollbackFailed(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	seedDriverGeneration(t, "winget", "old", "Old.App")
	seedDriverGeneration(t, "chocolatey", "new", "jq")

	winget := &namedFakeUninstaller{name: "winget"}
	withRollbackDrivers(t, winget, nil, func() {
		_, env := RunRollback(RollbackFlags{To: "1", Confirm: true})
		if env == nil || env.Code != envelope.ErrRollbackFailed {
			t.Fatalf("want ROLLBACK_FAILED, got %+v", env)
		}
	})
}
