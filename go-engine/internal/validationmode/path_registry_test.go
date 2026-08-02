// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveHostPathAllowsDeclaredAliasesAndDynamicRoot(t *testing.T) {
	context := activeTestContext(t, "paths")
	appdata, _ := context.VirtualRoot("APPDATA")
	want := filepath.Join(appdata, "Vendor", "settings.json")
	got, err := context.ResolveHostPath(`%appdata%\Vendor\settings.json`, HostPathPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("resolved path = %q, want %q", got, want)
	}

	dynamic, _ := context.VirtualRoot("instance-home")
	got, err = context.ResolveHostPath(`${instance.root}\profiles\%literal%`, HostPathPolicy{DynamicRoot: "instance-home"})
	if err == nil {
		t.Fatalf("recursive dynamic expansion unexpectedly succeeded: %q", got)
	}
	got, err = context.ResolveHostPath(`${instance.root}\profiles\default`, HostPathPolicy{DynamicRoot: "instance-home"})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dynamic, "profiles", "default"); got != want {
		t.Fatalf("dynamic path = %q, want %q", got, want)
	}
	got, err = context.ResolveHostPath(`%instance-home%\profiles\default`, HostPathPolicy{})
	if err != nil || got != filepath.Join(dynamic, "profiles", "default") {
		t.Fatalf("declared dynamic alias = %q, %v", got, err)
	}

	if _, err := context.ResolveHostPath(`%APPDATA%`, HostPathPolicy{}); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("root path error = %v, want ErrUnsafePath", err)
	}
	got, err = context.ResolveHostPath(`%APPDATA%`, HostPathPolicy{AllowRoot: true})
	if err != nil || got != appdata {
		t.Fatalf("allowed root = %q, %v; want %q", got, err, appdata)
	}
}

func TestResolveHostPathAcceptsOnlyContainedConcreteInstanceRoot(t *testing.T) {
	outside := t.TempDir()
	context := activeTestContext(t, "instance-root")
	appdata, _ := context.VirtualRoot("APPDATA")
	instanceRoot := filepath.Join(appdata, "Vendor", "Profile-1")
	if err := os.MkdirAll(instanceRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := context.ResolveHostPath(`${instance.root}\settings.json`, HostPathPolicy{InstanceRoot: instanceRoot})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(instanceRoot, "settings.json"); got != want {
		t.Fatalf("resolved instance path = %q, want %q", got, want)
	}
	for _, root := range []string{outside, filepath.Join(appdata, "missing", "..", "Profile-1")} {
		if _, err := context.ResolveHostPath(`${instance.root}\settings.json`, HostPathPolicy{InstanceRoot: root}); !errors.Is(err, ErrUnsafePath) {
			t.Fatalf("instance root %q error = %v, want ErrUnsafePath", root, err)
		}
	}
}

func TestContextValidatesAndDisplaysSandboxPathsWithoutRootLeakage(t *testing.T) {
	outside := t.TempDir()
	context := activeTestContext(t, "display-path")
	appdata, _ := context.VirtualRoot("APPDATA")
	path := filepath.Join(appdata, "Vendor", "settings.json")
	if err := context.ValidateSandboxPath(path); err != nil {
		t.Fatal(err)
	}
	display, err := context.DisplayPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if display != `%APPDATA%\Vendor\settings.json` {
		t.Fatalf("display = %q", display)
	}
	if strings.Contains(strings.ToLower(display), strings.ToLower(context.Root())) {
		t.Fatalf("display leaked root: %q", display)
	}
	if err := context.ValidateSandboxPath(outside); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("outside path error = %v, want ErrUnsafePath", err)
	}
}

func TestValidateSandboxPathAllowsOnlyVirtualAndOwnedInternalTargets(t *testing.T) {
	context := activeTestContext(t, "sandbox-authority")
	appdata, _ := context.VirtualRoot("APPDATA")
	allowed := []string{
		appdata,
		filepath.Join(appdata, "Vendor", "settings.json"),
		filepath.Join(context.Root(), "manifests", "active.jsonc"),
		filepath.Join(context.Root(), "state", "journals", "run.json"),
		filepath.Join(context.Root(), "logs", "run.jsonl"),
		filepath.Join(context.Root(), ".endstate", packageStateFilename),
	}
	for _, path := range allowed {
		if err := context.ValidateSandboxPath(path); err != nil {
			t.Errorf("allowed path rejected: %v", err)
		}
	}
	rejected := []string{
		filepath.Join(context.Root(), "control", "command.json"),
		filepath.Join(context.Root(), ".endstate", descriptorFilename),
		filepath.Join(context.Root(), ".endstate", "future-control.json"),
		filepath.Join(context.Root(), "future-owned-someday", "value"),
		filepath.Join(context.Root(), "modules", "apps", "target", "module.jsonc"),
	}
	for _, path := range rejected {
		if err := context.ValidateSandboxPath(path); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("unowned path %q error = %v, want ErrUnsafePath", filepath.Base(path), err)
		}
	}
}

func TestDisplayPathPreservesDynamicAndInstanceProvenance(t *testing.T) {
	context := activeTestContext(t, "display-provenance")
	dynamic, _ := context.VirtualRoot("instance-home")
	path := filepath.Join(dynamic, "Studio One", "Profile")
	display, err := context.DisplayPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if display != `%instance-home%\Studio One\Profile` {
		t.Fatalf("dynamic display = %q", display)
	}
	instanceDisplay, err := context.DisplayHostPath(path, HostPathPolicy{InstanceRoot: filepath.Join(dynamic, "Studio One"), InstanceAlias: "instance-home"})
	if err != nil {
		t.Fatal(err)
	}
	if instanceDisplay != `${instance.root}\Profile` {
		t.Fatalf("instance display = %q", instanceDisplay)
	}
}

func TestOriginalHostPathTransfersSuffixAndProtectsWildcardParent(t *testing.T) {
	context := activeTestContext(t, "original-path")
	original := filepath.Join(canonicalTestPath(t, t.TempDir()), "host-appdata")
	if err := os.MkdirAll(original, 0o700); err != nil {
		t.Fatal(err)
	}
	context.original["APPDATA"] = originalEnvironmentValue{value: original, set: true}
	got, err := context.OriginalHostPath(`%APPDATA%\Vendor\settings.json`, HostPathPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(original, "Vendor", "settings.json"); got != want {
		t.Fatalf("original target = %q, want %q", got, want)
	}
	got, err = context.OriginalHostPath(`%APPDATA%\Vendor\profiles-[0-9]\settings.json`, HostPathPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(original, "Vendor"); got != want {
		t.Fatalf("wildcard protected target = %q, want %q", got, want)
	}
	got, err = context.OriginalHostPath(`%APPDATA%\Vendor\[0-9]\settings.json`, HostPathPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(original, "Vendor"); got != want {
		t.Fatalf("component wildcard target = %q, want %q", got, want)
	}
}

func TestOriginalHostPathTransfersConcreteInstanceProvenance(t *testing.T) {
	context := activeTestContext(t, "original-instance")
	virtual, _ := context.VirtualRoot("APPDATA")
	original := filepath.Join(canonicalTempDir(t), "host-appdata-original-instance")
	if err := os.MkdirAll(filepath.Join(original, "PreSonus", "Studio One 7"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(original) })
	context.original["APPDATA"] = originalEnvironmentValue{value: original, set: true}
	instance := filepath.Join(virtual, "PreSonus", "Studio One 7")
	if err := os.MkdirAll(instance, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := context.OriginalHostPath(`${instance.root}\Profiles\User-*\settings.xml`, HostPathPolicy{InstanceRoot: instance, InstanceAlias: "APPDATA"})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(original, "PreSonus", "Studio One 7", "Profiles"); got != want {
		t.Fatalf("instance original = %q, want %q", got, want)
	}
	if _, err := context.OriginalHostPath(`${instance.root}\settings.xml`, HostPathPolicy{InstanceRoot: instance, InstanceAlias: "LOCALAPPDATA"}); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("mismatched instance alias error = %v, want ErrUnsafePath", err)
	}
}

func TestResolveHostPathRejectsUnsafeAuthoredPaths(t *testing.T) {
	context := activeTestContext(t, "unsafe-paths")
	unsafe := []string{
		"", `.`, `relative\settings`, `C:\Users\host\settings`, `C:relative`,
		`\\server\share\settings`, `//server/share/settings`, `\\?\C:\settings`,
		`\\.\PhysicalDrive0`, `%APPDATA%\file.txt:stream`, `%APPDATA%\..\escape`,
		`%APPDATA%\\empty`, `%APPDATA%\.\dot`, `%UNKNOWN%\settings`,
		`$APPDATA/settings`, `${APPDATA}/settings`,
		`${instance.root}\settings`, `%APPDATA%\${instance.root}`,
		`%APPDATA%\%TEMP%\mixed`, `%APPDATA%\<instance>\settings`,
	}
	for _, value := range unsafe {
		t.Run(value, func(t *testing.T) {
			if _, err := context.ResolveHostPath(value, HostPathPolicy{}); !errors.Is(err, ErrUnsafePath) {
				t.Fatalf("ResolveHostPath(%q) error = %v, want ErrUnsafePath", value, err)
			}
		})
	}
	if _, err := context.ResolveHostPath(`${instance.root}\settings`, HostPathPolicy{DynamicRoot: "undeclared"}); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("undeclared dynamic root error = %v, want ErrUnsafePath", err)
	}
}

func TestResolveHostPathMapsSupportedProductionDialect(t *testing.T) {
	context := activeTestContext(t, "production-dialect")
	profile, _ := context.VirtualRoot("USERPROFILE")
	for authored, want := range map[string]string{
		`~/.config/tool/settings`: filepath.Join(profile, ".config", "tool", "settings"),
	} {
		got, err := context.ResolveHostPath(authored, HostPathPolicy{})
		if err != nil {
			t.Fatalf("ResolveHostPath(%q): %v", authored, err)
		}
		if got != want {
			t.Fatalf("ResolveHostPath(%q) = %q, want %q", authored, got, want)
		}
	}
	if _, err := context.ResolveHostPath(`C:\Program Files\Vendor\Tool.exe`, HostPathPolicy{}); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("raw Program Files error = %v, want ErrUnsafePath", err)
	}
	if _, err := context.ResolveHostPath(`D:\Program Files\Vendor\Tool.exe`, HostPathPolicy{}); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("unknown drive error = %v, want ErrUnsafePath", err)
	}
}

func TestNormalizeProductionAuthoredPathMapsTildeHomeSpellings(t *testing.T) {
	for authored, want := range map[string]string{
		`~/config/tool/settings`: `%USERPROFILE%\config/tool/settings`,
		`~\config\tool\settings`: `%USERPROFILE%\config\tool\settings`,
	} {
		if got := NormalizeProductionAuthoredPath(authored); got != want {
			t.Fatalf("NormalizeProductionAuthoredPath(%q) = %q, want %q", authored, got, want)
		}
	}
}

func TestResolveHostPathAndPortablePathRejectLinks(t *testing.T) {
	context := activeTestContext(t, "links")
	appdata, _ := context.VirtualRoot("APPDATA")
	outside := t.TempDir()
	link := filepath.Join(appdata, "linked")
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink unavailable: %v", err)
		}
		t.Fatal(err)
	}
	if _, err := context.ResolveHostPath(`%APPDATA%\linked\settings`, HostPathPolicy{}); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("host link error = %v, want ErrUnsafePath", err)
	} else if strings.Contains(strings.ToLower(err.Error()), strings.ToLower(context.Root())) || strings.Contains(strings.ToLower(err.Error()), strings.ToLower(outside)) {
		t.Fatalf("host link error leaked absolute path: %v", err)
	}

	manifestRoot := filepath.Join(context.Root(), "manifest")
	if err := os.MkdirAll(manifestRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(manifestRoot, "linked")); err != nil {
		t.Skipf("second symlink unavailable: %v", err)
	}
	if _, err := ResolvePortablePath(manifestRoot, `linked/settings.json`); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("portable link error = %v, want ErrUnsafePath", err)
	}
}

func TestResolvePortablePathRequiresCleanContainedRelativePath(t *testing.T) {
	root := t.TempDir()
	got, err := ResolvePortablePath(root, `payload/apps/notepad/settings.json`)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "payload", "apps", "notepad", "settings.json"); got != want {
		t.Fatalf("portable path = %q, want %q", got, want)
	}
	unsafe := []string{"", ".", "../escape", `payload\..\escape`, `/absolute`, `C:\absolute`, `file:stream`, `%APPDATA%/x`, `${instance.root}/x`, `a//b`}
	for _, value := range unsafe {
		if _, err := ResolvePortablePath(root, value); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("ResolvePortablePath(%q) error = %v, want ErrUnsafePath", value, err)
		}
	}
}

func TestMapHKCU(t *testing.T) {
	context := activeTestContext(t, "registry")
	want := `HKCU\Software\Endstate\Validation\registry\Software\Vendor\App`
	for _, input := range []string{`HKCU\Software\Vendor\App`, `HKCU:\Software\Vendor\App`, `HKEY_CURRENT_USER\Software\Vendor\App`, want} {
		got, err := context.MapHKCU(input)
		if err != nil {
			t.Fatalf("MapHKCU(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("MapHKCU(%q) = %q, want %q", input, got, want)
		}
	}
	unsafe := []string{
		"", "HKCU", `HKCU\`, `HKCU\\Software`, `HKCU\..\Software`, `HKCU\.\Software`,
		`HKCU\Software/Bad`, `HKCU\Software\Bad:Value`, `HKCU\Software\%APPDATA%`,
		`HKLM\Software\Vendor`, `HKCR\Thing`, `HKU\S-1-5`, `HKCC\System`,
		`HKEY_LOCAL_MACHINE\Software\Vendor`,
		`HKCU\Software\Endstate\Validation\other\Software\Vendor`,
	}
	for _, input := range unsafe {
		if _, err := context.MapHKCU(input); !errors.Is(err, ErrUnsafeRegistry) {
			t.Errorf("MapHKCU(%q) error = %v, want ErrUnsafeRegistry", input, err)
		}
	}
}

func activeTestContext(t *testing.T, nonce string) *Context {
	t.Helper()
	root := makeValidationRoot(t, nonce)
	writeDescriptor(t, root, validDescriptor(nonce))
	t.Setenv(TestModeEnvironment, "1")
	t.Setenv(RootEnvironment, root)
	context, restore, err := ActivateFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restore() })
	return context
}
