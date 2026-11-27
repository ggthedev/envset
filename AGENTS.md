# Agent instructions (all LLM tools)

These rules apply to every AI coding tool working in this repository
(Claude Code, Codex, Cursor, Windsurf, Gemini, …).

## COMLOG — the commit protocol

**Trigger:** any message containing the word `COMLOG`.

**Committing is done only through this protocol.** Do not run `git commit` at any
other time unless the user explicitly asks for a commit in other words.

The rules are in `.claude/agents/comlog.md`, the single source of truth.

- **Claude Code:** delegate to the `comlog` subagent. Don't commit inline.
- **Other tools:** read that file and follow its rules yourself.

## NOTES protocol

For a question about the project (not a change request), or when a message contains **NOTES**:
follow `.claude/agents/notes.md`. Answer from the code, then append the question and answer to
`NOTES.md` at the repository root. That file is local: never commit it.
