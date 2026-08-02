// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLiveWingetResolverRejectsUnsafeOrAmbiguousPackages(t *testing.T) {
	valid := liveWingetPackage{
		fullName: "Microsoft.DesktopAppInstaller_1.2.3.4_x64__8wekyb3d8bbwe",
		path:     `C:\Program Files\WindowsApps\Microsoft.DesktopAppInstaller_1.2.3.4_x64__8wekyb3d8bbwe`,
	}
	tests := []struct {
		name     string
		packages []liveWingetPackage
		want     liveWingetResolveFailure
	}{
		{name: "absent", want: liveWingetResolveUnavailable},
		{name: "wrong family", packages: []liveWingetPackage{{fullName: "Vendor.App_1.2.3.4_x64__8wekyb3d8bbwe", path: valid.path}}, want: liveWingetResolveIdentity},
		{name: "wrong publisher", packages: []liveWingetPackage{{fullName: "Microsoft.DesktopAppInstaller_1.2.3.4_x64__other", path: valid.path}}, want: liveWingetResolveIdentity},
		{name: "resource package", packages: []liveWingetPackage{{fullName: "Microsoft.DesktopAppInstaller_1.2.3.4_x64_resource_8wekyb3d8bbwe", path: valid.path}}, want: liveWingetResolveIdentity},
		{name: "wrong architecture", packages: []liveWingetPackage{{fullName: "Microsoft.DesktopAppInstaller_1.2.3.4_arm64__8wekyb3d8bbwe", path: valid.path}}, want: liveWingetResolveIdentity},
		{name: "framework", packages: []liveWingetPackage{{fullName: valid.fullName, path: valid.path, properties: livePackagePropertyFramework}}, want: liveWingetResolveIdentity},
		{name: "two compatible packages", packages: []liveWingetPackage{valid, valid}, want: liveWingetResolveAmbiguous},
		{name: "unsafe path", packages: []liveWingetPackage{{fullName: valid.fullName, path: `C:\Users\user\AppData\Local\Microsoft\WindowsApps\winget.exe`}}, want: liveWingetResolvePath},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := newLiveWingetResolverForTests(&fakeLiveWingetAppModel{packages: test.packages}, fakeLiveWingetTrust{}, fakeLiveWingetBinder{})
			resolver.digest = func(string) ([32]byte, error) { return [32]byte{}, nil }
			_, err := resolver.ResolveLiveWinget(context.Background())
			var failure *liveWingetResolveError
			if !errors.As(err, &failure) || failure.category != test.want {
				t.Fatalf("ResolveLiveWinget() error = %T %v, want category %q", err, err, test.want)
			}
		})
	}
}

func TestLiveWingetResolverDesktopAppInstallerSmoke(t *testing.T) {
	appmodel, err := newLiveWindowsAppModel()
	if err != nil {
		t.Fatalf("Desktop App Installer AppModel API unavailable: %v", err)
	}
	packages, err := appmodel.Packages(context.Background())
	if err != nil {
		t.Fatalf("Desktop App Installer AppModel query failed: %v", err)
	}
	if len(packages) == 0 {
		t.Skip("Desktop App Installer is not installed")
	}
	resolver, err := newLiveWingetResolver()
	if err != nil {
		t.Fatalf("newLiveWingetResolver() error = %v", err)
	}
	target, err := resolver.ResolveLiveWinget(context.Background())
	if err != nil {
		t.Fatalf("ResolveLiveWinget() error = %v", err)
	}
	path := filepath.Join(target.binding.metadata.packageRoot, target.binding.metadata.executableName)
	alias := filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WindowsApps")
	if strings.EqualFold(filepath.Dir(path), alias) || !target.binding.metadata.receipt.valid {
		t.Fatal("ResolveLiveWinget() returned an App Execution Alias or unbound target")
	}
}

func TestLiveTrustedAppXBindingRejectsChangedResolverReceipt(t *testing.T) {
	binding := liveResolvedDesktopAppInstaller(t)
	binding.metadata.receipt.indexLow++
	if bound, err := bindLiveTrustedAppXExecutable(binding); err == nil {
		bound.Close()
		t.Fatal("bindLiveTrustedAppXExecutable() accepted a changed resolver receipt")
	}
}

func TestLiveWingetResolverRetriesBoundedPackageEnumeration(t *testing.T) {
	valid := liveWingetPackage{fullName: "Microsoft.DesktopAppInstaller_1.2.3.4_x64__8wekyb3d8bbwe", path: `C:\Program Files\WindowsApps\Microsoft.DesktopAppInstaller_1.2.3.4_x64__8wekyb3d8bbwe`}
	api := &fakeLiveWingetAppModel{packages: []liveWingetPackage{valid}, resizeOnce: true}
	resolver := newLiveWingetResolverForTests(api, fakeLiveWingetTrust{}, fakeLiveWingetBinder{})
	resolver.digest = func(string) ([32]byte, error) { return [32]byte{}, nil }
	if _, err := resolver.ResolveLiveWinget(context.Background()); err != nil {
		t.Fatalf("ResolveLiveWinget() error = %v", err)
	}
	if api.enumerations != 2 {
		t.Fatalf("package enumeration attempts = %d, want 2", api.enumerations)
	}
}

func TestLiveWingetResolverRejectsTrustAndBindingFailures(t *testing.T) {
	valid := liveWingetPackage{fullName: "Microsoft.DesktopAppInstaller_1.2.3.4_x64__8wekyb3d8bbwe", path: `C:\Program Files\WindowsApps\Microsoft.DesktopAppInstaller_1.2.3.4_x64__8wekyb3d8bbwe`}
	for _, test := range []struct {
		name  string
		trust fakeLiveWingetTrust
		bind  fakeLiveWingetBinder
		want  liveWingetResolveFailure
	}{
		{name: "unsigned", trust: fakeLiveWingetTrust{err: errors.New("unsigned")}, want: liveWingetResolveTrust},
		{name: "wrong package origin", bind: fakeLiveWingetBinder{err: errors.New("alias")}, want: liveWingetResolveBinding},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolver := newLiveWingetResolverForTests(&fakeLiveWingetAppModel{packages: []liveWingetPackage{valid}}, test.trust, test.bind)
			resolver.digest = func(string) ([32]byte, error) { return [32]byte{}, nil }
			_, err := resolver.ResolveLiveWinget(context.Background())
			var failure *liveWingetResolveError
			if !errors.As(err, &failure) || failure.category != test.want {
				t.Fatalf("ResolveLiveWinget() error = %T %v, want category %q", err, err, test.want)
			}
		})
	}
}

type fakeLiveWingetAppModel struct {
	packages     []liveWingetPackage
	err          error
	resizeOnce   bool
	enumerations int
}

func (fake *fakeLiveWingetAppModel) Packages(context.Context) ([]liveWingetPackage, error) {
	fake.enumerations++
	if fake.err != nil {
		return nil, fake.err
	}
	if fake.resizeOnce && fake.enumerations == 1 {
		return nil, errLiveWingetResize
	}
	return append([]liveWingetPackage(nil), fake.packages...), nil
}

type fakeLiveWingetTrust struct{ err error }

func (fake fakeLiveWingetTrust) Verify(string) error { return fake.err }

type fakeLiveWingetBinder struct{ err error }

func (fake fakeLiveWingetBinder) Bind(binding liveTrustedAppXBinding) (*liveWindowsExecutableBinding, error) {
	if fake.err != nil {
		return nil, fake.err
	}
	return &liveWindowsExecutableBinding{path: binding.metadata.packageRoot + `\winget.exe`}, nil
}
