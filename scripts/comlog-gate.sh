#!/usr/bin/env bash
# comlog-gate.sh — verify that what is currently STAGED is safe to commit.
#
# Used by the COMLOG protocol (see .claude/agents/comlog.md). Run it after staging a group of
# changes and before `git commit`. It never modifies the working tree or index:
# the staged tree and HEAD are exported to temp dirs and tested there.
#
# Checks, in order:
#   1. Branch guard   — refuse main/master, detached HEAD, rebase/merge in progress.
#   2. Content guard  — refuse staged binaries, files > 1 MB, secrets-looking files.
#   3. Build + vet    — `go build ./...` and `go vet ./...` must pass on the staged tree.
#   4. Tests          — `go test ./...` must not introduce failures that HEAD lacks
#                       (pre-existing failures are reported but do not block).
#
# Exit 0 = safe to commit. Non-zero = do not commit; reason printed on stderr.
set -euo pipefail

say()  { printf '[comlog-gate] %s\n' "$*" >&2; }
fail() { say "BLOCKED: $*"; exit 1; }

root=$(git rev-parse --show-toplevel)
cd "$root"
gitdir=$(git rev-parse --git-dir)

# ---- 1. Branch guard -------------------------------------------------------
branch=$(git symbolic-ref --quiet --short HEAD 2>/dev/null) || fail "detached HEAD; check out a working branch first"
case "$branch" in
  main|master) fail "current branch is '$branch'; COMLOG never commits to main/master" ;;
esac
for marker in rebase-merge rebase-apply MERGE_HEAD CHERRY_PICK_HEAD REVERT_HEAD; do
  [ -e "$gitdir/$marker" ] && fail "a git operation is in progress ($marker); finish or abort it first"
done
say "branch: $branch"

# ---- 2. Content guard ------------------------------------------------------
staged=$(git diff --cached --name-only --diff-filter=ACMR)
[ -n "$(git diff --cached --name-only)" ] || fail "nothing is staged"

bad=""
while IFS= read -r f; do
  [ -z "$f" ] && continue
  case "$f" in
    .env|.env.*|*/.env|*/.env.*|*.local.json|*.pem|*.key|*.p12|*credentials.json|*secrets.*|.DS_Store|*/.DS_Store|scratch/*)
      bad="$bad\n  $f (excluded path pattern)"; continue ;;
  esac
  size=$(git cat-file -s ":$f" 2>/dev/null || echo 0)
  if [ "$size" -gt 1048576 ]; then
    bad="$bad\n  $f (staged blob is ${size} bytes > 1 MB)"; continue
  fi
done <<EOF
$staged
EOF

# git marks binary files as "-<TAB>-<TAB>path" in --numstat.
binaries=$(git diff --cached --numstat --diff-filter=ACMR | awk -F'\t' '$1=="-" && $2=="-" {print $3}')
for f in $binaries; do
  bad="$bad\n  $f (binary content)"
done
[ -z "$bad" ] || fail "$(printf 'staged files must not be committed:%b' "$bad")"

# ---- 3 & 4. Build, vet, tests ---------------------------------------------
if [ ! -f go.mod ] && ! git cat-file -e :go.mod 2>/dev/null; then
  say "no go.mod — skipping build/test gate"
  say "OK"
  exit 0
fi

tmp=$(mktemp -d "${TMPDIR:-/tmp}/comlog-gate.XXXXXX")
trap 'rm -rf "$tmp"' EXIT
mkdir -p "$tmp/staged" "$tmp/base"

git checkout-index -a --prefix="$tmp/staged/"

# Extract failing test / package names from `go test` output.
failures() {
  {
    grep -E '^[[:space:]]*--- FAIL: ' "$1" | sed -E 's/^[[:space:]]*--- FAIL: ([^ ]+).*/test:\1/' || true
    grep -E '^FAIL[[:space:]]+[^[:space:]]+[[:space:]]+\[' "$1" | awk '{print "pkg:"$2}' || true
  } | sort -u
}

say "go build ./... (staged)"
( cd "$tmp/staged" && go build ./... ) >"$tmp/build.log" 2>&1 || { cat "$tmp/build.log" >&2; fail "go build failed on the staged tree"; }
say "go vet ./... (staged)"
( cd "$tmp/staged" && go vet ./... ) >"$tmp/vet.log" 2>&1 || { cat "$tmp/vet.log" >&2; fail "go vet failed on the staged tree"; }

say "go test ./... (staged)"
( cd "$tmp/staged" && go test ./... ) >"$tmp/staged.log" 2>&1 || true
failures "$tmp/staged.log" >"$tmp/staged.fail"

if git rev-parse --verify --quiet HEAD >/dev/null; then
  say "go test ./... (HEAD baseline)"
  git archive HEAD | tar -x -C "$tmp/base"
  ( cd "$tmp/base" && go test ./... ) >"$tmp/base.log" 2>&1 || true
  failures "$tmp/base.log" >"$tmp/base.fail"
else
  : >"$tmp/base.fail"
fi

new=$(comm -23 "$tmp/staged.fail" "$tmp/base.fail")
pre=$(comm -12 "$tmp/staged.fail" "$tmp/base.fail")
fixed=$(comm -13 "$tmp/staged.fail" "$tmp/base.fail")

[ -n "$pre" ]   && say "pre-existing failures (not blocking): $(echo $pre)"
[ -n "$fixed" ] && say "now passing (fixed by this commit): $(echo $fixed)"
if [ -n "$new" ]; then
  grep -E -A20 -- '--- FAIL' "$tmp/staged.log" | head -60 >&2 || true
  fail "new test failures vs HEAD: $(echo $new)"
fi

say "OK — staged changes are non-breaking"
