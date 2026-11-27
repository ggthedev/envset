# TODO

Work that has been planned but deliberately deferred. Each item says why it
was deferred and what "done" means, so it can be picked up on its own branch.

## launchd: environment for GUI apps (deferred from Phase 2, item 2E)

**Status:** deferred on 2026-11-27, so that Phase 2 can finish and merge without it.
**Branch when picked up:** a new branch from `develop`, e.g. `launchd-path`.
**Who's affected:** anyone using `--target launchd`, which sets variables for apps
launched from the Dock, Spotlight or Finder rather than from a terminal.
Shell-file targets (`.zshenv`, `.zshrc`, …) are unaffected.

### Problems (confirmed in `pkg/targets/launchd.go`)

1. **GUI apps lose Homebrew from PATH.** `launchd.sh` writes
   `launchctl setenv PATH "$PATH:<dir>"`. At login the script runs under launchd's
   minimal PATH (`/usr/bin:/bin:/usr/sbin:/sbin`), so `$PATH` expands to that,
   without `/opt/homebrew/bin` or `/usr/local/bin`.
2. **The live session and the next login disagree.** `applyImmediately` builds
   the value from the *terminal's* `$PATH` instead, so today's session gets
   one PATH and the next login gets another.
3. **A second PATH entry replaces the first.** `Add` matches existing lines by
   the prefix `launchctl setenv PATH `, so adding `/a` then `/b` leaves only `/b`.
4. **`export_and_path_append` drops its PATH half.** `Preview` falls through to
   the plain export case and writes only `launchctl setenv KEY value`.
5. **The plist is written but never registered.**
   `~/Library/LaunchAgents/com.envset.environment.plist` only takes effect at
   the next login. It is also built with `fmt.Sprintf`, so a script path
   containing `&` or `<` produces invalid XML.

### Plan

1. Manage PATH as one envset-owned block in `launchd.sh`, one line per entry:
   ```sh
   # >>> envset PATH >>>
   ENVSET_PATH="/usr/bin:/bin:/usr/sbin:/sbin"
   [ -d /opt/homebrew/bin ] && ENVSET_PATH="/opt/homebrew/bin:$ENVSET_PATH"
   [ -d /usr/local/bin ]    && ENVSET_PATH="/usr/local/bin:$ENVSET_PATH"
   ENVSET_PATH="$ENVSET_PATH:/my/append"
   ENVSET_PATH="/my/prepend:$ENVSET_PATH"
   launchctl setenv PATH "$ENVSET_PATH"
   # <<< envset PATH <<<
   ```
   - Adding an entry inserts one line, and removing one deletes only its line.
   - The same script works on Apple Silicon and Intel Macs.
   - Lines outside the block are left exactly as they are.
2. `applyImmediately` runs the script itself (`sh launchd.sh`), so the live
   session gets exactly what the next login will.
3. `export_and_path_append` writes the variable line plus a PATH-block entry,
   and removing the key removes both.
4. After creating the plist, run `launchctl bootstrap gui/$UID <plist>`. If
   that fails, say the change takes effect at the next login. XML-escape the
   script path.
5. Migrate existing `launchctl setenv PATH …` lines into the block on first
   write, and back up the file first as usual.

### Open question to settle first

Apple's documented way to set PATH for GUI apps is
`sudo launchctl config user path <value>`, which needs a reboot.
`launchctl setenv PATH` is known not to reach every app. Before building the
block, test on a real Mac whether an app launched from the Dock (e.g. VS Code
→ integrated terminal → `echo $PATH`) actually sees a `setenv PATH` value. If
it doesn't, the block should feed `launchctl config user path` instead
(needs sudo, opt-in), and envset should say so.

### Tests

- A golden file of `launchd.sh` after append, prepend, remove and re-add.
- `sh -n` passes on the generated script.
- The fake `runLaunchctl` receives `setenv PATH` with the computed value.
- The plist parses with `plutil -lint`, including for a script path containing `&`.

### Done when

On both an Apple Silicon and an Intel Mac, an app launched from the Dock sees
`/opt/homebrew/bin` (or `/usr/local/bin`) plus every entry you added, and
adding a second entry keeps the first.
