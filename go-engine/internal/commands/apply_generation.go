// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/packagesource"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
)

// writeProvisioningGeneration records a Provisioning Generation for an apply, but
// only when something was committed — i.e. at least one ref was installed OR
// removed (pruned) this run, OR a home-manager config was activated. Idempotent
// re-runs (nothing added, removed, or activated) write no generation. It returns
// persistence failures so callers can report committed state without provenance.
//
// Separation of concerns: this records package facts plus the engine-owned
// home-manager config reference (a flakeref + generation number — still install/
// provisioning facts, not config file contents). It never touches the config
// backup directory (state/backups/) or the restore revert journal.
func writeProvisioningGeneration(runID, backend string, actions []ApplyAction, removed []string, native string, partial bool, home *provision.HomeGenRef) error {
	items := make([]provision.ProvItem, 0, len(actions))
	added := make([]string, 0)
	addedPackages := make([]provision.PackageRecord, 0)
	changedExisting := false
	for _, a := range actions {
		switch a.Status {
		case "installed":
			ref := derefRef(a.Ref)
			items = append(items, provision.ProvItem{ID: a.ID, Ref: ref, Status: "installed", Version: a.Version, Resolution: a.Resolution})
			if a.WasPresent {
				changedExisting = true
			} else {
				added = append(added, ref)
				if backend == "winget" {
					addedPackages = append(addedPackages, provision.PackageRecord{Ref: ref, Source: packagesource.ResolveWinget(ref, a.Source)})
				}
			}
		case "present":
			items = append(items, provision.ProvItem{ID: a.ID, Ref: derefRef(a.Ref), Status: "present", Version: a.Version, Resolution: a.Resolution})
		}
	}
	if len(added) == 0 && len(removed) == 0 && home == nil && !changedExisting {
		return nil // nothing added, removed, or activated → no new generation
	}
	return provision.Write(&provision.Generation{
		RunID:         runID,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Backend:       backend,
		Items:         items,
		AddedRefs:     added,
		AddedPackages: addedPackages,
		RemovedRefs:   removed,
		Native:        native,
		Partial:       partial,
		HomeManager:   home,
	})
}

// derefRef returns the value of an ApplyAction ref pointer, or "" when nil
// (e.g. manual apps carry no package ref).
func derefRef(ref *string) string {
	if ref == nil {
		return ""
	}
	return *ref
}

func provenanceWriteError(err error) *envelope.Error {
	return envelope.NewError(envelope.ErrorCode("PROVENANCE_WRITE_FAILED"), "State committed, but provisioning provenance could not be saved.").WithDetail(map[string]interface{}{"committed": true, "raw": err.Error()})
}
