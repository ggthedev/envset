#!/usr/bin/env bash
# Build envset binaries into output/. Each build comes from a branch's committed
# tree (git archive), so the working tree and checkout are never touched.
#   scripts/build-branches.sh                    # every local branch, this machine
#   scripts/build-branches.sh develop qa-fixes   # only the named branches
#   MACS=1 scripts/build-branches.sh develop     # Intel and Apple Silicon builds
#   INSTALL=1 scripts/build-branches.sh develop  # also refresh output/envset
set -euo pipefail
root=$(git rev-parse --show-toplevel)
out="$root/output"
mkdir -p "$out"
branches=("$@")
if [ ${#branches[@]} -eq 0 ]; then
  while IFS= read -r b; do branches+=("$b"); done < <(git -C "$root" for-each-ref --format='%(refname:short)' refs/heads)
fi
if [ -n "${INSTALL:-}" ] && [ ${#branches[@]} -ne 1 ]; then
  echo "INSTALL=1 needs exactly one branch" >&2
  exit 1
fi
platforms=("$(go env GOOS)/$(go env GOARCH)")
[ -n "${MACS:-}" ] && platforms=(darwin/amd64 darwin/arm64)

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
failed=0
for b in "${branches[@]}"; do
  name="envset-${b//\//-}"
  [ "$b" = develop ] && name=envset-dev
  src="$tmp/$name"
  mkdir -p "$src"
  git -C "$root" archive "$b" | tar -x -C "$src"
  rev=$(git -C "$root" rev-parse --short "$b")
  flags="-X github.com/experimentalist/envset/pkg/version.Version=$b@$rev"
  for p in "${platforms[@]}"; do
    file="$name"
    [ -n "${MACS:-}" ] && file="$name-${p//\//-}"
    if (cd "$src" && GOOS="${p%/*}" GOARCH="${p#*/}" CGO_ENABLED=0 go build -ldflags "$flags" -o "$out/$file" .); then
      printf '%-36s %s  (%s)\n' "output/$file" "$rev" "$b"
    else
      echo "FAILED: $b $p ($rev)" >&2
      failed=1
    fi
  done
  if [ -n "${INSTALL:-}" ]; then
    (cd "$src" && go build -ldflags "$flags" -o "$out/envset" .)
    printf '%-36s %s  (%s)\n' "output/envset" "$rev" "$b"
  fi
done
exit $failed
