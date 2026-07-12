// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package importer parses external package-list formats (currently UniGetUI
// `.ubundle` backups/bundles) and maps them onto Endstate manifest app entries.
// It is a pure, hermetic transform: no network access, no package operations,
// and byte-identical output for identical input.
package importer

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ExpectedExportVersion is the UniGetUI SerializableBundle version this parser
// was written against (matches SerializableBundle.ExpectedVersion upstream). A
// bundle declaring a different version parses anyway but yields a warning — the
// format is additive, so hard-failing would break on UniGetUI's next release.
const ExpectedExportVersion = 3

// ErrNotUniGetUIBundle is returned when the input is valid JSON but carries
// neither an export_version nor a packages key — i.e. it parses as JSON but is
// not a UniGetUI bundle (a mistakenly-passed package.json, for example). The
// command surfaces this as a validation error pointing back at --path.
var ErrNotUniGetUIBundle = errors.New(
	"importer: does not look like a UniGetUI bundle (missing export_version and packages)")

// Bundle is a UniGetUI SerializableBundle. The top-level field names are
// snake_case in UniGetUI's serialization while the package-level fields are
// PascalCase — that mixed casing is faithful to upstream (System.Text.Json
// serializes each model with its own property names). Unknown fields (e.g.
// incompatible_packages_info) are ignored by Go's decoder.
type Bundle struct {
	// ExportVersion is UniGetUI's export_version (a C# double). It is decoded
	// out of band (see coerceExportVersion) rather than via struct tags so a
	// non-numeric encoding degrades to a warning instead of failing the parse;
	// the json:"-" tag keeps the strict struct decode from choking on a string.
	ExportVersion        float64               `json:"-"`
	Packages             []Package             `json:"packages"`
	IncompatiblePackages []IncompatiblePackage `json:"incompatible_packages"`
}

// Package is a UniGetUI SerializablePackage. Only the fields Endstate maps are
// modelled; the rest of the upstream shape (Updates, the full InstallOptions
// surface) is ignored.
type Package struct {
	ID                  string         `json:"Id"`
	Name                string         `json:"Name"`
	Version             string         `json:"Version"`
	Source              string         `json:"Source"`
	ManagerName         string         `json:"ManagerName"`
	InstallationOptions InstallOptions `json:"InstallationOptions"`
}

// InstallOptions is a UniGetUI InstallOptions object. Only the Version pin is
// modelled — it is the sole field Endstate consumes (authored install intent).
// Upstream omits this object entirely when it holds only default values, so it
// is frequently absent; the zero value (empty Version) is correct in that case.
type InstallOptions struct {
	Version string `json:"Version"`
}

// IncompatiblePackage is a UniGetUI SerializableIncompatiblePackage: an entry
// UniGetUI itself could not install (a local source such as "Local PC", or an
// unavailable manager). It carries no ManagerName. Endstate passes these
// through to the import report verbatim — never silently dropped.
type IncompatiblePackage struct {
	ID      string `json:"Id"`
	Name    string `json:"Name"`
	Version string `json:"Version"`
	Source  string `json:"Source"`
}

// utf8BOM is the UTF-8 byte-order mark some editors prepend; it must be stripped
// before json.Unmarshal, which rejects a leading BOM.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// ParseUniGetUI reads a UniGetUI `.ubundle` (plain JSON, not JSONC) from r and
// returns the parsed Bundle. It hard-fails only on malformed JSON or a payload
// that is not a UniGetUI bundle at all (neither export_version nor packages —
// see ErrNotUniGetUIBundle). Otherwise it is forward-compatible: an
// export_version other than ExpectedExportVersion (or a non-numeric encoding)
// yields a warning (returned in the warnings slice), not an error, and unknown
// JSON fields are ignored.
func ParseUniGetUI(r io.Reader) (*Bundle, []string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, fmt.Errorf("importer: cannot read bundle: %w", err)
	}
	data = bytes.TrimPrefix(data, utf8BOM)

	// Probe the top-level object first: this validates that the payload is a JSON
	// object (malformed JSON hard-fails here) and lets us check for the bundle's
	// signature keys and coerce export_version tolerantly.
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, nil, fmt.Errorf("importer: invalid UniGetUI bundle JSON: %w", err)
	}

	// Wrong-file shape guard: a UniGetUI bundle always carries at least one of
	// export_version or packages. Neither present means this is some other JSON
	// file (e.g. a package.json), not a bundle.
	_, hasVersion := probe["export_version"]
	_, hasPackages := probe["packages"]
	if !hasVersion && !hasPackages {
		return nil, nil, ErrNotUniGetUIBundle
	}

	var b Bundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, nil, fmt.Errorf("importer: invalid UniGetUI bundle JSON: %w", err)
	}

	// Decode export_version out of band: a number (or quoted number) is honoured,
	// anything else degrades to a warning and an unknown version.
	version, recognized, coerceWarn := coerceExportVersion(probe["export_version"])
	b.ExportVersion = version

	var warnings []string
	if coerceWarn != "" {
		warnings = append(warnings, coerceWarn)
	}
	// Only flag a version mismatch when we actually recognized a version — an
	// unrecognized encoding already carries its own warning.
	if recognized && version != ExpectedExportVersion {
		warnings = append(warnings, fmt.Sprintf(
			"bundle export_version is %s; this build was written for version %d — proceeding, but the format may have changed",
			FormatExportVersion(version), ExpectedExportVersion))
	}

	return &b, warnings, nil
}

// coerceExportVersion tolerantly interprets the raw export_version value. A JSON
// number (integer or C# double) or a quoted number (e.g. "3") is honoured and
// reported as recognized. An absent key is treated as a recognized version 0 so
// the caller's mismatch warning fires (mirroring UniGetUI backups that omit it).
// Any other encoding (a non-numeric string, an object, an array, a bool) yields
// an unknown version and a warning — never a parse failure.
func coerceExportVersion(raw json.RawMessage) (value float64, recognized bool, warning string) {
	if raw == nil {
		return 0, true, ""
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return f, true, ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if parsed, perr := strconv.ParseFloat(strings.TrimSpace(s), 64); perr == nil {
			return parsed, true, ""
		}
	}
	return 0, false, "unrecognized export_version — treating the bundle as an unknown version and proceeding"
}

// FormatExportVersion renders the export_version number without a trailing ".0"
// so a message reads "4" rather than "4.000000".
func FormatExportVersion(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}
