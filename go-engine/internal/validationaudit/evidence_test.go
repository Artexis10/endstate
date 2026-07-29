// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationaudit

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAttemptEvidenceRoundTripForEveryKind(t *testing.T) {
	for _, evidence := range []AttemptEvidence{
		validControlEvidence(),
		validBaselineEvidence(1),
		validBaselineEvidence(2),
		validMutationEvidence(1),
		validMutationEvidence(2),
	} {
		raw, digest, err := EncodeAttemptEvidence(evidence)
		if err != nil {
			t.Fatalf("EncodeAttemptEvidence(%s) error = %v", evidence.Kind, err)
		}
		if len(raw) == 0 || raw[len(raw)-1] != '\n' || digest != evidenceDigest(raw) {
			t.Fatalf("EncodeAttemptEvidence(%s) = %q, %q; want canonical newline bytes and digest", evidence.Kind, raw, digest)
		}
		decoded, err := DecodeAttemptEvidence(raw)
		if err != nil || !reflect.DeepEqual(decoded, evidence) {
			t.Fatalf("DecodeAttemptEvidence(%s) = %#v, %v; want %#v", evidence.Kind, decoded, err, evidence)
		}
	}
}

func TestAttemptEvidenceRejectsInvalidCombinations(t *testing.T) {
	tests := []struct {
		name   string
		base   func() AttemptEvidence
		mutate func(*AttemptEvidence)
	}{
		{"control repetition two", validControlEvidence, func(e *AttemptEvidence) { e.Repetition = 2 }},
		{"control detector", validControlEvidence, func(e *AttemptEvidence) { e.Detector = DetectorEvidence{ID: "detector", ContractSHA256: testDigest} }},
		{"baseline candidate", func() AttemptEvidence { return validBaselineEvidence(1) }, func(e *AttemptEvidence) { e.CandidateID = "candidate" }},
		{"baseline patch", func() AttemptEvidence { return validBaselineEvidence(1) }, func(e *AttemptEvidence) { e.PatchSHA256 = testDigest }},
		{"baseline mutated tree", func() AttemptEvidence { return validBaselineEvidence(1) }, func(e *AttemptEvidence) { e.MutatedTree = testTree }},
		{"baseline lane", func() AttemptEvidence { return validBaselineEvidence(1) }, func(e *AttemptEvidence) { e.Lane = "windows-go" }},
		{"mutation legacy lane", func() AttemptEvidence { return validMutationEvidence(1) }, func(e *AttemptEvidence) { e.Lane = "windows-go" }},
		{"mutation missing candidate", func() AttemptEvidence { return validMutationEvidence(1) }, func(e *AttemptEvidence) { e.CandidateID = "" }},
		{"mutation missing detector", func() AttemptEvidence { return validMutationEvidence(1) }, func(e *AttemptEvidence) { e.Detector = DetectorEvidence{} }},
		{"unknown exit class", func() AttemptEvidence { return validMutationEvidence(1) }, func(e *AttemptEvidence) { e.ExitClass = "crashed" }},
		{"failure on pass", func() AttemptEvidence { return validMutationEvidence(1) }, func(e *AttemptEvidence) {
			e.Failure = &StableFailure{Class: "contract", Phase: "validation", Coordinate: "items[0]"}
		}},
		{"failure on canceled", func() AttemptEvidence { return validMutationEvidence(1) }, func(e *AttemptEvidence) {
			e.ExitClass = ExitClassCanceled
			e.Failure = &StableFailure{Class: "contract", Phase: "validation", Coordinate: "items[0]"}
		}},
		{"path in failure", func() AttemptEvidence { return validMutationEvidence(1) }, func(e *AttemptEvidence) {
			e.ExitClass = ExitClassRejected
			e.Failure = &StableFailure{Class: "contract", Phase: "validation", Coordinate: "C:/secret"}
		}},
		{"token in runner image", func() AttemptEvidence { return validMutationEvidence(1) }, func(e *AttemptEvidence) { e.Runner.Image = "token=secret" }},
		{"environment in toolchain", func() AttemptEvidence { return validMutationEvidence(1) }, func(e *AttemptEvidence) { e.Toolchain.Go = "GOPATH=/secret" }},
		{"non canonical timestamp", func() AttemptEvidence { return validMutationEvidence(1) }, func(e *AttemptEvidence) { e.StartedAt = "2026-07-29T12:00:00+00:00" }},
		{"negative duration", func() AttemptEvidence { return validMutationEvidence(1) }, func(e *AttemptEvidence) { e.DurationMillis = -1 }},
		{"mismatched duration", func() AttemptEvidence { return validMutationEvidence(1) }, func(e *AttemptEvidence) { e.DurationMillis++ }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evidence := tt.base()
			tt.mutate(&evidence)
			if _, _, err := EncodeAttemptEvidence(evidence); !errors.Is(err, ErrInvalidEvidence) {
				t.Fatalf("EncodeAttemptEvidence() error = %v, want %v", err, ErrInvalidEvidence)
			}
		})
	}
}

func TestDecodeAttemptEvidenceRejectsPresentForbiddenFields(t *testing.T) {
	tests := []struct {
		name  string
		base  AttemptEvidence
		field string
	}{
		{"control detector", validControlEvidence(), `"detector":null`},
		{"baseline candidate", validBaselineEvidence(1), `"candidateId":null`},
		{"baseline patch", validBaselineEvidence(1), `"patchSha256":""`},
		{"baseline mutated tree", validBaselineEvidence(1), `"mutatedTree":""`},
		{"baseline lane", validBaselineEvidence(1), `"lane":""`},
		{"mutation lane", validMutationEvidence(1), `"lane":""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, _, err := EncodeAttemptEvidence(tt.base)
			if err != nil {
				t.Fatal(err)
			}
			wire := strings.TrimSuffix(string(raw), "\n")
			wire = strings.TrimSuffix(wire, "}") + "," + tt.field + "}\n"
			if _, err := DecodeAttemptEvidence([]byte(wire)); !errors.Is(err, ErrInvalidEvidence) {
				t.Fatalf("DecodeAttemptEvidence() error = %v, want %v", err, ErrInvalidEvidence)
			}
		})
	}
}

func TestEncodeAttemptEvidenceOmitsForbiddenConditionalFields(t *testing.T) {
	tests := []struct {
		name      string
		evidence  AttemptEvidence
		forbidden []string
	}{
		{"control", validControlEvidence(), []string{`"detector"`}},
		{"baseline", validBaselineEvidence(1), []string{`"candidateId"`, `"patchSha256"`, `"mutatedTree"`, `"lane"`}},
		{"mutation", validMutationEvidence(1), []string{`"lane"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, _, err := EncodeAttemptEvidence(tt.evidence)
			if err != nil {
				t.Fatal(err)
			}
			for _, field := range tt.forbidden {
				if strings.Contains(string(raw), field) {
					t.Fatalf("EncodeAttemptEvidence() emitted forbidden field %s: %s", field, raw)
				}
			}
		})
	}
}

func TestDecodeAttemptEvidenceIsStrictAndBounded(t *testing.T) {
	evidence := validMutationEvidence(1)
	evidence.ExitClass = ExitClassRejected
	evidence.Failure = &StableFailure{Class: "contract", Phase: "validation", Coordinate: "items[0]"}
	raw, _, err := EncodeAttemptEvidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		raw  []byte
		want error
	}{
		{"empty", nil, ErrInvalidEvidence},
		{"duplicate nested key", []byte(strings.Replace(string(raw), `"class":"contract"`, `"class":"contract","class":"other"`, 1)), ErrDuplicateJSONKey},
		{"unknown log", []byte(strings.Replace(string(raw), `"exitClass":"rejected"`, `"exitClass":"rejected","stdout":"private log"`, 1)), ErrUnknownField},
		{"unknown environment", []byte(strings.Replace(string(raw), `"exitClass":"rejected"`, `"exitClass":"rejected","environment":"TOKEN=secret"`, 1)), ErrUnknownField},
		{"trailing value", append(append([]byte{}, raw...), []byte(" true")...), ErrTrailingJSONValue},
		{"wrong schema", []byte(strings.Replace(string(raw), `"schemaVersion":1`, `"schemaVersion":2`, 1)), ErrUnsupportedSchema},
		{"size boundary", append(append([]byte{}, raw...), []byte(strings.Repeat(" ", MaxEvidenceSize-len(raw)))...), nil},
		{"oversized", append(append([]byte{}, raw...), []byte(strings.Repeat(" ", MaxEvidenceSize-len(raw)+1))...), ErrInputTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got := DecodeAttemptEvidence(tt.raw)
			if got != tt.want {
				t.Fatalf("DecodeAttemptEvidence() error = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEncodeAttemptEvidenceIsDeterministic(t *testing.T) {
	evidence := validMutationEvidence(2)
	first, firstDigest, err := EncodeAttemptEvidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	second, secondDigest, err := EncodeAttemptEvidence(evidence)
	if err != nil || string(first) != string(second) || firstDigest != secondDigest {
		t.Fatalf("EncodeAttemptEvidence() = %q, %q, %v; want deterministic exact bytes", second, secondDigest, err)
	}
}

func validControlEvidence() AttemptEvidence {
	evidence := validMutationEvidence(1)
	evidence.Kind = EvidenceKindControl
	evidence.Repetition = 1
	evidence.Detector = DetectorEvidence{}
	evidence.Lane = "windows-go"
	return evidence
}

func validBaselineEvidence(repetition int) AttemptEvidence {
	evidence := validMutationEvidence(repetition)
	evidence.Kind = EvidenceKindDetectorBaseline
	evidence.CandidateID = ""
	evidence.PatchSHA256 = ""
	evidence.MutatedTree = ""
	evidence.Lane = ""
	return evidence
}

func validMutationEvidence(repetition int) AttemptEvidence {
	return AttemptEvidence{
		SchemaVersion:  SchemaVersion,
		AuditVersion:   "v1",
		AuditSHA256:    testDigest,
		CorpusVersion:  "v1",
		CorpusSHA256:   strings.Repeat("d", 64),
		Reference:      ReferenceIdentity{Commit: testCommit, Tree: testTree},
		Kind:           EvidenceKindDetectorMutation,
		AttemptID:      "attempt-01",
		Repetition:     repetition,
		CandidateID:    "candidate-01",
		PatchSHA256:    strings.Repeat("e", 64),
		MutatedTree:    strings.Repeat("f", 40),
		Detector:       DetectorEvidence{ID: "module-contract", ContractSHA256: testDigest},
		Runner:         RunnerIdentity{OS: "windows", Architecture: "amd64", Image: "windows-latest"},
		Toolchain:      ToolchainIdentity{Go: "go1.22.0"},
		CommandSHA256:  strings.Repeat("1", 64),
		StartedAt:      "2026-07-29T12:00:00Z",
		EndedAt:        "2026-07-29T12:00:01Z",
		DurationMillis: 1000,
		ExitClass:      ExitClassPassed,
	}
}

func evidenceDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return fmtDigest(sum)
}

func TestAttemptEvidenceDurationUsesUTCInstants(t *testing.T) {
	evidence := validMutationEvidence(1)
	evidence.StartedAt = time.Date(2026, 7, 29, 12, 0, 0, 999_999_999, time.UTC).Format(time.RFC3339Nano)
	evidence.EndedAt = time.Date(2026, 7, 29, 12, 0, 1, 0, time.UTC).Format(time.RFC3339Nano)
	evidence.DurationMillis = 0
	if _, _, err := EncodeAttemptEvidence(evidence); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("EncodeAttemptEvidence() error = %v, want exact millisecond duration rejection", err)
	}
}

func TestAttemptEvidenceRejectsSubMillisecondDurations(t *testing.T) {
	evidence := validMutationEvidence(1)
	evidence.EndedAt = "2026-07-29T12:00:01.000000001Z"
	evidence.DurationMillis = 1000
	if _, _, err := EncodeAttemptEvidence(evidence); !errors.Is(err, ErrInvalidEvidence) {
		t.Fatalf("EncodeAttemptEvidence() error = %v, want %v", err, ErrInvalidEvidence)
	}
}

func TestAttemptEvidenceRejectsTokenAndPathText(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*AttemptEvidence)
	}{
		{"token candidate", func(e *AttemptEvidence) { e.CandidateID = "ghp_secret" }},
		{"token runner", func(e *AttemptEvidence) { e.Runner.Image = "github_pat_secret" }},
		{"aws access key", func(e *AttemptEvidence) { e.CandidateID = "AKIAIOSFODNN7EXAMPLE" }},
		{"openai key", func(e *AttemptEvidence) { e.Runner.Image = "sk-proj-secret" }},
		{"gitlab token", func(e *AttemptEvidence) { e.Toolchain.Go = "glpat-secret" }},
		{"npm token", func(e *AttemptEvidence) { e.AttemptID = "npm_secret" }},
		{"slack token", func(e *AttemptEvidence) { e.AuditVersion = "xoxb-secret" }},
		{"bearer token", func(e *AttemptEvidence) { e.CorpusVersion = "bearer-secret" }},
		{"drive relative failure", func(e *AttemptEvidence) {
			e.ExitClass = ExitClassRejected
			e.Failure = &StableFailure{Class: "contract", Phase: "validation", Coordinate: "C:secret"}
		}},
		{"unix failure path", func(e *AttemptEvidence) {
			e.ExitClass = ExitClassRejected
			e.Failure = &StableFailure{Class: "contract", Phase: "validation", Coordinate: "/secret"}
		}},
		{"unc failure path", func(e *AttemptEvidence) {
			e.ExitClass = ExitClassRejected
			e.Failure = &StableFailure{Class: "contract", Phase: "validation", Coordinate: `\\server\share`}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evidence := validMutationEvidence(1)
			tt.mutate(&evidence)
			if _, _, err := EncodeAttemptEvidence(evidence); !errors.Is(err, ErrInvalidEvidence) {
				t.Fatalf("EncodeAttemptEvidence() error = %v, want %v", err, ErrInvalidEvidence)
			}
		})
	}
}

func TestPublishEvidenceCreatesAndVerifiesExactBytes(t *testing.T) {
	root := evidenceResultRoot(t)
	evidence := validMutationEvidence(1)
	publication, err := PublishEvidence(root, "attempt-01.json", evidence)
	if err != nil {
		t.Fatalf("PublishEvidence() error = %v", err)
	}
	raw, digest, err := EncodeAttemptEvidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(filepath.Join(root, "attempt-01.json"))
	if err != nil || string(stored) != string(raw) || publication.SHA256 != digest || publication.Size != int64(len(raw)) {
		t.Fatalf("PublishEvidence() = %#v, stored %q, %v; want exact verified bytes", publication, stored, err)
	}
	if info, statErr := os.Stat(filepath.Join(root, "attempt-01.json")); runtime.GOOS != "windows" && (statErr != nil || info.Mode().Perm()&0o077 != 0) {
		t.Fatalf("published mode = %v, %v; want private file", info.Mode(), statErr)
	}
}

func TestPublishEvidenceRefusesExistingLeafWithoutModification(t *testing.T) {
	root := evidenceResultRoot(t)
	evidence := validMutationEvidence(1)
	if _, err := PublishEvidence(root, "attempt-01.json", evidence); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(root, "attempt-01.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PublishEvidence(root, "attempt-01.json", evidence); !errors.Is(err, ErrEvidenceAlreadyExists) {
		t.Fatalf("second PublishEvidence() error = %v, want %v", err, ErrEvidenceAlreadyExists)
	}
	after, err := os.ReadFile(filepath.Join(root, "attempt-01.json"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("existing bytes changed: %q, %v", after, err)
	}
}

func TestPublishEvidenceRejectsUnsafeAuthorityAndNamesWithoutLeakingInputs(t *testing.T) {
	root := evidenceResultRoot(t)
	evidence := validMutationEvidence(1)
	for _, leaf := range []string{"", ".attempt.json", "attempt.JSON", "attempt.txt", "attempt/01.json", "..json", "con.json", "attempt .json", "attempt..json", "attempt-01.json."} {
		t.Run(leaf, func(t *testing.T) {
			_, err := PublishEvidence(root, leaf, evidence)
			if !errors.Is(err, ErrUnsafeEvidencePath) || strings.Contains(err.Error(), root) || leaf != "" && strings.Contains(err.Error(), leaf) {
				t.Fatalf("PublishEvidence() error = %q, want path-free %v", err, ErrUnsafeEvidencePath)
			}
		})
	}
	if _, err := PublishEvidence(filepath.Join(root, "missing"), "attempt-01.json", evidence); !errors.Is(err, ErrUnsafeEvidencePath) {
		t.Fatalf("PublishEvidence() missing root error = %v, want %v", err, ErrUnsafeEvidencePath)
	}
}

func TestPublishEvidenceRejectsLinkedRootAndIntermediate(t *testing.T) {
	root := evidenceResultRoot(t)
	evidence := validMutationEvidence(1)
	linkedRoot := filepath.Join(t.TempDir(), "linked-root")
	if err := os.Symlink(root, linkedRoot); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if _, err := PublishEvidence(linkedRoot, "attempt-01.json", evidence); !errors.Is(err, ErrUnsafeEvidencePath) {
		t.Fatalf("PublishEvidence() linked root error = %v, want %v", err, ErrUnsafeEvidencePath)
	}
	intermediate := filepath.Join(root, "results")
	if err := os.Symlink(t.TempDir(), intermediate); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if _, err := PublishEvidence(intermediate, "attempt-01.json", evidence); !errors.Is(err, ErrUnsafeEvidencePath) {
		t.Fatalf("PublishEvidence() linked intermediate error = %v, want %v", err, ErrUnsafeEvidencePath)
	}
}

func TestPublishEvidenceRejectsRootReplacedBeforeOpen(t *testing.T) {
	root := evidenceResultRoot(t)
	outside := evidenceResultRoot(t)
	previous := beforeEvidenceRootOpen
	beforeEvidenceRootOpen = func() {
		if err := os.Remove(root); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, root); err != nil {
			t.Skipf("symbolic links unavailable: %v", err)
		}
	}
	t.Cleanup(func() { beforeEvidenceRootOpen = previous })
	if _, err := PublishEvidence(root, "attempt-01.json", validMutationEvidence(1)); !errors.Is(err, ErrUnsafeEvidencePath) {
		t.Fatalf("PublishEvidence() replacement error = %v, want %v", err, ErrUnsafeEvidencePath)
	}
}

func evidenceResultRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}
