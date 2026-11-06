// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

func (r *SystemdRegistrar) Inspect(ctx context.Context, cfg *Config) Observation {
	out := Observation{Availability: r.Availability(ctx, cfg), Units: map[string]UnitObservation{}}
	dir := r.UnitDir
	if cfg.Registration != nil {
		dir = cfg.Registration.UnitDir
	}
	failure := func(reason string, err error) {
		if out.StateReason == "" {
			out.StateReason = reason
			out.StateDetail = err.Error()
		}
	}
	for _, name := range []string{ServiceName, TimerName} {
		path := filepath.Join(dir, name)
		u := UnitObservation{Path: path}
		data, _, err := safepath.ReadRegularFileBounded(path, 1024*1024)
		if os.IsNotExist(err) {
			value := false
			u.Present = &value
		} else if err != nil {
			failure("UNIT_READ_FAILED", fmt.Errorf("%s: %w", path, err))
		} else {
			present := true
			u.Present = &present
			u.SHA256 = unitHash(data)
			owned, integrity := false, false
			if reg := cfg.Registration; reg != nil && reg.OwnerRoot == cfg.Root && reg.Backend == systemdBackend && reg.Version == unitFormatVersion && reg.UnitDir == dir {
				admitted := reg.Units[name]
				owned = u.SHA256 == admitted.Previous || u.SHA256 == admitted.Desired
				integrity = u.SHA256 == admitted.Desired
			}
			u.Owned = &owned
			u.Integrity = &integrity
			if !owned {
				failure("OWNERSHIP_CONFLICT", fmt.Errorf("%s differs from admitted contents", path))
			}
		}
		timerFound := true
		if out.Supported {
			p, err := r.Manager.Properties(ctx, name, "Unit")
			if err != nil {
				failure("INSPECTION_FAILED", err)
			} else {
				var fragment, load string
				var drops []string
				if err := property(p, "LoadState", &load); err != nil {
					failure("INSPECTION_FAILED", err)
				}
				if err := property(p, "FragmentPath", &fragment); err != nil {
					failure("INSPECTION_FAILED", err)
				}
				u.FragmentPath = fragment
				if err := property(p, "DropInPaths", &drops); err != nil {
					failure("INSPECTION_FAILED", err)
				}
				u.DropInPaths = drops
				if load != "not-found" && fragment != path {
					failure("REGISTRATION_OVERRIDDEN", fmt.Errorf("%s FragmentPath=%q", name, fragment))
				}
				if name == TimerName {
					if load == "not-found" {
						timerFound = false
						value := false
						out.Active = &value
						out.Enabled = &value
					} else {
						var active, enabled string
						if err := property(p, "ActiveState", &active); err != nil {
							failure("INSPECTION_FAILED", err)
						} else {
							// These states are the closed systemd ActiveState protocol vocabulary.
							switch active {
							case "active", "reloading":
								value := true
								out.Active = &value
							case "inactive", "failed":
								value := false
								out.Active = &value
							default:
								failure("NATIVE_STATE_UNKNOWN", fmt.Errorf("timer ActiveState=%s", active))
							}
						}
						if err := property(p, "UnitFileState", &enabled); err != nil {
							failure("INSPECTION_FAILED", err)
						} else {
							// These states are systemd's documented UnitFileState enum.
							switch enabled {
							case "enabled", "enabled-runtime":
								value := true
								out.Enabled = &value
							case "disabled", "masked", "masked-runtime", "static", "indirect", "linked", "linked-runtime", "generated", "transient":
								value := false
								out.Enabled = &value
							default:
								failure("NATIVE_STATE_UNKNOWN", fmt.Errorf("timer UnitFileState=%s", enabled))
							}
						}
					}
				}
			}
			if name == ServiceName && u.FragmentPath != "" {
				service, err := r.Manager.Properties(ctx, name, "Service")
				if err != nil {
					failure("INSPECTION_FAILED", err)
				} else {
					if err := property(service, "Result", &out.ServiceResult); err != nil {
						failure("INSPECTION_FAILED", err)
					}
					var status int32
					if err := property(service, "ExecMainStatus", &status); err != nil {
						failure("INSPECTION_FAILED", err)
					} else {
						out.ServiceExitStatus = &status
					}
				}
				if p != nil {
					if err := property(p, "ActiveState", &out.ServiceActiveState); err != nil {
						failure("INSPECTION_FAILED", err)
					}
				}
			}
			if name == TimerName && timerFound {
				timer, err := r.Manager.Properties(ctx, name, "Timer")
				if err != nil {
					failure("INSPECTION_FAILED", err)
				} else {
					var next uint64
					if err := property(timer, "NextElapseUSecRealtime", &next); err != nil {
						failure("INSPECTION_FAILED", err)
					} else if next > 0 && next < 1<<63 {
						out.NextTriggerUTC = time.UnixMicro(int64(next)).UTC().Format(time.RFC3339)
					}
				}
			}
		}
		out.Units[name] = u
	}
	if out.Supported && cfg.Registration != nil {
		intent := Intent{Executable: cfg.Registration.Executable, Root: cfg.Root, Manifest: cfg.Manifest, Interval: cfg.Interval, Time: cfg.Time, Args: RunArgs(cfg.Root)}
		if out.Units[ServiceName].Present != nil && *out.Units[ServiceName].Present && out.Units[TimerName].Present != nil && *out.Units[TimerName].Present {
			if err := r.checkEffective(ctx, cfg, intent); err != nil {
				var detail *RegistrationError
				if errors.As(err, &detail) {
					failure(detail.Reason, err)
				} else {
					failure("INSPECTION_FAILED", err)
				}
			}
		}
	}
	if cfg.Enabled && out.StateReason == "" && (out.Enabled != nil && !*out.Enabled || out.Active != nil && !*out.Active) {
		failure("REGISTRATION_MISMATCH", fmt.Errorf("saved schedule is enabled; re-enable to reassert registration"))
	}
	return out
}
