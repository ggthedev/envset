# envset tutorial

This tutorial takes about 10 minutes. You'll add, update, inspect, remove and
undo environment variables, all inside a **sandbox home directory**, so your
real `~/.zshrc` and other files are never touched. All output shown here is real;
paths are shortened to `~`.

## 0. Install and open a sandbox

```bash
cd /path/to/envset && go install .        # puts envset in ~/go/bin
mkdir -p /tmp/envset-play
env -i HOME=/tmp/envset-play PATH="$HOME/go/bin:/usr/bin:/bin" USER="$USER" SHELL=/bin/zsh zsh -f
```

You're now in a bare zsh whose `$HOME` is `/tmp/envset-play`. Type `exit` at
any point to leave; your real environment is untouched. To start over,
`rm -rf /tmp/envset-play`.

## 1. See what envset sees

```console
$ envset detect
Detected OS:         darwin
Current Shell:       zsh (from process tree)
Login Shell:         zsh (from $SHELL)
Home Directory:      ~
Shell Configs:
  .zshrc          ~/.zshrc (missing)
  .zprofile       ~/.zprofile (missing)
  .zshenv         ~/.zshenv (missing)
  ...
```

envset reads your shell from the process tree, so the answer is correct even if
`$SHELL` says something else.

## 2. Add a variable and let envset pick the file

Preview first with `--dry-run`:

```console
$ envset add --key JAVA_HOME --value /opt/jdk-21 --yes --dry-run
Auto-placed: zshenv (~/.zshenv)
Reason: Application environments should be available in all sessions (confidence: 80%)
[DRY-RUN] would have added to ~/.zshenv:
  export JAVA_HOME=/opt/jdk-21
```

`JAVA_HOME` must reach scripts, cron and IDE terminals as well as interactive shells,
so envset picks `.zshenv`, the one file zsh reads for every session. Now
run it for real:

```console
$ envset add --key JAVA_HOME --value /opt/jdk-21 --yes
Auto-placed: zshenv (~/.zshenv)
...
added to ~/.zshenv:
  export JAVA_HOME=/opt/jdk-21
```

`--yes` accepts the suggestion. Without it, auto-placement stops and asks you
to confirm, so a script never silently writes to a file it guessed.

## 3. Run it again: nothing breaks

```console
$ envset add --key JAVA_HOME --value /opt/jdk-21 --yes
...
No change: ~/.zshenv already contains:
  export JAVA_HOME=/opt/jdk-21
```

Re-running is always safe: no duplicate lines are written. That makes envset
suitable for a dotfiles bootstrap script you run on every Mac.

## 4. Change a value: it's updated in place

```console
$ envset add --key JAVA_HOME --value /opt/jdk-22 --target zshenv
updated ~/.zshenv line 1:
  - export JAVA_HOME=/opt/jdk-21
  + export JAVA_HOME=/opt/jdk-22
Backup saved: ~/.config/envset/backups/home__.zshenv.20261126T231331.040811000
```

Two things to notice:
- the existing line was replaced in place, not appended to;
- a **backup** was taken before the write. Every write takes one.

## 5. More kinds of entries

```console
$ envset add --key EDITOR --value nvim --target zshrc
added to ~/.zshrc:
  export EDITOR=nvim

$ envset add --type path_append --value "~/go/bin" --target zshenv
added to ~/.zshenv:
  export PATH="$PATH:$HOME/go/bin"
```

`~/go/bin` was written as `$HOME/go/bin`. A `~` inside quotes doesn't expand,
but `$HOME` does, so the entry works.

## 6. Safety: values that would run code

```console
$ envset add --key GREETING --value '$(whoami)' --target zshrc
Error: value contains command substitution ($(...) or backticks), which would run on
every shell start; pass --literal to store it as plain text, or --raw if you really
want it executed
```

A `$(...)` in a startup file runs every time a terminal opens, so envset
refuses it by default. You have two ways forward:

```console
$ envset add --key GREETING --value '$(whoami)' --target zshrc --literal
added to ~/.zshrc:
  export GREETING='$(whoami)'
```

- `--literal` stores the text exactly, single-quoted.
- `--raw` writes it unquoted, if you really do want it executed.

Variable names are checked too: `--key 'X;rm -rf ~'` is rejected.

## 7. Inspect and apply

```console
$ envset list --target zshenv
Entries in ~/.zshenv:
  line 1: export JAVA_HOME=/opt/jdk-22
  line 2: export PATH="$PATH:$HOME/go/bin"

$ eval "$(envset reload --target zshenv)"; echo "JAVA_HOME=$JAVA_HOME"
JAVA_HOME=/opt/jdk-22
```

A program can't change the environment of the shell that started it. That's
why envset prints `source` lines and you `eval` them. New terminals pick up
changes automatically.

## 8. Remove

```console
$ envset remove --key EDITOR --target zshrc
commented out 1 line(s) in ~/.zshrc:
  line 1: export EDITOR=nvim
```

The default is to comment the line out, so it's easy to bring back.
`--comment=false` deletes the line instead.

PATH gets an extra guard:

```console
$ envset remove --key PATH --target zshenv
Error: refusing to remove every PATH line in ~/.zshenv; pass --value <dir> to pick which entry

$ envset remove --key PATH --value go/bin --target zshenv --comment=false
deleted 1 line(s) in ~/.zshenv:
  line 2: export PATH="$PATH:$HOME/go/bin"
```

Only that line is deleted. Blank lines, comments and everything else in
the file stay exactly as they were.

## 9. Undo

```console
$ envset restore --target zshenv --list
Backups for ~/.zshenv (newest first):
  2026-11-26 23:13:31  ~/.config/envset/backups/home__.zshenv.20261126T231331.329628000
  2026-11-26 23:13:31  ~/.config/envset/backups/home__.zshenv.20261126T231331.122163000
  2026-11-26 23:13:31  ~/.config/envset/backups/home__.zshenv.20261126T231331.040811000

$ envset restore --target zshenv
Restored ~/.zshenv from ~/.config/envset/backups/home__.zshenv.20261126T231331.329628000

$ cat ~/.zshenv
export JAVA_HOME=/opt/jdk-22
export PATH="$PATH:$HOME/go/bin"
```

The PATH line is back. envset keeps the last 10 backups per file. Before
restoring, it saves the current contents as a backup marked *pre-restore*. To
undo a restore, pick that backup with `--backup <file>`.

## 10. Health check

```console
$ envset audit
✓ No issues found. Your shell configs look clean.
```

On a real machine, `audit` finds problems such as:
- PATH lines that corrupt `$PATH`
- exports that only reach interactive shells
- slow `eval "$(pyenv init -)"`-style inits
- keys defined in several files

`envset audit --fix` previews the safe fixes and applies them only after you
confirm, with backups.

## Using it on your real machine

Once you're comfortable, `exit` the sandbox and repeat the read-only steps for real:

```bash
envset detect
envset audit
envset add --key ENVSET_SMOKE --value ok --target zshenv --dry-run
```

Then try a real add and remove of a throwaway key. If anything ever looks
wrong, `envset restore --target <file>` puts the file back.

**Tip:** if your dotfiles are symlinks into a git repo, envset writes through the
link and the link is preserved, so `git diff` in your dotfiles repo shows exactly
what changed.
