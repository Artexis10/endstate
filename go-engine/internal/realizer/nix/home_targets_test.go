// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

func TestPrepareHomeProjectsNativeFlatTargetsWithoutBuilding(t *testing.T) {
	for _, test := range []struct {
		name, projection string
		want             []string
		code             envelope.ErrorCode
	}{
		{name: "evaluated placements", projection: `{"drvPath":"/nix/store/package.drv","targets":["/declared-home/xdg-config/tool/config","/declared-home//outside-name"]}`, want: []string{"/declared-home/xdg-config/tool/config", "/declared-home/outside-name"}},
		{name: "known empty", projection: `{"drvPath":"/nix/store/package.drv","targets":[]}`, want: []string{}},
		{name: "activation only", projection: `{"drvPath":"/nix/store/package.drv","targets":null}`},
		{name: "changed derivation", projection: `{"drvPath":"/nix/store/changed.drv","targets":[]}`, code: realizer.ErrInputProvenanceUnresolved},
		{name: "invalid JSON", projection: `{`, code: realizer.ErrInputProvenanceUnresolved},
		{name: "missing result", projection: `{"drvPath":"/nix/store/package.drv"}`, code: realizer.ErrInputProvenanceUnresolved},
		{name: "invalid target type", projection: `{"drvPath":"/nix/store/package.drv","targets":[42]}`, code: realizer.ErrInputProvenanceUnresolved},
		{name: "relative target", projection: `{"drvPath":"/nix/store/package.drv","targets":["relative"]}`, code: realizer.ErrInputProvenanceUnresolved},
		{name: "permission", code: envelope.ErrPermissionDenied},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", "/ambient-config")
			projected := false
			backend := &Backend{Profile: filepath.Join(t.TempDir(), "profile")}
			defer backend.FinalizePreparation(false)
			backend.Run = func(args ...string) ([]byte, []byte, int, error) {
				if args[0] == "build" && strings.Contains(args[len(args)-1], "^") {
					dry := false
					for _, arg := range args {
						if arg == "--dry-run" {
							dry = true
						}
					}
					if !dry {
						t.Fatalf("preview built activation: %v", args)
					}
				}
				if args[0] == "eval" && args[len(args)-1] == "github:NixOS/nixpkgs/1111111111111111111111111111111111111111#homeConfigurations.profile" {
					projected = true
					lockFound := false
					for i, arg := range args {
						if arg == "--reference-lock-file" {
							data, err := os.ReadFile(args[i+1])
							if err != nil || !bytes.Equal(data, []byte(`{"version":7,"root":"root","nodes":{"root":{}}}`)) {
								t.Fatalf("projection lost retained lock: %s %v", data, err)
							}
							lockFound = true
						}
					}
					if !lockFound {
						t.Fatal("projection had no retained lock")
					}
					if test.name == "permission" {
						return nil, []byte("error: Permission denied"), 1, nil
					}
					return []byte(test.projection), nil, 0, nil
				}
				data, ok := nativeResolutionFixture(args)
				if !ok {
					t.Fatalf("unexpected command: %v", args)
				}
				return data, nil, 0, nil
			}
			preparation, err := backend.PrepareHome("github:example/config#profile", false)
			if !projected {
				t.Fatal("native parent configuration was not evaluated")
			}
			if test.code != "" {
				failure, ok := err.(*realizer.Error)
				if !ok || failure.Code != test.code {
					t.Fatalf("wrong native failure: %+v", err)
				}
				return
			}
			if err != nil || preparation == nil || preparation.Resolution.Derivation != "/nix/store/package.drv" || !reflect.DeepEqual(preparation.FlatTargets, test.want) {
				t.Fatalf("incorrect flat target evidence: %+v %v", preparation, err)
			}
		})
	}
}
