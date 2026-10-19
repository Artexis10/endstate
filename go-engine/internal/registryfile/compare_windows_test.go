// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package registryfile

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

// TestSameKeyAndSameValueNameAgreeWithTheRegistry catches a name comparison
// that differs from the registry's own. strings.EqualFold merges the Kelvin
// sign and k, strings.ToLower merges İ and i, and an exact comparison splits É
// and é. For each pair the registry decides: a key or value created under one
// name either opens under the other or it does not.
func TestSameKeyAndSameValueNameAgreeWithTheRegistry(t *testing.T) {
	pairs := [][2]string{{"K", "k"}, {"İ", "i"}, {"É", "é"}}
	root := fmt.Sprintf(`Software\Endstate\NameTest-%d`, time.Now().UnixNano())
	parent, _, err := registry.CreateKey(registry.CURRENT_USER, root, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, pair := range pairs {
			if err := registry.DeleteKey(parent, pair[0]); err != nil && !errors.Is(err, registry.ErrNotExist) {
				t.Errorf("delete scratch key %s: %v", pair[0], err)
			}
		}
		parent.Close()
		if err := registry.DeleteKey(registry.CURRENT_USER, root); err != nil {
			t.Errorf("delete scratch key: %v", err)
		}
	})

	for _, pair := range pairs {
		created, _, err := registry.CreateKey(parent, pair[0], registry.QUERY_VALUE)
		if err != nil {
			t.Fatal(err)
		}
		created.Close()
		opened, err := registry.OpenKey(parent, pair[1], registry.QUERY_VALUE)
		if err == nil {
			opened.Close()
		} else if !errors.Is(err, registry.ErrNotExist) {
			t.Fatal(err)
		}
		// One side spells the hive in full, as module definitions may.
		if got := SameKey(`HKEY_CURRENT_USER\`+root+`\`+pair[0], `HKCU\`+root+`\`+pair[1]); got != (err == nil) {
			t.Errorf("SameKey for %q and %q = %v, registry = %v", pair[0], pair[1], got, err == nil)
		}

		if err := parent.SetStringValue(pair[0], "data"); err != nil {
			t.Fatal(err)
		}
		_, _, err = parent.GetStringValue(pair[1])
		if err != nil && !errors.Is(err, registry.ErrNotExist) {
			t.Fatal(err)
		}
		if got := SameValueName(pair[0], pair[1]); got != (err == nil) {
			t.Errorf("SameValueName(%q, %q) = %v, registry = %v", pair[0], pair[1], got, err == nil)
		}
	}
}
