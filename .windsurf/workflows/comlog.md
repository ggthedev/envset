---
description: COMLOG. Commit the current work on the checked-out branch (never main) as small, non-breaking commits
---

# COMLOG

Run this workflow whenever the user mentions COMLOG.

1. Read `AGENTS.md`, section "COMLOG — the commit protocol", and follow it exactly.
2. Branch guard. If `git symbolic-ref --short HEAD` prints `main` or `master`, or HEAD is detached, stop and report.
3. Survey the work with:
   - `git status --porcelain`
   - `git diff`
   - `git log -10 --oneline`
4. For each logical group of changes:
   1. Stage it with `git add -- <paths>`.
   2. Run `scripts/comlog-gate.sh`.
   3. Commit only if the gate prints OK. Use a Conventional Commits message with a short "why" body.
5. Never stage:
   - binaries (`./envset`, `./envset_test`)
   - `.env*`
   - `*.local.json`
   - `scratch/`
6. Never use any of these:
   - `push`
   - `--amend`
   - `--force`
   - `--no-verify`
   - `reset --hard`
   - `rebase`
7. Never edit any files.
8. Report:
   - each commit as `hash subject`
   - each file left uncommitted, with the reason
