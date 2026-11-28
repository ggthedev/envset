#!/usr/bin/env bash
# Build the newest release tag and point your envset link at it (make release).
# The tag is built in a temporary git worktree, so your checkout and uncommitted
# changes are never touched. The binary is kept in output/releases/<tag>/.
#
#   BIN_DIR=<dir>   where the link goes (default ~/.local/bin)
set -euo pipefail

fail() { echo "release: $*" >&2; exit 1; }

root=$(git rev-parse --show-toplevel)
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
link="$BIN_DIR/envset"
# Check first: a real file there is somebody's binary, not ours to replace.
if [ -e "$link" ] && [ ! -L "$link" ]; then
  fail "$link is a file, not a link; move it away first"
fi

# Release tags are created on GitHub, so ask for the new ones. Only tags move.
if ! git -C "$root" fetch --quiet --tags origin; then
  echo "release: could not fetch tags; using the ones already here" >&2
fi
tag=$(git -C "$root" tag --list --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sed -n 1p || true)
[ -n "$tag" ] || fail "no release tag (vX.Y.Z) found"

tmp=$(mktemp -d)
cleanup() {
  git -C "$root" worktree remove --force "$tmp/src" >/dev/null 2>&1 || true
  rm -rf "$tmp"
  git -C "$root" worktree prune
}
trap cleanup EXIT

echo "Building $tag"
git -C "$root" worktree add --quiet --detach "$tmp/src" "refs/tags/$tag"
# MACS= keeps a MACS=1 given to the outer make from changing what is built.
make --no-print-directory -C "$tmp/src" build MACS= >/dev/null

built=$("$tmp/src/output/envset" version)
[[ $built == "envset $tag ("* ]] || fail "the build calls itself '$built', not $tag"

dest="$root/output/releases/$tag/envset"
mkdir -p "$(dirname "$dest")"
# Replace the file instead of writing into it: it may be running.
cp "$tmp/src/output/envset" "$dest.new"
mv -f "$dest.new" "$dest"

old=""
[ -L "$link" ] && old=$(readlink "$link")
mkdir -p "$BIN_DIR"
ln -sfn "$dest" "$link"

echo "$built"
echo "Linked $link -> $dest"
if [ -n "$old" ] && [ "$old" != "$dest" ]; then
  echo "It pointed to $old. To switch back:"
  printf '  ln -sfn %q %q\n' "$old" "$link"
fi
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo "Note: $BIN_DIR is not on your PATH." >&2 ;;
esac
