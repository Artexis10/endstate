# Held-out CI efficacy preflight v1

## Decision

The v0 hosted run produced useful directional evidence but no valid score. Six
realistic production regressions passed the Windows and Ubuntu Go controls and
were rejected twice by the new detector, but the run mixed an impossible
Notepad++ integration precondition into a no-install experiment, lacked Go on
macOS, assembled evidence unsafely in PowerShell, treated fresh binary hashes
as source authority, and matched only one preregistered failure identity.

V1 is the final bounded proof before finalizing the PR #205 engine/module CI.
It is not a prerequisite for hosted-live, GUI, installer, or release work, and
the full 30-mutation audit is explicitly non-blocking post-merge work.

## Claim under test

At one exact reviewed reference, do the PR #205 verified-module detectors
correctly reject three previously unseen realistic module-lifecycle regressions
that the contemporaneous cross-platform Go gate accepts?

The comparator is exactly `go vet ./...` followed by `go test ./...` on fixed
Windows, Ubuntu, and macOS hosted runner families. Each lane uses the same
immutable `actions/setup-go` revision and exact Go `1.26.5` patch as the
required Go workflow, preserves that workflow's setup-cache behavior, supplies
no token input, and records the exact resolved toolchain and runner image.
Windows Notepad++ integration is
absent from this comparator rather than represented as failed infrastructure.

## Calibration and held-out boundary

The six v0 mutations and their actual hosted artifacts are calibration inputs
only. They test v1 evidence parsing, causal failure extraction, classification,
and infrastructure precedence, but receive no v1 efficacy credit.

After those paths pass locally, the detector, controller, and final operative
workflow are frozen. Only then may three new module-lifecycle product mutations
be authored across the `capture-contract` and `config-roundtrip-v1` scenario
modes, with at least one candidate in each mode. Each patch must change exactly
one production `.go` file, introduce one plausible user-visible or invariant-
level defect, and bind its expected stable detector failure before any detector
execution.

The controller uses a closed portable-file registry rather than directory
globs:

- capture: `bundle/capture_bundle.go`, `bundle/collect.go`,
  `bundle/config_capture.go`, `bundle/create.go`,
  `bundle/module_snapshot.go`, and `bundle/payload_manifest.go`;
- schema-v1 restore: `restore/append.go`, `restore/backup.go`,
  `restore/copy.go`, `restore/delete_glob.go`, `restore/merge_ini.go`,
  `restore/merge_json.go`, `restore/registry_import.go`, `restore/restore.go`,
  `restore/revert.go`, and `restore/target_safety.go`.

Every path is beneath `go-engine/internal/`; files outside this exact registry,
OS-suffixed files, validation-boundary files, and `_test.go` files are not
eligible.

The controller rejects changes to detector or audit machinery, validation and
matrix packages, workflow or CI code, CLI validation entry points, tests,
fixtures, `testdata`, expected/golden output, production modules or bundles,
generated files, binaries, and every path outside the allowlist. A candidate
must not alter the detector, harness, reason mapping, or its own evidence
contract. This keeps the mutant on the product side of the boundary while the
frozen detector source and command contract remain the observer.

A candidate also must not mention its candidate, module, bundle, scenario, or
detector identifier in an added line; inspect validation/test-mode environment
state; alter Go build constraints; or branch on validation-only behavior. The
fault must be a context-independent deletion, substitution, or boundary change
to existing product logic, not logic constructed to fool one known detector.

Each candidate manifest record includes its lifecycle, exact production file,
fault description, normal product entrypoint, live-reachability explanation,
and immutable independent-review record digest. The controller enforces the
closed path/lifecycle mapping and record shape. The reviewer—not a path
heuristic—must establish that the changed statement is reachable from the
normal non-validation CLI entrypoint and the chosen scenario, is not validation-
only adaptation, and does not alter detector result types, reason mapping, or
evidence contracts.

The controller also loads the selected sidecar and binds lifecycle to authored
scenario mode: capture files require `capture-contract`; schema-v1 restore files
require `config-roundtrip-v1`. Aggregation counts the verified sidecar mode, not
the manifest label, and requires at least one candidate in each mode.

Held-out means operator- and invariant-level disjointness, not merely different
files. V1 candidates must not reuse v0's duplicate membership, missing module,
bundle-ID drift, backup-disabled, restore-source-drift, or restore-target-drift
operators, or re-express the same violated invariant under another application
or bundle. The manifest records an independently reviewed operator and violated-
invariant fingerprint, and the controller rejects any fingerprint present in
the v0 calibration registry.

An independent reviewer checks realism, non-equivalence, operator/invariant
disjointness, patch scope, expected failure identity, and ordering without
running a held-out mutation through the detector. Applying a patch and running
comparator controls does not open the detector outcome. The first official
hosted detector execution opens the held-out results. A mismatch remains a
wrong kill; it may not be repaired by editing the expected result or denominator.

Targets already exercised end to end by the Windows Go comparator are
ineligible: `apps.notepad-plus-plus/default-v1`,
`apps.kubectl/install-v1`, `apps.mgba/reviewed-capture-v1`,
`apps.windows-terminal/generation-preferences-g1-97631ba2d2e5`,
`apps.owncloud/generation-preferences-g1-1c4479cb88b9`,
`apps.owncloud/generation-preferences-g2-899536c068d4`,
`apps.owncloud/migration-preferences-g1-to-g2`, and
`apps.studio-one/generation-preferences-g1-61e9f6f3c254`. The controller freezes
and enforces this exclusion registry before corpus authoring.

Candidates must leave the production data parseable, schema-valid,
revision-consistent, and admitted to the exact targeted module/scenario.
Compile, revision, schema, selection, fixture construction, admission,
aggregate, envelope, and other shallow guard failures are ineligible for kill
credit. `unsupported_fixture` is always ineligible. Correct-kill credit uses a
closed positive table only: `artifact_contract` in `capture` or `rebuild`;
`content_mismatch` or `event_contract` in `capture`, `rebuild`, `verify`, or
`revert`; and `revert_failure` in `revert`. Every other class/phase pair is
uncreditable even if preregistered.

Catalog-matrix mutations are excluded from v1 because the contemporaneous
Windows Go comparator already builds the same mutated engine and runs the full
production catalog matrix end to end. That job may improve visibility, but it
cannot supply a new-only kill against this comparator and is not efficacy
evidence. Schema-v2 mutations are excluded for the same reason: Windows Go
tests already execute all five production generation scenarios and the sole
production migration scenario end to end.

## Authority and evidence

One typed Go controller owns acquisition, patch verification/application,
bounded command execution, detector invocation, evidence publication, and
aggregation. Workflow shell only provisions the pinned Go action, invokes the
controller, and uploads its bounded artifacts. It must not compose or classify
JSON.

Every detector attempt reproduces PR #205's construction boundary: after the
exact patch is applied to the evaluated checkout, it builds both
`./cmd/endstate` and `./cmd/endstate-validation` from that same mutated source
tree and invokes the co-built validation CLI for the exact target. The frozen
controller never calls its own linked `validationharness` as the product oracle;
it only enforces the command, validates the emitted typed result, and classifies
it. Detector/harness source cannot change, but shared product dependencies are
co-built with the mutant exactly as they are in PR #205. The unmodified
baselines use the same two-binary construction from the evaluated tree.

The persisted experiment binds three distinct authorities: the evaluated PR #205
commit and tree; the detector/controller/workflow freeze commit; and the later
patch-corpus commit. The corpus commit must be a descendant of the freeze and
its repository diff may touch only the registered v1 corpus root. Each
controller invocation separately receives the trusted exact GitHub run SHA,
verifies a clean checkout at that SHA, derives its commit/tree as the runtime
dispatch authority, and records that hydrated authority in every attempt and
aggregate comparison. A persisted manifest field for dispatch is invalid.
Production files change only inside disposable evaluated-tree checkouts when an
exact corpus patch is applied. The controller mechanically
verifies that detector, controller, workflow, and command-contract bytes are
identical to the freeze before any attempt runs. Audit tests added after PR
#205 are never part of the evaluated Go checkout.

Every attempt binds those authorities, the mutated tree, patch digest, runner
family and image, one exact resolved Go patch version shared by every comparator
and detector lane, exact command contract digest, detector contract digest,
repetition, timing, and structured outcome. The exact Go version is data-bound
in the frozen experiment manifest and supplied to the same immutable
`actions/setup-go` revision with the same cache behavior as the required Go
workflow. Engine and validation-binary SHA-256 values may be recorded
diagnostically but are not part of repetition equality: source tree, toolchain,
and command authority define the build input identity.

Missing tools, acquisition or patch failure, process-launch error, timeout,
cancellation, output overflow, malformed or missing evidence, and artifact
loss are infrastructure failures. They can never become product failures or
kill credit. Product rejection must be a bounded detector result with the exact
predeclared class, phase, coordinate, and optional causal reason.

## Execution and decision

All three targeted module detectors run twice on the unmodified reference and
must be green with identical authority and proof identity. Each held-out
mutation then runs through all three Go comparator lanes and two fresh Windows
detector attempts.

V1 reports `meaningful-signal` only when:

- all three mutations pass every comparator lane;
- all three are rejected twice for their exact predeclared causal failure;
- `capture-contract` and `config-roundtrip-v1` are both represented; and
- wrong kills, survivors, flakes, and infrastructure failures are all zero.

Any other complete result is `insufficient-signal`; incomplete or malformed
evidence is `inconclusive`. A complete comparator rejection is `already-covered`
and makes the result `insufficient-signal`; it is not infrastructure and cannot
be replaced. V1 never reports a percentage or extrapolates its three cases into
a future-defect coverage rate.

The earliest created workflow run for the exact dispatch commit is the sole
authoritative v1 run. The workflow has no inputs and forbids reruns. Any second
dispatch or attempt greater than one for that commit invalidates the experiment
version; it is never ignored in favor of a better result. The independent
verifier inspects the complete workflow/run history for the exact SHA. If the
proof machinery fails, v1 remains inconclusive. Any repair is a new reviewed
v1.1 experiment with a new held-out corpus; known outcomes are never reused as
held-out proof.

## Hosted budget and closeout

The workflow remains `workflow_dispatch`-only, permissionless, credential-free,
schedule-free, target-install-free, and mutation-isolated below runner-owned
temporary roots. It performs one prepare job, bounded comparator/detector jobs,
and one aggregate job with fixed timeouts and concurrency. The exact maximum
job and runner-minute envelope is recorded and reviewed before dispatch.

After the run, an independent verifier inspects every artifact and the actual
GitHub run, reproduces aggregation locally, and publishes the exact commit,
run, corpus, runner/toolchain identities, and three classifications. A valid
`meaningful-signal` result clears the efficacy blocker for PR #205 without
waiting for the 30-mutation audit. PR #205 still requires its own green required
checks, aggregate/shard wiring verification, independent review, and merge
criteria. V1 does not prove hosted-live, Notepad++, GUI/Playwright, installer,
release, or deployment behavior.
