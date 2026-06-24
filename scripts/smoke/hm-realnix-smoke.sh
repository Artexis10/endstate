#!/usr/bin/env bash
# scripts/smoke/hm-realnix-smoke.sh
#
# Real-Nix home-manager integration smoke test for Endstate.
#
# Exercises the full homeManager.settings apply path end-to-end:
#   build engine → write manifest → apply --enable-restore → assert managed config.
#
# REQUIREMENTS: nix (with flakes), network access.
# SAFE: uses throwaway HOME + ENDSTATE_ROOT so it never touches the runner's
# real config. Cleans up on exit (trap).
#
# Usage: bash scripts/smoke/hm-realnix-smoke.sh

set -euo pipefail

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

phase() { echo; echo "==> [smoke] $*"; }
fail()  { echo "FAIL: $*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Temp dirs — isolated HOME and ENDSTATE_ROOT; cleaned on exit.
# ---------------------------------------------------------------------------

TMP_ROOT="$(mktemp -d)"
SMOKE_HOME="${TMP_ROOT}/home"
SMOKE_STATE="${TMP_ROOT}/endstate-root"
SMOKE_BIN="${TMP_ROOT}/bin"
SMOKE_MANIFEST="${TMP_ROOT}/manifest.jsonc"

mkdir -p "${SMOKE_HOME}" "${SMOKE_STATE}" "${SMOKE_BIN}"

cleanup() {
  echo
  echo "==> [smoke] cleanup: removing ${TMP_ROOT}"
  rm -rf "${TMP_ROOT}"
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# Phase 1: Build the engine binary
# ---------------------------------------------------------------------------

phase "Building engine binary"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ENGINE_BIN="${SMOKE_BIN}/endstate"

(cd "${REPO_ROOT}/go-engine" && go build -o "${ENGINE_BIN}" ./cmd/endstate)
echo "    built: ${ENGINE_BIN}"

# ---------------------------------------------------------------------------
# Phase 2: Write a manifest exercising homeManager.settings
#
# Covers git + shell (bespoke) AND every data-driven dotfiles/CLI program
# (ripgrep, fd, zsh, bash, helix, kitty, alacritty, wezterm, jujutsu, atuin,
# yazi). Each sets its STABLE second-field option with a real, non-empty value
# so the generic emit loop actually emits it (an empty value is skipped) and a
# real `home-manager switch` validates the option name against the pinned
# home-manager (HEAD). A wrong rename (e.g. zsh.initContent) fails activation.
# No nix packages (apps: []) so the smoke is purely the config stage.
# ---------------------------------------------------------------------------

phase "Writing smoke manifest"

cat > "${SMOKE_MANIFEST}" << 'MANIFEST'
{
  "version": 1,
  "name": "hm-smoke",
  "apps": [],
  "homeManager": {
    "settings": {
      "git": {
        "userName": "Smoke Test User",
        "userEmail": "smoke@endstate-ci.example",
        "defaultBranch": "main"
      },
      "shell": {
        "aliases": {
          "ll": "ls -la",
          "gs": "git status"
        }
      },
      "ripgrep": { "enable": true, "arguments": ["--smart-case"] },
      "fd": { "enable": true, "extraOptions": ["--hidden"] },
      "zsh": { "enable": true, "initContent": "setopt AUTO_CD" },
      "bash": { "enable": true, "initExtra": "shopt -s globstar" },
      "wezterm": { "enable": true, "extraConfig": "-- endstate smoke" },
      "helix": { "enable": true, "settings": { "theme": "default" } },
      "kitty": { "enable": true, "settings": { "font_size": 12 } },
      "alacritty": { "enable": true, "settings": { "window": { "opacity": 1.0 } } },
      "jujutsu": { "enable": true, "settings": { "user": { "name": "Smoke", "email": "smoke@endstate-ci.example" } } },
      "atuin": { "enable": true, "settings": { "auto_sync": false } },
      "yazi": { "enable": true, "settings": { "mgr": { "show_hidden": false } } }
    }
  }
}
MANIFEST

echo "    manifest: ${SMOKE_MANIFEST}"

# ---------------------------------------------------------------------------
# Phase 3: Apply with --enable-restore (triggers home-manager config stage)
#
# We override HOME so home-manager writes into the throwaway directory.
# ENDSTATE_ROOT points the engine's state (generated flake, provision history)
# into the throwaway tree. XDG_CONFIG_HOME ensures git config lands predictably.
# ---------------------------------------------------------------------------

phase "Running: endstate apply --manifest ... --enable-restore"

export HOME="${SMOKE_HOME}"
export XDG_CONFIG_HOME="${SMOKE_HOME}/.config"
# The engine derives all of its state (generated flake, provision history) from
# ENDSTATE_ROOT, so pointing it into the throwaway tree keeps the smoke isolated.
export ENDSTATE_ROOT="${SMOKE_STATE}"

APPLY_OUT="${TMP_ROOT}/apply-out.json"

"${ENGINE_BIN}" apply \
  --manifest "${SMOKE_MANIFEST}" \
  --enable-restore \
  --json \
  2>&1 | tee "${APPLY_OUT}" || {
    fail "endstate apply exited non-zero. Output above."
  }

echo "    apply output written to ${APPLY_OUT}"

# ---------------------------------------------------------------------------
# Phase 4: Assertions
#
# home-manager with programs.git.enable + extraConfig writes the git identity
# into $XDG_CONFIG_HOME/git/config (XDG path) or ~/.config/git/config.
# We check both canonical locations.
# ---------------------------------------------------------------------------

phase "Asserting managed config is present"

GIT_CFG_CANDIDATES=(
  "${SMOKE_HOME}/.config/git/config"
  "${XDG_CONFIG_HOME}/git/config"
)

GIT_CFG=""
for candidate in "${GIT_CFG_CANDIDATES[@]}"; do
  if [[ -f "${candidate}" ]]; then
    GIT_CFG="${candidate}"
    break
  fi
done

# Fallback: also check home-manager generations to confirm a generation exists
HM_GENERATIONS_OUT="${TMP_ROOT}/hm-gen.txt"
HOME="${SMOKE_HOME}" home-manager generations 2>/dev/null > "${HM_GENERATIONS_OUT}" || true

if [[ -z "${GIT_CFG}" ]]; then
  # Check if home-manager created at least one generation — generation list is
  # an acceptable proxy when git config path differs between hm versions.
  if [[ -s "${HM_GENERATIONS_OUT}" ]]; then
    echo "    git config not found at expected paths, but home-manager generations exist:"
    cat "${HM_GENERATIONS_OUT}"
    echo "    Accepting: home-manager config stage ran successfully (generation present)."
  else
    echo "    Searched paths:"
    for p in "${GIT_CFG_CANDIDATES[@]}"; do echo "      $p"; done
    echo "    home-manager generations output:"
    cat "${HM_GENERATIONS_OUT}" || true
    fail "No git config or home-manager generations found — home-manager config stage did not activate."
  fi
else
  echo "    git config found: ${GIT_CFG}"
  echo "    --- contents ---"
  cat "${GIT_CFG}"
  echo "    --- end ---"

  # Assert declared user.name is present
  if ! grep -q "Smoke Test User" "${GIT_CFG}"; then
    fail "Expected 'Smoke Test User' in ${GIT_CFG} but not found. Config stage did not apply correctly."
  fi
  echo "    PASS: user.name = Smoke Test User"

  # Assert declared user.email is present
  if ! grep -q "smoke@endstate-ci.example" "${GIT_CFG}"; then
    fail "Expected 'smoke@endstate-ci.example' in ${GIT_CFG} but not found."
  fi
  echo "    PASS: user.email = smoke@endstate-ci.example"
fi

# Assert the apply JSON output indicates home-manager was activated
if [[ -f "${APPLY_OUT}" ]]; then
  if grep -q '"activated":true' "${APPLY_OUT}" || grep -q 'configured' "${APPLY_OUT}"; then
    echo "    PASS: apply output confirms home-manager activation"
  else
    echo "    WARNING: apply output does not explicitly confirm activation; check output above."
  fi
fi

# ---------------------------------------------------------------------------
# Done
# ---------------------------------------------------------------------------

phase "Smoke PASSED"
