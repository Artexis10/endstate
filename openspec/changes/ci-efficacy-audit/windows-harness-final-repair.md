# Windows harness final repair

## Status

Approved on 2026-07-30 for the final hosted v1 efficacy-pilot repair.

## Consumed-run evidence

Hosted run `30511284540` is preserved as `insufficient-signal`. Ubuntu and
macOS completed the exact comparator for all three frozen candidates. Windows
did not evaluate candidate semantics:

- the comparator child environment omitted `ComSpec`, so the first
  Windows-only live-process test failed before the comparator could classify
  any candidate; and
- the detector attempt environment retained inherited `TEMP` before appending
  the attempt-owned value, so the controller returned
  `detector_result_root` before launching the external validator.

Both failures have focused local reproductions. Mutation-relevant ordinary
tests still pass on all three frozen candidate trees, so the held-out corpus
remains eligible and unchanged.

## Considered repairs

1. Normalize the child and attempt environments, preserve only the required
   Windows command interpreter authority, and persist a typed infrastructure
   coordinate. This is selected because it repairs the causes without weakening
   hermeticity.
2. Make environment lookup use the last duplicate value. Rejected because the
   child process would still receive ambiguous duplicate keys and Windows
   lookup behavior would remain brittle.
3. Inherit the broader hosted-runner environment. Rejected because credentials,
   workflow authority, and unrelated ambient state must stay excluded.

## Design

`V1ChildEnvironment` retains the Windows `ComSpec` variable in addition to the
existing closed allowlist. Environment-name matching is case-insensitive only
on Windows; Unix matching remains case-sensitive.

`v1AttemptEnvironment` removes every key for which the attempt supplies an
owned value before appending those values. On Windows, removal and lookup are
case-insensitive. Each of `HOME`, `USERPROFILE`, `APPDATA`, `LOCALAPPDATA`,
`TEMP`, `TMP`, `TMPDIR`, `GOCACHE`, and `GOMODCACHE` appears exactly once and
points to the approved attempt or runner-owned location. The external detector
continues to fail closed unless its result root is the strict attempt-owned
`TEMP` descendant.

`V1Attempt` gains one bounded `infrastructureCoordinate` field. Infrastructure
attempts must contain a valid closed-form coordinate and non-infrastructure
attempts must not contain one. `finishV1Infrastructure` records the coordinate
it already receives. No raw command output, host path, token, or free-form log
is added to proof evidence.

## Tests and proof preservation

Focused RED tests must establish all four failures before implementation:

1. `V1ChildEnvironment` loses `ComSpec`, and the real Windows live-process test
   fails under that child environment.
2. An inherited `TEMP`/profile/cache set produces duplicate or non-owned values.
3. The duplicate environment prevents an injected external detector runner
   from being reached.
4. An infrastructure attempt loses its coordinate or accepts a coordinate on a
   non-infrastructure attempt.

GREEN requires those tests, the existing validation-pilot and workflow-contract
suites, `go vet`, exact Go `1.26.5` build/manifest validation, actionlint, and
authority/corpus identity checks. The three patches, reviews, candidate
metadata, evaluated commit/tree, expected detector coordinates, action pins,
manual-only trigger, and empty permissions remain byte-for-byte unchanged.

The repair becomes a new freeze commit with the corpus temporarily absent,
followed by an unchanged corpus-authority commit and a manifest commit binding
the new freeze/corpus/dispatch authorities. After independent review and
verification, the exact pushed SHA is dispatched once. No rerun of a consumed
SHA is permitted.

## Success criterion

The pilot is meaningful only if the new hosted run reports two green unmodified
Windows baselines per target, all three OS comparators passing every candidate,
two exact expected Windows detector rejections per candidate, and aggregate
`meaningful-signal` with 3/3 correct kills and zero survivors, wrong kills,
flakes, or infrastructure failures.
