# envset

Interactive environment variable setter for shell configuration files.

## Overview

`envset` helps you add, list, and remove environment variables from your shell configuration files (`.zshrc`, `.bashrc`, `.bash_profile`, `.zprofile`, `.profile`, `.zshenv`, `config.fish`).

It detects your current shell from the process tree (not just `$SHELL`), suggests the right config file, and warns you before writing secrets.

## Features

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

### Add a variable (flags mode)

```bash
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
