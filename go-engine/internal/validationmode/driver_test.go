// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

func TestPackageDriverImplementsProductionCapabilities(t *testing.T) {
	var value any = (*PackageDriver)(nil)
	asserts := []func(any) bool{
		func(v any) bool { _, ok := v.(driver.Driver); return ok },
		func(v any) bool { _, ok := v.(driver.SourceDriver); return ok },
		func(v any) bool { _, ok := v.(driver.BatchDetector); return ok },
		func(v any) bool { _, ok := v.(driver.SourceBatchDetector); return ok },
		func(v any) bool { _, ok := v.(driver.InstalledEnumerator); return ok },
		func(v any) bool { _, ok := v.(driver.Uninstaller); return ok },
		func(v any) bool { _, ok := v.(driver.SourceUninstaller); return ok },
		func(v any) bool { _, ok := v.(driver.VersionedInstaller); return ok },
		func(v any) bool { _, ok := v.(driver.SourceVersionedInstaller); return ok },
	}
	for index, assertion := range asserts {
		if !assertion(value) {
			t.Fatalf("capability %d is not implemented", index)
		}
	}
}

func TestPackageDriverLifecycleAndPersistence(t *testing.T) {
	context := activeTestContext(t, "driver")
	packageDriver, err := context.NewPackageDriver()
	if err != nil {
		t.Fatal(err)
	}
	if packageDriver.Name() != "winget" {
		t.Fatalf("Name = %q", packageDriver.Name())
	}
	present, display, err := packageDriver.DetectSource("Notepad++.Notepad++", "winget")
	if err != nil || present || display != "" {
		t.Fatalf("initial DetectSource = %v, %q, %v", present, display, err)
	}
	result, err := packageDriver.InstallSource("Notepad++.Notepad++", "winget")
	if err != nil || result.Status != driver.StatusInstalled {
		t.Fatalf("InstallSource = %#v, %v", result, err)
	}
	result, err = packageDriver.InstallSource("Notepad++.Notepad++", "winget")
	if err != nil || result.Status != driver.StatusPresent || result.Reason != driver.ReasonAlreadyInstalled {
		t.Fatalf("repeat InstallSource = %#v, %v", result, err)
	}
	detected, err := packageDriver.DetectBatchSource([]string{"Notepad++.Notepad++"}, "winget")
	if err != nil || !detected["Notepad++.Notepad++"].Installed || detected["Notepad++.Notepad++"].Version != "8.8.1" {
		t.Fatalf("DetectBatchSource = %#v, %v", detected, err)
	}
	installed, err := packageDriver.EnumerateInstalled()
	if err != nil || len(installed) != 1 || installed[0].Source != "winget" || installed[0].DisplayName != "Notepad++" {
		t.Fatalf("EnumerateInstalled = %#v, %v", installed, err)
	}

	versioned, err := packageDriver.ReinstallVersionSource("Notepad++.Notepad++", "8.9.0", "winget")
	if err != nil || versioned.Status != driver.StatusInstalled {
		t.Fatalf("ReinstallVersionSource = %#v, %v", versioned, err)
	}
	reloaded, err := context.NewPackageDriver()
	if err != nil {
		t.Fatal(err)
	}
	detected, err = reloaded.DetectBatchSource([]string{"Notepad++.Notepad++"}, "winget")
	if err != nil || detected["Notepad++.Notepad++"].Version != "8.9.0" {
		t.Fatalf("persisted version = %#v, %v", detected, err)
	}
	uninstalled, err := reloaded.UninstallSource("Notepad++.Notepad++", "winget")
	if err != nil || uninstalled.Status != driver.StatusUninstalled {
		t.Fatalf("UninstallSource = %#v, %v", uninstalled, err)
	}
	uninstalled, err = reloaded.UninstallSource("Notepad++.Notepad++", "winget")
	if err != nil || uninstalled.Status != driver.StatusAbsent {
		t.Fatalf("repeat UninstallSource = %#v, %v", uninstalled, err)
	}
	installed, err = reloaded.EnumerateInstalled()
	if err != nil || len(installed) != 0 {
		t.Fatalf("absent enumeration = %#v, %v", installed, err)
	}
	if reloaded.ExternalCallCount() != 0 {
		t.Fatalf("external call count = %d, want zero", reloaded.ExternalCallCount())
	}
	statePath := filepath.Join(context.Root(), ".endstate", packageStateFilename)
	if info, err := os.Stat(statePath); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("state file is not regular: %v", err)
	}
}

func TestPackageDriverFailsClosedOnWrongIdentity(t *testing.T) {
	context := activeTestContext(t, "driver-identity")
	packageDriver, err := context.NewPackageDriver()
	if err != nil {
		t.Fatal(err)
	}
	checks := []func() error{
		func() error { _, _, err := packageDriver.Detect("Notepad++.Wrong"); return err },
		func() error { _, _, err := packageDriver.Detect("Notepad++.Notepad++"); return err },
		func() error { _, err := packageDriver.Install("Notepad++.Notepad++"); return err },
		func() error {
			_, err := packageDriver.DetectBatchSource([]string{"Notepad++.Notepad++", "Extra.Ref"}, "winget")
			return err
		},
		func() error {
			_, err := packageDriver.DetectBatchSource([]string{"Notepad++.Notepad++"}, "wrong")
			return err
		},
		func() error { _, err := packageDriver.InstallSource("Notepad++.Notepad++", "wrong"); return err },
		func() error {
			_, err := packageDriver.InstallVersionSource("Notepad++.Wrong", "1.0", "winget")
			return err
		},
	}
	for index, check := range checks {
		if err := check(); !errors.Is(err, ErrPackageIdentity) {
			t.Errorf("identity check %d error = %v, want ErrPackageIdentity", index, err)
		}
	}
}

func TestPackageDriverWithoutSourceUsesCoreCapabilities(t *testing.T) {
	root := makeValidationRoot(t, "driver-no-source")
	body := stringsReplaceOnce(t, validDescriptor("driver-no-source"), `,"source":"winget"`, "")
	writeDescriptor(t, root, body)
	t.Setenv(TestModeEnvironment, "1")
	t.Setenv(RootEnvironment, root)
	context, restore, err := ActivateFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restore() })
	packageDriver, err := context.NewPackageDriver()
	if err != nil {
		t.Fatal(err)
	}
	if result, err := packageDriver.Install("Notepad++.Notepad++"); err != nil || result.Status != driver.StatusInstalled {
		t.Fatalf("Install = %#v, %v", result, err)
	}
	if _, err := packageDriver.InstallVersion("Notepad++.Notepad++", "9.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := packageDriver.ReinstallVersion("Notepad++.Notepad++", "9.1"); err != nil {
		t.Fatal(err)
	}
	if _, err := packageDriver.Uninstall("Notepad++.Notepad++"); err != nil {
		t.Fatal(err)
	}
}

func stringsReplaceOnce(t *testing.T, value, old, replacement string) string {
	t.Helper()
	result := strings.Replace(value, old, replacement, 1)
	if result == value {
		t.Fatalf("fixture substring %q not found", old)
	}
	return result
}
