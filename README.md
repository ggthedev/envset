# envset

Interactive environment variable setter for shell configuration files.

## Overview

`envset` helps you add, list, and remove environment variables from your shell configuration files (`.zshrc`, `.bashrc`, `.bash_profile`, `.zprofile`, `.profile`, `.zshenv`, `config.fish`).

It detects your current shell from the process tree (not just `$SHELL`), suggests the right config file, and warns you before writing secrets.

## Features

- **Smart placement**: Automatically figures out the best config file based on variable semantics (PATH → `.zshenv`, `EDITOR` → `.zshrc`, etc.)
- **Custom file support**: Recognizes your `~/.gg_system_exports` and `~/.gg_system_interactive` files
- **Both modes**: Use flags for quick scripting or run without flags for an interactive wizard
- **Smart detection**: Detects your current running shell via parent process tree, not just login shell
- **Dry-run**: Preview changes before writing (`--dry-run`)
- **Secret warnings**: Warns when you're about to write variables that look like API keys or passwords
- **Comment or delete**: Remove can comment out lines instead of deleting them (`--comment`)
- **Multiple variable types**: `export`, `eval`, `source`, `path_append`, `path_prepend`, `fish_set`

## Installation

```bash
cd /path/to/envset
go build -o envset .
cp envset ~/bin/  # or anywhere in your $PATH
```

## Usage

### Detect your environment

```bash
envset detect
```

### Add a variable (wizard mode)

```bash
envset add
```

### Add a variable (auto-placement, flags mode)

```bash
# Let envset figure out the best file (requires --yes)
envset add --key JAVA_HOME --value /opt/java --type export --yes
envset add --key PATH --value /usr/local/bin --type path_append --yes --dry-run

# Explicit target (old behavior)
envset add --key FOO --value bar --target zshrc --type export
envset add --key FOO --value bar --target zshrc --type export --dry-run
```

### List variables

```bash
envset list --target zshrc
envset list --target zshrc --key FOO
```

### Remove a variable

```bash
# Comment out (default)
envset remove --key FOO --target zshrc

# Actually delete
envset remove --key FOO --target zshrc --comment=false

# Dry-run
envset remove --key FOO --target zshrc --dry-run
```

### Apply changes immediately (sourcing)

A child process **cannot** modify its parent shell's environment, so `envset` writes the line to disk but you must `source` the file in your current shell to load it. There are three ways:

```bash
# 1. Manual — envset prints a hint after every successful write:
#    "To apply changes in your current shell, run: source ~/.zshenv"
source ~/.zshenv

# 2. eval-able one-liner with --print-source on add/remove:
eval "$(envset add --key JAVA_HOME --value /opt/java --yes --print-source)"

# 3. Standalone reload subcommand:
eval "$(envset reload --target zshenv)"     # one file
eval "$(envset reload)"                      # all existing files
```

For convenience add this alias to your `~/.zshrc` / `~/.bashrc`:

```bash
alias eset='function _eset(){ eval "$(envset add "$@" --print-source)"; }; _eset'
# usage: eset --key FOO --value bar --yes
```

### Audit & logging

Run a placement / performance health-check across every detected config file:

```bash
envset audit                       # report on stdout
envset --verbose audit             # adds per-method TRACE entries in the log
tail -f ~/.config/envset/logs/envset.log   # watch flow live in another terminal
```

Every CLI run is recorded to an audit log. The terminal stays clean — log output
goes only to the file. Each invocation is delimited by an 80-char rule so
multiple runs stay easy to scan.

```
================================================================================
RUN  2026-05-06T10:48:28+05:30  pid=48673
cmd  envset add --key FOO --value bar
opts verbose=false
================================================================================
10:48:28.594 [STEP  ] ops.Add
10:48:28.594 [INPUT ] key="FOO" value="bar" target="" type="export" dry_run=false ...
10:48:28.594 [STEP  ] auto-placement
10:48:28.661 [DECIDE] placed=zshenv path=/Users/.../.zshenv confidence=85% ...
10:48:28.661 [RESULT] add: wrote FOO to /Users/.../.zshenv
10:48:28.661 [DONE ] elapsed=68ms
================================================================================
```

| Verbosity | What lands in the log file | Stderr |
|-----------|---------------------------|--------|
| Default | inputs, wizard Q&A, placement decision, final result | nothing |
| `-v` / `--verbose` | + per-method trace + every placement candidate | one-line `[envset] verbose log: <path>` hint at startup |

**Log location** is resolved in this order (first non-empty wins):

1. `$ENVSET_LOG_FILE` (full path)
2. `$ENVSET_LOG_DIR/envset.log`
3. `~/.config/envset/.env` containing either of the above keys
4. `./.env` in the current working directory containing either of the above keys
5. **Default**: `~/.config/envset/logs/envset.log`

Paths beginning with `~/` or `$HOME/` are expanded.

### Source-chain awareness

The placer doesn't just look at a file's *declared* scope — it follows
`source ...` directives in your config files and computes each file's
**effective scope** (the broadest scope that actually reaches it).

Concrete example: you keep cross-shell exports in `~/.gg_system_exports`,
declared `all_sessions`. If you only `source ~/.gg_system_exports` from
`~/.zprofile`, its **effective scope is login** — variables defined there
won't reach scripts, cron jobs, or `(zsh -c ...)` invocations.

When you run `envset add JAVA_HOME /opt/jdk`, the placer:

1. Detects the intent as `category=application scope=all_sessions`.
2. Penalises every file whose effective scope can't reach `all_sessions`
   (`gg_system_exports` drops by 0.20 — see `[TRACE]` lines in the log).
3. Promotes `~/.zshenv` (effective scope still `all_sessions`) to the top.
4. **Asks you in the wizard** when a familiar custom file is broken — three
   options: write to it anyway, write to the correct primary file, or bail
   and run `envset audit` to fix the source-chain.

Non-interactive runs (`--yes`) print a `WARNING:` to stderr if the chosen
file has a scope mismatch or if the same key already exists in other files
(those copies will be shadowed).

## Variable Types

| Type | Description | Example |
|------|-------------|---------|
| `export` | Standard export | `export FOO=bar` |
| `eval` | Eval command output | `eval $(rbenv init -)` |
| `source` | Source a file | `source /path/to/file` |
| `path_append` | Append to PATH | `export PATH="$PATH:/new/path"` |
| `path_prepend` | Prepend to PATH | `export PATH="/new/path:$PATH"` |
| `fish_set` | Fish shell set | `set -x FOO bar` |

## Project Structure

```
envset/
├── cmd/           # Cobra CLI commands
├── pkg/
│   ├── detector/  # Shell and OS detection
│   ├── placer/    # Smart placement engine (intent → config file)
│   │   ├── registry.go    # Config file registry (standard + custom)
│   │   ├── intent.go      # Variable classification
│   │   ├── rules.go       # Heuristic scoring
│   │   └── placer.go      # Main Analyze() → ranked suggestions
│   ├── targets/   # Shell config file operations
│   ├── wizard/    # Interactive prompts
│   └── ops/       # Business logic orchestration
├── main.go
└── go.mod
```

## TBD / Future Enhancements

- `.env` file support
- `launchd` plist support (macOS)
- IDE settings (VSCode `settings.json`, `launch.json`)
- Linux `systemd` environment
- Docker environment support
- Kubernetes ConfigMap/Secret support

## License

BSD-3-Clause — see [LICENSE](LICENSE) for details.
