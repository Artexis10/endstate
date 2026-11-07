// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package settingscodec

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// A typed codec decodes one reviewed settings file into booleans, integers,
// strings and tables, rejects everything else, and encodes the decoded value
// canonically. Equivalence between a captured value and a built file is
// measured by decoding both with the same codec.
//
// The value model is: bool, int64, string and map[string]any.

//go:embed typed
var typedAssets embed.FS

// Key schema and format are data of a versioned codec, not logic: a spec file is
// reviewed with its codec ID, and a changed schema ships as a new ID. Tokens in
// the spec are closed sets that this file fixes ("toml" below; "bool", "int" and
// "string" leaves).
const (
	typedFormatTOML = "toml"
	leafBool        = "bool"
	leafInt         = "int"
	leafString      = "string"
)

// TypedCodec is one loaded codec spec.
type TypedCodec struct {
	id     string
	format string
	option string
	target string
	root   *schemaNode
}

type schemaNode struct {
	leaf     string
	children map[string]*schemaNode
}

type typedSpec struct {
	Format string         `json:"format"`
	Option string         `json:"option"`
	Target string         `json:"target"`
	Schema map[string]any `json:"schema"`
}

var (
	typedOnce   sync.Once
	typedLoaded map[string]*TypedCodec
)

func loadTypedCodecs() {
	typedLoaded = map[string]*TypedCodec{}
	entries, err := fs.ReadDir(typedAssets, "typed")
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || path.Ext(name) != ".jsonc" {
			continue
		}
		id := strings.TrimSuffix(name, ".jsonc")
		data, err := typedAssets.ReadFile("typed/" + name)
		if err != nil {
			continue
		}
		codec, err := parseTypedSpec(id, data)
		if err != nil {
			// A spec that does not parse leaves its ID unknown, which fails closed.
			continue
		}
		typedLoaded[id] = codec
	}
}

func parseTypedSpec(id string, data []byte) (*TypedCodec, error) {
	var spec typedSpec
	decoder := json.NewDecoder(bytes.NewReader(manifest.StripJsoncComments(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		return nil, fmt.Errorf("typed codec %q: %w", id, err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return nil, fmt.Errorf("typed codec %q: multiple JSON values", id)
	}
	if spec.Format != typedFormatTOML {
		return nil, fmt.Errorf("typed codec %q: format %q is not implemented", id, spec.Format)
	}
	if spec.Option == "" || spec.Target == "" {
		return nil, fmt.Errorf("typed codec %q: option and target are required", id)
	}
	root, err := parseSchemaTable(spec.Schema, "")
	if err != nil {
		return nil, fmt.Errorf("typed codec %q: %w", id, err)
	}
	if len(root.children) == 0 {
		return nil, fmt.Errorf("typed codec %q: schema is empty", id)
	}
	return &TypedCodec{id: id, format: spec.Format, option: spec.Option, target: spec.Target, root: root}, nil
}

func parseSchemaTable(table map[string]any, prefix string) (*schemaNode, error) {
	node := &schemaNode{children: map[string]*schemaNode{}}
	for key, value := range table {
		if key == "" || strings.ContainsAny(key, ".\x00") {
			return nil, fmt.Errorf("schema key %q%q is not a plain key", prefix, key)
		}
		switch typed := value.(type) {
		case string:
			if typed != leafBool && typed != leafInt && typed != leafString {
				return nil, fmt.Errorf("schema key %s%s has unknown leaf type %q", prefix, key, typed)
			}
			node.children[key] = &schemaNode{leaf: typed}
		case map[string]any:
			child, err := parseSchemaTable(typed, prefix+key+".")
			if err != nil {
				return nil, err
			}
			if len(child.children) == 0 {
				return nil, fmt.Errorf("schema table %s%s is empty", prefix, key)
			}
			node.children[key] = child
		default:
			return nil, fmt.Errorf("schema key %s%s is neither a leaf type nor a table", prefix, key)
		}
	}
	return node, nil
}

// Typed returns the embedded codec with this ID.
func Typed(id string) (*TypedCodec, bool) {
	typedOnce.Do(loadTypedCodecs)
	codec, ok := typedLoaded[id]
	return codec, ok
}

// TypedIDs lists every loadable typed codec in name order.
func TypedIDs() []string {
	typedOnce.Do(loadTypedCodecs)
	ids := make([]string, 0, len(typedLoaded))
	for id := range typedLoaded {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (c *TypedCodec) ID() string     { return c.id }
func (c *TypedCodec) Format() string { return c.format }

// Option is the Home Manager option that renders the decoded value.
func (c *TypedCodec) Option() string { return c.option }

// Target is the one reviewed live coordinate that this codec owns.
func (c *TypedCodec) Target() string { return c.target }

// Leaves lists every schema leaf as a dotted path, in name order.
func (c *TypedCodec) Leaves() []string {
	var leaves []string
	var walk func(node *schemaNode, prefix string)
	walk = func(node *schemaNode, prefix string) {
		for key, child := range node.children {
			if child.leaf != "" {
				leaves = append(leaves, prefix+key)
			} else {
				walk(child, prefix+key+".")
			}
		}
	}
	walk(c.root, "")
	sort.Strings(leaves)
	return leaves
}

// ValueLeaves lists the schema leaves that a decoded value sets.
func (c *TypedCodec) ValueLeaves(value map[string]any) []string {
	var leaves []string
	var walk func(table map[string]any, prefix string)
	walk = func(table map[string]any, prefix string) {
		for key, item := range table {
			if child, ok := item.(map[string]any); ok {
				walk(child, prefix+key+".")
			} else {
				leaves = append(leaves, prefix+key)
			}
		}
	}
	walk(value, "")
	sort.Strings(leaves)
	return leaves
}

// Decode parses data and returns its value. An empty document decodes to an
// empty value. Errors never carry document text or user-chosen key names.
func (c *TypedCodec) Decode(data []byte) (map[string]any, error) {
	if int64(len(data)) > MaxContentSize {
		return nil, fmt.Errorf("settings content exceeds the codec read bound")
	}
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return nil, fmt.Errorf("%s settings are not portable text", c.format)
	}
	raw, err := parseTOMLDocument(data)
	if err != nil {
		return nil, err
	}
	return c.normalize(raw)
}

// Encode returns the canonical encoding of value: sorted keys, no comments, no
// empty tables. It rejects a value that is outside the schema.
func (c *TypedCodec) Encode(value map[string]any) ([]byte, error) {
	normalized, err := c.normalize(value)
	if err != nil {
		return nil, err
	}
	return marshalTOMLDocument(normalized)
}

// Equal reports whether two values are the same canonical value.
func (c *TypedCodec) Equal(left, right map[string]any) bool {
	a, errA := c.Encode(left)
	b, errB := c.Encode(right)
	return errA == nil && errB == nil && bytes.Equal(a, b)
}

func (c *TypedCodec) normalize(raw map[string]any) (map[string]any, error) {
	value, err := normalizeTable(c.root, raw, "")
	if err != nil {
		return nil, err
	}
	return value, nil
}

// normalizeTable checks one table against its schema node and drops empty
// tables, which carry no setting. Anything the schema does not name is rejected,
// which covers unknown, empty and NUL keys, arrays, floats and datetimes.
func normalizeTable(node *schemaNode, raw map[string]any, prefix string) (map[string]any, error) {
	result := map[string]any{}
	for key, item := range raw {
		child, known := node.children[key]
		if !known {
			// The key is user-chosen and may be private, so only its table is named.
			return nil, fmt.Errorf("settings have a key outside the reviewed schema in table %q", strings.TrimSuffix(prefix, "."))
		}
		name := prefix + key
		if child.leaf == "" {
			table, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("settings key %s must be a table", name)
			}
			inner, err := normalizeTable(child, table, name+".")
			if err != nil {
				return nil, err
			}
			if len(inner) > 0 {
				result[key] = inner
			}
			continue
		}
		switch child.leaf {
		case leafBool:
			if _, ok := item.(bool); !ok {
				return nil, fmt.Errorf("settings key %s must be a boolean", name)
			}
		case leafInt:
			if _, ok := item.(int64); !ok {
				return nil, fmt.Errorf("settings key %s must be an integer", name)
			}
		case leafString:
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("settings key %s must be a string", name)
			}
			if !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 {
				return nil, fmt.Errorf("settings key %s is not portable text", name)
			}
		}
		result[key] = item
	}
	return result, nil
}

// TypedFixture is one reviewed live document of a typed codec.
type TypedFixture struct {
	Name string
	Data []byte
}

// TypedFixtures returns the reviewed live documents of a typed codec, in name
// order. Release tooling builds each one; the registry test checks that they
// cover every schema leaf.
func TypedFixtures(id string) ([]TypedFixture, error) {
	dir := "typed/fixtures/" + id
	entries, err := fs.ReadDir(typedAssets, dir)
	if err != nil {
		return nil, nil
	}
	var fixtures []TypedFixture
	for _, entry := range entries {
		if entry.IsDir() || path.Ext(entry.Name()) != ".toml" {
			continue
		}
		data, err := typedAssets.ReadFile(dir + "/" + entry.Name())
		if err != nil {
			return nil, err
		}
		fixtures = append(fixtures, TypedFixture{Name: strings.TrimSuffix(entry.Name(), ".toml"), Data: data})
	}
	return fixtures, nil
}
