# Logging

Every run is logged to `~/.config/envset/logs/envset.log`, one line per event
with date, time, level, process id and the place in the code:

```
2026-11-28 09:12:03.114 INFO  [4812] pkg/ops/ops.go:168 ops.Add   add: appended EDITOR in /Users/you/.zshrc
```

| Level | Written | What |
|---|---|---|
| `ERROR` | always | failures and refusals |
| `WARN` | always | duplicate definitions, scope mismatches, a skipped syntax check, `launchctl` failures |
| `INFO` | always | start of the run (version and command), placement, each change, every backup path, exit code and duration |
| `DEBUG` | with `-v` | every function entered and left, inputs, decisions, wizard answers, file reads, locks and writes |

`-v/--verbose` writes only to the log and prints its path on stderr; follow it
with `tail -f`. It is large: one command on a 150-line file writes around 2,000
lines (about 200 KB).

```bash
grep -E ' (ERROR|WARN) ' ~/.config/envset/logs/envset.log     # problems only
grep '\[4812\]' ~/.config/envset/logs/envset.log               # one run
```

- **No values.** Values, lines from your files, init commands, command output
  and unrecognised arguments are never written, at any level. Variable names,
  file paths, `--target`, `--type` and `--shell` are. A name that is not a valid
  variable name is logged as `[invalid]`. The terminal still shows the full
  error.
- **Private.** The log and its folder are readable only by you (modes 0600 and 0700).
- **Bounded.** A log over 5 MB is rotated to `envset.log.1` when the next run
  starts; three old copies are kept.
- Logs written by earlier versions are not rewritten, and may hold values.

The log location is the first of these that is set:
1. `$ENVSET_LOG_FILE`
2. `$ENVSET_LOG_DIR/envset.log`
3. either of those keys in `~/.config/envset/.env`

A `.env` in the current directory is never read, so running envset inside a
project can't redirect its log.

`$XDG_CONFIG_HOME` replaces `~/.config` everywhere (log, rules and backups).
