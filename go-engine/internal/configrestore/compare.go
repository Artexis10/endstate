// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"fmt"
)

// Comparison counts participating filesystem assertions separately from
// operations that this read-only comparison cannot verify.
type Comparison struct {
	Checked     int
	Unsupported int
	Drifted     bool
	Missing     bool
}

// CompareMaterialized compares current bytes with the materialized desired
// state. It creates no snapshots, backups, journals, or target writes.
func CompareMaterialized(ctx context.Context, set *MaterializedSet) (Comparison, error) {
	var result Comparison
	if ctx == nil || set == nil {
		return result, fmt.Errorf("comparison requires a context and materialized set")
	}
	ctx = withHostBoundary(ctx, set.boundary)
	for _, action := range set.Actions {
		if action.Kind == ActionDesktopValue {
			if action.DesktopTarget == nil || action.DesktopValue == nil {
				return result, fmt.Errorf("desktop desired state is missing")
			}
			actual, err := readDesktopSnapshot(ctx, *action.DesktopTarget)
			if err != nil {
				return result, err
			}
			again, err := readDesktopSnapshot(ctx, *action.DesktopTarget)
			if err != nil {
				return result, err
			}
			if actual.digest() != again.digest() {
				return result, fmt.Errorf("desktop state changed during comparison")
			}
			result.Checked++
			if !actual.Value.Equal(*action.DesktopValue) {
				result.Drifted = true
				result.Missing = result.Missing || !actual.Value.Present && action.DesktopValue.Present
			}
			continue
		}
		// ActionKind is the closed operation vocabulary of the materializer.
		switch action.Kind {
		case ActionCopy, ActionWriteFile, ActionDeleteFile:
		default:
			result.Unsupported++
			continue
		}
		ioAction, err := resolveActionForHostIO(action, set.boundary, set.instance)
		if err != nil {
			return result, err
		}
		current, desired, sourceDigest, checked, err := observeFilesystemAction(ctx, ioAction)
		if err != nil {
			return result, err
		}
		rechecked, err := scanFilesystemState(ctx, ioAction.Target)
		if err != nil {
			return result, err
		}
		if !statesEqual(current, rechecked) {
			return result, fmt.Errorf("filesystem target changed during comparison")
		}
		if action.Kind == ActionCopy {
			source, err := scanFilesystemState(ctx, ioAction.Source)
			if err != nil {
				return result, err
			}
			if source.Digest != sourceDigest {
				return result, fmt.Errorf("copy source changed during comparison")
			}
		}
		if checked == 0 {
			continue
		}
		result.Checked += checked
		if !statesEqual(current, desired) {
			result.Drifted = true
			result.Missing = result.Missing || current.Kind == StateAbsent
		}
	}
	return result, nil
}

// observeFilesystemAction is shared by comparison and snapshot preparation;
// only snapshot preparation publishes backups of the observed state.
func observeFilesystemAction(ctx context.Context, action Action) (prior, desired filesystemState, sourceDigest string, checked int, err error) {
	prior, err = scanFilesystemState(ctx, action.Target)
	if err != nil {
		return
	}
	if (action.Kind == ActionWriteFile || action.Kind == ActionDeleteFile) && prior.Kind == StateDirectory {
		err = fmt.Errorf("%s target changed to a directory", action.Kind)
		return
	}
	switch action.Kind {
	case ActionCopy:
		var source filesystemState
		source, err = scanFilesystemState(ctx, action.Source)
		if err != nil {
			return
		}
		if source.Kind == StateAbsent || action.SourceIsDirectory != (source.Kind == StateDirectory) {
			err = fmt.Errorf("copy source kind differs from materialized action")
			return
		}
		sourceDigest = source.Digest
		desired, checked, err = desiredCopyState(prior, source, action.Exclude)
	case ActionWriteFile:
		checked = 1
		desired = desiredWriteState(prior, action.DesiredContent)
	case ActionDeleteFile:
		checked = 1
		desired = absentFilesystemState()
	default:
		err = fmt.Errorf("unsupported filesystem action kind %q", action.Kind)
	}
	return
}
