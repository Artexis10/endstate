// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/state"
)

// Dir returns the directory where Provisioning Generations are stored. It is
// resolved under the engine state directory via the config path resolver and is
// never hardcoded.
func Dir() string {
	return filepath.Join(state.StateDir(), "generations")
}

// Write assigns the next generation number and writes g to the default
// generations directory. See WriteTo.
func Write(g *Generation) error {
	return WriteTo(Dir(), g)
}

// List returns all Provisioning Generations from the default directory, newest
// first. A missing directory yields an empty slice and no error.
func List() ([]*Generation, error) {
	return ListFrom(Dir())
}

// WriteTo assigns g.Number = one greater than the highest existing number in
// dir, defaults g.SchemaVersion, and writes g to
// dir/<zero-padded number>.json using the atomic temp-file + rename pattern
// (matching internal/state). The directory is created if it does not exist.
func WriteTo(dir string, g *Generation) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	n, err := nextNumber(dir)
	if err != nil {
		return err
	}
	g.Number = n
	if g.SchemaVersion == "" {
		g.SchemaVersion = SchemaVersion
	}
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, fmt.Sprintf("%06d.json", n))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ListFrom returns all Provisioning Generations in dir, newest first (highest
// number first). A missing directory yields an empty slice and no error; .tmp
// and non-.json files are ignored. Mirrors state.ListRunHistory.
func ListFrom(dir string) ([]*Generation, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*Generation{}, nil
		}
		return nil, err
	}

	var names []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(name, ".json") && !strings.HasSuffix(name, ".tmp") {
			names = append(names, name)
		}
	}

	// Zero-padded numeric names: reverse lexical sort == newest (highest) first.
	sort.Sort(sort.Reverse(sort.StringSlice(names)))

	gens := make([]*Generation, 0, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		var g Generation
		if err := json.Unmarshal(data, &g); err != nil {
			return nil, err
		}
		gens = append(gens, &g)
	}
	return gens, nil
}

// nextNumber returns one greater than the highest existing generation number in
// dir, or 1 when none exist. Numbering is engine-owned and independent of any
// backend-native generation number.
func nextNumber(dir string) (int, error) {
	gens, err := ListFrom(dir)
	if err != nil {
		return 0, err
	}
	max := 0
	for _, g := range gens {
		if g.Number > max {
			max = g.Number
		}
	}
	return max + 1, nil
}
