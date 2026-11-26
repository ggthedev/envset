# Agent instructions (all LLM tools)

These rules apply to every AI coding tool working in this repository
(Claude Code, Codex, Cursor, Windsurf, Gemini, …).

## COMLOG — the commit protocol

**Trigger:** any message containing the word `COMLOG`.

**Committing is done only through this protocol.** Do not run `git commit` at any
other time unless the user explicitly asks for a commit in other words.

- **Claude Code:** delegate to the `comlog` subagent (`.claude/agents/comlog.md`). Don't commit inline.
- **Other tools:** follow the steps below yourself.

### Steps

1. **Branch guard.** Run `git symbolic-ref --short HEAD`.
   - Stop and report if the branch is `main` or `master`, if HEAD is detached, or if a rebase or merge is in progress.
   - Never switch, create, or reset branches to get around this.
2. **Survey.** Read the current state before staging anything:
   - `git status --porcelain`
   - `git diff`
   - `git diff --cached`
   - `git log -10 --oneline`, to match the existing message style
3. **Exclude.** Never stage any of these. List each skipped file in your report:
   - build outputs and binaries (e.g. `./envset`, `./envset_test`)
   - files over 1 MB
   - `.env*`, `*.local.json`, keys and certs, credentials
   - `scratch/`, `.DS_Store`
4. **Group.** Split the changes into small logical commits, one concern each (`fix`, `feat`, `test`, `docs`, `refactor`, `chore`).
   - Stage by path: `git add -- <paths>`.
   - To split hunks inside a file, write a filtered patch to a temp file and run `git apply --cached <patch>`.
   - Never use interactive flags (`-p`, `-i`).
   - Keep a change and its tests in the same commit.
5. **Gate every commit.** Run `scripts/comlog-gate.sh` with the group staged.
   - It checks branch, content, `go build`, `go vet`, and that `go test` has no failures HEAD doesn't already have.
   - It never touches the working tree.
   - If it fails, regroup and gate again. For example, pull a dependent hunk into the same commit.
   - If it still fails, unstage that group (`git restore --staged -- <paths>`), leave it uncommitted, and report why.
6. **Commit** using Conventional Commits:
   ```
   type(scope): imperative summary under ~72 chars

   Why this change was made, and anything non-obvious about it.

   <your tool's Co-Authored-By trailer, if it has one>
   ```
   Pass the message through a heredoc, e.g. `git commit -F - <<'EOF' … EOF`.
7. **Repeat** steps 4–6 until every eligible change is committed.
8. **Report:**
   - each commit as `hash subject`
   - every file left uncommitted, with the reason
   - the gate results, including pre-existing test failures

### Never

- `git push`
- `--amend`, `--force`, `--no-verify`
- `reset --hard`, `rebase`, `stash drop`, `clean`
- committing to `main`/`master`
- editing source files as part of COMLOG: commit what is there and nothing else
