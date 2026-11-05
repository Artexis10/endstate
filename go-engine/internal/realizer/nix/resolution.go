// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

func unresolved(err error) *realizer.Error {
	if rerr, ok := err.(*realizer.Error); ok && (rerr.Code == envelope.ErrRealizerUnavailable || rerr.Code == envelope.ErrPermissionDenied) {
		return rerr
	}
	return &realizer.Error{Code: realizer.ErrInputProvenanceUnresolved, Subcode: "input-provenance", Stage: "eval", Raw: err.Error()}
}

func (b *Backend) native(args ...string) ([]byte, error) {
	out, stderr, exit, err := b.Run(args...)
	if err != nil {
		return nil, classify(-1, parsePlainLog(stderr), false)
	}
	if exit != 0 {
		classified := classify(exit, parsePlainLog(stderr), false)
		if classified.Code == envelope.ErrRealizerUnavailable || classified.Code == envelope.ErrPermissionDenied {
			return nil, classified
		}
		return nil, fmt.Errorf("native resolution failed (%d): %s", exit, stderr)
	}
	return out, nil
}

// Native Nix owns the installable grammar, including quoted attributes and ^*.
// These separators only retain its unchanged target beside native root metadata.
func (b *Backend) resolve(ref string) (*realizer.InputResolution, error) {
	expanded := b.ResolveInstallable(ref)
	root, target, hasTarget := strings.Cut(expanded, "#")
	defaultOutputs := ""
	if !hasTarget {
		if i := strings.LastIndexByte(root, '^'); i >= 0 {
			defaultOutputs = root[i:]
			root = root[:i]
		}
	}
	metadata, err := b.native("flake", "metadata", "--json", "--no-write-lock-file", root)
	if err != nil {
		return nil, unresolved(err)
	}
	var source struct {
		URL    string          `json:"url"`
		Path   string          `json:"path"`
		Locked json.RawMessage `json:"locked"`
	}
	if err = json.Unmarshal(metadata, &source); err != nil || source.URL == "" {
		return nil, unresolved(fmt.Errorf("native source metadata has no canonical URL"))
	}
	canonical := source.URL + defaultOutputs
	if hasTarget {
		canonical += "#" + target
	}
	if err = os.MkdirAll(filepath.Dir(b.Profile), 0700); err != nil {
		return nil, unresolved(err)
	}
	dir, err := os.MkdirTemp(filepath.Dir(b.Profile), "resolution-")
	if err != nil {
		return nil, unresolved(err)
	}
	defer os.RemoveAll(dir)
	lock := filepath.Join(dir, "flake.lock")
	result, err := b.native("build", "--dry-run", "--json", "--no-link", "--output-lock-file", lock, canonical)
	if err != nil {
		return nil, unresolved(err)
	}
	var built []struct {
		DrvPath string             `json:"drvPath"`
		Outputs map[string]*string `json:"outputs"`
	}
	if err = json.Unmarshal(result, &built); err != nil || len(built) != 1 || !storeDerivation(built[0].DrvPath) || len(built[0].Outputs) == 0 {
		// Ambiguous native selections cannot identify one app. Refusing this operation
		// prevents a changed target; the user pays a retry with a representable target.
		return nil, unresolved(fmt.Errorf("native build did not select one derivation and its outputs"))
	}
	if _, err = b.retain(built[0].DrvPath); err != nil {
		return nil, unresolved(err)
	}
	graph, err := os.ReadFile(lock)
	if err != nil || !json.Valid(graph) {
		return nil, unresolved(fmt.Errorf("native build did not retain its effective lock graph"))
	}
	// CmdEval and CmdBuild select the same native cursor. outputSpecified follows
	// native profile priority semantics, including explicit output selection.
	priorityExpr := `p: { drvPath = p.drvPath; priority = if p ? outputSpecified then 5 else p.meta.priority or 5; }`
	value, err := b.native("eval", "--json", "--no-write-lock-file", "--reference-lock-file", lock, "--apply", priorityExpr, canonical)
	if err != nil {
		return nil, unresolved(err)
	}
	var selected struct {
		DrvPath  string `json:"drvPath"`
		Priority *int   `json:"priority"`
	}
	if err = json.Unmarshal(value, &selected); err != nil || selected.DrvPath != built[0].DrvPath || selected.Priority == nil {
		// A mutable root can change between native commands. Never combine metadata
		// from different targets; retry costs less than installing an unintended app.
		return nil, unresolved(fmt.Errorf("native priority evaluation selected a different or unknown derivation"))
	}
	strength := "unknown"
	var locked map[string]json.RawMessage
	if json.Unmarshal(source.Locked, &locked) == nil {
		var rev, narHash string
		_ = json.Unmarshal(locked["rev"], &rev)
		_ = json.Unmarshal(locked["narHash"], &narHash)
		if rev != "" || narHash != "" {
			strength = "immutable"
		}
		var typ string
		_ = json.Unmarshal(locked["type"], &typ)
		if typ == "path" {
			strength = "mutable"
		}
	}
	r := &realizer.InputResolution{ExpandedRef: expanded, Source: realizer.InputSource{URL: source.URL, Path: source.Path, Locked: source.Locked, Strength: strength}, LockGraph: graph, Derivation: built[0].DrvPath, Priority: *selected.Priority, OutputPaths: map[string]string{}}
	for name, path := range built[0].Outputs {
		if !outputName(name) {
			return nil, unresolved(fmt.Errorf("invalid native output name"))
		}
		r.Outputs = append(r.Outputs, name)
		if path != nil && *path != "" {
			if !storePath(*path) {
				return nil, unresolved(fmt.Errorf("invalid native output path"))
			}
			r.OutputPaths[name] = *path
		}
	}
	sort.Strings(r.Outputs)
	r.MutationRef = r.Derivation + "^" + strings.Join(r.Outputs, ",")
	return r, nil
}

func (b *Backend) Prepare(items []realizer.Installable) ([]realizer.Installable, error) {
	if len(items) == 0 {
		return items, nil
	}
	if err := os.MkdirAll(filepath.Dir(b.Profile), 0700); err != nil {
		return nil, unresolved(err)
	}
	prepared := append([]realizer.Installable(nil), items...)
	data := map[string]any{}
	for i, item := range prepared {
		r, err := b.resolve(item.Ref)
		if err != nil {
			return nil, err
		}
		prepared[i].Resolution = r
		// The metadata identity also binds source provenance. Equal derivations
		// from different unused pins must not rewrite the installed source history.
		identity, err := json.Marshal(r)
		if err != nil {
			return nil, unresolved(err)
		}
		digest := sha256.Sum256(identity)
		data["p"+strconv.Itoa(i)] = map[string]any{"drvPath": r.Derivation, "priority": r.Priority, "id": item.ID, "requestedRef": item.Ref, "resolutionDigest": fmt.Sprintf("%x", digest)}
	}
	dir, err := os.MkdirTemp(filepath.Dir(b.Profile), "metadata-")
	if err != nil {
		return nil, unresolved(err)
	}
	defer os.RemoveAll(dir)
	encoded, err := json.Marshal(map[string]any{"system": nixSystem(), "packages": data})
	if err != nil {
		return nil, unresolved(err)
	}
	// Only typed JSON carries native paths and priorities. No requested Nix syntax
	// is interpolated into this input-free metadata flake.
	flake := `{ outputs = { self }: let data = builtins.fromJSON (builtins.readFile ./packages.json); in { packages.${data.system} = builtins.mapAttrs (_: p: (import (builtins.toPath p.drvPath)) // { meta.priority = p.priority; }) data.packages; }; }`
	if err = os.WriteFile(filepath.Join(dir, "packages.json"), encoded, 0600); err != nil {
		return nil, unresolved(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "flake.nix"), []byte(flake), 0600); err != nil {
		return nil, unresolved(err)
	}
	stored, err := b.native("store", "add-path", dir)
	if err != nil {
		return nil, unresolved(err)
	}
	wrapper := strings.TrimSpace(string(stored))
	if !storePath(wrapper) {
		return nil, unresolved(fmt.Errorf("native store did not snapshot the metadata flake"))
	}
	if _, err = b.retain(wrapper); err != nil {
		return nil, unresolved(err)
	}
	for i := range prepared {
		r := prepared[i].Resolution
		r.WrapperURL = "path:" + wrapper
		r.AttrPath = "packages." + nixSystem() + ".p" + strconv.Itoa(i)
		r.MutationRef = r.WrapperURL + "#p" + strconv.Itoa(i) + "^" + strings.Join(r.Outputs, ",")
	}
	b.prepared = prepared
	return prepared, nil
}

func (b *Backend) observeOutputs(r *realizer.InputResolution) error {
	if len(r.OutputPaths) == len(r.Outputs) {
		return nil
	}
	value, err := b.native("build", "--json", "--no-link", r.Derivation+"^"+strings.Join(r.Outputs, ","))
	if err != nil {
		return err
	}
	var built []struct {
		DrvPath string            `json:"drvPath"`
		Outputs map[string]string `json:"outputs"`
	}
	if json.Unmarshal(value, &built) != nil || len(built) != 1 || built[0].DrvPath != r.Derivation {
		return fmt.Errorf("committed output identity unavailable")
	}
	for _, name := range r.Outputs {
		path := built[0].Outputs[name]
		if !storePath(path) {
			return fmt.Errorf("committed output identity unavailable")
		}
		r.OutputPaths[name] = path
	}
	return nil
}

// Store paths and output names are closed native formats, never human meaning.
func storePath(path string) bool {
	return strings.HasPrefix(path, "/nix/store/") && filepath.Dir(path) == "/nix/store" && !strings.ContainsAny(path, "\x00\n\r")
}
func storeDerivation(path string) bool { return storePath(path) && strings.HasSuffix(path, ".drv") }
func outputName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '\'') {
			return false
		}
	}
	return true
}

// Bare store paths are native opaque installables. Unlike ^outputs, they root
// the derivation file itself without building package outputs.
func (b *Backend) retain(paths ...string) (string, error) {
	dir, err := os.MkdirTemp(filepath.Dir(b.Profile), "frozen-roots-")
	if err != nil {
		return "", err
	}
	root := filepath.Join(dir, "retained")
	b.rootDirs = append(b.rootDirs, dir)
	_, err = b.native(append([]string{"build", "--out-link", root}, paths...)...)
	if err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

// FinalizePreparation keeps committed roots as native history anchors. A dry
// run or failed preflight releases only the roots created by this backend run.
func (b *Backend) FinalizePreparation(committed bool) {
	if !committed {
		for _, dir := range b.rootDirs {
			_ = os.RemoveAll(dir)
		}
	}
	b.rootDirs = nil
}
