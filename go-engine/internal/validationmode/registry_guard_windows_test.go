//go:build windows

// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestRegistryGuardDetectsExactValueAndSubtreeChangesWithoutDataLeakage(t *testing.T) {
	context := activeTestContext(t, "registry-guard")
	base := `Software\EndstateValidationGuard\registry-guard`
	key, _, err := registry.CreateKey(registry.CURRENT_USER, base, registry.ALL_ACCESS)
	if err != nil {
		t.Skipf("HKCU unavailable: %v", err)
	}
	t.Cleanup(func() {
		_ = registry.DeleteKey(registry.CURRENT_USER, base+`\Child`)
		_ = registry.DeleteKey(registry.CURRENT_USER, base)
	})
	if err := key.SetStringValue("Exact", "secret-before"); err != nil {
		_ = key.Close()
		t.Fatal(err)
	}
	if err := key.SetDWordValue("Tree", 1); err != nil {
		_ = key.Close()
		t.Fatal(err)
	}
	_ = key.Close()

	guard := NewRegistryGuard(context)
	if err := guard.Protect([]ProtectedRegistry{
		{Key: `HKCU\` + base, ValueName: "Exact", Label: "exact-value"},
		{Key: `HKCU\` + base, WholeKey: true, Label: "whole-key"},
	}); err != nil {
		t.Fatal(err)
	}
	guard.Seal()
	key, err = registry.OpenKey(registry.CURRENT_USER, base, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	if err := key.SetStringValue("Exact", "secret-after"); err != nil {
		_ = key.Close()
		t.Fatal(err)
	}
	_ = key.Close()
	child, _, err := registry.CreateKey(registry.CURRENT_USER, base+`\Child`, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.SetStringValue("Created", "never-leak-this"); err != nil {
		_ = child.Close()
		t.Fatal(err)
	}
	_ = child.Close()

	changes, err := guard.Check()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) < 2 {
		t.Fatalf("changes = %#v", changes)
	}
	joined := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(formatRegistryChanges(changes)), "secret-after", "LEAK"), "never-leak-this", "LEAK")))
	if strings.Contains(joined, "leak") || strings.Contains(joined, strings.ToLower(base)) {
		t.Fatalf("registry report leaked data/key: %q", joined)
	}
}

func TestRegistrySnapshotDiffIsDeterministicAndSafeWithoutHostRegistry(t *testing.T) {
	before := map[string]registrySnapshotEntry{"v:a": {kind: "value", valueType: registry.DWORD}, "k:deleted": {kind: "key"}}
	after := map[string]registrySnapshotEntry{"v:a": {kind: "value", valueType: registry.SZ}, "k:created": {kind: "key"}}
	owners := map[string]string{"v:a": "exact-value", "k:deleted": "subtree", "k:created": "subtree"}
	changes := diffRegistrySnapshots(before, after, owners, owners)
	want := []RegistryChange{{Label: "subtree", Kind: ChangeCreated}, {Label: "subtree", Kind: ChangeDeleted}, {Label: "exact-value", Kind: ChangeType}}
	if !reflect.DeepEqual(changes, want) {
		t.Fatalf("changes = %#v, want %#v", changes, want)
	}
	if strings.Contains(formatRegistryChanges(changes), "v:a") {
		t.Fatal("diff leaked internal registry identity")
	}
}

func TestRegistrySnapshotBudgetRejectsValueBeforeAllocation(t *testing.T) {
	budget := registrySnapshotBudget{limits: registryGuardLimits{values: 1, bytes: 3}, seenKeys: map[string]struct{}{}}
	if err := budget.consumeValue(4); !errors.Is(err, ErrGuardBudget) {
		t.Fatalf("consumeValue error = %v", err)
	}
}

func TestRegistryGuardRejectsWrongHiveMappedOverlapAndLateProtect(t *testing.T) {
	context := activeTestContext(t, "registry-guard-reject")
	guard := NewRegistryGuard(context)
	for _, key := range []string{`HKLM\Software\Vendor`, context.RegistryNamespace() + `\Software\Vendor`, `HKCU\Software\Endstate`} {
		if err := guard.Protect([]ProtectedRegistry{{Key: key, WholeKey: true, Label: "unsafe"}}); !errors.Is(err, ErrUnsafeRegistry) {
			t.Fatalf("Protect(%q) error = %v, want ErrUnsafeRegistry", key, err)
		}
	}
	guard.Seal()
	if err := guard.Protect([]ProtectedRegistry{{Key: `HKCU\Software\Vendor`, WholeKey: true, Label: "late"}}); !errors.Is(err, ErrUnsafeRegistry) {
		t.Fatalf("late Protect error = %v, want ErrUnsafeRegistry", err)
	}
}

func TestRegistryGuardExactValueDetectsTypeChange(t *testing.T) {
	context := activeTestContext(t, "registry-exact")
	base := `Software\EndstateValidationGuard\registry-exact`
	key, _, err := registry.CreateKey(registry.CURRENT_USER, base, registry.ALL_ACCESS)
	if err != nil {
		t.Skipf("HKCU unavailable: %v", err)
	}
	t.Cleanup(func() { _ = registry.DeleteKey(registry.CURRENT_USER, base) })
	if err := key.SetDWordValue("Exact", 1); err != nil {
		_ = key.Close()
		t.Fatal(err)
	}
	_ = key.Close()
	guard := NewRegistryGuard(context)
	if err := guard.Protect([]ProtectedRegistry{{Key: `HKCU\` + base, ValueName: "Exact", Label: "exact-value"}}); err != nil {
		t.Fatal(err)
	}
	guard.Seal()
	key, err = registry.OpenKey(registry.CURRENT_USER, base, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	if err := key.SetStringValue("Exact", "changed"); err != nil {
		_ = key.Close()
		t.Fatal(err)
	}
	_ = key.Close()
	changes, err := guard.Check()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0] != (RegistryChange{Label: "exact-value", Kind: ChangeType}) {
		t.Fatalf("changes = %#v", changes)
	}
}

func TestRegistryGuardRejectsValueByMetadataBudget(t *testing.T) {
	context := activeTestContext(t, "registry-budget")
	base := `Software\EndstateValidationGuard\registry-budget`
	key, _, err := registry.CreateKey(registry.CURRENT_USER, base, registry.ALL_ACCESS)
	if err != nil {
		t.Skipf("HKCU unavailable: %v", err)
	}
	t.Cleanup(func() { _ = registry.DeleteKey(registry.CURRENT_USER, base) })
	if err := key.SetStringValue("Value", "larger-than-budget"); err != nil {
		_ = key.Close()
		t.Fatal(err)
	}
	_ = key.Close()
	guard := newRegistryGuardWithLimits(context, registryGuardLimits{keys: 1, values: 1, bytes: 1})
	if err := guard.Protect([]ProtectedRegistry{{Key: `HKCU\` + base, ValueName: "Value", Label: "budget"}}); !errors.Is(err, ErrGuardBudget) {
		t.Fatalf("Protect error = %v, want ErrGuardBudget", err)
	}
}

func TestRegistryGuardCountsDistinctExactValueKeysAgainstKeyBudget(t *testing.T) {
	unitBudget := registrySnapshotBudget{limits: registryGuardLimits{keys: 1}, seenKeys: map[string]struct{}{}}
	if err := unitBudget.consumeKey(`Software\One`); err != nil {
		t.Fatal(err)
	}
	if err := unitBudget.consumeKey(`software\one`); err != nil {
		t.Fatalf("same key consumed twice: %v", err)
	}
	if err := unitBudget.consumeKey(`Software\Two`); !errors.Is(err, ErrGuardBudget) {
		t.Fatalf("second key error = %v", err)
	}

	context := activeTestContext(t, "registry-key-budget")
	base := `Software\EndstateValidationGuard\registry-key-budget`
	for _, child := range []string{"One", "Two"} {
		key, _, err := registry.CreateKey(registry.CURRENT_USER, base+`\`+child, registry.ALL_ACCESS)
		if err != nil {
			t.Skipf("HKCU unavailable: %v", err)
		}
		if err := key.SetStringValue("Value", "x"); err != nil {
			_ = key.Close()
			t.Fatal(err)
		}
		_ = key.Close()
	}
	t.Cleanup(func() {
		_ = registry.DeleteKey(registry.CURRENT_USER, base+`\One`)
		_ = registry.DeleteKey(registry.CURRENT_USER, base+`\Two`)
		_ = registry.DeleteKey(registry.CURRENT_USER, base)
	})
	guard := newRegistryGuardWithLimits(context, registryGuardLimits{keys: 1, values: 2, bytes: 100})
	err := guard.Protect([]ProtectedRegistry{{Key: `HKCU\` + base + `\One`, ValueName: "Value"}, {Key: `HKCU\` + base + `\Two`, ValueName: "Value"}})
	if !errors.Is(err, ErrGuardBudget) {
		t.Fatalf("Protect error = %v, want ErrGuardBudget", err)
	}
}

func formatRegistryChanges(values []RegistryChange) string {
	var result strings.Builder
	for _, value := range values {
		result.WriteString(value.Label)
		result.WriteByte(':')
		result.WriteString(string(value.Kind))
		result.WriteByte(';')
	}
	return result.String()
}
