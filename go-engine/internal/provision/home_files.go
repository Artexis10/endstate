// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
)

// HomeFileExpectations records only the declared files in a frozen activation.
type HomeFileExpectations struct {
	Files []HomeFileExpectation `json:"files"`
}

type HomeFileExpectation struct {
	Target string `json:"target"`
	SHA256 string `json:"sha256"`
}

func (e *HomeFileExpectations) UnmarshalJSON(data []byte) error {
	type plain HomeFileExpectations
	var value plain
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("unexpected data after Home Manager file expectations")
	}
	result := HomeFileExpectations(value)
	if err := result.Validate(); err != nil {
		return err
	}
	*e = result
	return nil
}

func (e *HomeFileExpectations) Validate() error {
	if e == nil || e.Files == nil {
		return fmt.Errorf("Home Manager file expectations require a files array")
	}
	seen := make(map[string]bool, len(e.Files))
	for _, file := range e.Files {
		// Home Manager fixes POSIX absolute targets and lowercase SHA-256 hex.
		if !path.IsAbs(file.Target) || path.Clean(file.Target) != file.Target || file.Target == "/" || strings.ContainsRune(file.Target, 0) {
			return fmt.Errorf("Home Manager file expectation has an invalid absolute target")
		}
		if seen[file.Target] {
			return fmt.Errorf("Home Manager file expectations repeat a target")
		}
		seen[file.Target] = true
		digest, err := hex.DecodeString(file.SHA256)
		if err != nil || len(digest) != sha256.Size || strings.ToLower(file.SHA256) != file.SHA256 {
			return fmt.Errorf("Home Manager file expectation has an invalid SHA-256")
		}
	}
	return nil
}
