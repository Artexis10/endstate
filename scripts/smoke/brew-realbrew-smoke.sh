#!/usr/bin/env bash
# scripts/smoke/brew-realbrew-smoke.sh
#
# Real-Homebrew integration smoke test for Endstate's two-lane darwin apply.
#
# Exercises the brew driver lane end-to-end ALONGSIDE the Nix realizer:
#   build engine → write manifest with a driver:"brew" formula (and a tiny cask)
#   → apply --json (assert the formula installed/present) → capture --json
#   (assert the captured manifest round-trips: an app with "driver":"brew" and
#   ref "hello") → brew uninstall in the cleanup trap.
#
# This is the ONLY place brew's real-output anchors (brew leaves / list --cask /
# list --versions columns, install idempotency exit codes) are confirmed — the
# hermetic Go tests only lock the ASSUMED shapes (the winget lesson).
#
# REQUIREMENTS: brew (macOS). The `command -v brew` guard makes the linux matrix
# leg a no-op (exit 0) so this script can live in a cross-OS CI job.
# SAFE: throwaway HOME + ENDSTATE_ROOT; cleans up on exit (trap), including a
# `brew uninstall hello`.
#
# Usage: bash scripts/smoke/brew-realbrew-smoke.sh

set -euo pipefail

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

phase() { echo; echo "==> [brew-smoke] $*"; }
fail()  { echo "FAIL: $*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Guard: brew must be present. On the linux matrix leg it is absent → no-op.
# ---------------------------------------------------------------------------

if ! command -v brew >/dev/null 2>&1; then
  echo "==> [brew-smoke] brew absent on this host, skipping (no-op)."
  exit 0
fi

# Formula and cask under test. `hello` is a tiny, dependency-light GNU formula.
# The cask leg is BEST-EFFORT (warn-only) since cask installs can require
# privileges / network that a runner may lack; the formula leg is STRICT.
FORMULA="hello"
CASK="cask:gnu-typist" # tiny; cask leg is warn-only regardless of outcome

# ---------------------------------------------------------------------------
# Temp dirs — isolated HOME and ENDSTATE_ROOT; cleaned on exit.
# ---------------------------------------------------------------------------

TMP_ROOT="$(mktemp -d)"
SMOKE_HOME="${TMP_ROOT}/home"
SMOKE_STATE="${TMP_ROOT}/endstate-root"
SMOKE_BIN="${TMP_ROOT}/bin"
SMOKE_MANIFEST="${TMP_ROOT}/manifest.jsonc"
CAPTURE_OUT="${TMP_ROOT}/captured.jsonc"

mkdir -p "${SMOKE_HOME}" "${SMOKE_STATE}" "${SMOKE_BIN}"

cleanup() {
  echo
  echo "==> [brew-smoke] cleanup: uninstalling ${FORMULA} and removing ${TMP_ROOT}"
  brew uninstall --formula "${FORMULA}" >/dev/null 2>&1 || true
  rm -rf "${TMP_ROOT}"
}
trap cleanup EXIT

# Ensure a clean starting point (the formula must not be pre-installed).
brew uninstall --formula "${FORMULA}" >/dev/null 2>&1 || true

# ---------------------------------------------------------------------------
# Phase 1: Build the engine binary
# ---------------------------------------------------------------------------

phase "Building engine binary"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ENGINE_BIN="${SMOKE_BIN}/endstate"

(cd "${REPO_ROOT}/go-engine" && go build -o "${ENGINE_BIN}" ./cmd/endstate)
echo "    built: ${ENGINE_BIN}"

# ---------------------------------------------------------------------------
# Phase 2: Write a manifest with a driver:"brew" formula + a tiny cask
#
# The realizer (nix) lane is exercised by hm-realnix-smoke.sh; here apps: is
# brew-only so the brew lane is the focus. The engine routes each by its
# driver field: the realizer owns the (empty) default lane, the brew driver
# owns these two apps.
# ---------------------------------------------------------------------------

phase "Writing smoke manifest"

cat > "${SMOKE_MANIFEST}" << MANIFEST
{
  "version": 1,
  "name": "brew-smoke",
  "apps": [
    { "id": "${FORMULA}", "displayName": "${FORMULA}", "driver": "brew", "refs": { "darwin": "${FORMULA}" } },
    { "id": "gnu-typist", "displayName": "gnu-typist", "driver": "brew", "refs": { "darwin": "${CASK}" } }
  ]
}
MANIFEST

echo "    manifest: ${SMOKE_MANIFEST}"

export HOME="${SMOKE_HOME}"
export ENDSTATE_ROOT="${SMOKE_STATE}"

# ---------------------------------------------------------------------------
# Phase 3: apply --json (formula leg STRICT, cask leg best-effort)
# ---------------------------------------------------------------------------

phase "Running: endstate apply --manifest ... --json"

APPLY_OUT="${TMP_ROOT}/apply-out.json"

# The cask leg may fail in a sandboxed runner; we do NOT let that fail the whole
# apply assertion — we assert the FORMULA specifically below.
"${ENGINE_BIN}" apply --manifest "${SMOKE_MANIFEST}" --json 2>&1 | tee "${APPLY_OUT}" || {
  echo "    apply exited non-zero (cask leg may have failed); inspecting formula result below."
}

echo "    apply output written to ${APPLY_OUT}"

# STRICT: the formula must be installed or present after apply.
if grep -q "\"id\":\"${FORMULA}\"" "${APPLY_OUT}" && grep -Eq "\"status\":\"(installed|present)\"" "${APPLY_OUT}"; then
  echo "    PASS: ${FORMULA} reported installed/present by apply"
else
  echo "    --- apply output ---"; cat "${APPLY_OUT}"; echo "    --- end ---"
  fail "Formula ${FORMULA} was not reported installed/present by apply."
fi

# Cross-check with brew directly (the real-output anchor).
if brew list --formula "${FORMULA}" >/dev/null 2>&1; then
  echo "    PASS: brew list confirms ${FORMULA} is installed"
else
  fail "brew list does not show ${FORMULA} after apply — install did not take."
fi

# ---------------------------------------------------------------------------
# Phase 4: capture --json (round-trip — the captured manifest must carry the
# brew app with driver:"brew" and the formula ref).
# ---------------------------------------------------------------------------

phase "Running: endstate capture --json"

CAPTURE_STDOUT="${TMP_ROOT}/capture-out.json"

"${ENGINE_BIN}" capture --out "${CAPTURE_OUT}" --json 2>&1 | tee "${CAPTURE_STDOUT}" || {
  fail "endstate capture exited non-zero. Output above."
}

if [[ ! -f "${CAPTURE_OUT}" ]]; then
  fail "capture did not write ${CAPTURE_OUT}."
fi

echo "    --- captured manifest ---"; cat "${CAPTURE_OUT}"; echo "    --- end ---"

# The captured manifest MUST contain an app with "driver":"brew" and the formula
# ref — proving the brew capture lane ran and the round-trip survives.
if grep -q '"driver": *"brew"' "${CAPTURE_OUT}" && grep -q "\"darwin\": *\"${FORMULA}\"" "${CAPTURE_OUT}"; then
  echo "    PASS: captured manifest round-trips ${FORMULA} as a driver:brew app"
else
  fail "Captured manifest does not contain the brew-driver ${FORMULA} app — capture round-trip failed."
fi

# ---------------------------------------------------------------------------
# Done
# ---------------------------------------------------------------------------

phase "Brew smoke PASSED"
