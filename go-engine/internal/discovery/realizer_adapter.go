// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	realizernix "github.com/Artexis10/endstate/go-engine/internal/realizer/nix"
)

// RealizerInventoryAdapter reads the Endstate-owned package profile. Its Nix
// intent is already portable authority and therefore does not need a native
// alias mapping; catalog evidence can still enrich it with settings identity.
type RealizerInventoryAdapter struct {
	Realizer     realizer.Realizer
	DefaultInput string
}

func (RealizerInventoryAdapter) ID() string { return "endstate-profile" }

func (adapter RealizerInventoryAdapter) Inventory(ctx context.Context) (AdapterInventory, error) {
	if adapter.Realizer == nil {
		return AdapterInventory{}, ErrNotAvailable
	}
	if err := ctx.Err(); err != nil {
		return AdapterInventory{}, err
	}
	set, err := adapter.Realizer.Current()
	if err != nil {
		return AdapterInventory{}, fmt.Errorf("read Endstate-managed package profile: %w", err)
	}
	names := make([]string, 0, len(set.Elements))
	for name := range set.Elements {
		names = append(names, name)
	}
	sort.Strings(names)
	resolved := make([]ResolvedIntent, 0, len(names))
	for _, name := range names {
		element := set.Elements[name]
		attribute := profileAttribute(name, element)
		installable := profileInstallable(attribute, element, adapter.DefaultInput)
		input := installable
		if index := strings.LastIndexByte(input, '#'); index >= 0 {
			input = input[:index]
		}
		resolved = append(resolved, ResolvedIntent{
			ID: name, DisplayName: name, Attribute: attribute, Input: input, Installable: installable,
			MappingRevision: "endstate-profile-v1",
			Evidence: []Evidence{{
				Source: "endstate-profile", Kind: EvidencePackage, Ref: installable,
				Version: realizerNixVersion(element), DisplayName: name, UserFacing: true,
			}},
		})
	}
	return AdapterInventory{Resolved: resolved}, nil
}

func profileAttribute(name string, element realizer.Element) string {
	attribute := strings.TrimSpace(element.AttrPath)
	if index := strings.LastIndexByte(attribute, '.'); index >= 0 {
		attribute = attribute[index+1:]
	}
	if index := strings.LastIndexByte(attribute, '#'); index >= 0 {
		attribute = attribute[index+1:]
	}
	if attribute == "" {
		attribute = strings.TrimSpace(element.Name)
	}
	if attribute == "" {
		attribute = name
	}
	return attribute
}

func profileInstallable(attribute string, element realizer.Element, defaultInput string) string {
	for _, candidate := range []string{element.URL, element.OriginalURL} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || strings.HasPrefix(candidate, "path:") || strings.HasPrefix(candidate, "/") {
			continue
		}
		if strings.Contains(candidate, "#") {
			return candidate
		}
		refAttribute := strings.TrimSpace(element.AttrPath)
		if refAttribute == "" {
			refAttribute = attribute
		}
		return candidate + "#" + refAttribute
	}
	return strings.TrimSuffix(defaultInput, "#") + "#" + attribute
}

func realizerNixVersion(element realizer.Element) string {
	return realizernix.StorePathVersion(element.Name, element.StorePaths)
}
