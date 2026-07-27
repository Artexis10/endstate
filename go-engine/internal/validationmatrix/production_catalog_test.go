// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestHashBoundTextAssetsArePinnedToLF(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(productionCatalogRepoRoot(t), ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	lines := map[string]struct{}{}
	for _, line := range strings.Split(string(data), "\n") {
		lines[strings.TrimSpace(line)] = struct{}{}
	}
	for _, required := range []string{
		"modules/apps/**/seed.ps1 text eol=lf",
		"modules/apps/**/validation-fixtures/** text eol=lf",
		"go-engine/internal/validationharness/testdata/restore-contract/** text eol=lf",
	} {
		if _, ok := lines[required]; !ok {
			t.Errorf(".gitattributes missing %q", required)
		}
	}
}

func TestProductionCatalogValidationMetadataIsComplete(t *testing.T) {
	t.Parallel()

	repoRoot := productionCatalogRepoRoot(t)
	catalog, err := LoadCatalog(repoRoot, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	if got := len(catalog.Modules); got != 359 {
		t.Fatalf("production module count = %d, want 359", got)
	}
	if got := len(catalog.Records); got != 359 {
		t.Fatalf("validation record count = %d, want 359", got)
	}

	wantOneWay := prefixedModuleIDs(strings.Fields(`
		aida64 claude-code claude-desktop cobian-reflector cursor dbgate dbpoweramp
		glasswire gpu-tweak-iii marmoset-toolbag maven meshlab mgba minikube modo
		moonlight motrix mullvad-browser netbeans nomachine ocenaudio okular openscad
		openshot parsec podman-desktop portmaster proton-vpn qownnotes rambox
		realvnc-viewer redisinsight rekordbox screenpresso serato-dj snipaste spyder
		sqlitestudio staxrip tigervnc treesize-free vorta vscode vscodium waveform
		windsurf wireguard xtreme-download-manager yubikey-manager zettlr
	`))
	wantInstallOnly := prefixedModuleIDs(strings.Fields(`
		bizhawk brave btop clementine discord duplicati fancontrol flycast
		free-download-manager internet-download-manager itunes ksnip kubectl mailbird
		makemkv malwarebytes mremoteng onlyoffice-desktop openvpn-connect picgo rclone
		remmina resilio-sync rpcs3 signal spotify strawberry telegram warp-terminal winbox
	`))
	wantMixedReasons := map[string]string{
		"apps.aida64":         "asymmetric-install-layout",
		"apps.claude-code":    "mixed-oauth-state",
		"apps.claude-desktop": "non-restorable-inventory",
		"apps.cursor":         "non-restorable-inventory",
		"apps.vscode":         "non-restorable-inventory",
		"apps.vscodium":       "non-restorable-inventory",
		"apps.windsurf":       "non-restorable-inventory",
	}

	scenarioKinds := map[ScenarioKind]int{}
	liveModes := map[LiveMode]int{}
	gotOneWay := make([]string, 0, len(wantOneWay))
	gotInstallOnly := make([]string, 0, len(wantInstallOnly))
	totalScenarios := 0
	for moduleID, record := range catalog.Records {
		liveModes[record.Live.Mode]++
		if len(record.Quarantines) != 0 {
			t.Errorf("%s quarantines = %d, want zero", moduleID, len(record.Quarantines))
		}
		for _, scenario := range record.Synthetic.Scenarios {
			totalScenarios++
			scenarioKinds[scenario.Mode]++
			switch scenario.Mode {
			case ScenarioCaptureContract, ScenarioRestoreContract:
				gotOneWay = append(gotOneWay, moduleID)
				review := scenario.Review
				if review == nil {
					t.Errorf("%s one-way scenario has no review", moduleID)
					continue
				}
				wantReason := "documented-sensitive-one-way"
				if mixedReason, ok := wantMixedReasons[moduleID]; ok {
					wantReason = mixedReason
				}
				if review.Decision != "approved-one-way" || review.ReasonCode != wantReason ||
					review.Reviewer != "endstate-maintainers" || review.ReviewedOn != "2026-07-22" ||
					strings.TrimSpace(review.Evidence) == "" {
					t.Errorf("%s one-way review = %+v, want frozen reviewed decision with reason %q", moduleID, review, wantReason)
				}
			case ScenarioInstallContract:
				gotInstallOnly = append(gotInstallOnly, moduleID)
			}
		}
	}

	if totalScenarios != 362 {
		t.Errorf("synthetic scenario count = %d, want 362", totalScenarios)
	}
	wantScenarioKinds := map[ScenarioKind]int{
		ScenarioConfigRoundtripV1:  276,
		ScenarioCaptureContract:    50,
		ScenarioInstallContract:    30,
		ScenarioConfigGenerationV2: 5,
		ScenarioConfigMigrationV2:  1,
		ScenarioRestoreContract:    0,
	}
	for kind, want := range wantScenarioKinds {
		if got := scenarioKinds[kind]; got != want {
			t.Errorf("scenario kind %s count = %d, want %d", kind, got, want)
		}
	}
	wantLiveModes := map[LiveMode]int{
		LiveCandidate:     310,
		LiveManual:        49,
		LiveHosted:        0,
		LiveBlocked:       0,
		LiveLab:           0,
		LiveNotApplicable: 0,
	}
	for mode, want := range wantLiveModes {
		if got := liveModes[mode]; got != want {
			t.Errorf("live mode %s count = %d, want %d", mode, got, want)
		}
	}

	assertModuleIDSet(t, "reviewed one-way", gotOneWay, wantOneWay)
	assertModuleIDSet(t, "install-only", gotInstallOnly, wantInstallOnly)
}

func productionCatalogRepoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
}

func prefixedModuleIDs(names []string) []string {
	ids := make([]string, len(names))
	for index, name := range names {
		ids[index] = "apps." + name
	}
	sort.Strings(ids)
	return ids
}

func assertModuleIDSet(t *testing.T, name string, got, want []string) {
	t.Helper()
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s module IDs:\n got %v\nwant %v", name, got, want)
	}
}
