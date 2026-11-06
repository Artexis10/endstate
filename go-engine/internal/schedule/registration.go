// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"context"
	"fmt"
	"runtime"
)

// Intent carries literal arguments; each native backend owns its encoding.
type Intent struct {
	Executable                     string
	Args                           []string
	Root, Manifest, Interval, Time string
}

// RunArgs is the shared native payload. The saved config selects the manifest.
func RunArgs(root string) []string {
	return []string{"schedule", "run", "--root", root, "--json"}
}

type Availability struct {
	Supported bool   `json:"supported"`
	Backend   string `json:"backend,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type Registration struct {
	Backend      string                `json:"backend"`
	OwnerRoot    string                `json:"ownerRoot"`
	UnitDir      string                `json:"unitDir"`
	Version      int                   `json:"version"`
	Executable   string                `json:"executable"`
	Operation    string                `json:"operation,omitempty"`
	Phase        string                `json:"phase,omitempty"`
	CleanupPhase string                `json:"cleanupPhase,omitempty"`
	Units        map[string]UnitHashes `json:"units"`
}

type UnitHashes struct {
	Previous string `json:"previous,omitempty"`
	Desired  string `json:"desired,omitempty"`
}

type UnitObservation struct {
	Path         string   `json:"path"`
	Present      *bool    `json:"present,omitempty"`
	Owned        *bool    `json:"owned,omitempty"`
	Integrity    *bool    `json:"integrity,omitempty"`
	SHA256       string   `json:"sha256,omitempty"`
	FragmentPath string   `json:"fragmentPath,omitempty"`
	DropInPaths  []string `json:"dropInPaths,omitempty"`
}

type Observation struct {
	Availability
	StateReason        string                     `json:"stateReason,omitempty"`
	StateDetail        string                     `json:"stateDetail,omitempty"`
	Enabled            *bool                      `json:"enabled,omitempty"`
	Active             *bool                      `json:"active,omitempty"`
	ServiceResult      string                     `json:"serviceResult,omitempty"`
	ServiceActiveState string                     `json:"serviceActiveState,omitempty"`
	ServiceExitStatus  *int32                     `json:"serviceExitStatus,omitempty"`
	NextTriggerUTC     string                     `json:"nextTriggerUtc,omitempty"`
	Units              map[string]UnitObservation `json:"units,omitempty"`
}

// Registrar serializes registration before commands acquire their root run lock.
// Save always writes the same command-owned config store.
type Registrar interface {
	Availability(context.Context, *Config) Availability
	LockRegistration(context.Context) (func(), error)
	Enable(context.Context, Intent, *Config, func() error) error
	Disable(context.Context, *Config, func() error) error
	Inspect(context.Context, *Config) Observation
}

type RegistrationError struct {
	Phase  string `json:"phase"`
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

func (e *RegistrationError) Error() string {
	return fmt.Sprintf("%s: %s: %s", e.Phase, e.Reason, e.Detail)
}
func registrationError(phase, reason string, err error) error {
	return &RegistrationError{Phase: phase, Reason: reason, Detail: err.Error()}
}

func NewRegistrar() Registrar {
	switch runtime.GOOS {
	case "windows":
		return &SchtasksRegistrar{}
	case "linux":
		return newSystemdRegistrar()
	default:
		return unsupportedRegistrar{}
	}
}
func ManualSupported() bool { return runtime.GOOS == "windows" || runtime.GOOS == "linux" }

type unsupportedRegistrar struct{}

func (unsupportedRegistrar) Availability(context.Context, *Config) Availability {
	return Availability{Reason: "PLATFORM_UNSUPPORTED"}
}
func (unsupportedRegistrar) LockRegistration(context.Context) (func(), error) { return func() {}, nil }
func (unsupportedRegistrar) Enable(context.Context, Intent, *Config, func() error) error {
	return fmt.Errorf("scheduler unsupported")
}
func (unsupportedRegistrar) Disable(context.Context, *Config, func() error) error {
	return fmt.Errorf("scheduler unsupported")
}
func (r unsupportedRegistrar) Inspect(ctx context.Context, cfg *Config) Observation {
	return Observation{Availability: r.Availability(ctx, cfg)}
}
