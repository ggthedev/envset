# envset

A Go CLI that adds, lists and removes environment variables in shell startup files.

## Checks

Before reporting any work as done, run:

```
go build ./... && go vet ./... && go test ./...
```

## Commits: COMLOG

When a message contains **COMLOG**, delegate to the `comlog` subagent.
- Don't commit inline.
- Don't commit at any other time unless the user explicitly asks.

The full protocol is in `.claude/agents/comlog.md`. The gate script is `scripts/comlog-gate.sh`.

## Questions: NOTES

When a message is a question about the project (not a request to change it), or contains **NOTES**,
delegate to the `notes` subagent. Pass it the question and the context it needs from the conversation,
or the answer already given so it can be recorded. `NOTES.md` is local: never commit it.
