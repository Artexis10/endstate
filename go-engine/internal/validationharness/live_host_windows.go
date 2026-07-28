// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

var (
	liveVersionDLL              = syscall.NewLazyDLL("version.dll")
	liveGetFileVersionInfoSizeW = liveVersionDLL.NewProc("GetFileVersionInfoSizeW")
	liveGetFileVersionInfoW     = liveVersionDLL.NewProc("GetFileVersionInfoW")
	liveVerQueryValueW          = liveVersionDLL.NewProc("VerQueryValueW")
)

type windowsLiveVersionSource struct{}

type liveVSFixedFileInfo struct {
	Signature, StructVersion, FileVersionMS, FileVersionLS uint32
	ProductVersionMS, ProductVersionLS, FileFlagsMask      uint32
	FileFlags, FileOS, FileType, FileSubtype               uint32
	FileDateMS, FileDateLS                                 uint32
}

func (windowsLiveVersionSource) FileVersion(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("Windows file version path is empty")
	}
	encoded, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", fmt.Errorf("Windows file version path is invalid")
	}
	var ignored uint32
	size, _, callErr := liveGetFileVersionInfoSizeW.Call(uintptr(unsafe.Pointer(encoded)), uintptr(unsafe.Pointer(&ignored)))
	if size == 0 || callErr != syscall.Errno(0) || size > maxLiveObserverOutputBytes {
		return "", fmt.Errorf("Windows file version resource is unavailable")
	}
	buffer := make([]byte, size)
	ok, _, callErr := liveGetFileVersionInfoW.Call(uintptr(unsafe.Pointer(encoded)), 0, size, uintptr(unsafe.Pointer(&buffer[0])))
	if ok == 0 || callErr != syscall.Errno(0) {
		return "", fmt.Errorf("Windows file version resource cannot be read")
	}
	root, err := syscall.UTF16PtrFromString(`\`)
	if err != nil {
		return "", fmt.Errorf("Windows file version query is invalid")
	}
	var value unsafe.Pointer
	var length uint32
	ok, _, callErr = liveVerQueryValueW.Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(root)), uintptr(unsafe.Pointer(&value)), uintptr(unsafe.Pointer(&length)))
	if ok == 0 || callErr != syscall.Errno(0) || value == nil || length < uint32(unsafe.Sizeof(liveVSFixedFileInfo{})) {
		return "", fmt.Errorf("Windows file fixed version is unavailable")
	}
	fixed := (*liveVSFixedFileInfo)(value)
	if fixed.Signature != 0xFEEF04BD {
		return "", fmt.Errorf("Windows file fixed version is invalid")
	}
	return fmt.Sprintf("%d.%d.%d.%d", fixed.FileVersionMS>>16, fixed.FileVersionMS&0xffff, fixed.FileVersionLS>>16, fixed.FileVersionLS&0xffff), nil
}

const (
	liveUninstallKey       = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`
	liveMachineEnvironment = `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`
	liveSessionManagerKey  = `SYSTEM\CurrentControlSet\Control\Session Manager`
	liveUserEnvironment    = `Environment`

	// WinGet's reviewed no-package HRESULT, returned as signed process status.
	// This numeric contract is the only non-zero result that proves absence.
	liveWingetNoInstalledExitCode = -1978335212
	liveWingetNoInstalledHRESULT  = uint32(0x8A150014)
)

// LiveVersionSource isolates native file-version extraction. Callers may use a
// reviewed Windows implementation without making observer tests host-dependent.
type LiveVersionSource interface {
	FileVersion(string) (string, error)
}

// NewWindowsLiveObserver provides the read-only Windows host adapters. A
// version source is injected because version-resource policy is separate from
// registry, process, and file observation.
func NewWindowsLiveObserver(versions LiveVersionSource) (LiveObserver, error) {
	if versions == nil {
		versions = windowsLiveVersionSource{}
	}
	resolver, err := newWindowsLiveWingetResolver()
	if err != nil {
		return LiveObserver{}, fmt.Errorf("live observer trusted winget resolver unavailable")
	}
	return LiveObserver{
		Process:  windowsLiveProcess{resolver: resolver},
		Registry: windowsLiveRegistry{},
		Path:     windowsLivePath{},
		Files:    windowsLiveFiles{versions: versions},
	}, nil
}

var newWindowsLiveWingetResolver = newLiveWingetResolver

// liveTrustedWingetResolver must resolve the genuine App Installer executable
// from reviewed package metadata. It must never return an app-execution alias,
// PATH result, or reparse point. This slice has no such resolver yet, so the
// production adapter uses the fail-closed implementation below.
type liveTrustedWingetResolver interface {
	ResolveLiveWinget(context.Context) (liveTrustedWingetTarget, error)
}

type liveTrustedWingetTarget struct {
	binding     liveTrustedAppXBinding
	environment map[string]string
}

type windowsLiveProcess struct{ resolver liveTrustedWingetResolver }

func (process windowsLiveProcess) Run(ctx context.Context, name string, args ...string) (LiveProcessResult, error) {
	if name != "winget" || process.resolver == nil {
		return LiveProcessResult{}, fmt.Errorf("live process rejects untrusted executable selection")
	}
	target, err := process.resolver.ResolveLiveWinget(ctx)
	if err != nil {
		return LiveProcessResult{}, fmt.Errorf("live process trusted resolver unavailable")
	}
	ref, ok := liveWingetListProbeReference(args)
	if !ok {
		return LiveProcessResult{}, fmt.Errorf("live process rejects an unreviewed winget operation")
	}
	nonce, err := newLiveReceiptNonce()
	if err != nil {
		return LiveProcessResult{}, fmt.Errorf("live process receipt nonce unavailable")
	}
	issuer := newLiveReceiptIssuer()
	admission, err := issuer.admit(liveOperationWingetExactList, 1, nonce)
	if err != nil {
		return LiveProcessResult{}, fmt.Errorf("live process receipt admission rejected")
	}
	receipt, err := runLiveProcess(ctx, newLiveTrustedAppXWingetListProbe(admission, target.binding, ref, target.environment, maxLiveObserverOutputBytes))
	if err == nil {
		result, handoffErr := classifyWingetListReceipt(receipt, ref, 1, nonce)
		if handoffErr != nil {
			return LiveProcessResult{}, fmt.Errorf("live process receipt handoff rejected")
		}
		return result, nil
	}
	var execution *LiveExecutionError
	if errors.As(err, &execution) && execution.Code == LiveExecutionProcessExit {
		result := LiveProcessResult{ExitCode: receipt.exitCode, Classification: classifyLiveWingetExitCode(receipt.exitCode)}
		return result, nil
	}
	if execution != nil {
		return LiveProcessResult{}, fmt.Errorf("live process execution %s", execution.Code)
	}
	return LiveProcessResult{}, fmt.Errorf("live process execution failed")
}

func classifyLiveWingetExitCode(exitCode int) LiveProcessClassification {
	if exitCode == 0 {
		return LiveProcessCompleted
	}
	if uint32(exitCode) == liveWingetNoInstalledHRESULT {
		return LiveProcessNoInstalled
	}
	return ""
}

type windowsLiveRegistry struct{}

func (windowsLiveRegistry) UninstallRecords(context.Context) ([]LiveUninstallRecord, error) {
	views := []struct {
		view   LiveRegistryView
		hive   registry.Key
		access uint32
	}{
		{LiveRegistryHKLM64, registry.LOCAL_MACHINE, registry.READ | registry.WOW64_64KEY},
		{LiveRegistryHKLM32, registry.LOCAL_MACHINE, registry.READ | registry.WOW64_32KEY},
		{LiveRegistryHKCU, registry.CURRENT_USER, registry.READ},
	}
	var records []LiveUninstallRecord
	for _, view := range views {
		entries, err := readWindowsLiveUninstallView(view.hive, view.access, view.view)
		if err != nil {
			return nil, err
		}
		records = append(records, entries...)
		if len(records) > maxLiveObserverRecords {
			return nil, fmt.Errorf("live uninstall records exceed bound")
		}
	}
	return records, nil
}

func readWindowsLiveUninstallView(hive registry.Key, access uint32, view LiveRegistryView) ([]LiveUninstallRecord, error) {
	root, err := registry.OpenKey(hive, liveUninstallKey, access)
	if err == registry.ErrNotExist {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := root.Stat()
	if err != nil {
		return nil, err
	}
	if info.SubKeyCount > maxLiveObserverRecords {
		return nil, fmt.Errorf("live uninstall records exceed bound")
	}
	names, err := root.ReadSubKeyNames(int(info.SubKeyCount))
	if err != nil && err != io.EOF {
		return nil, err
	}
	records := make([]LiveUninstallRecord, 0, len(names))
	for _, name := range names {
		if !validLiveObserverValue(name) {
			return nil, fmt.Errorf("live uninstall key name is unsafe")
		}
		key, openErr := registry.OpenKey(root, name, access)
		if openErr != nil {
			return nil, openErr
		}
		record, readErr := readWindowsLiveUninstallRecord(key, view, name)
		key.Close()
		if readErr != nil {
			return nil, readErr
		}
		records = append(records, record)
	}
	return records, nil
}

func readWindowsLiveUninstallRecord(key registry.Key, view LiveRegistryView, keyIdentity string) (LiveUninstallRecord, error) {
	read := func(name string) (string, error) {
		value, _, err := key.GetStringValue(name)
		if err == registry.ErrNotExist {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		if value != "" && !validLiveObserverValue(value) {
			return "", fmt.Errorf("live uninstall value is unsafe")
		}
		return value, nil
	}
	name, err := read("DisplayName")
	if err != nil {
		return LiveUninstallRecord{}, err
	}
	version, err := read("DisplayVersion")
	if err != nil {
		return LiveUninstallRecord{}, err
	}
	location, err := read("InstallLocation")
	if err != nil {
		return LiveUninstallRecord{}, err
	}
	icon, err := read("DisplayIcon")
	if err != nil {
		return LiveUninstallRecord{}, err
	}
	publisher, err := read("Publisher")
	if err != nil {
		return LiveUninstallRecord{}, err
	}
	uninstall, err := read("UninstallString")
	if err != nil {
		return LiveUninstallRecord{}, err
	}
	return LiveUninstallRecord{View: view, KeyIdentity: keyIdentity, DisplayName: name, DisplayVersion: version, InstallLocation: location, DisplayIcon: icon, Publisher: publisher, UninstallString: uninstall}, nil
}

type windowsLivePath struct{}

func (windowsLivePath) MachineAndUserPath(context.Context) ([]string, error) {
	machine, err := readWindowsLivePath(registry.LOCAL_MACHINE, liveMachineEnvironment, registry.READ|registry.WOW64_64KEY)
	if err != nil {
		return nil, err
	}
	user, err := readWindowsLivePath(registry.CURRENT_USER, liveUserEnvironment, registry.READ)
	if err != nil {
		return nil, err
	}
	values := append(machine, user...)
	if len(values) > maxLiveObserverRecords {
		return nil, fmt.Errorf("live PATH entries exceed bound")
	}
	return values, nil
}

func readWindowsLivePath(hive registry.Key, location string, access uint32) ([]string, error) {
	key, err := registry.OpenKey(hive, location, access)
	if err == registry.ErrNotExist {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer key.Close()
	value, valueType, err := key.GetStringValue("Path")
	if err == registry.ErrNotExist || value == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if valueType == registry.EXPAND_SZ {
		value, err = registry.ExpandString(value)
		if err != nil {
			return nil, err
		}
	}
	if len(value) > maxLiveObserverOutputBytes || !validLiveObserverText(value) {
		return nil, fmt.Errorf("live PATH is unsafe")
	}
	entries := strings.Split(value, ";")
	if len(entries) > maxLiveObserverRecords {
		return nil, fmt.Errorf("live PATH entries exceed bound")
	}
	return entries, nil
}

type windowsLiveFiles struct{ versions LiveVersionSource }

func (files windowsLiveFiles) Stat(path string) (LiveFileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return LiveFileInfo{}, err
	}
	reparse := info.Mode()&os.ModeSymlink != 0
	if data, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		reparse = reparse || data.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
	}
	return LiveFileInfo{Regular: info.Mode().IsRegular(), ReparsePoint: reparse}, nil
}

func (files windowsLiveFiles) FileVersion(path string) (string, error) {
	return files.versions.FileVersion(path)
}

func runWindowsLiveWingetExactUninstall(ctx context.Context, admission liveReceiptAdmission, permit trustedLiveMutationPermit, outputLimit int) (*liveExecutionReceipt, error) {
	return runWindowsLiveWingetMutation(ctx, admission, permit, liveOperationWingetExactUninstall, outputLimit)
}

func runWindowsLiveWingetExactInstall(ctx context.Context, admission liveReceiptAdmission, permit trustedLiveMutationPermit, outputLimit int) (*liveExecutionReceipt, error) {
	return runWindowsLiveWingetMutation(ctx, admission, permit, liveOperationWingetExactInstall, outputLimit)
}

func runWindowsLiveWingetMutation(ctx context.Context, admission liveReceiptAdmission, permit trustedLiveMutationPermit, operation liveOperation, outputLimit int) (*liveExecutionReceipt, error) {
	resolver, err := newWindowsLiveWingetResolver()
	if err != nil {
		return nil, fmt.Errorf("trusted winget resolver unavailable")
	}
	target, err := resolver.ResolveLiveWinget(ctx)
	if err != nil {
		return nil, fmt.Errorf("trusted winget target unavailable")
	}
	switch operation {
	case liveOperationWingetExactUninstall:
		return runLiveProcess(ctx, newLiveTrustedAppXWingetExactUninstall(admission, permit, target.binding, outputLimit))
	case liveOperationWingetExactInstall:
		return runLiveProcess(ctx, newLiveTrustedAppXWingetExactInstall(admission, permit, target.binding, outputLimit))
	default:
		return nil, fmt.Errorf("trusted winget operation is invalid")
	}
}

type windowsLiveBoundaryReader struct {
	observer LiveObserver
	appData  string
}

func (reader windowsLiveBoundaryReader) Observe(ctx context.Context, definition LiveObserverDefinition) LiveObservation {
	return reader.observer.Observe(ctx, definition)
}

func (reader windowsLiveBoundaryReader) Target(_ context.Context, target LiveDeclaredTarget) (liveBoundaryTargetState, error) {
	if err := safepath.ValidateRoot(reader.appData); err != nil {
		return liveBoundaryTargetState{}, err
	}
	relative, ok := liveAppDataTargetRelative(target.Template)
	if !ok {
		return liveBoundaryTargetState{}, fmt.Errorf("live declared target is outside APPDATA")
	}
	path, err := safepath.Resolve(reader.appData, relative)
	if err != nil {
		return liveBoundaryTargetState{}, err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return liveBoundaryTargetState{kind: target.Kind}, nil
	}
	if err != nil || safepath.IsLinkOrReparse(info) || target.Kind == LiveDeclaredTargetFile && !info.Mode().IsRegular() || target.Kind == LiveDeclaredTargetDirectory && !info.IsDir() {
		return liveBoundaryTargetState{}, fmt.Errorf("live declared target is unsafe")
	}
	return liveBoundaryTargetState{present: true, kind: target.Kind}, nil
}

func (windowsLiveBoundaryReader) Services(context.Context) ([]string, error) {
	services, _, err := windowsLiveServiceAndDriverNames()
	return services, err
}

func (windowsLiveBoundaryReader) Drivers(context.Context) ([]string, error) {
	_, drivers, err := windowsLiveServiceAndDriverNames()
	return drivers, err
}

func (windowsLiveBoundaryReader) Tasks(context.Context) ([]string, error) {
	return windowsLiveRegistryTree(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Schedule\TaskCache\Tree`, registry.READ|registry.WOW64_64KEY)
}

func (windowsLiveBoundaryReader) PendingReboot(context.Context) ([]string, error) {
	var indicators []string
	for _, location := range []string{
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based Servicing\RebootPending`,
		`SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update\RebootRequired`,
	} {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, location, registry.READ|registry.WOW64_64KEY)
		if err == registry.ErrNotExist {
			continue
		}
		if err != nil {
			return nil, err
		}
		key.Close()
		indicators = append(indicators, location)
	}
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, liveSessionManagerKey, registry.READ|registry.WOW64_64KEY)
	if err != nil {
		return nil, err
	}
	_, _, err = key.GetStringsValue("PendingFileRenameOperations")
	key.Close()
	if err != registry.ErrNotExist {
		if err != nil {
			return nil, err
		}
		indicators = append(indicators, liveSessionManagerKey+`\PendingFileRenameOperations`)
	}
	return indicators, nil
}

func windowsLiveServiceAndDriverNames() ([]string, []string, error) {
	root, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Services`, registry.READ|registry.WOW64_64KEY)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	info, err := root.Stat()
	if err != nil || info.SubKeyCount > maxLiveObserverRecords {
		return nil, nil, fmt.Errorf("Windows service boundary is invalid")
	}
	names, err := root.ReadSubKeyNames(int(info.SubKeyCount))
	if err != nil && err != io.EOF {
		return nil, nil, err
	}
	var services, drivers []string
	for _, name := range names {
		if !validLiveObserverValue(name) {
			return nil, nil, fmt.Errorf("Windows service name is unsafe")
		}
		key, err := registry.OpenKey(root, name, registry.READ|registry.WOW64_64KEY)
		if err != nil {
			return nil, nil, err
		}
		kind, _, kindErr := key.GetIntegerValue("Type")
		key.Close()
		if kindErr == registry.ErrNotExist {
			continue
		}
		if kindErr != nil {
			return nil, nil, kindErr
		}
		driver, service, classifyErr := classifyWindowsLiveServiceType(kind)
		if classifyErr != nil {
			return nil, nil, classifyErr
		}
		if driver {
			drivers = append(drivers, name)
		} else if service {
			services = append(services, name)
		}
	}
	return services, drivers, nil
}

func classifyWindowsLiveServiceType(kind uint64) (driver, service bool, err error) {
	const (
		driverMask      = 0x1 | 0x2 | 0x4 | 0x8
		serviceMask     = 0x10 | 0x20
		interactiveMask = 0x100
		knownMask       = driverMask | serviceMask | interactiveMask
	)
	if kind == 0 || kind&^knownMask != 0 || kind&driverMask != 0 && kind&serviceMask != 0 || kind&serviceMask == serviceMask || kind&interactiveMask != 0 && kind&serviceMask == 0 {
		return false, false, fmt.Errorf("Windows service type is unsupported")
	}
	return kind&driverMask != 0, kind&serviceMask != 0, nil
}

func windowsLiveRegistryTree(hive registry.Key, location string, access uint32) ([]string, error) {
	root, err := registry.OpenKey(hive, location, access)
	if err == registry.ErrNotExist {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	var result []string
	var walk func(registry.Key, string) error
	walk = func(parent registry.Key, prefix string) error {
		info, err := parent.Stat()
		if err != nil || len(result)+int(info.SubKeyCount) > maxLiveObserverRecords {
			return fmt.Errorf("Windows task boundary exceeds bound")
		}
		names, err := parent.ReadSubKeyNames(int(info.SubKeyCount))
		if err != nil && err != io.EOF {
			return err
		}
		for _, name := range names {
			if !validLiveObserverValue(name) {
				return fmt.Errorf("Windows task name is unsafe")
			}
			identity := name
			if prefix != "" {
				identity = prefix + `\` + name
			}
			child, err := registry.OpenKey(parent, name, access)
			if err != nil {
				return err
			}
			result = append(result, identity)
			err = walk(child, identity)
			child.Close()
			if err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(root, ""); err != nil {
		return nil, err
	}
	return result, nil
}
