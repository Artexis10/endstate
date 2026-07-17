// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"os"

	"github.com/Artexis10/endstate/go-engine/internal/configvalidate"
	"github.com/Artexis10/endstate/go-engine/internal/migration"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

// ProcessObserver provides a read-only point-in-time process snapshot. The
// interface deliberately exposes no stop or kill capability.
type ProcessObserver interface {
	RunningProcessBasenames(context.Context) ([]string, error)
}

// ProcessObserverFunc adapts a function to ProcessObserver.
type ProcessObserverFunc func(context.Context) ([]string, error)

func (f ProcessObserverFunc) RunningProcessBasenames(ctx context.Context) ([]string, error) {
	return f(ctx)
}

// Request binds a successful disposable stage to the planner-pinned target
// generation. ProcessPatterns must come from the same trusted catalog snapshot
// as Plan; bundle data must never populate them.
type Request struct {
	Stage *migration.StageResult
	Plan  planner.PlanSet

	ProcessPatterns []string
	ProcessObserver ProcessObserver
}

// ActionKind is a concrete operation understood by the later transaction
// layer. Declarative merge/append/glob strategies no longer remain here.
type ActionKind string

const (
	ActionCopy        ActionKind = "copy"
	ActionWriteFile   ActionKind = "write-file"
	ActionDeleteFile  ActionKind = "delete-file"
	ActionRegistrySet ActionKind = "registry-set"
)

// RegistryValue is one normalized HKCU named-value desired state.
type RegistryValue struct {
	Key       string
	ValueName string
	ValueType string
	Data      string
}

// Action is one exact target operation. SnapshotRequired is unconditional for
// every action; legacy RestoreDef.Backup is intentionally ignored.
type Action struct {
	Kind     ActionKind
	Strategy string
	Source   string
	Target   string

	SourceMode        os.FileMode
	SourceIsDirectory bool
	Exclude           []string
	DesiredContent    []byte
	RegistryValue     *RegistryValue
	SnapshotRequired  bool
}

// MaterializedSet is safe to pass to a future backup/journal/commit layer. Its
// actions are ordered deterministically and contain no unresolved glob or
// merge operation.
type MaterializedSet struct {
	Actions     []Action
	Validations []configvalidate.ResolvedValidation
}
