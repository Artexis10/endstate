// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	realizernix "github.com/Artexis10/endstate/go-engine/internal/realizer/nix"
)

var nixProfilePortableID = regexp.MustCompile(`^[a-z0-9]+(?:[-._][a-z0-9]+)*$`)

// NixUserProfileAdapter reads the user's ordinary default Nix profile. It is
// distinct from Endstate's owned profile and preserves a locked flake URL plus
// full attribute path whenever Nix recorded them.
type NixUserProfileAdapter struct {
	Runner       CommandRunner
	DefaultInput string
}

func (NixUserProfileAdapter) ID() string { return "nix-user-profile" }

func (adapter NixUserProfileAdapter) Inventory(ctx context.Context) (AdapterInventory, error) {
	runner := adapter.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	output, err := runner.Run(ctx, Command{
		Path: "nix", Args: []string{"profile", "list", "--json", "--no-pretty"},
		MaxOutputBytes: MaxCommandOutputBytes,
	})
	if err != nil {
		return AdapterInventory{}, fmt.Errorf("read the user Nix profile: %w", err)
	}
	set, err := realizernix.ParseProfileList(output)
	if err != nil {
		return AdapterInventory{}, fmt.Errorf("parse the user Nix profile: %w", err)
	}
	names := make([]string, 0, len(set.Elements))
	for name := range set.Elements {
		names = append(names, name)
	}
	sort.Strings(names)
	result := AdapterInventory{}
	seen := make(map[string]bool)
	for _, name := range names {
		element := set.Elements[name]
		id := strings.ToLower(strings.TrimSpace(name))
		if !nixProfilePortableID.MatchString(id) {
			id = strings.ToLower(profileAttribute(name, element))
		}
		if !nixProfilePortableID.MatchString(id) || seen[id] {
			result.Ignored++
			continue
		}
		seen[id] = true
		attribute := strings.TrimSpace(element.AttrPath)
		if attribute == "" {
			attribute = profileAttribute(name, element)
		}
		installable := profileInstallable(profileAttribute(name, element), element, adapter.DefaultInput)
		input := installable
		if index := strings.LastIndexByte(input, '#'); index >= 0 {
			input = input[:index]
		}
		result.Resolved = append(result.Resolved, ResolvedIntent{
			ID: id, DisplayName: name, Attribute: attribute, Input: input, Installable: installable,
			MappingRevision: "nix-user-profile-v1",
			Evidence: []Evidence{{
				Source: "nix-user-profile", Kind: EvidencePackage, Ref: installable,
				Version: realizerNixVersion(realizer.Element{
					Name: element.Name, StorePaths: element.StorePaths,
				}),
				DisplayName: name, UserFacing: true,
			}},
		})
	}
	return result, nil
}
