// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

type sourceApplyDriver struct {
	present        map[string]bool
	detectSources  []string
	installSources []string
}

func (d *sourceApplyDriver) Name() string                        { return "winget" }
func (d *sourceApplyDriver) Detect(string) (bool, string, error) { panic("unscoped Detect used") }
func (d *sourceApplyDriver) Install(string) (*driver.InstallResult, error) {
	panic("unscoped Install used")
}
func (d *sourceApplyDriver) DetectSource(ref, source string) (bool, string, error) {
	return d.present[source+"\x00"+ref], ref, nil
}
func (d *sourceApplyDriver) InstallSource(ref, source string) (*driver.InstallResult, error) {
	d.installSources = append(d.installSources, source)
	if d.present == nil {
		d.present = map[string]bool{}
	}
	d.present[source+"\x00"+ref] = true
	return &driver.InstallResult{Status: driver.StatusInstalled}, nil
}
func (d *sourceApplyDriver) DetectBatchSource(refs []string, source string) (map[string]driver.DetectResult, error) {
	d.detectSources = append(d.detectSources, source)
	results := map[string]driver.DetectResult{}
	for _, ref := range refs {
		results[ref] = driver.DetectResult{Installed: d.present[source+"\x00"+ref]}
	}
	return results, nil
}

func TestRunApplyDriverLanes_RoutesDetectionInstallAndVerifyBySource(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	d := &sourceApplyDriver{present: map[string]bool{"winget\x00Same.Ref": true}}
	orig := newNamedDriverFn
	t.Cleanup(func() { newNamedDriverFn = orig })
	newNamedDriverFn = func(name string) (driver.Driver, error) { return d, nil }
	m := &manifest.Manifest{Name: "sources", Apps: []manifest.App{
		{ID: "community", Driver: "winget", Source: "winget", Refs: map[string]string{"windows": "Same.Ref"}},
		{ID: "store", Driver: "winget", Source: "msstore", Refs: map[string]string{"windows": "Same.Ref"}},
	}}
	raw, eerr := runApplyDriverLanes(ApplyFlags{}, m, events.NewEmitter("apply-source", false), "apply-source", nil, nil, nil)
	if eerr != nil {
		t.Fatalf("apply: %+v", eerr)
	}
	result := raw.(*ApplyResult)
	if len(result.Actions) != 2 || result.Actions[0].Status != driver.StatusPresent || result.Actions[1].Status != driver.StatusInstalled {
		t.Fatalf("actions = %+v", result.Actions)
	}
	if len(d.installSources) != 1 || d.installSources[0] != "msstore" {
		t.Fatalf("install sources = %v", d.installSources)
	}
	if len(d.detectSources) != 4 {
		t.Fatalf("detect batches = %v, want winget+msstore for plan and verify", d.detectSources)
	}
}
