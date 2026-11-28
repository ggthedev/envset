# Development

Requires Go 1.22 or newer (`brew install go`).

## Checks

```bash
go build ./... && go vet ./... && go test ./...
```

The tests never touch your real files. Backups go to a temp directory, and
`launchctl` is replaced by a fake. Some tests start real zsh, bash and fish
with a temporary `HOME`; a shell that isn't installed is skipped.

One test uses the real `launchctl`, so it sets (and then unsets) a variable in
your login session, and runs only when asked:

```bash
ENVSET_TEST_LAUNCHD=1 go test -count=1 -run TestRealLaunchd -v ./cmd
```

### On GitHub

`.github/workflows/test.yml` runs on every pull request into `main`, and again
when the pull request gets new commits, on Apple Silicon (macOS 15 and 26) and
Intel (macOS 15) runners. It runs the checks above with the race detector and
the real `launchctl` test, then builds the binary and checks its architecture.
A missing shell fails the run there instead of being skipped
(`ENVSET_TEST_REQUIRE_SHELLS=1`).

## Building

From a clone:

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
make release          # build the newest release tag and link to it
make clear            # delete the development binaries in output/
```

For daily use, `make release` fetches the tags, builds the newest `vX.Y.Z` one
in a temporary git worktree and points `~/.local/bin/envset` at it (set
`BIN_DIR` for another folder). Your checkout and uncommitted changes are left
alone, each release is kept in `output/releases/<tag>/`, and a real file called
`envset` in that folder is never replaced.

The link then stays on that release: `make build` no longer changes what
`envset` runs. To go back to your own build:

```bash
ln -sfn "$PWD/output/envset" ~/.local/bin/envset
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

## Pull requests

`.github/workflows/pr.yml` opens the pull request from `develop` into `main`,
with a title and description written from the commit messages: what each commit
changes and why, whether merging publishes a release, and the files changed.
Run it after pushing `develop`, from the Actions tab (**pull request** > **Run
workflow**) or with:

```bash
gh workflow run pr.yml
gh workflow run pr.yml -f title="My own title"
```

Then review and merge it yourself. Running it again refreshes the title and
description of the open pull request, replacing edits made by hand. To preview
the text first:

```bash
git fetch origin && scripts/pr-text.sh origin/main origin/develop
```

- The repository setting **Settings > Actions > General > Allow GitHub Actions
  to create and approve pull requests** must be on.
- GitHub holds the tests of a pull request opened this way until you select
  **Approve workflows to run** on it.
- GitHub offers **Run workflow** only once the file is on `main`.

The title also appears in the generated release notes.

## Releases

A pull request merged into `main` publishes a
[release](https://github.com/ggthedev/envset/releases) when something in the
download changed since the last release: Go code (not tests), `go.mod`,
`go.sum`, the `Makefile` or the licence files. Other merges, such as
documentation, publish nothing.

Each release holds Intel (`darwin_amd64`) and Apple Silicon (`darwin_arm64`)
archives, checksums and generated notes. The patch number goes up each time;
label the pull request `release:minor` to raise the minor number instead (this
also releases when nothing in the download changed). The workflow is
`.github/workflows/release.yml`, and `scripts/release-version.sh` holds the rule
that it shares with the pull request description.

After changing dependencies, regenerate the licence notices with
`scripts/third-party-notices.sh`.

## Commits

Commits in this repo go through the **COMLOG** protocol (see `.claude/agents/comlog.md`).
`scripts/comlog-gate.sh` checks the staged tree: no binaries or secrets, and
build, vet and tests pass, with no new test failures compared with HEAD.

## Project layout

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
├── scripts/          commit gate, builds and installs, pull request text, release
│                     versions, licence notices
├── Makefile          make help | build | clear
└── docs/             tutorial, reference, logging, development, deferred work
```
