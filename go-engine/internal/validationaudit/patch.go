// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationaudit

import (
	"crypto/sha256"
	"errors"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// MaxPatchSize bounds the exact reviewed patch bytes accepted by the audit.
	MaxPatchSize = 1024 * 1024
)

var (
	// ErrInvalidPatch reports a malformed or unsupported reviewed patch.
	ErrInvalidPatch = errors.New("validation audit invalid patch")
	// ErrUnsafePatchPath reports an unsafe corpus authority or filesystem path.
	ErrUnsafePatchPath = errors.New("validation audit unsafe patch path")
	// ErrIneligiblePatch reports a patch that changes unsupported production scope.
	ErrIneligiblePatch = errors.New("validation audit ineligible patch scope")
	// ErrPatchIdentityMismatch reports a patch digest or declared changed-path mismatch.
	ErrPatchIdentityMismatch = errors.New("validation audit patch identity mismatch")
	hunkPattern              = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@(?: .*)?$`)
	indexPattern             = regexp.MustCompile(`^index [0-9a-f]{7,64}\.\.[0-9a-f]{7,64}(?: 100[0-7]{3})?$`)
	patchPathSegmentPattern  = regexp.MustCompile(`^[A-Za-z0-9.][A-Za-z0-9._-]{0,127}$`)
)

// PatchIdentity is the compact identity of one exact eligible reviewed patch.
type PatchIdentity struct {
	SHA256       string
	TouchedPaths []string
}

// LoadCandidatePatch reads and validates one exact candidate patch below the
// fixed audit corpus root for corpusVersion.
func LoadCandidatePatch(repositoryRoot, corpusVersion string, candidate Candidate) (PatchIdentity, error) {
	root, err := canonicalRepositoryRoot(repositoryRoot)
	if err != nil {
		return PatchIdentity{}, ErrUnsafePatchPath
	}
	patchPath, err := candidatePatchPath(root, corpusVersion, candidate.PatchPath)
	if err != nil {
		return PatchIdentity{}, ErrUnsafePatchPath
	}
	raw, err := readSafePatch(root, candidate.PatchPath, patchPath)
	if err != nil {
		if errors.Is(err, ErrInvalidPatch) {
			return PatchIdentity{}, ErrInvalidPatch
		}
		return PatchIdentity{}, ErrUnsafePatchPath
	}
	digest := sha256.Sum256(raw)
	digestText := hexDigest(digest)
	if !sha256Pattern.MatchString(candidate.PatchSHA256) || candidate.PatchSHA256 != digestText {
		return PatchIdentity{}, ErrPatchIdentityMismatch
	}
	paths, err := parsePatch(raw)
	if err != nil {
		return PatchIdentity{}, ErrInvalidPatch
	}
	for _, path := range paths {
		if !eligiblePatchPath(path) {
			return PatchIdentity{}, ErrIneligiblePatch
		}
	}
	if !sameOrderedPaths(paths, candidate.TouchedPaths) {
		return PatchIdentity{}, ErrPatchIdentityMismatch
	}
	return PatchIdentity{SHA256: digestText, TouchedPaths: append([]string(nil), paths...)}, nil
}

func canonicalRepositoryRoot(root string) (string, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return "", ErrUnsafePatchPath
	}
	abs, err := filepath.Abs(root)
	if err != nil || abs != root {
		return "", ErrUnsafePatchPath
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || filepath.Clean(resolved) != root {
		return "", ErrUnsafePatchPath
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || unsafePathInfo(root, info) {
		return "", ErrUnsafePatchPath
	}
	return root, nil
}

func candidatePatchPath(root, corpusVersion, declared string) (string, error) {
	if !validCorpusVersion(corpusVersion) || !validRepositoryPath(declared) {
		return "", ErrUnsafePatchPath
	}
	prefix := "validation/ci-efficacy/" + corpusVersion + "/patches/"
	if !strings.HasPrefix(declared, prefix) || len(declared) == len(prefix) {
		return "", ErrUnsafePatchPath
	}
	path := filepath.Join(root, filepath.FromSlash(declared))
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", ErrUnsafePatchPath
	}
	return path, nil
}

func validCorpusVersion(value string) bool {
	return pathSegmentPattern.MatchString(value) && value != "." && value != ".." && !strings.HasSuffix(value, ".") && !strings.HasSuffix(value, " ") && !windowsReservedDeviceName(value)
}

func parsePatch(raw []byte) ([]string, error) {
	if len(raw) == 0 || len(raw) > MaxPatchSize || !utf8.Valid(raw) || strings.IndexByte(string(raw), 0) >= 0 {
		return nil, ErrInvalidPatch
	}
	text := string(raw)
	if strings.Contains(text, "\r") {
		text = strings.ReplaceAll(text, "\r\n", "\n")
		if strings.Contains(text, "\r") {
			return nil, ErrInvalidPatch
		}
	}
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil, ErrInvalidPatch
	}
	var paths []string
	seenPaths := map[string]struct{}{}
	for index := 0; index < len(lines); {
		path, next, err := parseFilePatch(lines, index)
		if err != nil || seen(seenPaths, path) {
			return nil, ErrInvalidPatch
		}
		paths = append(paths, path)
		index = next
	}
	return paths, nil
}

func parseFilePatch(lines []string, start int) (string, int, error) {
	path, err := parseDiffHeader(lines[start])
	if err != nil {
		return "", 0, err
	}
	index := start + 1
	headers := false
	for index < len(lines) && !strings.HasPrefix(lines[index], "@@ ") {
		line := lines[index]
		switch {
		case indexPattern.MatchString(line):
			index++
		case strings.HasPrefix(line, "--- "):
			oldPath, oldErr := parsePatchHeader(line, "--- a/")
			if oldErr != nil || oldPath != path || index+1 >= len(lines) {
				return "", 0, ErrInvalidPatch
			}
			newPath, newErr := parsePatchHeader(lines[index+1], "+++ b/")
			if newErr != nil || newPath != path {
				return "", 0, ErrInvalidPatch
			}
			headers = true
			index += 2
			if index >= len(lines) || !strings.HasPrefix(lines[index], "@@ ") {
				return "", 0, ErrInvalidPatch
			}
		default:
			return "", 0, ErrInvalidPatch
		}
	}
	if !headers || index >= len(lines) {
		return "", 0, ErrInvalidPatch
	}
	changed := false
	oldStart, oldEnd := -1, -1
	newStart, newEnd := -1, -1
	for index < len(lines) {
		if strings.HasPrefix(lines[index], "diff --git ") {
			break
		}
		hunk, hunkErr := parseHunkHeader(lines[index])
		if hunkErr != nil {
			return "", 0, ErrInvalidPatch
		}
		if oldStart >= 0 && (hunk.oldStart < oldStart || hunk.oldStart < oldEnd || hunk.newStart < newStart || hunk.newStart < newEnd) {
			return "", 0, ErrInvalidPatch
		}
		oldStart, oldEnd = hunk.oldStart, hunk.oldStart+hunk.oldCount
		newStart, newEnd = hunk.newStart, hunk.newStart+hunk.newCount
		oldRemaining, newRemaining := hunk.oldCount, hunk.newCount
		index++
		seenContent := false
		lastWasContent := false
		for index < len(lines) && !strings.HasPrefix(lines[index], "diff --git ") && !strings.HasPrefix(lines[index], "@@ ") {
			line := lines[index]
			switch {
			case strings.HasPrefix(line, " "):
				oldRemaining--
				newRemaining--
				seenContent = true
				lastWasContent = true
			case strings.HasPrefix(line, "-"):
				oldRemaining--
				changed = true
				seenContent = true
				lastWasContent = true
			case strings.HasPrefix(line, "+"):
				newRemaining--
				changed = true
				seenContent = true
				lastWasContent = true
			case line == "\\ No newline at end of file" && lastWasContent:
				lastWasContent = false
			default:
				return "", 0, ErrInvalidPatch
			}
			if oldRemaining < 0 || newRemaining < 0 {
				return "", 0, ErrInvalidPatch
			}
			index++
		}
		if oldRemaining != 0 || newRemaining != 0 || !seenContent {
			return "", 0, ErrInvalidPatch
		}
	}
	if !changed {
		return "", 0, ErrInvalidPatch
	}
	return path, index, nil
}

func parseDiffHeader(line string) (string, error) {
	if !strings.HasPrefix(line, "diff --git ") {
		return "", ErrInvalidPatch
	}
	parts := strings.Split(strings.TrimPrefix(line, "diff --git "), " ")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "a/") || !strings.HasPrefix(parts[1], "b/") {
		return "", ErrInvalidPatch
	}
	path := strings.TrimPrefix(parts[0], "a/")
	if path != strings.TrimPrefix(parts[1], "b/") || !validPatchRepositoryPath(path) {
		return "", ErrInvalidPatch
	}
	return path, nil
}

func parsePatchHeader(line, prefix string) (string, error) {
	if !strings.HasPrefix(line, prefix) {
		return "", ErrInvalidPatch
	}
	path := strings.TrimPrefix(line, prefix)
	if strings.ContainsAny(path, "\t ") || !validPatchRepositoryPath(path) {
		return "", ErrInvalidPatch
	}
	return path, nil
}

func validPatchRepositoryPath(path string) bool {
	if path == "" || len(path) > 256 || strings.Contains(path, `\`) || strings.HasPrefix(path, "/") || strings.Contains(path, "//") {
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if !patchPathSegmentPattern.MatchString(segment) || segment == "." || segment == ".." || strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") || windowsReservedDeviceName(segment) {
			return false
		}
	}
	return true
}

type patchHunk struct {
	oldStart int
	oldCount int
	newStart int
	newCount int
}

func parseHunkHeader(line string) (patchHunk, error) {
	matches := hunkPattern.FindStringSubmatch(line)
	if matches == nil {
		return patchHunk{}, ErrInvalidPatch
	}
	oldStart, oldCount, err := hunkCoordinates(matches[1], matches[2])
	if err != nil {
		return patchHunk{}, err
	}
	newStart, newCount, err := hunkCoordinates(matches[3], matches[4])
	if err != nil {
		return patchHunk{}, err
	}
	return patchHunk{oldStart: oldStart, oldCount: oldCount, newStart: newStart, newCount: newCount}, nil
}

func hunkCoordinates(startValue, countValue string) (int, int, error) {
	start, err := strconv.Atoi(startValue)
	if err != nil || start < 0 {
		return 0, 0, ErrInvalidPatch
	}
	count := 1
	if countValue != "" {
		count, err = strconv.Atoi(countValue)
		if err != nil || count < 0 {
			return 0, 0, ErrInvalidPatch
		}
	}
	if (start == 0 && count != 0) || (start != 0 && start > math.MaxInt-count) {
		return 0, 0, ErrInvalidPatch
	}
	return start, count, nil
}

func eligiblePatchPath(path string) bool {
	if strings.HasSuffix(path, "_test.go") || containsExcludedPathSegment(path) || containsHiddenPathSegment(path) {
		return false
	}
	if strings.HasPrefix(path, "go-engine/") {
		if !strings.HasSuffix(path, ".go") || strings.HasPrefix(path, "go-engine/cmd/endstate-validation/") {
			return false
		}
		for _, prefix := range []string{
			"go-engine/internal/validationaudit/",
			"go-engine/internal/validationci/",
			"go-engine/internal/validationharness/",
			"go-engine/internal/validationmatrix/",
			"go-engine/internal/validationmode/",
		} {
			if strings.HasPrefix(path, prefix) {
				return false
			}
		}
		return true
	}
	segments := strings.Split(path, "/")
	if len(segments) == 4 && segments[0] == "modules" && segments[1] == "apps" && validModuleID(segments[2]) && segments[3] == "module.jsonc" {
		return true
	}
	return len(segments) == 2 && segments[0] == "bundles" && validBundleFilename(segments[1])
}

func validModuleID(value string) bool {
	return pathSegmentPattern.MatchString(value) && !strings.HasPrefix(value, ".") && !windowsReservedDeviceName(value)
}

func validBundleFilename(value string) bool {
	if !strings.HasSuffix(value, ".jsonc") || strings.HasPrefix(value, ".") || !pathSegmentPattern.MatchString(value) {
		return false
	}
	name := strings.TrimSuffix(value, ".jsonc")
	switch name {
	case "config", "local", "manifest", "manifests", "payload", "runtime", "state":
		return false
	}
	return true
}

func containsExcludedPathSegment(path string) bool {
	segments := strings.Split(strings.ToLower(path), "/")
	for _, segment := range segments {
		if segment == "testdata" || segment == "fixtures" || strings.Contains(segment, "expected") || strings.Contains(segment, "golden") {
			return true
		}
	}
	return false
}

func containsHiddenPathSegment(path string) bool {
	for _, segment := range strings.Split(path, "/") {
		if strings.HasPrefix(segment, ".") {
			return true
		}
	}
	return false
}

func sameOrderedPaths(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func hexDigest(sum [sha256.Size]byte) string {
	const hex = "0123456789abcdef"
	encoded := make([]byte, len(sum)*2)
	for index, value := range sum {
		encoded[index*2] = hex[value>>4]
		encoded[index*2+1] = hex[value&0x0f]
	}
	return string(encoded)
}
