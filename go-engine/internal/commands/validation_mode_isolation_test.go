// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestValidationModeSessionConcurrentFindingsAreDeterministicAndSafe(t *testing.T) {
	context := validationContext(t, validationmode.Inventory{
		AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
		DisplayName: "Notepad++", Source: "winget", InitialState: "absent",
	})
	session, err := ActivateValidationMode(context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Restore() })

	findings := []struct {
		coordinate string
		target     string
		reason     isolationReason
	}{
		{coordinate: "restore[1].target", target: "localappdata-settings", reason: isolationReasonUnsafePath},
		{coordinate: "capture.files[0].source", target: "appdata-settings", reason: isolationReasonUnsafePath},
		{coordinate: "verify[2].path", target: "userprofile-config", reason: isolationReasonGuardChanged},
	}
	var workers sync.WaitGroup
	for index := 0; index < 48; index++ {
		finding := findings[index%len(findings)]
		workers.Add(1)
		go func() {
			defer workers.Done()
			_ = session.recordIsolationFinding(finding.coordinate, finding.target, finding.reason)
		}()
	}
	workers.Wait()

	isolationErr := session.IsolationError()
	if isolationErr == nil {
		t.Fatal("concurrent findings did not poison the session")
	}
	text := isolationErr.Error()
	for _, coordinate := range []string{"capture.files[0].source", "restore[1].target", "verify[2].path"} {
		if count := strings.Count(text, "coordinate="+coordinate); count != 1 {
			t.Fatalf("coordinate %q count = %d in %q", coordinate, count, text)
		}
	}
	first := strings.Index(text, "coordinate=capture.files[0].source")
	second := strings.Index(text, "coordinate=restore[1].target")
	third := strings.Index(text, "coordinate=verify[2].path")
	if first < 0 || second <= first || third <= second {
		t.Fatalf("findings are not deterministically ordered: %q", text)
	}
	for _, forbidden := range []string{context.Root(), `HKCU\Software\Sensitive`, "registry-value-secret"} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(forbidden)) {
			t.Fatalf("isolation error leaked forbidden sentinel %q: %q", forbidden, text)
		}
	}
	if secondErr := session.IsolationError(); secondErr == nil || secondErr.Error() != text {
		t.Fatalf("cached isolation error changed: first=%q second=%v", text, secondErr)
	}
}

func TestValidationModeFindingSanitizesRawSensitiveInputs(t *testing.T) {
	context := validationContext(t, validationmode.Inventory{
		AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
		DisplayName: "Notepad++", InitialState: "absent",
	})
	session, err := ActivateValidationMode(context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Restore() })
	rawRegistry := `HKCU\Software\Sensitive`
	rawValueData := "value:data:secret"
	_ = session.recordIsolationFinding("capture.files[0].source", context.Root(), isolationReasonUnsafePath)
	_ = session.recordIsolationFinding("capture.registryValues[0].key", rawRegistry, isolationReasonUnsafeRegistry)
	_ = session.recordIsolationFinding("capture.registryValues[0].value", rawValueData, isolationReasonUnsafeRegistry)

	isolationErr := session.IsolationError()
	if isolationErr == nil {
		t.Fatal("raw sensitive findings did not poison the session")
	}
	text := isolationErr.Error()
	for _, forbidden := range []string{context.Root(), filepath.Base(context.Root()), rawRegistry, rawValueData} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(forbidden)) {
			t.Fatalf("isolation error leaked raw input %q: %q", forbidden, text)
		}
	}
	if count := strings.Count(text, "target=invalid-target"); count != 3 {
		t.Fatalf("raw targets were not safely tokenized: count=%d error=%q", count, text)
	}
}

func TestValidationModeSessionGuardsAuthorityPathsAndAllowsDisposableRoot(t *testing.T) {
	checkout := t.TempDir()
	githubWorkspace := t.TempDir()
	runnerWorkspace := t.TempDir()
	changeValidationTestWorkingDirectory(t, checkout)
	t.Setenv("GITHUB_WORKSPACE", githubWorkspace)
	t.Setenv("RUNNER_WORKSPACE", runnerWorkspace)
	markers := map[string]string{
		"checkout":         filepath.Join(checkout, "checkout.txt"),
		"github-workspace": filepath.Join(githubWorkspace, "github.txt"),
		"runner-workspace": filepath.Join(runnerWorkspace, "runner.txt"),
	}
	for _, marker := range markers {
		if err := os.WriteFile(marker, []byte("before"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	context := validationContext(t, validationmode.Inventory{
		AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
		DisplayName: "Notepad++", Source: "winget", InitialState: "absent",
	})
	session, err := ActivateValidationMode(context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Restore() })
	for _, marker := range markers {
		if err := os.WriteFile(marker, []byte("after"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(context.Root(), "allowed.txt"), []byte("allowed"), 0o600); err != nil {
		t.Fatal(err)
	}

	isolationErr := session.IsolationError()
	if !errors.Is(isolationErr, validationmode.ErrGuardChanged) {
		t.Fatalf("isolation error = %v, want guard change", isolationErr)
	}
	text := isolationErr.Error()
	for label := range markers {
		if !strings.Contains(text, "target="+label) {
			t.Fatalf("isolation error omitted authority label %q: %q", label, text)
		}
	}
	for _, forbidden := range []string{checkout, githubWorkspace, runnerWorkspace, context.Root(), filepath.Base(context.Root())} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(forbidden)) {
			t.Fatalf("isolation error leaked root sentinel %q: %q", forbidden, text)
		}
	}
	if err := session.Restore(); err != nil {
		t.Fatal(err)
	}
	if err := session.Restore(); err != nil {
		t.Fatalf("second Restore: %v", err)
	}
	if cached := session.IsolationError(); cached == nil || cached.Error() != text {
		t.Fatalf("Restore changed cached isolation result: first=%q second=%v", text, cached)
	}
}

func TestValidationModeSessionCompactsDuplicateAndDescendantAuthorityPaths(t *testing.T) {
	checkout := t.TempDir()
	runnerWorkspace := filepath.Join(checkout, "runner")
	if err := os.MkdirAll(runnerWorkspace, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(runnerWorkspace, "marker.txt")
	if err := os.WriteFile(marker, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	changeValidationTestWorkingDirectory(t, checkout)
	t.Setenv("GITHUB_WORKSPACE", checkout)
	t.Setenv("RUNNER_WORKSPACE", runnerWorkspace)
	context := validationContext(t, validationmode.Inventory{
		AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
		DisplayName: "Notepad++", InitialState: "absent",
	})
	session, err := ActivateValidationMode(context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Restore() })
	if err := os.WriteFile(marker, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}

	isolationErr := session.IsolationError()
	if !errors.Is(isolationErr, validationmode.ErrGuardChanged) {
		t.Fatalf("compacted authority error = %v", isolationErr)
	}
	if count := strings.Count(isolationErr.Error(), "reason=guard_changed"); count != 1 {
		t.Fatalf("compacted authority emitted %d findings: %v", count, isolationErr)
	}
	for _, forbidden := range []string{checkout, runnerWorkspace, marker, context.Root()} {
		if strings.Contains(strings.ToLower(isolationErr.Error()), strings.ToLower(forbidden)) {
			t.Fatalf("compacted authority leaked %q: %v", forbidden, isolationErr)
		}
	}
}

func TestValidationModeSessionRejectsOverlappingAuthorityRoot(t *testing.T) {
	context := validationContext(t, validationmode.Inventory{
		AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
		DisplayName: "Notepad++", InitialState: "absent",
	})
	changeValidationTestWorkingDirectory(t, context.Root())
	session, err := ActivateValidationMode(context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Restore() })

	isolationErr := session.IsolationError()
	if !errors.Is(isolationErr, validationmode.ErrUnsafePath) {
		t.Fatalf("overlap isolation error = %v, want unsafe path", isolationErr)
	}
	text := isolationErr.Error()
	for _, expected := range []string{"coordinate=authority.checkout", "target=checkout", "reason=unsafe_path"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("overlap finding omitted %q: %q", expected, text)
		}
	}
	if count := strings.Count(text, "reason=unsafe_path"); count != 1 {
		t.Fatalf("overlap emitted %d unsafe-path findings, want one: %q", count, text)
	}
	if strings.Contains(strings.ToLower(text), strings.ToLower(context.Root())) || strings.Contains(strings.ToLower(text), strings.ToLower(filepath.Base(context.Root()))) {
		t.Fatalf("overlap error leaked validation root: %q", text)
	}
	drv, err := newDriverFn()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := drv.Install("Notepad++.Notepad++"); !errors.Is(err, validationmode.ErrPackageIdentity) {
		t.Fatalf("overlap did not poison package mutation: %v", err)
	}
}

func TestValidationModeSessionFilesystemRegistrationClosesAtSeal(t *testing.T) {
	changeValidationTestWorkingDirectory(t, t.TempDir())
	context := validationContext(t, validationmode.Inventory{
		AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
		DisplayName: "Notepad++", InitialState: "absent",
	})
	session, err := ActivateValidationMode(context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Restore() })
	originalTarget := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(originalTarget, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.registerOriginalFilesystemPath("capture.files[0].source", "appdata-settings", originalTarget); err != nil {
		t.Fatalf("register before seal: %v", err)
	}
	session.sealIsolation()
	session.sealIsolation()
	lateTarget := filepath.Join(t.TempDir(), "late.json")
	if err := session.registerOriginalFilesystemPath("restore[0].target", "late-target", lateTarget); !errors.Is(err, validationmode.ErrUnsafePath) {
		t.Fatalf("late registration error = %v, want unsafe path", err)
	}
	isolationErr := session.IsolationError()
	if !errors.Is(isolationErr, validationmode.ErrUnsafePath) || !strings.Contains(isolationErr.Error(), "coordinate=restore[0].target") {
		t.Fatalf("late registration finding = %v", isolationErr)
	}
	for _, forbidden := range []string{originalTarget, lateTarget, context.Root()} {
		if strings.Contains(strings.ToLower(isolationErr.Error()), strings.ToLower(forbidden)) {
			t.Fatalf("registration error leaked path %q: %v", forbidden, isolationErr)
		}
	}
}

func TestValidationModeSessionAllowsOnlyExactRepeatedFilesystemRegistrationAfterSeal(t *testing.T) {
	context := validationContext(t, validationmode.Inventory{
		AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
		DisplayName: "Notepad++", Source: "winget", InitialState: "present",
	})
	session, err := ActivateValidationMode(context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Restore() })
	originalTarget := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(originalTarget, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.registerOriginalFilesystemPath("capture.files[0].source", "appdata-settings", originalTarget); err != nil {
		t.Fatal(err)
	}
	session.sealIsolation()

	if err := session.registerOriginalFilesystemPath("capture.files[0].source", "appdata-settings", originalTarget); err != nil {
		t.Fatalf("exact repeated registration after seal = %v", err)
	}
	if err := session.registerOriginalFilesystemPath("capture.files[0].source", "appdata-settings", filepath.Join(t.TempDir(), "different.json")); !errors.Is(err, validationmode.ErrUnsafePath) {
		t.Fatalf("changed repeated registration error = %v, want unsafe path", err)
	}
}

func TestValidationModeIsolationErrorChecksGuardsExactlyOnceAndCachesJoinedResult(t *testing.T) {
	filesystem := &countingFilesystemIsolationGuard{
		changes: []validationmode.Change{{Path: `C:\authority\secret.txt`, Kind: validationmode.ChangeContent}},
		label:   "checkout",
	}
	registry := &countingRegistryIsolationGuard{checkErr: validationmode.ErrGuardBudget}
	descriptor := validationmode.Descriptor{ScenarioID: "cache-check", ModuleID: "apps.notepad-plus-plus"}
	session := &ValidationModeSession{
		recorder:             newValidationIsolationRecorder(descriptor),
		filesystemGuard:      filesystem,
		registryGuard:        registry,
		filesystemCoordinate: map[string]string{"checkout": "authority.checkout"},
		registryCoordinate:   map[string]string{},
		restoreFn:            func() {},
	}
	_ = session.recordIsolationFinding("verify[0].path", "appdata-settings", isolationReasonUnsafePath)

	first := session.IsolationError()
	if first == nil || !errors.Is(first, validationmode.ErrUnsafePath) || !errors.Is(first, validationmode.ErrGuardChanged) || !errors.Is(first, validationmode.ErrGuardBudget) {
		t.Fatalf("joined isolation error = %v", first)
	}
	second := session.IsolationError()
	if first != second {
		t.Fatalf("IsolationError did not return the cached error: first=%p second=%p", first, second)
	}
	if filesystem.sealCalls != 1 || filesystem.checkCalls != 1 || registry.sealCalls != 1 || registry.checkCalls != 1 {
		t.Fatalf("guard calls filesystem=(seal:%d check:%d) registry=(seal:%d check:%d)", filesystem.sealCalls, filesystem.checkCalls, registry.sealCalls, registry.checkCalls)
	}
	text := first.Error()
	for _, expected := range []string{"coordinate=authority.checkout", "coordinate=guard.registry", "coordinate=verify[0].path"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("joined result omitted %q: %q", expected, text)
		}
	}
	for _, forbidden := range []string{`C:\authority\secret.txt`, `HKCU\Software\Sensitive`, "registry-value-secret"} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(forbidden)) {
			t.Fatalf("joined result leaked sentinel %q: %q", forbidden, text)
		}
	}
	if err := session.Restore(); err != nil {
		t.Fatal(err)
	}
	if err := session.Restore(); err != nil {
		t.Fatal(err)
	}
	if cached := session.IsolationError(); cached != first {
		t.Fatalf("Restore changed cached result: first=%p cached=%p", first, cached)
	}
}

type countingFilesystemIsolationGuard struct {
	sealCalls, checkCalls int
	changes               []validationmode.Change
	checkErr              error
	label                 string
}

func (*countingFilesystemIsolationGuard) Protect([]validationmode.ProtectedPath) error { return nil }
func (guard *countingFilesystemIsolationGuard) Seal()                                  { guard.sealCalls++ }
func (guard *countingFilesystemIsolationGuard) Check() ([]validationmode.Change, error) {
	guard.checkCalls++
	return guard.changes, guard.checkErr
}
func (guard *countingFilesystemIsolationGuard) Label(string) string { return guard.label }

type countingRegistryIsolationGuard struct {
	sealCalls, checkCalls int
	changes               []validationmode.RegistryChange
	checkErr              error
}

func (*countingRegistryIsolationGuard) Protect([]validationmode.ProtectedRegistry) error { return nil }
func (guard *countingRegistryIsolationGuard) Seal()                                      { guard.sealCalls++ }
func (guard *countingRegistryIsolationGuard) Check() ([]validationmode.RegistryChange, error) {
	guard.checkCalls++
	return guard.changes, guard.checkErr
}

func changeValidationTestWorkingDirectory(t *testing.T, directory string) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(directory); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
}

func TestValidationModeFindingPoisonsEveryPackageMutator(t *testing.T) {
	mutators := []struct {
		name   string
		source string
		run    func(driver.Driver) error
	}{
		{name: "install", run: func(drv driver.Driver) error { _, err := drv.Install("Notepad++.Notepad++"); return err }},
		{name: "install source", source: "winget", run: func(drv driver.Driver) error {
			_, err := drv.(driver.SourceDriver).InstallSource("Notepad++.Notepad++", "winget")
			return err
		}},
		{name: "uninstall", run: func(drv driver.Driver) error {
			_, err := drv.(driver.Uninstaller).Uninstall("Notepad++.Notepad++")
			return err
		}},
		{name: "uninstall source", source: "winget", run: func(drv driver.Driver) error {
			_, err := drv.(driver.SourceUninstaller).UninstallSource("Notepad++.Notepad++", "winget")
			return err
		}},
		{name: "install version", run: func(drv driver.Driver) error {
			_, err := drv.(driver.VersionedInstaller).InstallVersion("Notepad++.Notepad++", "8.8.2")
			return err
		}},
		{name: "reinstall version", run: func(drv driver.Driver) error {
			_, err := drv.(driver.VersionedInstaller).ReinstallVersion("Notepad++.Notepad++", "8.8.2")
			return err
		}},
		{name: "install version source", source: "winget", run: func(drv driver.Driver) error {
			_, err := drv.(driver.SourceVersionedInstaller).InstallVersionSource("Notepad++.Notepad++", "8.8.2", "winget")
			return err
		}},
		{name: "reinstall version source", source: "winget", run: func(drv driver.Driver) error {
			_, err := drv.(driver.SourceVersionedInstaller).ReinstallVersionSource("Notepad++.Notepad++", "8.8.2", "winget")
			return err
		}},
	}
	for _, mutator := range mutators {
		t.Run(mutator.name, func(t *testing.T) {
			context := validationContext(t, validationmode.Inventory{
				AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
				DisplayName: "Notepad++", Version: "8.8.2", Source: mutator.source, InitialState: "absent",
			})
			session, err := ActivateValidationMode(context)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = session.Restore() })
			_ = session.recordIsolationFinding("capture.files[0].source", "appdata-settings", isolationReasonUnsafePath)
			drv, err := newDriverFn()
			if err != nil {
				t.Fatal(err)
			}
			if err := mutator.run(drv); !errors.Is(err, validationmode.ErrPackageIdentity) {
				t.Fatalf("mutator error = %v, want package-identity rejection", err)
			}
			var present bool
			if mutator.source == "" {
				present, _, err = drv.Detect("Notepad++.Notepad++")
			} else {
				present, _, err = drv.(driver.SourceDriver).DetectSource("Notepad++.Notepad++", mutator.source)
			}
			if err != nil || present {
				t.Fatalf("poisoned session mutated package state: present=%v err=%v", present, err)
			}
		})
	}
}
