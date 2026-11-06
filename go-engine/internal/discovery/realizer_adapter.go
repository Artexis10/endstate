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
		// No Nix executable means there is no Endstate-managed profile to read.
		if realizer.IsSpawnFailure(err) {
			return AdapterInventory{}, ErrNotAvailable
		}
		return AdapterInventory{}, fmt.Errorf("read Endstate-managed package profile: %w", err)
	}
	names := make([]string, 0, len(set.Elements))
	for name := range set.Elements {
		names = append(names, name)
	}
	sort.Strings(names)
	resolved := make([]ResolvedIntent, 0, len(names))
	var items []InventoryItem
	var warnings []Warning
	for _, name := range names {
		element := set.Elements[name]
		id := name
		if element.ID != "" {
			id = element.ID
		}
		attribute := profileAttribute(name, element)
		installable := profileInstallable(attribute, element, adapter.DefaultInput)
		if installable == "" {
			items = append(items, InventoryItem{Source: adapter.ID(), Kind: EvidencePackage, Ref: id, DisplayName: id, Explicit: true, UserFacing: true})
			warnings = append(warnings, Warning{Code: "input_provenance_unavailable", Source: adapter.ID(), ItemID: id, Message: "The installed native runtime entry has no portable immutable source."})
			continue
		}
		input := installable
		if index := strings.LastIndexByte(input, '#'); index >= 0 {
			input = input[:index]
		}
		resolved = append(resolved, ResolvedIntent{
			ID: id, DisplayName: id, Attribute: attribute, Input: input, Installable: installable,
			MappingRevision: "endstate-profile-v1",
			Evidence: []Evidence{{
				Source: "endstate-profile", Kind: EvidencePackage, Ref: installable,
				Version: realizerNixVersion(element), DisplayName: name, UserFacing: true,
			}},
		})
	}
	return AdapterInventory{Resolved: resolved, Items: items, Warnings: warnings}, nil
}

func profileAttribute(name string, element realizer.Element) string {
	if element.Resolution != nil {
		_, target, ok := strings.Cut(element.Resolution.ExpandedRef, "#")
		if ok {
			return target
		}
		return ""
	}
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
	if element.Resolution != nil {
		return realizer.PortableRef(element.Resolution)
	}
	// Unknown store flakes cannot be exported or relabelled as a release package.
	if strings.HasPrefix(element.URL, "path:/nix/store/") || strings.HasPrefix(element.OriginalURL, "path:/nix/store/") {
		return ""
	}
	// The native URL is its locked source. OriginalURL alone records requested
	// intent and cannot prove which source produced the installed outputs.
	candidate := strings.TrimSpace(element.URL)
	if candidate != "" && !strings.HasPrefix(candidate, "path:") && !strings.HasPrefix(candidate, "/") && !strings.HasPrefix(candidate, "flake:") && strings.Contains(candidate, ":") {
		if strings.Contains(candidate, "#") {
			return candidate
		}
		refAttribute := strings.TrimSpace(element.AttrPath)
		if refAttribute == "" {
			refAttribute = attribute
		}
		return candidate + "#" + refAttribute
	}
	// A release input does not establish a mapping for an unknown native item.
	return ""
}

func realizerNixVersion(element realizer.Element) string {
	return realizernix.StorePathVersion(element.Name, element.StorePaths)
}
