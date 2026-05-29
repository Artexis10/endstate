# nix-realizer spike (THROWAWAY)

Isolated, non-shipping experiment that answers one question before we commit the
Endstate Nix realizer: **is Nix's steady-state error surface bounded and
translatable behind Endstate?** See `FINDINGS.md` for the goal, rubric, and verdict.

Lives in worktree `spike/nix-realizer-errors`. Nothing here merges. The real
engine (`go-engine/`) is never touched.

## What it does

`main.go` (stdlib-only Go) shells out to `nix profile install ... --log-format
internal-json`, stream-parses the `@nix {...}` event lines, captures the exit
code, tracks whether an isolated profile **generation advanced**, then runs
`classify()` — which tries to bucket each failure from **structural signals
only** and flags when it's forced to fall back to the (unstable) `msg` text.
It deliberately provokes the 6-class failure taxonomy and writes artifacts.

## Prerequisites

- **Linux with Nix installed.** Nix is *not* present in the WSL2 Ubuntu distro
  yet — install it first (Determinate Systems installer is the KB-endorsed one;
  MIT, clean uninstall):
  ```bash
  curl -fsSL https://install.determinate.systems/nix | sh -s -- install
  ```
- Go is only needed on the **build** side; the binary is cross-compiled from
  Windows, so WSL needs Nix but not Go.

## Build (from Windows / Git Bash)

```bash
SPIKE="C:/Users/win-laptop/projects/endstate-nix-spike/spike/nix-realizer"
GOOS=linux GOARCH=amd64 go -C "$SPIKE" build -o nixspike-linux-amd64 .
```

## Run (inside WSL Ubuntu)

```bash
# the worktree is visible from WSL under /mnt/c
cd /mnt/c/Users/win-laptop/projects/endstate-nix-spike/spike/nix-realizer

# profile MUST stay on the Linux fs (symlink semantics); artifacts can go to /mnt/c
./nixspike-linux-amd64 \
    -profile /tmp/endstate-nix-spike/profile \
    -out ./artifacts \
    -flake 'nixpkgs' \
    -repeat 3

# include the invasive disk/store case only when you've set up the condition:
#   ./nixspike-linux-amd64 -invasive ...
```

Artifacts land in `./artifacts/`:
- `nix-version.txt`, `profile-list-shape.json` — environment capture
- `results.json` — every run's signals + classification + expected
- `<case>.runN.stderr.txt`, `<case>.runN.events.json` — raw + parsed per run

## Score it

Transcribe `results.json` into `FINDINGS.md`'s taxonomy table, score each class
BOUNDED / PARTIAL / UNBOUNDED, and set the GREEN / YELLOW / RED verdict. That
verdict gates whether (and how) we build the production realizer.

## Teardown

```bash
rm -rf /tmp/endstate-nix-spike            # in WSL: remove the isolated profile
# from Windows, when done with the whole spike:
git worktree remove C:/Users/win-laptop/projects/endstate-nix-spike --force
git branch -D spike/nix-realizer-errors
# optional: uninstall Nix via the Determinate uninstaller if it was installed for this
```
