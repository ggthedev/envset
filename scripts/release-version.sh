#!/usr/bin/env bash
# release-version.sh <commit-or-tree> [patch|minor]
#
# The one place that decides releases, used by release.yml and pr-text.sh.
# Prints the version the content would be released as, or nothing when there is
# nothing to release. The reason goes to stderr.
set -euo pipefail

target=${1:?usage: release-version.sh <commit-or-tree> [patch|minor]}
bump=${2:-patch}
releases() { grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' || true; }

# A commit that already has a version keeps it (a re-run of the release).
tagged=$(git tag --points-at "$target" --sort=-v:refname | releases | sed -n 1p)
if [ -n "$tagged" ]; then
  echo "Already tagged $tagged." >&2
  echo "$tagged"
  exit 0
fi

latest=$(git tag --sort=-v:refname | releases | sed -n 1p)
if [ -z "$latest" ]; then
  echo "This is the first release." >&2
  echo v0.1.0
  exit 0
fi

# Everything that ends up in the archives, or decides how it is built.
changed=$(git diff --name-only "$latest" "$target" -- \
  '*.go' go.mod go.sum Makefile LICENSE NOTICE THIRD_PARTY_NOTICES.txt ':(exclude)*_test.go')
if [ -z "$changed" ] && [ "$bump" != minor ]; then
  echo "Nothing in the download changed since $latest." >&2
  exit 0
fi

IFS=. read -r major minor patch <<<"${latest#v}"
if [ "$bump" = minor ]; then
  echo "A minor release was asked for." >&2
  echo "v$major.$((minor + 1)).0"
else
  echo "Changed in the download since $latest: $(echo $changed | sed 's/ /, /g')." >&2
  echo "v$major.$minor.$((patch + 1))"
fi
