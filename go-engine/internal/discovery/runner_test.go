// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestCommandValidationRejectsShellAndUnboundedOutput(t *testing.T) {
	for name, command := range map[string]Command{
		"shell":     {Path: "sh", Args: []string{"-c", "whoami"}, MaxOutputBytes: 1024},
		"unbounded": {Path: "apt-mark", Args: []string{"showmanual"}},
		"too large": {Path: "apt-mark", Args: []string{"showmanual"}, MaxOutputBytes: MaxCommandOutputBytes + 1},
	} {
		t.Run(name, func(t *testing.T) {
			if err := command.Validate(); err == nil {
				t.Fatalf("command %+v was accepted", command)
			}
		})
	}
	if err := (Command{Path: "apt-mark", Args: []string{"showmanual"}, MaxOutputBytes: 1024}).Validate(); err != nil {
		t.Fatalf("fixed argv command rejected: %v", err)
	}
}

func TestExecRunnerBoundsRawOutput(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENDSTATE_DISCOVERY_RUNNER_HELPER", "1")
	_, err = (ExecRunner{}).Run(context.Background(), Command{
		Path: executable, Args: []string{"-test.run=^TestDiscoveryRunnerHelper$", "-test.v"}, MaxOutputBytes: 32,
	})
	var limitErr *OutputLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("Run error = %T %v, want OutputLimitError", err, err)
	}
}

func TestDiscoveryRunnerHelper(t *testing.T) {
	if os.Getenv("ENDSTATE_DISCOVERY_RUNNER_HELPER") != "1" {
		return
	}
	fmt.Print(strings.Repeat("x", 128))
	os.Exit(0)
}
