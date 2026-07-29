// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationpilot

import (
	"crypto/sha256"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/validationaudit"
)

const V1CorpusRoot = "validation/ci-efficacy/pilot-v1"

var (
	ErrV1EvidenceInventory = errors.New("invalid v1 evidence inventory")
	v1EvidenceInventory    = []string{"windows-comparator.json", "ubuntu-comparator.json", "macos-comparator.json", "windows-baseline.json", "windows-detector.json"}
)

// V1AuthorityValidator validates the four immutable authorities before any
// candidate checkout or detector execution. Git operations are injected so the
// contract remains hermetic under unit test.
type V1AuthorityValidator struct {
	Resolve      func(reference string) (V1Reference, error)
	IsAncestor   func(ancestor, descendant string) (bool, error)
	ChangedPaths func(from, to string) ([]string, error)
}

// V1ChildCommand is the complete fixed child-process authority. It deliberately
// contains no shell source, free-form command, or inherited credentials.
type V1ChildCommand struct {
	Name string
	Args []string
	Env  []string
}

type V1ChildResult struct {
	Infrastructure string
	Rejected       bool
	Value          string
}

type V1ComparatorResult struct {
	Infrastructure string
	Rejected       bool
}

// RunV1Comparator preserves the only allowed comparator order.
func RunV1Comparator(run func(V1ChildCommand) V1ChildResult, parentEnv []string) V1ComparatorResult {
	if run == nil {
		return V1ComparatorResult{Infrastructure: "runner"}
	}
	env := V1ChildEnvironment(parentEnv)
	for _, args := range [][]string{{"vet", "./..."}, {"test", "./..."}} {
		result := run(V1ChildCommand{Name: "go", Args: args, Env: env})
		if result.Infrastructure != "" {
			return V1ComparatorResult{Infrastructure: result.Infrastructure}
		}
		if result.Rejected {
			return V1ComparatorResult{Rejected: true}
		}
	}
	return V1ComparatorResult{}
}

func (validator V1AuthorityValidator) Validate(manifest V1Manifest) error {
	if !validV1Manifest(manifest) || validator.Resolve == nil || validator.IsAncestor == nil || validator.ChangedPaths == nil {
		return errors.New("invalid v1 authority validation request")
	}
	for _, authority := range []V1Reference{manifest.Authorities.Evaluated, manifest.Authorities.Freeze, manifest.Authorities.Corpus, manifest.Authorities.Dispatch} {
		resolved, err := validator.Resolve(authority.Commit)
		if err != nil || resolved != authority {
			return errors.New("v1 authority does not match its commit or tree")
		}
	}
	for _, descendant := range []V1Reference{manifest.Authorities.Corpus, manifest.Authorities.Dispatch} {
		ancestor, err := validator.IsAncestor(manifest.Authorities.Freeze.Commit, descendant.Commit)
		if err != nil || !ancestor {
			return errors.New("v1 post-freeze authority is not a descendant")
		}
		paths, err := validator.ChangedPaths(manifest.Authorities.Freeze.Commit, descendant.Commit)
		if err != nil || !v1CorpusOnlyPaths(paths) {
			return errors.New("v1 post-freeze authority changes proof machinery")
		}
	}
	return nil
}

func v1CorpusOnlyPaths(paths []string) bool {
	for _, path := range paths {
		if path == "" || path == V1CorpusRoot || len(path) <= len(V1CorpusRoot)+1 || path[:len(V1CorpusRoot)+1] != V1CorpusRoot+"/" {
			return false
		}
	}
	return true
}

// LoadV1Manifest reads only the canonical future corpus manifest below one
// checked-out dispatch root.
func LoadV1Manifest(root, path string) (V1Manifest, error) {
	canonicalRoot, err := canonicalV1Root(root)
	if err != nil || filepath.Clean(path) != filepath.Join(canonicalRoot, filepath.FromSlash(V1CorpusRoot), "manifest.json") {
		return V1Manifest{}, errors.New("invalid v1 manifest path")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return V1Manifest{}, err
	}
	return DecodeV1Manifest(raw)
}

// ValidateV1Repository verifies the closed authority and command contracts
// before an attempt root can be created.
func ValidateV1Repository(root, manifestPath string) (V1Manifest, error) {
	canonicalRoot, err := canonicalV1Root(root)
	if err != nil {
		return V1Manifest{}, err
	}
	manifest, err := LoadV1Manifest(canonicalRoot, manifestPath)
	if err != nil {
		return V1Manifest{}, err
	}
	if err := ensureV1AuthorityObjects(canonicalRoot, manifest.Authorities); err != nil {
		return V1Manifest{}, err
	}
	if err := prepareV1AuthorityGraph(canonicalRoot); err != nil {
		return V1Manifest{}, err
	}
	if err := ensureCleanV1Dispatch(canonicalRoot, manifest.Authorities.Dispatch); err != nil {
		return V1Manifest{}, err
	}
	if manifest.ComparatorContractSHA256 != V1ComparatorContractSHA256() || manifest.DetectorContractSHA256 != V1DetectorContractSHA256() {
		return V1Manifest{}, errors.New("v1 command contract digest differs")
	}
	validator := V1AuthorityValidator{
		Resolve: func(reference string) (V1Reference, error) {
			commit, err := runV1Git(canonicalRoot, "rev-parse", reference+"^{commit}")
			if err != nil {
				return V1Reference{}, err
			}
			tree, err := runV1Git(canonicalRoot, "rev-parse", reference+"^{tree}")
			return V1Reference{Commit: commit, Tree: tree}, err
		},
		IsAncestor: func(ancestor, descendant string) (bool, error) {
			command := exec.Command("git", "-C", canonicalRoot, "merge-base", "--is-ancestor", ancestor, descendant)
			command.Env = V1ChildEnvironment(os.Environ())
			err := command.Run()
			if err == nil {
				return true, nil
			}
			if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
				return false, nil
			}
			return false, err
		},
		ChangedPaths: func(from, to string) ([]string, error) {
			output, err := runV1Git(canonicalRoot, "diff", "--name-only", "--no-renames", from+".."+to)
			if err != nil {
				return nil, err
			}
			if output == "" {
				return nil, nil
			}
			return strings.Split(output, "\n"), nil
		},
	}
	if err := validator.Validate(manifest); err != nil {
		return V1Manifest{}, err
	}
	for _, candidate := range manifest.Candidates {
		if _, err := validationaudit.LoadV1CandidatePatch(canonicalRoot, v1PatchRequest(candidate)); err != nil {
			return V1Manifest{}, err
		}
		mode, err := v1LoadedSidecarMode(canonicalRoot, candidate.Target)
		if err != nil || mode != lifecycleV1Mode(candidate.Lifecycle) {
			return V1Manifest{}, errors.New("v1 sidecar mode differs")
		}
	}
	return manifest, nil
}

func ensureV1AuthorityObjects(root string, authorities V1Authorities) error {
	for _, reference := range []V1Reference{authorities.Evaluated, authorities.Freeze, authorities.Corpus, authorities.Dispatch} {
		if _, err := runV1Git(root, "cat-file", "-e", reference.Commit+"^{commit}"); err == nil {
			continue
		}
		command := exec.Command("git", "-C", root, "-c", "credential.helper=", "-c", "http.extraheader=", "fetch", "--no-tags", "--filter=blob:none", v1RepositoryURL, reference.Commit)
		command.Env = V1ChildEnvironment(os.Environ())
		if err := command.Run(); err != nil {
			return errors.New("unable to acquire v1 authority")
		}
	}
	return nil
}

func prepareV1AuthorityGraph(root string) error {
	shallow, err := runV1Git(root, "rev-parse", "--is-shallow-repository")
	if err != nil || (shallow != "true" && shallow != "false") {
		return errors.New("unable to inspect v1 authority graph")
	}
	if shallow == "true" {
		return errors.New("v1 authority graph is shallow")
	}
	return nil
}

func ensureCleanV1Dispatch(root string, dispatch V1Reference) error {
	head, err := runV1Git(root, "rev-parse", "HEAD^{commit}")
	if err != nil || head != dispatch.Commit {
		return errors.New("dispatch checkout differs")
	}
	tree, err := runV1Git(root, "write-tree")
	if err != nil || tree != dispatch.Tree {
		return errors.New("dispatch index differs")
	}
	for _, arguments := range [][]string{{"diff", "--quiet"}, {"diff", "--cached", "--quiet"}} {
		command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
		command.Env = V1ChildEnvironment(os.Environ())
		if err := command.Run(); err != nil {
			return errors.New("dispatch worktree differs")
		}
	}
	foreign, err := runV1Git(root, "ls-files", "--others", "--exclude-standard")
	if err != nil || foreign != "" {
		return errors.New("dispatch worktree has foreign bytes")
	}
	return nil
}

func v1PatchRequest(candidate V1Candidate) validationaudit.V1PatchRequest {
	return validationaudit.V1PatchRequest{CandidateID: candidate.ID, Family: candidate.Family, PatchSHA256: candidate.PatchSHA256, ProductionFile: candidate.ProductionFile}
}

func V1ComparatorContractSHA256() string {
	return v1Digest("go vet ./...\ngo test ./...\n")
}

func V1DetectorContractSHA256() string {
	return v1Digest("go build -buildvcs=false -o endstate ./cmd/endstate\ngo build -buildvcs=false -o endstate-validation ./cmd/endstate-validation\nendstate-validation --engine endstate --repo repository --module module --scenario scenario\n")
}

func v1Digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmtV1Digest(sum)
}

func fmtV1Digest(sum [sha256.Size]byte) string {
	const hex = "0123456789abcdef"
	encoded := make([]byte, len(sum)*2)
	for index, value := range sum {
		encoded[index*2], encoded[index*2+1] = hex[value>>4], hex[value&0x0f]
	}
	return string(encoded)
}

// V1ChildEnvironment retains only the process paths and locale needed by Git,
// Go, and typed detector child processes. Credential and workflow authority
// variables are categorically excluded.
func V1ChildEnvironment(parent []string) []string {
	allowed := map[string]bool{"PATH": true, "SystemRoot": true, "SYSTEMROOT": true, "TMP": true, "TEMP": true, "HOME": true, "USERPROFILE": true, "APPDATA": true, "LOCALAPPDATA": true, "GOCACHE": true, "GOMODCACHE": true, "GOTELEMETRY": true}
	child := make([]string, 0, len(allowed)+2)
	for _, value := range parent {
		name, _, found := strings.Cut(value, "=")
		if found && allowed[name] {
			child = append(child, value)
		}
	}
	child = append(child, "GIT_CONFIG_NOSYSTEM=1")
	return child
}

func canonicalV1Root(root string) (string, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return "", errors.New("invalid v1 repository root")
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil || resolved != root {
		return "", errors.New("invalid v1 repository root")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", errors.New("invalid v1 repository root")
	}
	return root, nil
}

func runV1Git(root string, args ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	command.Env = V1ChildEnvironment(os.Environ())
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(output), "\n"), "\r"), nil
}

// AggregateV1Evidence accepts exactly the five controller-owned lane records.
// It never examines process output or workflow state.
func AggregateV1Evidence(manifest V1Manifest, root string) (V1Aggregate, error) {
	canonicalRoot, err := canonicalV1Root(root)
	if err != nil {
		return V1Aggregate{}, ErrV1EvidenceInventory
	}
	entries, err := os.ReadDir(canonicalRoot)
	if err != nil || len(entries) != len(v1EvidenceInventory) {
		return V1Aggregate{}, ErrV1EvidenceInventory
	}
	expected := make(map[string]bool, len(v1EvidenceInventory))
	for _, leaf := range v1EvidenceInventory {
		expected[leaf] = true
	}
	for _, entry := range entries {
		if entry.IsDir() || !expected[entry.Name()] {
			return V1Aggregate{}, ErrV1EvidenceInventory
		}
	}
	all := V1Evidence{SchemaVersion: V1SchemaVersion}
	for _, leaf := range v1EvidenceInventory {
		raw, readErr := os.ReadFile(filepath.Join(canonicalRoot, leaf))
		if readErr != nil {
			return V1Aggregate{}, ErrV1EvidenceInventory
		}
		evidence, decodeErr := DecodeV1Evidence(raw)
		if decodeErr != nil {
			return V1Aggregate{}, ErrV1EvidenceInventory
		}
		all.Attempts = append(all.Attempts, evidence.Attempts...)
	}
	if !validV1Evidence(all) {
		return V1Aggregate{}, ErrV1EvidenceInventory
	}
	return ClassifyV1(manifest, all)
}

// WriteV1AggregateNew atomically links one verified canonical aggregate into
// place and refuses to overwrite a previous decision artifact.
func WriteV1AggregateNew(path string, aggregate V1Aggregate) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) != "aggregate.json" {
		return errors.New("invalid v1 aggregate path")
	}
	parent := filepath.Dir(path)
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("invalid v1 aggregate root")
	}
	raw, _, err := EncodeV1Aggregate(aggregate)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(parent, ".aggregate-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Link(temporaryName, path); err != nil {
		return err
	}
	stored, err := os.ReadFile(path)
	if err != nil || string(stored) != string(raw) {
		return errors.New("v1 aggregate publication differs")
	}
	return nil
}
