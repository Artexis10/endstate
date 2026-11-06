// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package hmharvest

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
)

type Field string

// These fields and change kinds describe the fixed registry schema, not an
// operator-defined vocabulary or arbitrary JSON paths.
const (
	SchemaVersionField Field = "schemaVersion"
	NixpkgsField       Field = "inputs.nixpkgsRevision"
	HomeManagerField   Field = "inputs.homeManagerRevision"
	DocsHashField      Field = "docsJsonSha256"
	ProgramField       Field = "programs"
	OptionField        Field = "option"
	OptionTypeField    Field = "option.type"
	DeclarationField   Field = "option.declarations"
	SourceHashField    Field = "sourceHashes"
	ProbeTargetField   Field = "probe.targets"
	TargetField        Field = "targets"
)

type ChangeKind string

const (
	Added   ChangeKind = "added"
	Removed ChangeKind = "removed"
	Changed ChangeKind = "changed"
)

type Value struct {
	Present bool
	Value   string
}

type Change struct {
	Adapter string
	Field   Field
	Subject string
	Kind    ChangeKind
	Before  Value
	After   Value
}

type Observation struct {
	Adapter string
	Field   Field
	Subject string
}

type Report struct {
	Complete bool
	Changes  []Change
	Stopped  *Observation
}

type Result struct {
	Candidate *hmregistry.Registry
	Report    Report
}

// ObservationError keeps the underlying error available to errors.Is/As while
// bounding its diagnostic text, including errors supplied by a target prober.
type ObservationError struct {
	Observation Observation
	Err         error
}

func (e *ObservationError) Error() string {
	return fmt.Sprintf("refresh stopped: adapter=%s field=%s subject=%s: %s",
		DisplayValue(e.Observation.Adapter), e.Observation.Field,
		DisplayValue(e.Observation.Subject), DisplayValue(e.Err.Error()))
}

func (e *ObservationError) Unwrap() error { return e.Err }

func (r *Report) change(adapter string, field Field, subject string, before, after Value) {
	if before == after {
		return
	}
	kind := Changed
	if !before.Present {
		kind = Added
	} else if !after.Present {
		kind = Removed
	}
	r.Changes = append(r.Changes, Change{adapter, field, subject, kind, before, after})
}

func present(value string) Value { return Value{Present: true, Value: value} }

func (r *Report) members(adapter string, field Field, before, after []string) {
	old := make(map[string]bool, len(before))
	current := make(map[string]bool, len(after))
	for _, value := range before {
		old[value] = true
	}
	for _, value := range after {
		current[value] = true
		if !old[value] {
			r.change(adapter, field, value, Value{}, present(value))
		}
	}
	for _, value := range before {
		if !current[value] {
			r.change(adapter, field, value, present(value), Value{})
		}
	}
}

func (r *Report) sort() {
	sort.Slice(r.Changes, func(i, j int) bool {
		a, b := r.Changes[i], r.Changes[j]
		if a.Adapter != b.Adapter {
			return a.Adapter < b.Adapter
		}
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		if a.Subject != b.Subject {
			return a.Subject < b.Subject
		}
		return a.Kind < b.Kind
	})
}

const (
	maxChangeRows = 128
	maxValueBytes = 256
)

// DisplayValue quotes control characters before applying the UTF-8-safe byte
// limit. Display bounds never change the full values held by the report.
func DisplayValue(value string) string {
	quoted := strconv.Quote(strings.ToValidUTF8(value, "\uFFFD"))
	if len(quoted) <= maxValueBytes {
		return quoted
	}
	const suffix = "… [shortened]"
	end := maxValueBytes - len(suffix)
	for !utf8.RuneStart(quoted[end]) {
		end--
	}
	return quoted[:end] + suffix
}

func (r Report) String() string {
	var output strings.Builder
	fmt.Fprintf(&output, "Home Manager registry observations: complete=%t; %d observed changes", r.Complete, len(r.Changes))
	for index, change := range r.Changes {
		if index == maxChangeRows {
			fmt.Fprintf(&output, "\n%d change rows omitted", len(r.Changes)-index)
			break
		}
		fmt.Fprintf(&output, "\nchange %s adapter=%s field=%s subject=%s before(present=%t)=%s after(present=%t)=%s",
			change.Kind, DisplayValue(change.Adapter), change.Field, DisplayValue(change.Subject),
			change.Before.Present, DisplayValue(change.Before.Value), change.After.Present, DisplayValue(change.After.Value))
	}
	if r.Stopped != nil {
		fmt.Fprintf(&output, "\nstopped: adapter=%s field=%s subject=%s",
			DisplayValue(r.Stopped.Adapter), r.Stopped.Field, DisplayValue(r.Stopped.Subject))
	}
	return output.String()
}
