// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/configvalidate"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

type stageCheckpointFunc func(context.Context, StagePhase, int) error

var errStageParentInsidePayload = errors.New("temporary stage parent is inside payload root")

// StageRequest is the exact planner-pinned input for one config-set staging
// run. Stage never looks up or rediscovers migration routes.
type StageRequest struct {
	CaptureID        string
	PayloadRoot      string
	PayloadManifest  []manifest.PayloadManifestEntry
	SourceGeneration string
	TargetGeneration *modules.GenerationDef
	MigrationEdges   []modules.MigrationEdgeDef
	TempParent       string
}

// StageResult owns a successful disposable staging root.
type StageResult struct {
	Root             string
	TargetGeneration string
	MigrationPath    []string

	closeOnce sync.Once
	closeErr  error
}

// Close removes the staging root. Repeated calls return the first result.
func (s *StageResult) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		if s.Root != "" {
			s.closeErr = os.RemoveAll(s.Root)
		}
	})
	return s.closeErr
}

// Stage verifies and copies one closed-world payload, applies only the pinned
// edge sequence, and validates every intermediate and final output.
func (e *Engine) Stage(ctx context.Context, request StageRequest) (result *StageResult, resultErr error) {
	if ctx == nil {
		return nil, newStageError(request, CodeInvalidStageRequest, PhaseRequestValidation, -1, fmt.Errorf("context is nil"))
	}
	if err := e.checkpoint(ctx, PhaseSourceIntegrity, -1); err != nil {
		return nil, newStageError(request, CodeCanceled, PhaseSourceIntegrity, -1, err)
	}
	if err := validateStageRequestShape(request); err != nil {
		return nil, newStageError(request, CodeInvalidStageRequest, PhaseRequestValidation, -1, err)
	}
	if err := bundle.VerifyPayloadManifest(request.PayloadRoot, request.PayloadManifest); err != nil {
		return nil, integrityStageError(request, PhaseSourceIntegrity, err)
	}

	tempParent, err := resolveStageParent(request.PayloadRoot, request.TempParent)
	if err != nil {
		code := stagePathCode(err)
		if errors.Is(err, errStageParentInsidePayload) {
			code = CodeUnsafeRoot
		}
		return nil, newStageError(request, code, PhaseStageCreate, -1, err)
	}
	stageRoot, err := os.MkdirTemp(tempParent, ".endstate-stage-*")
	if err != nil {
		return nil, newStageError(request, CodeIO, PhaseStageCreate, -1, err)
	}
	cleanup := true
	defer func() {
		if !cleanup {
			return
		}
		if err := os.RemoveAll(stageRoot); err != nil {
			cleanupErr := fmt.Errorf("remove failed stage %q: %w", stageRoot, err)
			if resultErr == nil {
				resultErr = newStageError(request, CodeIO, PhaseCleanup, -1, cleanupErr)
				return
			}
			var typed *Error
			if errors.As(resultErr, &typed) {
				typed.Err = errors.Join(typed.Err, cleanupErr)
			}
		}
	}()
	if err := os.Chmod(stageRoot, 0o700); err != nil {
		return nil, newStageError(request, CodeIO, PhaseStageCreate, -1, err)
	}
	if err := safepath.ValidateRoot(stageRoot); err != nil {
		return nil, newStageError(request, stagePathCode(err), PhaseStageCreate, -1, err)
	}

	if err := e.checkpoint(ctx, PhaseStageCopy, -1); err != nil {
		return nil, newStageError(request, CodeCanceled, PhaseStageCopy, -1, err)
	}
	manifestEntries := append([]manifest.PayloadManifestEntry(nil), request.PayloadManifest...)
	sort.Slice(manifestEntries, func(left, right int) bool {
		return manifestEntries[left].RelativePath < manifestEntries[right].RelativePath
	})
	for index, entry := range manifestEntries {
		if err := ctx.Err(); err != nil {
			stageErr := newStageError(request, CodeCanceled, PhaseStageCopy, -1, err)
			stageErr.Index = index
			return nil, stageErr
		}
		if err := copyStagePayloadFile(request.PayloadRoot, stageRoot, entry.RelativePath); err != nil {
			stageErr := newStageError(request, stagePathCode(err), PhaseStageCopy, -1, err)
			stageErr.Index = index
			stageErr.Operation = "file-copy"
			return nil, stageErr
		}
	}
	if err := bundle.VerifyPayloadManifest(stageRoot, request.PayloadManifest); err != nil {
		return nil, integrityStageError(request, PhaseStagedIntegrity, err)
	}

	migrationPath, err := validatePinnedEdges(request)
	if err != nil {
		return nil, newStageError(request, CodeInvalidMigrationPath, PhasePathValidation, -1, err)
	}
	for edgeIndex, edge := range request.MigrationEdges {
		if err := e.checkpoint(ctx, PhaseEdgeOperation, edgeIndex); err != nil {
			return nil, newStageError(request, CodeCanceled, PhaseEdgeOperation, edgeIndex, err)
		}
		if err := e.Apply(stageRoot, edge.Operations); err != nil {
			return nil, operationStageError(request, edgeIndex, err)
		}
		if err := e.checkpoint(ctx, PhaseEdgeValidation, edgeIndex); err != nil {
			return nil, newStageError(request, CodeCanceled, PhaseEdgeValidation, edgeIndex, err)
		}
		if err := configvalidate.ValidateStaging(stageRoot, edge.Validate); err != nil {
			return nil, validationStageError(request, PhaseEdgeValidation, edgeIndex, err)
		}
	}

	if err := e.checkpoint(ctx, PhaseTargetValidation, -1); err != nil {
		return nil, newStageError(request, CodeCanceled, PhaseTargetValidation, -1, err)
	}
	if err := configvalidate.ValidateStaging(stageRoot, request.TargetGeneration.Validate); err != nil {
		return nil, validationStageError(request, PhaseTargetValidation, -1, err)
	}

	result = &StageResult{
		Root:             stageRoot,
		TargetGeneration: request.TargetGeneration.ID,
		MigrationPath:    migrationPath,
	}
	cleanup = false
	return result, nil
}

func (e *Engine) checkpoint(ctx context.Context, phase StagePhase, index int) error {
	if e != nil && e.stageCheckpoint != nil {
		if err := e.stageCheckpoint(ctx, phase, index); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func resolveStageParent(payloadRoot, requested string) (string, error) {
	parent := requested
	if parent == "" {
		var err error
		parent, err = filepath.EvalSymlinks(os.TempDir())
		if err != nil {
			return "", err
		}
	}
	if err := safepath.ValidateRoot(parent); err != nil {
		return "", err
	}
	payloadRoot = filepath.Clean(payloadRoot)
	parent = filepath.Clean(parent)
	if containedHostPath(payloadRoot, parent) {
		return "", errStageParentInsidePayload
	}
	return parent, nil
}

func containedHostPath(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !filepath.IsAbs(relative) &&
		(relative == "." || len(relative) < 3 || relative[:3] != ".."+string(filepath.Separator))
}

func copyStagePayloadFile(payloadRoot, stageRoot, portablePath string) error {
	source, err := safepath.Resolve(payloadRoot, portablePath)
	if err != nil {
		return err
	}
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("payload source %q is not a regular file", portablePath)
	}
	if err := safepath.MkdirParent(stageRoot, portablePath, 0o700); err != nil {
		return err
	}
	destination, err := safepath.Resolve(stageRoot, portablePath)
	if err != nil {
		return err
	}
	return safepath.AtomicCopyFile(source, destination, info.Mode())
}

func validateStageRequestShape(request StageRequest) error {
	if request.CaptureID == "" {
		return fmt.Errorf("capture ID is required")
	}
	if request.PayloadRoot == "" || !filepath.IsAbs(request.PayloadRoot) {
		return fmt.Errorf("payload root must be an absolute path")
	}
	if request.PayloadManifest == nil {
		return fmt.Errorf("payload manifest is required")
	}
	if request.SourceGeneration == "" {
		return fmt.Errorf("source generation is required")
	}
	if request.TargetGeneration == nil || request.TargetGeneration.ID == "" {
		return fmt.Errorf("target generation is required")
	}
	return nil
}

func validatePinnedEdges(request StageRequest) ([]string, error) {
	if len(request.MigrationEdges) == 0 {
		if request.SourceGeneration != request.TargetGeneration.ID {
			return nil, fmt.Errorf("direct stage source %q does not equal target %q", request.SourceGeneration, request.TargetGeneration.ID)
		}
		return []string{}, nil
	}
	path := []string{request.SourceGeneration}
	seen := map[string]bool{request.SourceGeneration: true}
	current := request.SourceGeneration
	for index, edge := range request.MigrationEdges {
		if len(edge.Operations) == 0 {
			return nil, fmt.Errorf("edge %d %q -> %q has no operations", index, edge.From, edge.To)
		}
		if len(edge.Validate) == 0 {
			return nil, fmt.Errorf("edge %d %q -> %q has no validation", index, edge.From, edge.To)
		}
		if edge.From == "" || edge.To == "" || edge.From != current || edge.From == edge.To {
			return nil, fmt.Errorf("edge %d %q -> %q is not contiguous after %q", index, edge.From, edge.To, current)
		}
		if seen[edge.To] {
			return nil, fmt.Errorf("edge %d repeats generation %q", index, edge.To)
		}
		seen[edge.To] = true
		path = append(path, edge.To)
		current = edge.To
	}
	if current != request.TargetGeneration.ID {
		return nil, fmt.Errorf("pinned edges end at %q, target is %q", current, request.TargetGeneration.ID)
	}
	return path, nil
}

func integrityStageError(request StageRequest, phase StagePhase, err error) *Error {
	stageErr := newStageError(request, CodePayloadIntegrityFailed, phase, -1, err)
	stageErr.IntegrityCode = bundle.IntegrityDiagnosticCode(err)
	return stageErr
}

func operationStageError(request StageRequest, edgeIndex int, err error) *Error {
	stageErr := newStageError(request, CodeIO, PhaseEdgeOperation, edgeIndex, err)
	var operationErr *Error
	if errors.As(err, &operationErr) {
		stageErr.Code = operationErr.Code
		stageErr.Operation = operationErr.Operation
		stageErr.Index = operationErr.Index
	}
	return stageErr
}

func validationStageError(request StageRequest, phase StagePhase, edgeIndex int, err error) *Error {
	stageErr := newStageError(request, CodeValidationFailed, phase, edgeIndex, err)
	var validationErr *configvalidate.Error
	if errors.As(err, &validationErr) {
		stageErr.ValidationIndex = validationErr.Index
		stageErr.ValidationCode = string(validationErr.Code)
	}
	return stageErr
}

func newStageError(request StageRequest, code ErrorCode, phase StagePhase, edgeIndex int, err error) *Error {
	return &Error{
		Code:            code,
		Index:           -1,
		CaptureID:       request.CaptureID,
		Phase:           phase,
		EdgeIndex:       edgeIndex,
		ValidationIndex: -1,
		Err:             err,
	}
}

func stagePathCode(err error) ErrorCode {
	mapped := mapPathError(err)
	var typed *Error
	if errors.As(mapped, &typed) {
		return typed.Code
	}
	return CodeIO
}
