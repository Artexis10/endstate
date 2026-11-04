// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// SnapExperimentalSource stays outside the default and advertised sources until
// the real Ubuntu source-contract check passes.
const SnapExperimentalSource = "snap-applications-experimental"

// These field-format rules come from snapd 2.76's naming and version validators;
// they prevent malformed source text from becoming persisted inventory evidence.
var (
	snapInstanceName = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*(?:_[a-z0-9]{1,10})?$`)
	snapVersion      = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9:.+~-]{0,30}[a-zA-Z0-9+~])?$`)
	snapAppName      = regexp.MustCompile(`^[a-zA-Z0-9](?:-?[a-zA-Z0-9])*$`)
)

type SnapAdapter struct{ Runner CommandRunner }

func (SnapAdapter) ID() string { return SnapExperimentalSource }

func (adapter SnapAdapter) Inventory(ctx context.Context) (AdapterInventory, error) {
	runner := adapter.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	// The native debug client has no request deadline. A slow experimental
	// source must not hold capture indefinitely; other sources remain usable.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	output, err := runner.Run(ctx, Command{
		Path: "snap", Args: []string{"debug", "api", "--disable-auth", "--fail", "--request", "GET", "/v2/snaps?select=enabled"},
		MaxOutputBytes: MaxCommandOutputBytes,
	})
	if err != nil {
		return AdapterInventory{}, fmt.Errorf("read local Snap applications: %w", err)
	}
	var response struct {
		Type       string `json:"type"`
		StatusCode int    `json:"status-code"`
		Result     *[]struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Type    string `json:"type"`
			Status  string `json:"status"`
			Apps    []struct {
				Name string `json:"name"`
			} `json:"apps"`
		} `json:"result"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return AdapterInventory{}, fmt.Errorf("Snap inventory did not return the expected JSON schema")
	}
	if response.Type != "sync" || response.StatusCode != 200 || response.Result == nil {
		return AdapterInventory{}, fmt.Errorf("Snap inventory did not return a successful local snapshot")
	}
	inventory := AdapterInventory{}
	missingCommands := false
	for _, snap := range *response.Result {
		if err := ctx.Err(); err != nil {
			return AdapterInventory{}, err
		}
		base, _, _ := strings.Cut(snap.Name, "_")
		if !snapInstanceName.MatchString(snap.Name) || len(base) < 2 || len(base) > 40 || strings.IndexFunc(base, func(r rune) bool { return r >= 'a' && r <= 'z' }) < 0 || !snapVersion.MatchString(snap.Version) || strings.TrimSpace(snap.Type) == "" || strings.TrimSpace(snap.Status) == "" {
			return AdapterInventory{}, fmt.Errorf("Snap inventory contains an invalid package identity or status")
		}
		// snapd defines these protocol enum values. select=enabled alone does
		// not exclude disabled current revisions in snapd 2.76.
		if snap.Type != "app" || snap.Status != "active" {
			inventory.Ignored++
			continue
		}
		if len(snap.Apps) == 0 {
			// Missing commands can mean content-only metadata or a snapd service
			// status failure; the response cannot distinguish those cases.
			inventory.Ignored++
			missingCommands = true
			continue
		}
		for _, app := range snap.Apps {
			if !snapAppName.MatchString(app.Name) {
				return AdapterInventory{}, fmt.Errorf("Snap inventory contains invalid application command metadata")
			}
		}
		inventory.Items = append(inventory.Items, InventoryItem{
			Source: "snap", Kind: EvidencePackage, Ref: snap.Name, Version: snap.Version,
			DisplayName: snap.Name, UserFacing: true,
		})
	}
	if missingCommands {
		inventory.Warnings = append(inventory.Warnings, Warning{
			Code: "snap_command_evidence_unavailable", Source: adapter.ID(),
			Message: "Some Snap entries did not expose runnable application metadata and were left out.",
		})
	}
	sort.Slice(inventory.Items, func(i, j int) bool { return inventory.Items[i].Ref < inventory.Items[j].Ref })
	return inventory, nil
}
