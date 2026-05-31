// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/provision"
)

// writeProvisioningGeneration records a Provisioning Generation for an apply, but
// only when the committed package set changed — i.e. at least one ref was
// installed OR removed (pruned) this run. Idempotent re-runs (nothing added or
// removed) write no generation. It is best-effort: a write error never fails the
// apply, mirroring run-history persistence.
//
// Separation of concerns: this records package facts only (the installed,
// already-present, and pruned refs). It never touches the config backup directory
// (state/backups/) or the restore revert journal.
func writeProvisioningGeneration(runID, backend string, actions []ApplyAction, removed []string, native string, partial bool) {
	items := make([]provision.ProvItem, 0, len(actions))
	added := make([]string, 0)
	for _, a := range actions {
		switch a.Status {
		case "installed":
			ref := derefRef(a.Ref)
			items = append(items, provision.ProvItem{ID: a.ID, Ref: ref, Status: "installed", Version: a.Version})
			added = append(added, ref)
		case "present":
			items = append(items, provision.ProvItem{ID: a.ID, Ref: derefRef(a.Ref), Status: "present", Version: a.Version})
		}
	}
	if len(added) == 0 && len(removed) == 0 {
		return // nothing added or removed → no new generation
	}
	_ = provision.Write(&provision.Generation{
		RunID:       runID,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Backend:     backend,
		Items:       items,
		AddedRefs:   added,
		RemovedRefs: removed,
		Native:      native,
		Partial:     partial,
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
