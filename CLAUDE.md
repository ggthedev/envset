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

The full protocol is in `AGENTS.md`. The gate script is `scripts/comlog-gate.sh`.
