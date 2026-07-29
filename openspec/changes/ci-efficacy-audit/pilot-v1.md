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

At one exact reviewed reference, do the PR #205 catalog and verified-module
detectors correctly reject six previously unseen realistic regressions that
the contemporaneous cross-platform Go gate accepts?

The comparator is exactly `go vet ./...` followed by `go test ./...` on fixed
Windows, Ubuntu, and macOS hosted runner families. Each lane uses the same
immutable `actions/setup-go` revision and requested Go `1.26` line as the
required Go workflow, preserves that workflow's setup-cache behavior, supplies
no token input, and records the exact resolved toolchain and runner image.
Windows Notepad++ integration is
absent from this comparator rather than represented as failed infrastructure.

## Calibration and held-out boundary

The six v0 mutations and their actual hosted artifacts are calibration inputs
only. They test v1 evidence parsing, causal failure extraction, classification,
and infrastructure precedence, but receive no v1 efficacy credit.

After those paths pass locally, the detector, controller, and final operative
workflow are frozen. Only then may six new mutations be authored: three catalog
regressions and three module safety/config regressions. Each patch must change
production data, describe a plausible user-visible or invariant-level defect,
and bind its expected stable detector failure before any detector execution.

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

Module candidates must remain parseable, schema-valid, revision-consistent,
and admitted to the exact targeted module/scenario. Catalog candidates must
remain parseable and admitted to their exact targeted bundle row. Revision,
schema, selection, admission, aggregate, envelope, and other shallow guard
failures are ineligible for kill credit. Only a predeclared downstream domain
coordinate after admission can be a correct kill.

## Authority and evidence

One typed Go controller owns acquisition, patch verification/application,
bounded command execution, detector invocation, evidence publication, and
aggregation. Workflow shell only provisions the pinned Go action, invokes the
controller, and uploads its bounded artifacts. It must not compose or classify
JSON.

The experiment binds four distinct authorities: the evaluated PR #205 commit
and tree; the detector/controller/workflow freeze commit; the later data-only
corpus commit; and the dispatch commit. The corpus/dispatch commit must be a
descendant of the freeze and its diff may touch only the registered v1 corpus
root. The controller mechanically verifies that detector, controller, workflow,
and command-contract bytes are identical to the freeze before any attempt runs.
Audit tests added after PR #205 are never part of the evaluated Go checkout.

Every attempt binds those authorities, the mutated tree, patch digest, runner
family and image, one exact resolved Go patch version shared by every comparator
and detector lane, exact command contract digest, detector contract digest,
repetition, timing, and structured outcome. The exact Go version is data-bound
in the frozen experiment manifest and supplied to the same immutable
`actions/setup-go` revision with the same cache behavior as the required Go
workflow. A built engine SHA-256 may be recorded diagnostically but is not part
of repetition equality: source tree, toolchain, and command authority define
the build input identity.

Missing tools, acquisition or patch failure, process-launch error, timeout,
cancellation, output overflow, malformed or missing evidence, and artifact
loss are infrastructure failures. They can never become product failures or
kill credit. Product rejection must be a bounded detector result with the exact
predeclared class, phase, coordinate, and optional causal reason.

## Execution and decision

The unmodified catalog detector and all three targeted module detectors run
twice and must be green with identical authority and proof identity. Each held-
out mutation then runs through all three Go comparator lanes and two fresh
Windows detector attempts.

V1 reports `meaningful-signal` only when:

- all six mutations pass every comparator lane;
- all six are rejected twice for their exact predeclared causal failure;
- all three module regressions are correct kills;
- catalog and module detector families are both represented; and
- wrong kills, survivors, flakes, and infrastructure failures are all zero.

Any other complete result is `insufficient-signal`; incomplete or malformed
evidence is `inconclusive`. V1 never reports a percentage or extrapolates its
six cases into a future-defect coverage rate.

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
run, corpus, runner/toolchain identities, and six classifications. A valid
`meaningful-signal` result clears the efficacy blocker for PR #205 without
waiting for the 30-mutation audit. PR #205 still requires its own green required
checks, aggregate/shard wiring verification, independent review, and merge
criteria. V1 does not prove hosted-live, Notepad++, GUI/Playwright, installer,
release, or deployment behavior.
