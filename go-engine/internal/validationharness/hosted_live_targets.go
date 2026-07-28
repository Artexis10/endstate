// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"fmt"
	"os"
)

type hostedLiveTargetFile struct {
	identity string
	absent   bool
	mode     os.FileMode
	size     int64
	sha256   string
	bytes    []byte
}

type hostedLiveTargetDirectory struct {
	identity string
	present  bool
}

// hostedLiveTargets preserves only semantic identity and file proof material.
// It never carries a resolved host path.
type hostedLiveTargets struct {
	files     []hostedLiveTargetFile
	directory hostedLiveTargetDirectory
}

func (targets hostedLiveTargets) Equal(other hostedLiveTargets) error {
	if !sameHostedLiveTargetDirectory(targets.directory, other.directory) || len(targets.files) != len(other.files) {
		return fmt.Errorf("hosted live target state differs")
	}
	for index := range targets.files {
		if !sameHostedLiveTargetFile(targets.files[index], other.files[index]) {
			return fmt.Errorf("hosted live target state differs")
		}
	}
	return nil
}

func (targets hostedLiveTargets) RequireSeeded() error {
	if err := validateHostedLiveTargets(targets); err != nil || targets.directory.present {
		return fmt.Errorf("hosted live seeded target state is invalid")
	}
	for _, file := range targets.files {
		switch file.identity {
		case "apps/notepad-plus-plus/config.xml", "apps/notepad-plus-plus/shortcuts.xml":
			if file.absent || file.size == 0 {
				return fmt.Errorf("hosted live seeded target state is invalid")
			}
		case "apps/notepad-plus-plus/contextMenu.xml", "apps/notepad-plus-plus/langs.xml", "apps/notepad-plus-plus/stylers.xml":
			if !file.absent {
				return fmt.Errorf("hosted live seeded target state is invalid")
			}
		default:
			return fmt.Errorf("hosted live seeded target state is invalid")
		}
	}
	return nil
}

func (targets hostedLiveTargets) RequireAbsent() error {
	if err := validateHostedLiveTargets(targets); err != nil || targets.directory.present {
		return fmt.Errorf("hosted live targets are not absent")
	}
	for _, file := range targets.files {
		if !file.absent {
			return fmt.Errorf("hosted live targets are not absent")
		}
	}
	return nil
}

func (targets hostedLiveTargets) captureSnapshots() ([]liveTargetSnapshot, error) {
	if err := validateHostedLiveTargets(targets); err != nil {
		return nil, err
	}
	snapshots := make([]liveTargetSnapshot, len(targets.files))
	for index, file := range targets.files {
		snapshots[index] = liveTargetSnapshot{Identity: file.identity, Absent: file.absent, Mode: file.mode, Size: file.size, SHA256: file.sha256, Bytes: append([]byte(nil), file.bytes...)}
	}
	return snapshots, nil
}

func compareHostedLiveRestoredTargets(before, after hostedLiveTargets) error {
	return before.Equal(after)
}

func compareHostedLiveRecoveryTargets(before, after hostedLiveTargets) error {
	return before.Equal(after)
}

func compareHostedLiveConvergenceTargets(before, after hostedLiveTargets) error {
	return before.Equal(after)
}

func sameHostedLiveTargetFile(left, right hostedLiveTargetFile) bool {
	return left.identity == right.identity && left.absent == right.absent && left.mode == right.mode && left.size == right.size && left.sha256 == right.sha256 && bytes.Equal(left.bytes, right.bytes)
}

func sameHostedLiveTargetDirectory(left, right hostedLiveTargetDirectory) bool {
	return left.identity == right.identity && left.present == right.present
}

func validateHostedLiveTargets(targets hostedLiveTargets) error {
	if targets.directory.identity != "apps/notepad-plus-plus/userDefineLangs" || len(targets.files) != 5 {
		return fmt.Errorf("hosted live target state is invalid")
	}
	expected := map[string]struct{}{
		"apps/notepad-plus-plus/config.xml":      {},
		"apps/notepad-plus-plus/contextMenu.xml": {},
		"apps/notepad-plus-plus/langs.xml":       {},
		"apps/notepad-plus-plus/shortcuts.xml":   {},
		"apps/notepad-plus-plus/stylers.xml":     {},
	}
	for _, file := range targets.files {
		if _, exists := expected[file.identity]; !exists {
			return fmt.Errorf("hosted live target state is invalid")
		}
		delete(expected, file.identity)
		if file.absent {
			if file.mode != 0 || file.size != 0 || file.sha256 != "" || len(file.bytes) != 0 {
				return fmt.Errorf("hosted live target state is invalid")
			}
			continue
		}
		if !file.mode.IsRegular() || file.size < 0 || int64(len(file.bytes)) != file.size || file.sha256 == "" {
			return fmt.Errorf("hosted live target state is invalid")
		}
	}
	if len(expected) != 0 {
		return fmt.Errorf("hosted live target state is invalid")
	}
	return nil
}
