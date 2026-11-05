// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestValidationCoordinateInstancePreflightDetectsOriginalHostWrites(t *testing.T) {
	for _, test := range []struct{ name, coordinate, xdg, relative string }{
		{"native home", "${home}", "", ""},
		{"legacy profile", "%USERPROFILE%", "", ""},
		{"unset XDG", "${xdg.config}", "", ".config"},
		{"relative XDG", "${xdg.config}", "relative-config", ".config"},
		{"absolute XDG", "${xdg.config}", "absolute", ""},
		{"Darwin support", "${darwin.applicationSupport}", "", "Library/Application Support"},
	} {
		t.Run(test.name, func(t *testing.T) {
			home, legacy, xdg := t.TempDir(), t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("USERPROFILE", legacy)
			// Windows obtains native home from USERPROFILE; the two remain distinct on Unix.
			nativeHome, err := os.UserHomeDir()
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("XDG_CONFIG_HOME", test.xdg)
			if test.xdg == "absolute" {
				t.Setenv("XDG_CONFIG_HOME", xdg)
			}
			originalRoot := filepath.Join(nativeHome, filepath.FromSlash(test.relative))
			if test.coordinate == "%USERPROFILE%" {
				originalRoot = legacy
			}
			if test.xdg == "absolute" {
				originalRoot = xdg
			}
			originalTarget := filepath.Join(originalRoot, "Profiles", "one", "settings.json")
			if err := os.MkdirAll(filepath.Dir(originalTarget), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(originalTarget, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			context, session := validationPreflightSession(t)
			detector := modules.InstanceDetectorDef{ID: "profiles", Type: "path", Glob: test.coordinate + "/Profiles/*"}
			mod := &modules.Module{ID: "apps.notepad-plus-plus", Config: &modules.ConfigDef{InstanceDetectors: []modules.InstanceDetectorDef{detector}}}
			if test.coordinate != "%USERPROFILE%" {
				mod.SourceSchemaVersion = 3
				mod.Platform = "linux"
				if test.coordinate == "${darwin.applicationSupport}" {
					mod.Platform = "darwin"
				}
			}
			sandboxInstance, err := context.ResolveHostPath(test.coordinate+"/Profiles/one", validationmode.HostPathPolicy{})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(sandboxInstance, 0700); err != nil {
				t.Fatal(err)
			}
			_, pattern, err := validationDetectorPattern(context, detector.Glob)
			if err != nil {
				t.Fatal(err)
			}
			instance, err := validationDiscoveredPathInstance(context, mod, &detector, pattern, sandboxInstance)
			if err != nil || instance == nil {
				t.Fatalf("discover instance: %v", err)
			}
			policies, err := deriveValidationInstancePolicies(context, session, mod, []modules.ConfigInstance{*instance})
			if err != nil {
				t.Fatal(err)
			}
			policy := policies[instance.ID]
			if err := preflightHostPath(context, session, "restore.target", "${instance.root}/settings.json", policy); err != nil {
				t.Fatal(err)
			}
			session.sealIsolation()
			if err := os.WriteFile(filepath.Join(sandboxInstance, "settings.json"), []byte("sandbox"), 0600); err != nil {
				t.Fatal(err)
			}
			session.checkIsolationGuards()
			if err := session.recorder.isolationError(); err != nil {
				t.Fatalf("legitimate sandbox write rejected: %v", err)
			}
			if err := os.WriteFile(originalTarget, []byte("escaped"), 0600); err != nil {
				t.Fatal(err)
			}
			session.checkIsolationGuards()
			if err := session.recorder.isolationError(); err == nil {
				t.Fatal("original host write escaped the production guard")
			}
		})
	}
}
