// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"sync"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

type boundaryEntry struct {
	Kind     string
	Digest   [sha256.Size]byte
	Mode     os.FileMode
	Size     int64
	Identity os.FileInfo
}

type boundaryTree map[string]boundaryEntry

func (runtime *scenarioRuntime) captureIndependentBoundaries(repositoryRoot, enginePath string) error {
	repository, err := snapshotBoundaryTree(repositoryRoot)
	if err != nil {
		return fmt.Errorf("snapshot repository boundary: %w", err)
	}
	guard, err := snapshotBoundaryTree(runtime.GuardRoot)
	if err != nil {
		return fmt.Errorf("snapshot guard boundary: %w", err)
	}
	working, err := snapshotBoundaryTree(runtime.ChildWorkingDir)
	if err != nil {
		return fmt.Errorf("snapshot child working-directory boundary: %w", err)
	}
	engine, err := snapshotBoundaryFile(enginePath)
	if err != nil {
		return fmt.Errorf("snapshot engine boundary: %w", err)
	}
	if err := runtime.validateAuthorityChildren(); err != nil {
		return fmt.Errorf("snapshot task authority: %w", err)
	}
	runtime.repositoryRoot = repositoryRoot
	runtime.enginePath = enginePath
	runtime.repositoryBoundary = repository
	runtime.guardBoundary = guard
	runtime.workingBoundary = working
	runtime.engineBoundary = engine
	return nil
}

func (runtime *scenarioRuntime) assertIndependentBoundaries() *Failure {
	if runtime.repositoryRoot == "" || runtime.enginePath == "" || runtime.repositoryBoundary == nil || runtime.guardBoundary == nil || runtime.workingBoundary == nil {
		return fail(CodeIsolationFailure, "guard", "boundary", "independent boundary snapshot is unavailable")
	}
	repository, err := snapshotBoundaryTree(runtime.repositoryRoot)
	if err != nil {
		return fail(CodeIsolationFailure, "guard", "repository", "repository changed during engine execution")
	}
	if difference := firstBoundaryDifference(runtime.repositoryBoundary, repository); difference != "" {
		return fail(CodeIsolationFailure, "guard", "repository", "repository changed during engine execution: "+difference)
	}
	guard, err := snapshotBoundaryTree(runtime.GuardRoot)
	if err != nil {
		return fail(CodeIsolationFailure, "guard", "originalHost", "original-host guard tree could not be read safely")
	}
	if difference := firstBoundaryDifference(runtime.guardBoundary, guard); difference != "" {
		return fail(CodeIsolationFailure, "guard", "originalHost", "original-host guard tree changed: "+difference)
	}
	working, err := snapshotBoundaryTree(runtime.ChildWorkingDir)
	if err != nil {
		return fail(CodeIsolationFailure, "guard", "workingDirectory", "child working directory could not be read safely")
	}
	if difference := firstBoundaryDifference(runtime.workingBoundary, working); difference != "" {
		return fail(CodeIsolationFailure, "guard", "workingDirectory", "child working directory changed during engine execution: "+difference)
	}
	engine, err := snapshotBoundaryFile(runtime.enginePath)
	if err != nil {
		return fail(CodeIsolationFailure, "guard", "engine", "engine executable could not be read safely")
	}
	if difference := boundaryEntryDifference(runtime.engineBoundary, engine); difference != "" {
		return fail(CodeIsolationFailure, "guard", "engine", "engine executable changed during execution: "+difference)
	}
	if err := runtime.validateAuthorityChildren(); err != nil {
		return fail(CodeIsolationFailure, "guard", "authority", "validation task authority gained an unexpected sibling")
	}
	return runtime.assertGuards()
}

func snapshotBoundaryTree(root string) (boundaryTree, error) {
	if err := safepath.ValidateRoot(root); err != nil {
		return nil, err
	}
	result := boundaryTree{}
	type regularFile struct {
		path string
		key  string
		info os.FileInfo
	}
	var files []regularFile
	err := filepath.Walk(root, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, current)
		if err != nil || filepath.IsAbs(relative) || relative == ".." {
			return fmt.Errorf("boundary member escaped root")
		}
		key := filepath.ToSlash(relative)
		if safepath.IsLinkOrReparse(info) {
			return fmt.Errorf("boundary member %q is linked or reparse-backed", key)
		}
		if info.IsDir() {
			result[key] = newBoundaryEntry("directory", info, [sha256.Size]byte{})
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("boundary member %q has unsupported type", key)
		}
		files = append(files, regularFile{path: current, key: key, info: info})
		return nil
	})
	if err != nil {
		return nil, err
	}

	type hashResult struct {
		digest [sha256.Size]byte
		err    error
	}
	hashes := make([]hashResult, len(files))
	workers := goruntime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	if workers > 8 {
		workers = 8
	}
	if workers > len(files) {
		workers = len(files)
	}
	jobs := make(chan int)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for index := range jobs {
				hashes[index].digest, hashes[index].err = hashBoundaryRegularFile(files[index].path, files[index].info)
			}
		}()
	}
	for index := range files {
		jobs <- index
	}
	close(jobs)
	wait.Wait()
	for index, file := range files {
		if hashes[index].err != nil {
			return nil, fmt.Errorf("hash boundary member %q: %w", file.key, hashes[index].err)
		}
		result[file.key] = newBoundaryEntry("file", file.info, hashes[index].digest)
	}
	return result, nil
}

func hashBoundaryRegularFile(path string, walked os.FileInfo) ([sha256.Size]byte, error) {
	var zero [sha256.Size]byte
	file, err := os.Open(path)
	if err != nil {
		return zero, err
	}
	before, err := file.Stat()
	if err != nil || !sameBoundaryFile(walked, before) {
		_ = file.Close()
		return zero, fmt.Errorf("member changed between walk and open")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		_ = file.Close()
		return zero, err
	}
	after, err := file.Stat()
	closeErr := file.Close()
	if err != nil || !sameBoundaryFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		return zero, fmt.Errorf("member changed while hashing")
	}
	if closeErr != nil {
		return zero, closeErr
	}
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return digest, nil
}

func sameBoundaryFile(expected, actual os.FileInfo) bool {
	return expected != nil && actual != nil && expected.Mode() == actual.Mode() && expected.Size() == actual.Size() &&
		expected.Mode().IsRegular() && actual.Mode().IsRegular() && !safepath.IsLinkOrReparse(actual) && os.SameFile(expected, actual)
}

func newBoundaryEntry(kind string, info os.FileInfo, digest [sha256.Size]byte) boundaryEntry {
	return boundaryEntry{
		Kind: kind, Digest: digest, Mode: info.Mode(), Size: info.Size(),
		Identity: info,
	}
}

func snapshotBoundaryFile(path string) (boundaryEntry, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return boundaryEntry{}, fmt.Errorf("boundary file path must be canonical absolute")
	}
	if err := safepath.ValidateRoot(filepath.Dir(path)); err != nil {
		return boundaryEntry{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return boundaryEntry{}, err
	}
	if safepath.IsLinkOrReparse(info) || !info.Mode().IsRegular() {
		return boundaryEntry{}, fmt.Errorf("boundary file must be regular and link-free")
	}
	digest, err := hashBoundaryRegularFile(path, info)
	if err != nil {
		return boundaryEntry{}, err
	}
	return newBoundaryEntry("file", info, digest), nil
}

func equalBoundaryTrees(expected, actual boundaryTree) bool {
	return firstBoundaryDifference(expected, actual) == ""
}

func firstBoundaryDifference(expected, actual boundaryTree) string {
	paths := make([]string, 0, len(expected))
	for path := range expected {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		candidate, exists := actual[path]
		if !exists {
			return fmt.Sprintf("member %q was removed", path)
		}
		if difference := boundaryEntryDifference(expected[path], candidate); difference != "" {
			return fmt.Sprintf("member %q %s", path, difference)
		}
	}
	paths = paths[:0]
	for path := range actual {
		if _, exists := expected[path]; !exists {
			paths = append(paths, path)
		}
	}
	if len(paths) > 0 {
		sort.Strings(paths)
		return fmt.Sprintf("member %q was added", paths[0])
	}
	return ""
}

func boundaryEntryDifference(expected, actual boundaryEntry) string {
	if expected.Kind != actual.Kind {
		return fmt.Sprintf("type changed from %s to %s", expected.Kind, actual.Kind)
	}
	if expected.Mode != actual.Mode {
		return fmt.Sprintf("mode changed from %s to %s", expected.Mode, actual.Mode)
	}
	if expected.Kind != "directory" && expected.Size != actual.Size {
		return fmt.Sprintf("size changed from %d to %d", expected.Size, actual.Size)
	}
	if expected.Identity == nil || actual.Identity == nil || !os.SameFile(expected.Identity, actual.Identity) {
		return "identity changed"
	}
	if expected.Digest != actual.Digest {
		return "content digest changed"
	}
	return ""
}

func (runtime *scenarioRuntime) snapshotOwnedTree(path string) (boundaryTree, bool, error) {
	if runtime == nil || runtime.Plan == nil || runtime.Plan.context == nil {
		return nil, false, fmt.Errorf("validation context is absent")
	}
	if err := runtime.Plan.context.ValidateSandboxPath(path); err != nil {
		return nil, false, err
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil || safepath.IsLinkOrReparse(info) || !info.IsDir() {
		return nil, false, fmt.Errorf("owned tree is linked or not a directory")
	}
	tree, err := snapshotBoundaryTree(path)
	return tree, true, err
}

func (runtime *scenarioRuntime) validateAuthorityChildren() error {
	if runtime.AuthorityRoot == "" || !filepath.IsAbs(runtime.AuthorityRoot) || filepath.Clean(runtime.AuthorityRoot) != runtime.AuthorityRoot {
		return fmt.Errorf("task authority is not canonical absolute")
	}
	if err := safepath.ValidateRoot(runtime.AuthorityRoot); err != nil {
		return err
	}
	expected := map[string]struct{}{
		filepath.Base(runtime.Root):            {},
		filepath.Base(runtime.GuardRoot):       {},
		filepath.Base(runtime.ChildWorkingDir): {},
	}
	if len(expected) != 3 || filepath.Dir(runtime.Root) != runtime.AuthorityRoot || filepath.Dir(runtime.GuardRoot) != runtime.AuthorityRoot ||
		filepath.Dir(runtime.ChildWorkingDir) != runtime.AuthorityRoot {
		return fmt.Errorf("validation roots are not exact task-authority children")
	}
	entries, err := os.ReadDir(runtime.AuthorityRoot)
	if err != nil || len(entries) != len(expected) {
		return fmt.Errorf("task authority has unexpected children")
	}
	for _, entry := range entries {
		if _, ok := expected[entry.Name()]; !ok {
			return fmt.Errorf("task authority has unexpected child")
		}
		info, err := os.Lstat(filepath.Join(runtime.AuthorityRoot, entry.Name()))
		if err != nil || safepath.IsLinkOrReparse(info) || !info.IsDir() {
			return fmt.Errorf("task authority child changed type")
		}
	}
	return nil
}
