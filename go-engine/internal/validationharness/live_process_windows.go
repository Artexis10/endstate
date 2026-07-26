// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"context"
	"errors"
	"io"
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

func runLiveProcessPlatform(ctx context.Context, request LiveProcessRequest) (liveProcessOutput, error) {
	if err := validateLiveWindowsExecutable(request.Name); err != nil {
		return liveProcessOutput{}, liveExecutionError(LiveExecutionInvalidRequest, err)
	}
	environment, err := liveProcessEnvironment(request.Environment)
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
	defer windows.CloseHandle(stdinWrite)
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
	defer windows.CloseHandle(process.Thread)
	defer windows.CloseHandle(process.Process)
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

	limit := request.outputLimit()
	stdout, stderr := &liveWindowsOutputCollector{}, &liveWindowsOutputCollector{}
	overflow := make(chan struct{}, 2)
	var readers sync.WaitGroup
	readers.Add(2)
	go readLiveWindowsPipe(&readers, stdoutRead, limit, stdout, overflow)
	go readLiveWindowsPipe(&readers, stderrRead, limit, stderr, overflow)
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
	}
	readers.Wait()
	if stdout.exceeded || stderr.exceeded {
		terminalErr = liveExecutionError(LiveExecutionOutputLimit, nil)
	}
	if terminalErr != nil {
		return liveProcessOutput{}, terminalErr
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(process.Process, &exitCode); err != nil {
		return liveProcessOutput{}, liveExecutionError(LiveExecutionContainment, err)
	}
	result := liveProcessOutput{ExitCode: int(int32(exitCode)), Stdout: stdout.bytes, Stderr: stderr.bytes}
	if result.ExitCode != 0 {
		return result, liveExecutionError(LiveExecutionProcessExit, nil)
	}
	return result, nil
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
	application, err := windows.UTF16PtrFromString(request.Name)
	if err != nil {
		return windows.ProcessInformation{}, err
	}
	commandLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{request.Name}, request.Args...)))
	if err != nil {
		return windows.ProcessInformation{}, err
	}
	environmentBlock := liveWindowsEnvironmentBlock(environment)
	var currentDirectory *uint16
	if request.Dir != "" {
		currentDirectory, err = windows.UTF16PtrFromString(request.Dir)
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

func readLiveWindowsPipe(group *sync.WaitGroup, handle windows.Handle, limit int, collector *liveWindowsOutputCollector, overflow chan<- struct{}) {
	defer group.Done()
	file := os.NewFile(uintptr(handle), "")
	defer file.Close()
	buffer := make([]byte, 32*1024)
	for {
		count, err := file.Read(buffer)
		if count > 0 {
			remaining := limit - len(collector.bytes)
			if remaining > 0 {
				if count < remaining {
					collector.bytes = append(collector.bytes, buffer[:count]...)
				} else {
					collector.bytes = append(collector.bytes, buffer[:remaining]...)
				}
			}
			if count > remaining && !collector.exceeded {
				collector.exceeded = true
				select {
				case overflow <- struct{}{}:
				default:
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return
			}
			return
		}
	}
}
