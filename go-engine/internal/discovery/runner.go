// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// MaxCommandOutputBytes is the absolute ceiling for one adapter command. Each
// declaration normally chooses a smaller source-specific bound.
const MaxCommandOutputBytes = 4 << 20

// ErrNotAvailable marks a command/database absent from this host. It is not a
// source failure and discovery continues without attempting installation.
var ErrNotAvailable = errors.New("discovery source is not available")

var forbiddenShells = map[string]struct{}{
	"sh": {}, "bash": {}, "dash": {}, "zsh": {}, "fish": {},
	"cmd": {}, "cmd.exe": {}, "powershell": {}, "powershell.exe": {}, "pwsh": {}, "pwsh.exe": {},
}

// Command is an argv-only, bounded, read-only inventory invocation. Adapters
// own its fixed Path/Args; user input is never concatenated into a shell string.
type Command struct {
	Path           string
	Args           []string
	MaxOutputBytes int
}

// Validate rejects declarations that could evaluate a shell or retain
// unbounded output.
func (command Command) Validate() error {
	path := strings.TrimSpace(command.Path)
	if path == "" || path != command.Path || strings.ContainsRune(path, '\x00') {
		return fmt.Errorf("inventory command path is empty or invalid")
	}
	if _, forbidden := forbiddenShells[strings.ToLower(filepath.Base(path))]; forbidden {
		return fmt.Errorf("inventory command %q is a shell", command.Path)
	}
	if command.MaxOutputBytes <= 0 || command.MaxOutputBytes > MaxCommandOutputBytes {
		return fmt.Errorf("inventory command output bound %d is outside 1..%d", command.MaxOutputBytes, MaxCommandOutputBytes)
	}
	for _, arg := range command.Args {
		if strings.ContainsRune(arg, '\x00') {
			return fmt.Errorf("inventory command argument contains NUL")
		}
	}
	return nil
}

// CommandRunner executes one validated inventory declaration.
type CommandRunner interface {
	Run(context.Context, Command) ([]byte, error)
}

// ExecRunner is the production argv-only runner.
type ExecRunner struct{}

// OutputLimitError reports that normalized parsing must not proceed because a
// source exceeded its reviewed output bound.
type OutputLimitError struct {
	Path  string
	Limit int
}

func (err *OutputLimitError) Error() string {
	return fmt.Sprintf("inventory source %q exceeded its %d-byte output bound", err.Path, err.Limit)
}

// CommandError reports a bounded command failure without retaining raw output
// in discovery results.
type CommandError struct {
	Path     string
	ExitCode int
}

func (err *CommandError) Error() string {
	return fmt.Sprintf("inventory source %q exited with code %d", err.Path, err.ExitCode)
}

type boundedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	originalLength := len(data)
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining <= 0 {
		buffer.exceeded = buffer.exceeded || originalLength > 0
		return originalLength, nil
	}
	if len(data) > remaining {
		buffer.exceeded = true
		data = data[:remaining]
	}
	_, _ = buffer.buffer.Write(data)
	return originalLength, nil
}

func (buffer *boundedBuffer) Bytes() []byte { return buffer.buffer.Bytes() }

// Run executes a command directly through os/exec. Both stdout and stderr are
// memory-bounded; only stdout is returned to an adapter parser.
func (ExecRunner) Run(ctx context.Context, command Command) ([]byte, error) {
	if err := command.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := exec.LookPath(command.Path)
	if err != nil {
		return nil, ErrNotAvailable
	}
	stdout := &boundedBuffer{limit: command.MaxOutputBytes}
	stderr := &boundedBuffer{limit: command.MaxOutputBytes}
	cmd := exec.CommandContext(ctx, path, command.Args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	runErr := cmd.Run()
	if stdout.exceeded || stderr.exceeded {
		return nil, &OutputLimitError{Path: command.Path, Limit: command.MaxOutputBytes}
	}
	if runErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return nil, &CommandError{Path: command.Path, ExitCode: exitErr.ExitCode()}
		}
		if errors.Is(runErr, exec.ErrNotFound) {
			return nil, ErrNotAvailable
		}
		return nil, fmt.Errorf("run inventory source %q: %w", command.Path, runErr)
	}
	return append([]byte(nil), stdout.Bytes()...), nil
}
