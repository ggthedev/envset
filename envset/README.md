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
