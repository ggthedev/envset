# envset

Safely add, list and remove environment variables in your shell startup files
(zsh, bash, fish and macOS `launchd`) without hand-editing them.

New here? The [tutorial](https://github.com/ggthedev/envset/blob/main/docs/TUTORIAL.md) takes about 10 minutes and uses a
throwaway home directory, so nothing on your machine changes.

## Why not `echo >> ~/.zshrc`

- **Right file:** `PATH` goes where scripts and cron see it, `EDITOR` where your terminal does.
- **Safe writes:** atomic, backed up first, and refused if the file would no longer parse.
- **Safe to re-run:** no duplicate lines; a changed value updates its line in place.
- **No surprises:** it [refuses to edit code it could damage](https://github.com/ggthedev/envset/blob/main/docs/REFERENCE.md#lines-envset-wont-rewrite)
  and rejects command substitution in values unless you pass `--raw`.
- **Undo:** `envset restore --target zshrc`.

## Install

macOS, Apple Silicon or Intel. Download the latest release, verify it and put it in `~/.local/bin`:

```bash
(
  set -e
  arch=$(uname -m | sed 's/x86_64/amd64/')
  version=$(curl -fsSLo /dev/null -w '%{url_effective}' https://github.com/ggthedev/envset/releases/latest)
  version=${version##*/}
  file=envset_${version}_darwin_${arch}.tar.gz
  cd "$(mktemp -d)"
  curl -fsSLO "https://github.com/ggthedev/envset/releases/download/$version/$file"
  curl -fsSLO "https://github.com/ggthedev/envset/releases/download/$version/checksums.txt"
  shasum -a 256 --check --ignore-missing checksums.txt
  tar -xzf "$file" envset
  mkdir -p ~/.local/bin
  mv envset ~/.local/bin/
)
```

If `~/.local/bin` is not on your `PATH`, envset can add it:

```bash
~/.local/bin/envset add --type path_append --value '~/.local/bin' --target zshenv
```

If you downloaded with a browser and macOS blocks the file:
`xattr -d com.apple.quarantine <file>`.

With Go 1.22 or newer you can instead run
`go install github.com/ggthedev/envset@latest`, which installs to `~/go/bin`.

## Quick start

```bash
envset detect                                        # which shell / files envset sees
envset add --key JAVA_HOME --value /opt/jdk --yes    # auto-pick the file
envset add --key EDITOR --value nvim --target zshrc  # explicit file
envset add --type path_append --value /opt/homebrew/bin --target zshenv
envset list --target zshenv [--key JAVA_HOME]
envset remove --key EDITOR --target zshrc            # comments the line out
envset remove --key EDITOR --target zshrc --comment=false   # deletes it
envset remove --key PATH --value /opt/homebrew/bin --target zshenv
envset restore --target zshenv --list                # show backups
envset restore --target zshenv                       # undo the last change
envset audit                                         # health check, read-only
envset audit --dry-run                               # plus the diff of every safe fix
envset audit --fix                                   # review and apply safe fixes
envset lazy pyenv --write                            # lazy-load a slow tool
envset version                                       # which build is this
eval "$(envset reload --target zshenv)"              # apply in the current shell
```

Run `envset add` or `envset remove` with no flags for an interactive wizard.

## Commands

| Command | Does |
|---|---|
| [`add`](https://github.com/ggthedev/envset/blob/main/docs/REFERENCE.md#add) | Add or update a variable, choosing the file if you don't |
| [`remove`](https://github.com/ggthedev/envset/blob/main/docs/REFERENCE.md#remove) | Comment out or delete it |
| [`restore`](https://github.com/ggthedev/envset/blob/main/docs/REFERENCE.md#restore) | Undo from a backup |
| [`audit`](https://github.com/ggthedev/envset/blob/main/docs/REFERENCE.md#audit) | Find problems in your startup files and fix the safe ones |
| [`lazy`](https://github.com/ggthedev/envset/blob/main/docs/REFERENCE.md#lazy) | Delay a slow tool's setup until first use |
| [`detect`, `list`, `reload`](https://github.com/ggthedev/envset/blob/main/docs/REFERENCE.md#detect-list-reload) | Inspect, and apply changes to the current shell |
| `version` | Show which build is running |

## Documentation

- [Reference](https://github.com/ggthedev/envset/blob/main/docs/REFERENCE.md): commands, value encoding, where entries go, environment variables
- [Logging](https://github.com/ggthedev/envset/blob/main/docs/LOGGING.md): what is recorded, and where
- [Development](https://github.com/ggthedev/envset/blob/main/docs/DEVELOPMENT.md): building, releases, project layout
- [Deferred work](https://github.com/ggthedev/envset/blob/main/docs/TODO.md)

## Limitations

- envset reads shell files with a small scanner, not a full shell parser. When
  unsure it refuses. It can't tell that an unconditional `return` or an unusual
  zsh short loop stops a line from running, so check `envset list` if a change
  seems to have no effect.
- Locking coordinates envset runs only; an editor saving at the same instant is
  narrowed by a re-check, not prevented.
- `--target launchd` keeps only one added `PATH` entry (see [deferred work](https://github.com/ggthedev/envset/blob/main/docs/TODO.md)).

Not supported yet: `.env` files, IDE settings, Linux `systemd`, Docker and Kubernetes.

## License

Copyright 2026 Gaurav Gupta. Licensed under the Apache License, Version 2.0: see
[LICENSE](LICENSE). You may use, change and share this code; keep the copyright
and [NOTICE](NOTICE) text, and mark the files you change.

The binaries also contain third-party code, whose licences are in
[THIRD_PARTY_NOTICES.txt](THIRD_PARTY_NOTICES.txt). After changing dependencies,
regenerate it with `scripts/third-party-notices.sh`.
