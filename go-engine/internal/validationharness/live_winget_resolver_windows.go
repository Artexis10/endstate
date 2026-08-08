// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	liveWingetPackageFamily      = "Microsoft.DesktopAppInstaller_8wekyb3d8bbwe"
	liveWingetPackageName        = "Microsoft.DesktopAppInstaller"
	liveWingetPublisherID        = "8wekyb3d8bbwe"
	livePackageFilterHeadDirect  = 0x00000010 | 0x00000020
	livePackagePropertyFramework = 0x00000001
	livePackagePropertyResource  = 0x00000002
	livePackagePropertyBundle    = 0x00000004
	livePackagePropertyOptional  = 0x00000008
	livePackagePropertyDeveloper = 0x00010000
	liveWingetMaxPackages        = 8
	liveWingetMaxNameUTF16       = 1024
	liveWingetMaxPathUTF16       = 32768
	liveWingetEnumerationRetries = 3
)

var errLiveWingetResize = errors.New("AppModel package list resized")

type liveWingetResolveFailure string

const (
	liveWingetResolveUnavailable liveWingetResolveFailure = "unavailable"
	liveWingetResolveIdentity    liveWingetResolveFailure = "identity"
	liveWingetResolveAmbiguous   liveWingetResolveFailure = "ambiguous"
	liveWingetResolvePath        liveWingetResolveFailure = "path"
	liveWingetResolveTrust       liveWingetResolveFailure = "trust"
	liveWingetResolveBinding     liveWingetResolveFailure = "binding"
)

// liveWingetResolveError deliberately exposes only a stable internal category.
// Package paths, certificate details, and raw AppModel errors stay private.
type liveWingetResolveError struct{ category liveWingetResolveFailure }

func (err *liveWingetResolveError) Error() string {
	return "trusted winget resolver " + string(err.category)
}

type liveWingetPackage struct {
	fullName, path string
	properties     uint32
}

type liveWingetAppModel interface {
	Packages(context.Context) ([]liveWingetPackage, error)
}

type liveWingetTrust interface{ Verify(string) error }

type liveWingetBinder interface {
	Bind(liveTrustedAppXBinding) (*liveWindowsExecutableBinding, error)
}

type liveWingetResolver struct {
	appmodel liveWingetAppModel
	trust    liveWingetTrust
	binder   liveWingetBinder
	digest   func(string) ([32]byte, error)
}

func newLiveWingetResolver() (liveTrustedWingetResolver, error) {
	appmodel, err := newLiveWindowsAppModel()
	if err != nil {
		return nil, &liveWingetResolveError{category: liveWingetResolveUnavailable}
	}
	return newLiveWingetResolverForTests(appmodel, liveWindowsWingetTrust{}, liveWindowsWingetBinder{}), nil
}

func newLiveWingetResolverForTests(appmodel liveWingetAppModel, trust liveWingetTrust, binder liveWingetBinder) *liveWingetResolver {
	return &liveWingetResolver{appmodel: appmodel, trust: trust, binder: binder, digest: liveWindowsFileSHA256}
}

func (resolver *liveWingetResolver) ResolveLiveWinget(ctx context.Context) (liveTrustedWingetTarget, error) {
	if resolver == nil || resolver.appmodel == nil || resolver.trust == nil || resolver.binder == nil || resolver.digest == nil || ctx == nil {
		return liveTrustedWingetTarget{}, &liveWingetResolveError{category: liveWingetResolveUnavailable}
	}
	var packages []liveWingetPackage
	var err error
	for attempt := 0; attempt < liveWingetEnumerationRetries; attempt++ {
		if err = ctx.Err(); err != nil {
			return liveTrustedWingetTarget{}, &liveWingetResolveError{category: liveWingetResolveUnavailable}
		}
		packages, err = resolver.appmodel.Packages(ctx)
		if !errors.Is(err, errLiveWingetResize) {
			break
		}
	}
	if err != nil {
		return liveTrustedWingetTarget{}, &liveWingetResolveError{category: liveWingetResolveUnavailable}
	}
	if len(packages) == 0 {
		return liveTrustedWingetTarget{}, &liveWingetResolveError{category: liveWingetResolveUnavailable}
	}
	if len(packages) > liveWingetMaxPackages {
		return liveTrustedWingetTarget{}, &liveWingetResolveError{category: liveWingetResolveAmbiguous}
	}
	candidates := make([]liveWingetPackage, 0, len(packages))
	for _, candidate := range packages {
		if !validLiveWingetText(candidate.fullName, liveWingetMaxNameUTF16) || !validLiveWingetText(candidate.path, liveWingetMaxPathUTF16) {
			return liveTrustedWingetTarget{}, &liveWingetResolveError{category: liveWingetResolveIdentity}
		}
		identity, ok := parseLiveWingetPackageIdentity(candidate.fullName)
		if !ok || identity.name != liveWingetPackageName || identity.publisherID != liveWingetPublisherID || identity.familyName != liveWingetPackageFamily || identity.architecture != liveWingetCurrentArchitecture() || identity.resourceID != "" || candidate.properties&(livePackagePropertyFramework|livePackagePropertyResource|livePackagePropertyBundle|livePackagePropertyOptional|livePackagePropertyDeveloper) != 0 {
			return liveTrustedWingetTarget{}, &liveWingetResolveError{category: liveWingetResolveIdentity}
		}
		if !liveWingetPackagePathIsExact(candidate.path, candidate.fullName) {
			return liveTrustedWingetTarget{}, &liveWingetResolveError{category: liveWingetResolvePath}
		}
		candidates = append(candidates, candidate)
	}
	if len(candidates) != 1 {
		return liveTrustedWingetTarget{}, &liveWingetResolveError{category: liveWingetResolveAmbiguous}
	}
	metadata := liveAppXPackageMetadata{familyName: liveWingetPackageFamily, fullName: candidates[0].fullName, packageRoot: candidates[0].path, executableName: "winget.exe"}
	trusted, err := newLiveTrustedAppXBinding(metadata)
	if err != nil {
		return liveTrustedWingetTarget{}, &liveWingetResolveError{category: liveWingetResolvePath}
	}
	bound, err := resolver.binder.Bind(trusted)
	if err != nil {
		return liveTrustedWingetTarget{}, &liveWingetResolveError{category: liveWingetResolveBinding}
	}
	defer bound.Close()
	if err := resolver.trust.Verify(bound.path); err != nil {
		return liveTrustedWingetTarget{}, &liveWingetResolveError{category: liveWingetResolveTrust}
	}
	digest, err := resolver.digest(bound.path)
	if err != nil {
		return liveTrustedWingetTarget{}, &liveWingetResolveError{category: liveWingetResolveBinding}
	}
	trusted.metadata.receipt = liveTrustedAppXReceipt{volume: bound.identity.volume, indexHigh: bound.identity.indexHigh, indexLow: bound.identity.indexLow, sha256: digest, valid: true}
	environment, err := liveTrustedWingetEnvironment()
	if err != nil {
		return liveTrustedWingetTarget{}, &liveWingetResolveError{category: liveWingetResolveUnavailable}
	}
	return liveTrustedWingetTarget{binding: trusted, environment: environment}, nil
}

func liveWingetCurrentArchitecture() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "386":
		return "x86"
	default:
		return runtime.GOARCH
	}
}

type liveWingetPackageIdentity struct {
	name, familyName, publisherID, architecture, resourceID string
}

func parseLiveWingetPackageIdentity(fullName string) (liveWingetPackageIdentity, bool) {
	parts := strings.Split(fullName, "_")
	if len(parts) != 5 || parts[0] == "" || parts[1] == "" || parts[2] == "" || parts[4] == "" || !validLiveWingetVersion(parts[1]) {
		return liveWingetPackageIdentity{}, false
	}
	architecture := parts[2]
	if architecture != "x64" && architecture != "x86" && architecture != "arm64" && architecture != "arm" {
		return liveWingetPackageIdentity{}, false
	}
	return liveWingetPackageIdentity{name: parts[0], familyName: parts[0] + "_" + parts[4], publisherID: parts[4], architecture: architecture, resourceID: parts[3]}, true
}

func validLiveWingetVersion(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 4 {
		return false
	}
	for _, part := range parts {
		if part == "" || len(part) > 5 {
			return false
		}
		for _, character := range part {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
}

func validLiveWingetText(value string, limit int) bool {
	if value == "" || len([]rune(value)) > limit || strings.ContainsRune(value, '\ufffd') {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func liveWingetPackagePathIsExact(path, fullName string) bool {
	appsRoot, err := liveWindowsAppsRoot()
	return err == nil && filepath.Clean(path) == path && filepath.Base(path) == fullName && strings.EqualFold(filepath.Dir(path), appsRoot)
}

func liveTrustedWingetEnvironment() (map[string]string, error) {
	environment := make(map[string]string)
	for _, key := range []string{"SYSTEMROOT", "WINDIR", "COMSPEC", "APPDATA", "LOCALAPPDATA", "TEMP", "TMP", "USERPROFILE", "PATHEXT"} {
		if value := os.Getenv(key); value != "" {
			if !validLiveProcessEnvironmentValue(value) {
				return nil, errors.New("unsafe environment")
			}
			environment[key] = value
		}
	}
	return environment, nil
}

type liveWindowsAppModel struct{ findPackagesByPackageFamily *windows.LazyProc }

func newLiveWindowsAppModel() (*liveWindowsAppModel, error) {
	procedure := windows.NewLazySystemDLL("kernel32.dll").NewProc("FindPackagesByPackageFamily")
	if err := procedure.Find(); err != nil {
		return nil, err
	}
	return &liveWindowsAppModel{findPackagesByPackageFamily: procedure}, nil
}

func (appmodel *liveWindowsAppModel) Packages(ctx context.Context) ([]liveWingetPackage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	family, err := windows.UTF16PtrFromString(liveWingetPackageFamily)
	if err != nil {
		return nil, err
	}
	var count, length uint32
	code, _, _ := appmodel.findPackagesByPackageFamily.Call(uintptr(unsafe.Pointer(family)), livePackageFilterHeadDirect, uintptr(unsafe.Pointer(&count)), 0, uintptr(unsafe.Pointer(&length)), 0, 0)
	if status := int32(code); status != 0 && status != int32(windows.ERROR_INSUFFICIENT_BUFFER) {
		return nil, syscall.Errno(status)
	}
	if count == 0 && length == 0 {
		return nil, nil
	}
	if count == 0 || count > liveWingetMaxPackages || length == 0 || length > liveWingetMaxPackages*liveWingetMaxNameUTF16 {
		return nil, errors.New("AppModel package result exceeds bounds")
	}
	for attempt := 0; attempt < liveWingetEnumerationRetries; attempt++ {
		namePointers := make([]*uint16, count)
		buffer := make([]uint16, length)
		properties := make([]uint32, count)
		requestedCount, requestedLength := count, length
		code, _, _ = appmodel.findPackagesByPackageFamily.Call(uintptr(unsafe.Pointer(family)), livePackageFilterHeadDirect, uintptr(unsafe.Pointer(&requestedCount)), uintptr(unsafe.Pointer(&namePointers[0])), uintptr(unsafe.Pointer(&requestedLength)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&properties[0])))
		if int32(code) == int32(windows.ERROR_INSUFFICIENT_BUFFER) {
			if requestedCount == 0 || requestedCount > liveWingetMaxPackages || requestedLength == 0 || requestedLength > liveWingetMaxPackages*liveWingetMaxNameUTF16 {
				return nil, errors.New("AppModel resize exceeds bounds")
			}
			count, length = requestedCount, requestedLength
			continue
		}
		if int32(code) != 0 || requestedCount != count || requestedLength > length {
			return nil, errors.New("AppModel package enumeration failed")
		}
		packages := make([]liveWingetPackage, 0, count)
		for index, pointer := range namePointers {
			name, ok := liveWingetNameFromBuffer(pointer, buffer)
			if !ok {
				return nil, errors.New("AppModel package name is unsafe")
			}
			path, err := appmodel.packagePath(name)
			if err != nil {
				return nil, err
			}
			packages = append(packages, liveWingetPackage{fullName: name, path: path, properties: properties[index]})
		}
		return packages, nil
	}
	return nil, errLiveWingetResize
}

func liveWingetNameFromBuffer(pointer *uint16, buffer []uint16) (string, bool) {
	if pointer == nil || len(buffer) == 0 {
		return "", false
	}
	base := uintptr(unsafe.Pointer(&buffer[0]))
	address := uintptr(unsafe.Pointer(pointer))
	if address < base || (address-base)%unsafe.Sizeof(buffer[0]) != 0 {
		return "", false
	}
	start := int((address - base) / unsafe.Sizeof(buffer[0]))
	if start >= len(buffer) {
		return "", false
	}
	end := start
	for end < len(buffer) && buffer[end] != 0 {
		end++
	}
	if end == len(buffer) {
		return "", false
	}
	value := windows.UTF16ToString(buffer[start:end])
	return value, validLiveWingetText(value, liveWingetMaxNameUTF16)
}

func (appmodel *liveWindowsAppModel) packagePath(fullName string) (string, error) {
	procedure := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetPackagePathByFullName")
	if err := procedure.Find(); err != nil {
		return "", err
	}
	name, err := windows.UTF16PtrFromString(fullName)
	if err != nil {
		return "", err
	}
	var length uint32
	code, _, _ := procedure.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&length)), 0)
	if int32(code) != int32(windows.ERROR_INSUFFICIENT_BUFFER) || length == 0 || length > liveWingetMaxPathUTF16 {
		return "", fmt.Errorf("AppModel package path unavailable")
	}
	buffer := make([]uint16, length)
	code, _, _ = procedure.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&length)), uintptr(unsafe.Pointer(&buffer[0])))
	if int32(code) != 0 || length == 0 || length > uint32(len(buffer)) {
		return "", errors.New("AppModel package path failed")
	}
	value := windows.UTF16ToString(buffer[:length-1])
	if !validLiveWingetText(value, liveWingetMaxPathUTF16) {
		return "", errors.New("AppModel package path is unsafe")
	}
	return value, nil
}

type liveWindowsWingetBinder struct{}

func (liveWindowsWingetBinder) Bind(binding liveTrustedAppXBinding) (*liveWindowsExecutableBinding, error) {
	return bindLiveTrustedAppXExecutableUnverified(binding)
}

type liveWindowsWingetTrust struct{}

func (liveWindowsWingetTrust) Verify(path string) error {
	wide, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	file := windows.WinTrustFileInfo{Size: uint32(unsafe.Sizeof(windows.WinTrustFileInfo{})), FilePath: wide}
	data := windows.WinTrustData{Size: uint32(unsafe.Sizeof(windows.WinTrustData{})), UIChoice: windows.WTD_UI_NONE, RevocationChecks: windows.WTD_REVOKE_WHOLECHAIN, UnionChoice: windows.WTD_CHOICE_FILE, FileOrCatalogOrBlobOrSgnrOrCert: unsafe.Pointer(&file), StateAction: windows.WTD_STATEACTION_VERIFY, ProvFlags: windows.WTD_REVOCATION_CHECK_CHAIN_EXCLUDE_ROOT | windows.WTD_CACHE_ONLY_URL_RETRIEVAL | windows.WTD_DISABLE_MD2_MD4}
	verifyErr := windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, &data)
	data.StateAction = windows.WTD_STATEACTION_CLOSE
	closeErr := windows.WinVerifyTrustEx(windows.InvalidHWND, &windows.WINTRUST_ACTION_GENERIC_VERIFY_V2, &data)
	if verifyErr != nil {
		return verifyErr
	}
	return closeErr
}
