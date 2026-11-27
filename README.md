# envset

Safely add, list and remove environment variables in your shell startup files
(`.zshenv`, `.zshrc`, `.zprofile`, `.bashrc`, `.bash_profile`, `.profile`,
`config.fish`, and macOS `launchd`) without hand-editing them.

New here? Start with the **[tutorial](docs/TUTORIAL.md)**: it takes about 10 minutes
and uses a throwaway home directory, so nothing on your machine changes.

## Why use it instead of `echo >> ~/.zshrc`

- **It picks the right file.** `PATH` and `JAVA_HOME` go in `.zshenv` so scripts
  and cron see them; `EDITOR` and aliases go in `.zshrc`. It also follows your
  `source` chains and warns when a file never gets loaded.
- **It protects your config.**
  - Writes are atomic: a temp file is written first, then swapped in.
  - Every write is preceded by a backup.
  - Parallel runs wait for each other, and a file that changed while envset was editing it is left alone.
  - A file you made read-only (`chmod a-w`) is refused.
  - envset refuses any change that would stop the file parsing (checked with `zsh -n`, `bash -n` or `fish --no-execute`).
  - Symlinks into a dotfiles repo stay symlinks, and file permissions are kept.
- **It's safe to re-run.**
  - Adding an identical line does nothing.
  - Adding the same key with a new value updates that line in place, keeping its trailing comment.
  - Removing a line leaves the rest of the file byte-for-byte identical.
  - It [won't rewrite code it could damage](#lines-envset-wont-rewrite), such as a definition inside an `if`.
- **It blocks injection.**
  - Variable names are validated.
  - Command substitution (`$(...)` or backticks) in a value is rejected unless you explicitly pass `--raw`.
- **Undo is one command:** `envset restore --target zshrc`.

## Installation

Requires Go 1.22 or newer (`brew install go`). Works on Apple Silicon and Intel Macs.

```bash
git clone https://github.com/ggthedev/envset.git && cd envset
go install .                      # installs to $(go env GOPATH)/bin, usually ~/go/bin
envset --help
```

Make sure `~/go/bin` is on your `PATH`, which envset can do for you:

```bash
~/go/bin/envset add --type path_append --value '~/go/bin' --target zshenv
```

From a checkout, `make` builds the code as it is on disk, uncommitted changes
included (run `make` on its own to list the targets):

```bash
make build            # output/envset
make build MACS=1     # output/envset-darwin-amd64 and -arm64, for another Mac
make clear            # delete the binaries in output/
```

A link to `output/envset` is broken after `make clear` until the next build.

To build committed branches instead, side by side, whatever is checked out:

```bash
scripts/build-branches.sh develop                 # output/envset-dev
MACS=1 scripts/build-branches.sh develop          # Intel and Apple Silicon binaries
INSTALL=1 scripts/build-branches.sh develop       # also refresh output/envset
envset version                                    # shows the version and platform
```

The version comes from the newest `v*` git tag:

| Version | Built from |
|---|---|
| `v0.1.0` | the tagged commit |
| `v0.1.0-3-gabc1234` | 3 commits after the tag, at commit `abc1234` |
| `…-dirty` | `make build` with uncommitted changes |

A binary copied to another Mac may be blocked; `xattr -d com.apple.quarantine <file>` clears that.

With Go installed, you can also install or upgrade on any Mac without cloning:

```bash
go install github.com/ggthedev/envset@latest
```

Every pull request merged into `main` publishes a
[release](https://github.com/ggthedev/envset/releases) with binaries for Intel
(`darwin_amd64`) and Apple Silicon (`darwin_arm64`), checksums and notes. The
patch number goes up each time; label the pull request `release:minor` to raise
the minor number instead. To install a downloaded one:

```bash
shasum -a 256 --check --ignore-missing checksums.txt
tar -xzf envset_*_darwin_*.tar.gz envset
mv envset ~/.local/bin/
```

## Quick reference

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

Run `envset add` or `envset remove` with no flags to start an interactive wizard.
Give some flags and the wizard asks only for the rest.
Prompts go to stderr, so stdout stays clean for `eval`. Without a terminal
(cron, pipes, `< /dev/null`) or with `--yes`, a missing flag is an error
instead of a prompt, so envset never hangs in a script.

### Which shell?

envset edits the files of your shell: the nearest zsh, bash or fish in the
process tree, skipping `sh`, so scripts, cron and IDE tasks still target your
real shell. If none is found, it uses `$SHELL`, then your macOS account's login
shell, then zsh. Override it with `--shell zsh|bash|fish` or
`ENVSET_SHELL`. `~/.gg_system_exports` and `~/.gg_system_interactive` are used
only on Macs where they exist.

## Commands

### `add`

| Flag | Meaning |
|---|---|
| `-k, --key` | Variable name (`[A-Za-z_][A-Za-z0-9_]*`). Not needed for `path_*` types |
| `--value` | Value (see [Value encoding](#value-encoding)) |
| `-t, --target` | File: `zshenv`, `zshrc`, `zprofile`, `bashrc`, `bash_profile`, `profile`, `bash_login`, `config_fish`, `gg_system_exports`, `gg_system_interactive`, `launchd`, or a path such as `~/.my_exports` |
| `--type` | `export` (default), `export_and_path_append`, `path_append`, `path_prepend`, `eval`, `source`, `fish_set` |
| `--yes` | Accept the auto-placed file when `--target` is omitted (required for non-interactive auto-placement) |
| `--literal` / `--raw` | Change value encoding |
| `--dry-run` | Show exactly what would change; write nothing |
| `--append` | Never edit existing lines: add a new one at the end that overrides them |
| `--print-source` | Print only `source <file>` on stdout, so `eval "$(envset add … --print-source)"` applies it immediately |

What `add` does depends on what's already in the file:

| Already in the file | Result |
|---|---|
| Nothing for this key | The line is appended |
| An identical line | `No change` (nothing is written) |
| The key with a different value | That line is updated in place, and envset shows the old and new versions |

For PATH types, envset only checks whether the exact entry is already there. It never replaces your existing PATH lines.

If the same key is also set in another file, envset tells you which value your
shells will actually use (it follows load order and `source` lines). When you
don't pass `--target`, it suggests the definition that wins, so the change takes
effect. Commented-out lines don't count as definitions.

### Lines envset won't rewrite

envset edits a definition only when it is one whole statement that always runs.
Otherwise it changes nothing, shows the lines (secrets hidden) and says why:

```console
$ envset add --key EDITOR --value vim --target zshenv
  line 5: export EDITOR='nano'
  line 7: export EDITOR='zed'
Error: not changing EDITOR in ~/.zshenv: it is set inside a block (if, case, loop or function) (line 5, 7); edit it by hand, or pass --append to add a new line at the end that overrides it
```

This covers definitions:
- inside `if`, `case`, a loop, a function, `{ }` or `( )`;
- sharing a line with other code (`export A=1; export B=2`, or two assignments);
- continued from or onto another line (a trailing `\`, `&&`, `||` or `|`).

Text inside heredocs and multi-line strings is ignored. `add --append` writes a
new line instead; `remove` has no override, so edit those lines by hand.

### `remove`

Comments the matching line(s) out by default (`# export FOO=…`). Pass
`--comment=false` to delete them instead.

- **PATH:** removing it requires `--value <dir>`, so one entry can be removed without wiping the others. A bare `remove --key PATH` is refused.
- **`export_and_path_append` keys:** removing the key also removes its `export PATH="$PATH:$KEY"` companion line.
- **Already-commented lines:** commenting them again does nothing.

### `restore`

envset keeps the last 10 backups of each file in
`~/.config/envset/backups/` (mode 0600). Each backup is named after the file
and a timestamp, e.g. `home__.zshrc.20261126T230923.088904000`.

```bash
envset restore --target zshrc --list           # newest first
envset restore --target zshrc                  # restore the most recent backup
envset restore --target zshrc --backup <file>  # restore a specific one
envset restore --target zshrc --dry-run
```

Before restoring, envset saves the current contents as a backup marked
*pre-restore*. A plain `envset restore` skips that copy, so to undo a restore,
pick the pre-restore backup with `--backup`.

### `audit`

`envset audit` scans every known config file and reports:
- PATH assignments that corrupt `$PATH`
- a quoted `~` in `PATH` (`"~/bin:$PATH"`), which zsh and scripts never expand
- exports placed in interactive-only files
- `source` chains that never load a file
- duplicate `compinit` calls
- slow tool inits (pyenv, nvm, …)
- keys defined in more than one file

It exits non-zero if it finds errors.

`--dry-run` adds a diff of every safe fix (each one comments out a line)
without prompting or writing. `--fix` walks through the findings one by one and
needs a terminal; `--fix --yes` applies every safe fix without asking. If a file
changed since the audit ran, nothing is written. Every fixed file is backed up first.

`audit` and `audit --dry-run` never write anything. `~/.config/envset/ideals.json`
and `custom.json` (the findings you chose to acknowledge during `--fix`) are
written only by `--fix`.
- A malformed `custom.json` gives a warning, and the audit runs without it; the file is never overwritten.
- Rule paths are stored as `~/…`, so you can sync `~/.config/envset` between Macs.
  `--fix` only forgets a rule when its file is missing under this Mac's home.

### `lazy`

Prints, or with `--write` appends once, a wrapper function that delays a slow
tool's `init` until you first run it:

```bash
envset lazy pyenv          # print
envset lazy pyenv --write  # add to your interactive rc file (no duplicates)
envset lazy mytool --init-cmd "mytool init zsh"
```

Tool names may contain letters, digits and `_ . + -`.
`starship`, `oh-my-posh`, `direnv`, `atuin` and `mcfly` are refused: their init
installs a prompt or hook, which a lazy wrapper would leave missing.

### `detect`, `list`, `reload`

- `detect` shows the shell envset places entries for and how it chose it, the shell it is running under, and which of that shell's config files exist.
- `list --target X [--key K]` shows the definitions in a file (exports, assignments, `source`, `eval`, aliases), including commented-out ones.
- `reload [--target X]` prints `source` lines for `eval`. Without `--target`, it prints the files your shell reads at startup, in load order (for zsh: `.zshenv`, `.zprofile`, `.zshrc`). A child process can't change its parent shell's environment, so this is how you apply a change to the shell you're in:

```bash
eval "$(envset reload --target zshenv)"
# or put this alias in ~/.zshrc:
alias eset='function _eset(){ eval "$(envset add "$@" --print-source)"; }; _eset'
```

## Value encoding

| Mode | Example input | Written as | Notes |
|---|---|---|---|
| default | `/opt/jdk` | `export K=/opt/jdk` | Simple values stay unquoted |
| default | `$HOME/bin` | `export K="$HOME/bin"` | `$VAR` and `${VAR}` expand |
| default | `~/bin` | `export K="$HOME/bin"` | A leading `~/` becomes `$HOME/` so it still expands |
| default | `say "hi"` | `export K="say \"hi\""` | `"` and `\` are escaped |
| default | `$(brew --prefix)` | **rejected** | It would run on every shell start |
| `--literal` | `$HOME $(x)` | `export K='$HOME $(x)'` | Nothing expands |
| `--raw` | `$(brew --prefix)` | `export K=$(brew --prefix)` | Unchecked; you own the syntax |

Values that zsh would expand (starting `=`, or containing `:=`) are quoted, and
fish files use fish's own quoting.

Values must be a single line. For `--type eval`, the value is a command by
definition, e.g. `--value '"$(rbenv init -)"'`, so it is written as given.

## Where things go

When `--target` is omitted, envset works out what the variable is for, its intent,
and picks the file whose *effective* scope covers it:

| Intent | Example | zsh | bash |
|---|---|---|---|
| Every shell, including scripts and cron | `PATH`, `JAVA_HOME`, `GOPATH` | `.zshenv` | `.bash_profile` / `.profile` |
| Login shells | `SSH_AUTH_SOCK` | `.zprofile` | `.bash_profile` |
| Interactive only | `EDITOR`, `PS1`, aliases | `.zshrc` | `.bashrc` |
| GUI apps (Dock, Spotlight) | anything needed by IDEs | `launchd` | `launchd` |

If your shell has no file for a scope (bash has none that every shell reads;
fish has only `config.fish`), envset uses the closest one and says so.

A file's *effective* scope is the broadest scope that actually loads it. For example,
if `~/.gg_system_exports` is declared for all sessions but only `.zprofile`
sources it, its effective scope is *login*. The placer then picks a better file
and warns you. `envset audit` explains how to fix the chain.

### launchd (GUI apps)

`--target launchd` writes `launchctl setenv` lines to
`~/.config/envset/launchd.sh` and installs
`~/Library/LaunchAgents/com.envset.environment.plist`, which runs the script at
login. It also applies the change to the current login session right away.

## Logging

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

## Environment variables

| Variable | Effect |
|---|---|
| `ENVSET_LOG_FILE`, `ENVSET_LOG_DIR` | Log location |
| `ENVSET_SHELL` | Shell to place entries for (`zsh`, `bash`, `fish`); same as `--shell` |
| `ENVSET_BACKUP_DIR` | Backup location (default `~/.config/envset/backups`) |
| `ENVSET_NO_SYNTAX_CHECK=1` | Skip the shell parse check before writing. Not recommended |
| `XDG_CONFIG_HOME` | Base for the config directory |

## Development

```bash
go build ./... && go vet ./... && go test ./...
```

The tests never touch your real files. Backups go to a temp directory, and
`launchctl` is replaced by a fake.

Commits in this repo go through the **COMLOG** protocol (see `.claude/agents/comlog.md`).
`scripts/comlog-gate.sh` checks the staged tree: no binaries or secrets, and
build, vet and tests pass, with no new test failures compared with HEAD.

```
envset/
├── cmd/              CLI (cobra)
├── pkg/
│   ├── detector/     OS / shell detection
│   ├── placer/       intent → file ranking, source-chain analysis, wizard questions
│   ├── targets/      reading and editing shell files and launchd; value encoding; safe writes
│   ├── fsutil/       atomic writes, backups, restore
│   ├── audit/        health checks and --fix
│   ├── rules/        built-in and user-acknowledged audit rules (~/.config/envset/*.json)
│   ├── lazy/         lazy-load wrapper generator
│   ├── logger/       leveled run log and call tracing
│   ├── paths/        where envset's own files live
│   ├── wizard/       interactive prompts
│   ├── term/         TTY detection; prompts on stderr
│   ├── version/      which build is running
│   └── ops/          orchestration behind each command
├── scripts/          commit gate, per-branch builds, licence notices
├── Makefile          make help | build | clear
└── docs/             tutorial and deferred work
```

## Limitations

- envset reads shell files with a small scanner, not a full shell parser. When
  unsure it refuses. It can't tell that an unconditional `return` or an unusual
  zsh short loop stops a line from running, so check `envset list` if a change
  seems to have no effect.
- Locking coordinates envset runs only; an editor saving at the same instant is
  narrowed by a re-check, not prevented.
- `--target launchd` keeps only one added `PATH` entry (see [docs/TODO.md](docs/TODO.md)).

## Not yet supported

- `.env` files
- IDE settings
- Linux `systemd` environment
- Docker or Kubernetes environment

## License

Copyright 2026 Gaurav Gupta. Licensed under the Apache License, Version 2.0: see
[LICENSE](LICENSE). You may use, change and share this code; keep the copyright
and [NOTICE](NOTICE) text, and mark the files you change.

The binaries also contain third-party code, whose licences are in
[THIRD_PARTY_NOTICES.txt](THIRD_PARTY_NOTICES.txt). After changing dependencies,
regenerate it with `scripts/third-party-notices.sh`.
