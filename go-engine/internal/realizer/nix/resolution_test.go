// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

func TestCurrentPreservesAliasesWithTheSameOutputs(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	wrapper := "path:/nix/store/metadata"
	makeItem := func(id, attr string) realizer.Installable {
		return realizer.Installable{ID: id, Ref: id, Resolution: &realizer.InputResolution{WrapperURL: wrapper, AttrPath: "packages." + nixSystem() + "." + attr, Outputs: []string{"out"}, OutputPaths: map[string]string{"out": "/nix/store/shared-output"}}}
	}
	items := []realizer.Installable{makeItem("first-app", "p0"), makeItem("second-app", "p1")}
	data := []byte(fmt.Sprintf(`{"version":3,"elements":{"native-first":{"attrPath":"packages.%s.p0","originalUrl":"%s","storePaths":["/nix/store/shared-output"]},"native-second":{"attrPath":"packages.%s.p1","originalUrl":"%s","storePaths":["/nix/store/shared-output"]}}}`, nixSystem(), wrapper, nixSystem(), wrapper))
	b := &Backend{Profile: filepath.Join(t.TempDir(), "profile"), prepared: items, Run: fakeRun(nil, 0, nil, data)}
	set, err := b.Current()
	if err != nil {
		t.Fatal(err)
	}
	if set.Elements["native-first"].ID != "first-app" || set.Elements["native-second"].ID != "second-app" {
		t.Fatalf("output aliases lost their independent native identities: %+v", set)
	}
	diff, err := b.Plan(items)
	if err != nil || len(diff.Present) != 2 || len(diff.ToAdd) != 0 {
		t.Fatalf("plan lost aliases: %+v %v", diff, err)
	}
	// A native identity with different observed output paths cannot prove ownership.
	items[0].Resolution.OutputPaths["out"] = "/nix/store/unexpected-output"
	set, err = b.Current()
	if err != nil {
		t.Fatal(err)
	}
	if set.Elements["native-first"].ID != "" || set.Elements["native-second"].ID != "second-app" {
		t.Fatalf("native pair hid output drift: %+v", set)
	}
}

func TestRealizeRejectsAnUnpreparedResolution(t *testing.T) {
	mutated := false
	b := &Backend{Profile: filepath.Join(t.TempDir(), "profile"), Run: func(args ...string) ([]byte, []byte, int, error) { mutated = true; return nil, nil, 0, nil }}
	result, err := b.Realize([]realizer.Installable{{ID: "app", Ref: "floating", Resolution: &realizer.InputResolution{MutationRef: "nixpkgs#floating"}}})
	if err != nil || result.Err == nil || result.Err.Code != realizer.ErrInputProvenanceUnresolved || mutated {
		t.Fatalf("unprepared resolution reached native mutation: %+v %v", result, err)
	}
}

func TestCurrentKeepsInstalledInputsWhenAnUnusedPinHasTheSameDerivation(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	oldPin := "github:example/packages/1111111111111111111111111111111111111111"
	newPin := "github:example/packages/2222222222222222222222222222222222222222"
	var profile []byte
	b := &Backend{Profile: filepath.Join(t.TempDir(), "profile"), Pin: oldPin}
	b.Run = func(args ...string) ([]byte, []byte, int, error) {
		switch args[0] {
		case "flake":
			return []byte(fmt.Sprintf(`{"url":%q,"locked":{"type":"github","rev":"1111111111111111111111111111111111111111"}}`, args[len(args)-1])), nil, 0, nil
		case "store":
			data, err := os.ReadFile(filepath.Join(args[len(args)-1], "packages.json"))
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(data)
			return []byte(fmt.Sprintf("/nix/store/%x-metadata", sum[:16])), nil, 0, nil
		case "profile":
			return profile, nil, 0, nil
		}
		if data, ok := nativeResolutionFixture(args); ok {
			return data, nil, 0, nil
		}
		t.Fatalf("unexpected native command: %v", args)
		return nil, nil, 1, nil
	}
	installed, err := b.Prepare([]realizer.Installable{{ID: "app", Ref: "attribute"}})
	if err != nil {
		t.Fatal(err)
	}
	old := installed[0].Resolution
	if err := provision.Write(&provision.Generation{Backend: "nix", Items: []provision.ProvItem{{ID: "app", Ref: "attribute", Status: "installed", Resolution: old}}}); err != nil {
		t.Fatal(err)
	}
	profile, _ = json.Marshal(map[string]any{"version": 3, "elements": map[string]any{"native-name": map[string]any{"originalUrl": old.WrapperURL, "attrPath": old.AttrPath, "storePaths": []string{"/nix/store/package"}}}})
	b.Pin = newPin
	if _, err = b.Prepare([]realizer.Installable{{ID: "app", Ref: "attribute"}}); err != nil {
		t.Fatal(err)
	}
	set, err := b.Current()
	if err != nil {
		t.Fatal(err)
	}
	actual := set.Elements["native-name"]
	if actual.ID != "app" || actual.Resolution == nil || actual.Resolution.Source.URL != oldPin {
		t.Fatalf("unused input was attributed to an installed package: %+v", actual)
	}
	diff, err := b.Plan([]realizer.Installable{{ID: "app", Ref: "attribute"}})
	if err != nil || len(diff.Present) != 1 || len(diff.ToAdd) != 0 {
		t.Fatalf("default input change replaced an unchanged bare request: %+v %v", diff, err)
	}
}

func TestNativeStorePathsKeepPOSIXSeparators(t *testing.T) {
	for _, value := range []string{"/nix/store/package.drv", "/nix/store/package"} {
		if !storePath(value) {
			t.Fatalf("native POSIX path was interpreted with host separators: %q", value)
		}
	}
	for _, value := range []string{`\nix\store\package`, `/nix/store/package\activate`, "/nix/store/package/activate"} {
		if storePath(value) {
			t.Fatalf("a host path or nested script was accepted as a Nix store path: %q", value)
		}
	}
}
