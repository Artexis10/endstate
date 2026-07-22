// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

	if _, err := context.ResolveHostPath(`%APPDATA%`, HostPathPolicy{}); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("root path error = %v, want ErrUnsafePath", err)
	}
	got, err = context.ResolveHostPath(`%APPDATA%`, HostPathPolicy{AllowRoot: true})
	if err != nil || got != appdata {
		t.Fatalf("allowed root = %q, %v; want %q", got, err, appdata)
	}
}

func TestResolveHostPathRejectsUnsafeAuthoredPaths(t *testing.T) {
	context := activeTestContext(t, "unsafe-paths")
	unsafe := []string{
		"", `.`, `relative\settings`, `C:\Users\host\settings`, `C:relative`,
		`\\server\share\settings`, `//server/share/settings`, `\\?\C:\settings`,
		`\\.\PhysicalDrive0`, `%APPDATA%\file.txt:stream`, `%APPDATA%\..\escape`,
		`%APPDATA%\\empty`, `%APPDATA%\.\dot`, `%UNKNOWN%\settings`,
		`$APPDATA/settings`, `${APPDATA}/settings`, `~/settings`,
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
	for _, input := range []string{`HKCU\Software\Vendor\App`, `HKEY_CURRENT_USER\Software\Vendor\App`, want} {
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
