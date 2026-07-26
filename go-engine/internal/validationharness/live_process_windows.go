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
	"sync"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const liveWindowsJobDrainTimeout = 30 * time.Second
const liveWindowsReaderDrainTimeout = 2 * time.Second

func runLiveProcessPlatform(ctx context.Context, request LiveProcessRequest) (liveProcessOutput, error) {
	if err := validateLiveWindowsExecutable(request.executable); err != nil {
		return liveProcessOutput{}, liveExecutionError(LiveExecutionInvalidRequest, err)
	}
	environment, err := liveProcessEnvironment(request.environment)
	if err != nil {
		return liveProcessOutput{}, err
	}
	job, err := newLiveWindowsJob()
	if err != nil {
		return liveProcessOutput{}, liveExecutionError(LiveExecutionContainment, err)
	}
	jobOpen := true
	defer func() {
		if jobOpen {
			_ = windows.CloseHandle(job)
		}
	}()

	stdinRead, stdinWrite, err := newLiveWindowsPipe(true)
	if err != nil {
		return liveProcessOutput{}, liveExecutionError(LiveExecutionStartFailed, err)
	}
	stdinWriteOpen := true
	defer func() {
		if stdinWriteOpen {
			_ = windows.CloseHandle(stdinWrite)
		}
	}()
	stdoutRead, stdoutWrite, err := newLiveWindowsPipe(false)
	if err != nil {
		windows.CloseHandle(stdinRead)
		return liveProcessOutput{}, liveExecutionError(LiveExecutionStartFailed, err)
	}
	stderrRead, stderrWrite, err := newLiveWindowsPipe(false)
	if err != nil {
		windows.CloseHandle(stdinRead)
		windows.CloseHandle(stdoutRead)
		windows.CloseHandle(stdoutWrite)
		return liveProcessOutput{}, liveExecutionError(LiveExecutionStartFailed, err)
	}

	process, err := startLiveWindowsProcess(request, environment, stdinRead, stdoutWrite, stderrWrite)
	windows.CloseHandle(stdinRead)
	windows.CloseHandle(stdoutWrite)
	windows.CloseHandle(stderrWrite)
	if err != nil {
		windows.CloseHandle(stdoutRead)
		windows.CloseHandle(stderrRead)
		return liveProcessOutput{}, liveExecutionError(LiveExecutionStartFailed, err)
	}
	if err := windows.CloseHandle(stdinWrite); err != nil {
		_ = windows.TerminateProcess(process.Process, 1)
		_, _ = windows.WaitForSingleObject(process.Process, windows.INFINITE)
		windows.CloseHandle(stdoutRead)
		windows.CloseHandle(stderrRead)
		return liveProcessOutput{}, liveExecutionError(LiveExecutionContainment, err)
	}
	stdinWriteOpen = false
	defer windows.CloseHandle(process.Thread)
	defer windows.CloseHandle(process.Process)
	if err := verifyLiveWindowsProcessImage(process.Process, request.executable); err != nil {
		_ = windows.TerminateProcess(process.Process, 1)
		_, _ = windows.WaitForSingleObject(process.Process, windows.INFINITE)
		windows.CloseHandle(stdoutRead)
		windows.CloseHandle(stderrRead)
		return liveProcessOutput{}, liveExecutionError(LiveExecutionContainment, err)
	}
	if err := windows.AssignProcessToJobObject(job, process.Process); err != nil {
		_ = windows.TerminateProcess(process.Process, 1)
		_, _ = windows.WaitForSingleObject(process.Process, windows.INFINITE)
		windows.CloseHandle(stdoutRead)
		windows.CloseHandle(stderrRead)
		return liveProcessOutput{}, liveExecutionError(LiveExecutionContainment, err)
	}
	if _, err := windows.ResumeThread(process.Thread); err != nil {
		_ = stopLiveWindowsJob(job)
		jobOpen = false
		windows.CloseHandle(stdoutRead)
		windows.CloseHandle(stderrRead)
		return liveProcessOutput{}, liveExecutionError(LiveExecutionContainment, err)
	}

	limit := request.outputByteLimit()
	overflow := make(chan struct{}, 2)
	stdout := startLiveWindowsReader(stdoutRead, limit, overflow)
	stderr := startLiveWindowsReader(stderrRead, limit, overflow)
	finished := make(chan error, 1)
	go func() {
		state, waitErr := windows.WaitForSingleObject(process.Process, windows.INFINITE)
		if waitErr == nil && state != windows.WAIT_OBJECT_0 {
			waitErr = errors.New("process wait did not complete")
		}
		finished <- waitErr
	}()

	var terminalErr error
	select {
	case err := <-finished:
		if err != nil {
			terminalErr = liveExecutionError(LiveExecutionContainment, err)
		}
	case <-ctx.Done():
		terminalErr = liveProcessContextError(ctx.Err())
	case <-overflow:
		terminalErr = liveExecutionError(LiveExecutionOutputLimit, nil)
	}
	if err := stopLiveWindowsJob(job); err != nil && terminalErr == nil {
		terminalErr = liveExecutionError(LiveExecutionContainment, err)
	}
	jobOpen = false
	if terminalErr != nil {
		_, _ = windows.WaitForSingleObject(process.Process, windows.INFINITE)
		stdout.close()
		stderr.close()
	}
	if !waitLiveWindowsReaders(stdout, stderr) {
		stdout.close()
		stderr.close()
		if !waitLiveWindowsReaders(stdout, stderr) && terminalErr == nil {
			terminalErr = liveExecutionError(LiveExecutionContainment, errors.New("reader did not stop"))
		}
	}
	if stdout.collector.exceeded || stderr.collector.exceeded {
		terminalErr = liveExecutionError(LiveExecutionOutputLimit, nil)
	}
	if terminalErr != nil {
		return liveProcessOutput{}, terminalErr
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(process.Process, &exitCode); err != nil {
		return liveProcessOutput{}, liveExecutionError(LiveExecutionContainment, err)
	}
	result := liveProcessOutput{ExitCode: int(int32(exitCode)), Stdout: stdout.collector.bytes, Stderr: stderr.collector.bytes}
	if result.ExitCode != 0 {
		return result, liveExecutionError(LiveExecutionProcessExit, nil)
	}
	return result, nil
}

var liveWindowsProcessImagePath = queryLiveWindowsProcessImagePath

func verifyLiveWindowsProcessImage(process windows.Handle, expected string) error {
	actual, err := liveWindowsProcessImagePath(process)
	if err != nil || !strings.EqualFold(filepath.Clean(actual), expected) {
		return errors.New("created process image does not match trusted executable")
	}
	return validateLiveWindowsExecutable(actual)
}

func queryLiveWindowsProcessImagePath(process windows.Handle) (string, error) {
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(process, 0, &buffer[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buffer[:size]), nil
}

func validateLiveWindowsExecutable(path string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("executable is not a regular file")
	}
	for current := path; ; {
		wide, err := windows.UTF16PtrFromString(current)
		if err != nil {
			return err
		}
		attributes, err := windows.GetFileAttributes(wide)
		if err != nil || attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return errors.New("executable path contains a reparse point")
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
		current = parent
	}
}

func newLiveWindowsJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}

func newLiveWindowsPipe(childReads bool) (windows.Handle, windows.Handle, error) {
	security := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), InheritHandle: 1}
	var read, write windows.Handle
	if err := windows.CreatePipe(&read, &write, &security, 0); err != nil {
		return 0, 0, err
	}
	parent := read
	if childReads {
		parent = write
	}
	if err := windows.SetHandleInformation(parent, windows.HANDLE_FLAG_INHERIT, 0); err != nil {
		windows.CloseHandle(read)
		windows.CloseHandle(write)
		return 0, 0, err
	}
	return read, write, nil
}

func startLiveWindowsProcess(request LiveProcessRequest, environment []string, stdin, stdout, stderr windows.Handle) (windows.ProcessInformation, error) {
	application, err := windows.UTF16PtrFromString(request.executable)
	if err != nil {
		return windows.ProcessInformation{}, err
	}
	commandLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{request.executable}, request.args...)))
	if err != nil {
		return windows.ProcessInformation{}, err
	}
	environmentBlock := liveWindowsEnvironmentBlock(environment)
	var currentDirectory *uint16
	if request.dir != "" {
		currentDirectory, err = windows.UTF16PtrFromString(request.dir)
		if err != nil {
			return windows.ProcessInformation{}, err
		}
	}
	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return windows.ProcessInformation{}, err
	}
	defer attributes.Delete()
	handles := []windows.Handle{stdin, stdout, stderr}
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&handles[0]), uintptr(len(handles))*unsafe.Sizeof(handles[0])); err != nil {
		return windows.ProcessInformation{}, err
	}
	startup := windows.StartupInfoEx{StartupInfo: windows.StartupInfo{Cb: uint32(unsafe.Sizeof(windows.StartupInfoEx{})), Flags: windows.STARTF_USESTDHANDLES, StdInput: stdin, StdOutput: stdout, StdErr: stderr}, ProcThreadAttributeList: attributes.List()}
	var process windows.ProcessInformation
	err = windows.CreateProcess(application, commandLine, nil, nil, true,
		windows.CREATE_SUSPENDED|windows.CREATE_UNICODE_ENVIRONMENT|windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_DEFAULT_ERROR_MODE,
		&environmentBlock[0], currentDirectory, &startup.StartupInfo, &process)
	return process, err
}

func liveWindowsEnvironmentBlock(values []string) []uint16 {
	block := strings.Join(values, "\x00") + "\x00\x00"
	return utf16.Encode([]rune(block))
}

func stopLiveWindowsJob(job windows.Handle) error {
	terminateErr := windows.TerminateJobObject(job, 1)
	waitErr := waitLiveWindowsJobEmpty(job)
	closeErr := windows.CloseHandle(job)
	if waitErr != nil {
		return waitErr
	}
	if terminateErr != nil {
		return terminateErr
	}
	return closeErr
}

type liveWindowsJobAccounting struct {
	TotalUserTime          int64
	TotalKernelTime        int64
	ThisPeriodTotalUser    int64
	ThisPeriodTotalKernel  int64
	TotalPageFaultCount    uint32
	TotalProcesses         uint32
	ActiveProcesses        uint32
	TotalTerminatedProcess uint32
}

func waitLiveWindowsJobEmpty(job windows.Handle) error {
	deadline := time.Now().Add(liveWindowsJobDrainTimeout)
	for {
		var accounting liveWindowsJobAccounting
		if err := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil); err != nil {
			return err
		}
		if accounting.ActiveProcesses == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("job did not drain")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type liveWindowsOutputCollector struct {
	bytes    []byte
	exceeded bool
}

type liveWindowsPipeReader struct {
	handle    windows.Handle
	collector liveWindowsOutputCollector
	overflow  chan<- struct{}
	done      chan struct{}
	closeOnce sync.Once
}

func startLiveWindowsReader(handle windows.Handle, limit int, overflow chan<- struct{}) *liveWindowsPipeReader {
	reader := &liveWindowsPipeReader{handle: handle, overflow: overflow, done: make(chan struct{})}
	go reader.read(limit)
	return reader
}

func (reader *liveWindowsPipeReader) read(limit int) {
	defer close(reader.done)
	defer reader.close()
	buffer := make([]byte, 32*1024)
	for {
		var count uint32
		err := windows.ReadFile(reader.handle, buffer, &count, nil)
		if count > 0 {
			remaining := limit - len(reader.collector.bytes)
			if remaining > 0 {
				if int(count) < remaining {
					reader.collector.bytes = append(reader.collector.bytes, buffer[:count]...)
				} else {
					reader.collector.bytes = append(reader.collector.bytes, buffer[:remaining]...)
				}
			}
			if int(count) > remaining && !reader.collector.exceeded {
				reader.collector.exceeded = true
				select {
				case reader.overflow <- struct{}{}:
				default:
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func (reader *liveWindowsPipeReader) close() {
	reader.closeOnce.Do(func() {
		_ = windows.CancelIoEx(reader.handle, nil)
		_ = windows.CloseHandle(reader.handle)
	})
}

func (reader *liveWindowsPipeReader) closeAndWait(timeout time.Duration) bool {
	reader.close()
	select {
	case <-reader.done:
		return true
	case <-time.After(timeout):
		return false
	}
}

func waitLiveWindowsReaders(readers ...*liveWindowsPipeReader) bool {
	deadline := time.Now().Add(liveWindowsReaderDrainTimeout)
	for _, reader := range readers {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		select {
		case <-reader.done:
		case <-time.After(remaining):
			return false
		}
	}
	return true
}
