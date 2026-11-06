// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"context"
	"encoding/csv"
	"fmt"
	"os/exec"
	"strings"
)

type SchtasksRegistrar struct{}

func (*SchtasksRegistrar) Availability(context.Context, *Config) Availability {
	_, err := exec.LookPath("schtasks")
	if err != nil {
		return Availability{Backend: "windows-task-scheduler", Reason: "SCHEDULER_UNAVAILABLE", Detail: err.Error()}
	}
	return Availability{Supported: true, Backend: "windows-task-scheduler"}
}
func (*SchtasksRegistrar) LockRegistration(context.Context) (func(), error) { return func() {}, nil }
func (*SchtasksRegistrar) Enable(ctx context.Context, intent Intent, cfg *Config, save func() error) error {
	cfg.TaskName = TaskName
	cfg.Enabled = true
	if err := save(); err != nil {
		return registrationError("prepare", "CONFIG_WRITE_FAILED", err)
	}
	argv := append([]string{intent.Executable}, intent.Args...)
	encoded := make([]string, len(argv))
	for i, arg := range argv {
		encoded[i] = windowsArgument(arg)
	}
	sc := "DAILY"
	if intent.Interval == "weekly" {
		sc = "WEEKLY"
	}
	out, err := exec.CommandContext(ctx, "schtasks", "/Create", "/F", "/SC", sc, "/ST", intent.Time, "/TN", TaskName, "/TR", strings.Join(encoded, " "), "/RL", "LIMITED", "/IT").CombinedOutput()
	if err != nil {
		return registrationError("enable", "NATIVE_OPERATION_FAILED", fmt.Errorf("%w: %s", err, out))
	}
	return nil
}
func (*SchtasksRegistrar) Disable(ctx context.Context, cfg *Config, save func() error) error {
	cfg.TaskName = TaskName
	cfg.Enabled = false
	if err := save(); err != nil {
		return registrationError("prepare", "CONFIG_WRITE_FAILED", err)
	}
	// /FO CSV /NH defines the first field as the task name, independent of locale.
	out, err := exec.CommandContext(ctx, "schtasks", "/Query", "/FO", "CSV", "/NH").Output()
	if err != nil {
		return registrationError("inspect", "INSPECTION_FAILED", err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(out))).ReadAll()
	if err != nil {
		return registrationError("inspect", "INSPECTION_FAILED", err)
	}
	found := false
	for _, row := range rows {
		if len(row) > 0 && strings.EqualFold(strings.TrimPrefix(row[0], `\`), TaskName) {
			found = true
		}
	}
	if !found {
		return nil
	}
	out, err = exec.CommandContext(ctx, "schtasks", "/Delete", "/F", "/TN", TaskName).CombinedOutput()
	if err != nil {
		return registrationError("disable", "NATIVE_OPERATION_FAILED", fmt.Errorf("%w: %s", err, out))
	}
	return nil
}
func (r *SchtasksRegistrar) Inspect(ctx context.Context, cfg *Config) Observation {
	return Observation{Availability: r.Availability(ctx, cfg), StateReason: "NATIVE_STATE_UNKNOWN"}
}

// CommandLineToArgvW rules encode literal arguments, including trailing slashes.
func windowsArgument(arg string) string {
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, c := range arg {
		if c == '\\' {
			slashes++
			continue
		}
		if c == '"' {
			b.WriteString(strings.Repeat(`\`, 2*slashes+1))
		} else {
			b.WriteString(strings.Repeat(`\`, slashes))
		}
		slashes = 0
		b.WriteRune(c)
	}
	b.WriteString(strings.Repeat(`\`, 2*slashes))
	b.WriteByte('"')
	return b.String()
}
