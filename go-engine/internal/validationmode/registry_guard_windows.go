//go:build windows

// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sys/windows/registry"
)

type registrySnapshotEntry struct {
	kind      string
	valueType uint32
	hash      [sha256.Size]byte
}
type canonicalRegistryProtection struct {
	key, valueName, label string
	whole                 bool
}

type RegistryGuard struct {
	mu          sync.Mutex
	context     *Context
	sealed      bool
	protections []canonicalRegistryProtection
	before      map[string]registrySnapshotEntry
	owners      map[string]string
	limits      registryGuardLimits
}

func NewRegistryGuard(context *Context) *RegistryGuard {
	return newRegistryGuardWithLimits(context, defaultRegistryGuardLimits)
}

func newRegistryGuardWithLimits(context *Context, limits registryGuardLimits) *RegistryGuard {
	return &RegistryGuard{context: context, before: map[string]registrySnapshotEntry{}, owners: map[string]string{}, limits: limits}
}

func (guard *RegistryGuard) Protect(values []ProtectedRegistry) error {
	if guard == nil || guard.context == nil {
		return fmt.Errorf("%w: registry guard is inactive", ErrUnsafeRegistry)
	}
	guard.mu.Lock()
	defer guard.mu.Unlock()
	if guard.sealed {
		return fmt.Errorf("%w: registry guard is sealed", ErrUnsafeRegistry)
	}
	all := append([]canonicalRegistryProtection(nil), guard.protections...)
	for _, value := range values {
		key, err := NormalizeHKCU(value.Key)
		if err != nil {
			return err
		}
		mapped := strings.ToLower(guard.context.RegistryNamespace())
		lower := strings.ToLower(key)
		if lower == mapped || strings.HasPrefix(lower, mapped+`\`) || (value.WholeKey && strings.HasPrefix(mapped, lower+`\`)) {
			return fmt.Errorf("%w: protected identity overlaps mapped namespace", ErrUnsafeRegistry)
		}
		label := strings.TrimSpace(value.Label)
		if label == "" {
			label = "registry-target"
		}
		if value.WholeKey == (strings.TrimSpace(value.ValueName) != "") {
			return fmt.Errorf("%w: protection must select a subtree or one value", ErrUnsafeRegistry)
		}
		all = append(all, canonicalRegistryProtection{key: key, valueName: value.ValueName, label: label, whole: value.WholeKey})
	}
	all = compactRegistryProtections(all)
	before, owners, err := snapshotRegistry(all, guard.limits)
	if err != nil {
		return err
	}
	guard.protections, guard.before, guard.owners = all, before, owners
	return nil
}

func (guard *RegistryGuard) Seal() {
	if guard != nil {
		guard.mu.Lock()
		guard.sealed = true
		guard.mu.Unlock()
	}
}

func (guard *RegistryGuard) Check() ([]RegistryChange, error) {
	if guard == nil {
		return nil, fmt.Errorf("%w: nil registry guard", ErrUnsafeRegistry)
	}
	guard.mu.Lock()
	defer guard.mu.Unlock()
	guard.sealed = true
	after, afterOwners, err := snapshotRegistry(guard.protections, guard.limits)
	if err != nil {
		return nil, err
	}
	return diffRegistrySnapshots(guard.before, after, guard.owners, afterOwners), nil
}

func diffRegistrySnapshots(beforeSnapshot, afterSnapshot map[string]registrySnapshotEntry, beforeOwners, afterOwners map[string]string) []RegistryChange {
	keys := map[string]struct{}{}
	for key := range beforeSnapshot {
		keys[key] = struct{}{}
	}
	for key := range afterSnapshot {
		keys[key] = struct{}{}
	}
	ordered := make([]string, 0, len(keys))
	for key := range keys {
		ordered = append(ordered, key)
	}
	sort.Strings(ordered)
	result := make([]RegistryChange, 0)
	for _, key := range ordered {
		before, bok := beforeSnapshot[key]
		current, aok := afterSnapshot[key]
		kind := ChangeKind("")
		switch {
		case !bok:
			kind = ChangeCreated
		case !aok:
			kind = ChangeDeleted
		case before.kind != current.kind || before.valueType != current.valueType:
			kind = ChangeType
		case before.hash != current.hash:
			kind = ChangeContent
		}
		if kind == "" {
			continue
		}
		label := beforeOwners[key]
		if label == "" {
			label = afterOwners[key]
		}
		result = append(result, RegistryChange{Label: label, Kind: kind})
	}
	return result
}

func compactRegistryProtections(values []canonicalRegistryProtection) []canonicalRegistryProtection {
	sort.SliceStable(values, func(i, j int) bool {
		if len(values[i].key) != len(values[j].key) {
			return len(values[i].key) < len(values[j].key)
		}
		if values[i].whole != values[j].whole {
			return values[i].whole
		}
		return strings.ToLower(values[i].key+"\x00"+values[i].valueName) < strings.ToLower(values[j].key+"\x00"+values[j].valueName)
	})
	result := make([]canonicalRegistryProtection, 0, len(values))
	seen := map[string]int{}
	for _, value := range values {
		identity := strings.ToLower(value.key + "\x00" + value.valueName + fmt.Sprint(value.whole))
		if index, ok := seen[identity]; ok {
			result[index].label = value.label
			continue
		}
		covered := false
		for _, ancestor := range result {
			if ancestor.whole && (strings.EqualFold(ancestor.key, value.key) || strings.HasPrefix(strings.ToLower(value.key), strings.ToLower(ancestor.key)+`\`)) {
				covered = true
				break
			}
		}
		if !covered {
			seen[identity] = len(result)
			result = append(result, value)
		}
	}
	return result
}

func snapshotRegistry(values []canonicalRegistryProtection, limits registryGuardLimits) (map[string]registrySnapshotEntry, map[string]string, error) {
	result := map[string]registrySnapshotEntry{}
	owners := map[string]string{}
	budget := registrySnapshotBudget{limits: limits, seenKeys: map[string]struct{}{}}
	for _, value := range values {
		subkey := value.key[len(`HKCU\`):]
		if value.whole {
			if err := snapshotRegistryTree(subkey, value.label, result, owners, &budget); err != nil {
				return nil, nil, err
			}
		} else {
			if err := snapshotRegistryValue(subkey, value.valueName, value.label, result, owners, &budget); err != nil {
				return nil, nil, err
			}
		}
	}
	return result, owners, nil
}

type registrySnapshotBudget struct {
	limits   registryGuardLimits
	seenKeys map[string]struct{}
}

func (budget *registrySnapshotBudget) consumeKey(subkey string) error {
	identity := strings.ToLower(subkey)
	if _, ok := budget.seenKeys[identity]; ok {
		return nil
	}
	if budget.limits.keys <= 0 {
		return fmt.Errorf("%w: registry key budget", ErrGuardBudget)
	}
	budget.limits.keys--
	budget.seenKeys[identity] = struct{}{}
	return nil
}

func (budget *registrySnapshotBudget) consumeValue(size int) error {
	if size < 0 || budget.limits.values <= 0 || int64(size) > budget.limits.bytes {
		return fmt.Errorf("%w: registry value budget", ErrGuardBudget)
	}
	budget.limits.values--
	budget.limits.bytes -= int64(size)
	return nil
}

func snapshotRegistryTree(subkey, label string, result map[string]registrySnapshotEntry, owners map[string]string, budget *registrySnapshotBudget) error {
	handle, err := registry.OpenKey(registry.CURRENT_USER, subkey, registry.QUERY_VALUE|registry.ENUMERATE_SUB_KEYS)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: registry subtree unavailable", ErrUnsafeRegistry)
	}
	defer handle.Close()
	if err := budget.consumeKey(subkey); err != nil {
		return err
	}
	keyID := "k:" + strings.ToLower(subkey)
	result[keyID] = registrySnapshotEntry{kind: "key"}
	owners[keyID] = label
	names, err := handle.ReadValueNames(-1)
	if err != nil {
		return fmt.Errorf("%w: registry value enumeration failed", ErrUnsafeRegistry)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := snapshotOpenRegistryValue(handle, subkey, name, label, result, owners, budget); err != nil {
			return err
		}
	}
	children, err := handle.ReadSubKeyNames(-1)
	if err != nil {
		return fmt.Errorf("%w: registry subtree enumeration failed", ErrUnsafeRegistry)
	}
	sort.Strings(children)
	for _, child := range children {
		if err := snapshotRegistryTree(subkey+`\`+child, label, result, owners, budget); err != nil {
			return err
		}
	}
	return nil
}

func snapshotRegistryValue(subkey, name, label string, result map[string]registrySnapshotEntry, owners map[string]string, budget *registrySnapshotBudget) error {
	handle, err := registry.OpenKey(registry.CURRENT_USER, subkey, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: registry value unavailable", ErrUnsafeRegistry)
	}
	defer handle.Close()
	if err := budget.consumeKey(subkey); err != nil {
		return err
	}
	return snapshotOpenRegistryValue(handle, subkey, name, label, result, owners, budget)
}

func snapshotOpenRegistryValue(handle registry.Key, subkey, name, label string, result map[string]registrySnapshotEntry, owners map[string]string, budget *registrySnapshotBudget) error {
	size, valueType, err := handle.GetValue(name, nil)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil && !errors.Is(err, registry.ErrShortBuffer) {
		return fmt.Errorf("%w: registry value query failed", ErrUnsafeRegistry)
	}
	if err := budget.consumeValue(size); err != nil {
		return err
	}
	data := make([]byte, size)
	actual, valueType, err := handle.GetValue(name, data)
	if err != nil {
		return fmt.Errorf("%w: registry value read failed", ErrUnsafeRegistry)
	}
	data = data[:actual]
	if actual != size {
		return fmt.Errorf("%w: registry value changed during snapshot", ErrUnsafeRegistry)
	}
	id := "v:" + strings.ToLower(subkey+"\x00"+name)
	result[id] = registrySnapshotEntry{kind: "value", valueType: valueType, hash: sha256.Sum256(data)}
	owners[id] = label
	return nil
}
