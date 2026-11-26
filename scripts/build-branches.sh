#!/usr/bin/env bash
# Build one envset binary per local branch into output/envset-<branch>.
# Each build comes from the branch's committed tree (git archive), so the
# working tree and the checked-out branch are never touched.
#   scripts/build-branches.sh             # every local branch
#   scripts/build-branches.sh develop     # only the named branches
set -euo pipefail
root=$(git rev-parse --show-toplevel)
out="$root/output"
mkdir -p "$out"
branches=("$@")
if [ ${#branches[@]} -eq 0 ]; then
  while IFS= read -r b; do branches+=("$b"); done < <(git -C "$root" for-each-ref --format='%(refname:short)' refs/heads)
fi
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
for b in "${branches[@]}"; do
  name="envset-${b//\//-}"
  [ "$b" = develop ] && name=envset-dev
  src="$tmp/$name"; mkdir -p "$src"
  git -C "$root" archive "$b" | tar -x -C "$src"
  rev=$(git -C "$root" rev-parse --short "$b")
  if (cd "$src" && go build -o "$out/$name" .); then
    printf '%-28s %s  (%s)\n' "output/$name" "$rev" "$b"
  else
    echo "FAILED: $b ($rev)" >&2
  fi
done
