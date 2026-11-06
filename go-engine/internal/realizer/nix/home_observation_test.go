// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

func TestObserveHomeDistinguishesMissingFromUnknownProfiles(t *testing.T) {
	for _, tc := range []struct {
		name, target, output, stderr string
		regular, change              bool
		exit                         int
		spawn                        error
		want                         envelope.ErrorCode
	}{
		{name: "missing"},
		{name: "regular file", regular: true, want: realizer.ErrInputProvenanceUnresolved},
		{name: "malformed link", target: "unknown-profile", want: realizer.ErrInputProvenanceUnresolved},
		{name: "nonpositive generation", target: "home-manager-0-link", want: realizer.ErrInputProvenanceUnresolved},
		{name: "other directory", target: "../home-manager-7-link", want: realizer.ErrInputProvenanceUnresolved},
		{name: "observed", target: "home-manager-7-link", output: "/nix/store/active-home"},
		{name: "invalid output", target: "home-manager-7-link", output: "not-an-output", want: realizer.ErrInputProvenanceUnresolved},
		{name: "multiple outputs", target: "home-manager-7-link", output: "/nix/store/one\n/nix/store/two", want: realizer.ErrInputProvenanceUnresolved},
		{name: "incomplete generation", target: "home-manager-7-link", exit: 1, want: realizer.ErrInputProvenanceUnresolved},
		{name: "native permission", target: "home-manager-7-link", exit: 1, stderr: "error: Permission denied", want: envelope.ErrPermissionDenied},
		{name: "native unavailable", target: "home-manager-7-link", spawn: errors.New("nix unavailable"), want: envelope.ErrRealizerUnavailable},
		{name: "changed profile", target: "home-manager-7-link", output: "/nix/store/active-home", change: true, want: realizer.ErrInputProvenanceUnresolved},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			profile := homeProfilePath()
			if err := os.MkdirAll(filepath.Dir(profile), 0700); err != nil {
				t.Fatal(err)
			}
			if tc.regular {
				if err := os.WriteFile(profile, []byte("not a native profile"), 0600); err != nil {
					t.Fatal(err)
				}
			} else if tc.target != "" {
				if err := os.Symlink(tc.target, profile); err != nil {
					t.Fatal(err)
				}
			}
			backend := &Backend{Run: func(args ...string) ([]byte, []byte, int, error) {
				if len(args) != 2 || args[0] != "path-info" || args[1] != profile+"-7-link" {
					t.Fatalf("inspection did not select the observed generation: %q", args)
				}
				if tc.change {
					setActiveHomeLink(t, 8)
				}
				return []byte(tc.output + "\n"), []byte(tc.stderr), tc.exit, tc.spawn
			}}
			observed, err := backend.ObserveHome()
			if tc.want != "" {
				failure := asRealizerErr(t, err)
				if failure.Code != tc.want || failure.Subcode != "home-ownership" || observed != (realizer.HomeObservation{}) {
					t.Fatalf("unknown profile returned identity: %+v, %+v", observed, failure)
				}
			} else if err != nil || observed.Present != (tc.target != "") {
				t.Fatalf("profile observation: %+v, %v", observed, err)
			} else if observed.Present && (observed.Generation != 7 || observed.ActivationOutput != tc.output) {
				t.Fatalf("profile identity: %+v", observed)
			}
		})
	}
}

func TestHomeActivationUnreadableAfterScriptRemainsUncertain(t *testing.T) {
	for _, exit := range []int{0, 1} {
		t.Run(strconv.Itoa(exit), func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			backend := &Backend{Run: func(args ...string) ([]byte, []byte, int, error) {
				t.Fatalf("malformed profile must not reach native inspection: %v", args)
				return nil, nil, 1, nil
			}}
			backend.Script = func(path string, args ...string) ([]byte, []byte, int, error) {
				setActiveHomeLink(t, 1)
				if err := os.Remove(homeProfilePath()); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("unknown-profile", homeProfilePath()); err != nil {
					t.Fatal(err)
				}
				return nil, []byte("activation result unavailable"), exit, nil
			}
			generation, err := backend.ActivatePreparedHome(&realizer.InputResolution{Outputs: []string{"out"}, OutputPaths: map[string]string{"out": "/nix/store/prepared-home"}})
			failure := asRealizerErr(t, err)
			if generation != 0 || failure.Stage != "commit" || failure.Subcode != "home-ownership" {
				t.Fatalf("uncertain activation acquired an identity: %d, %+v", generation, failure)
			}
		})
	}
}
