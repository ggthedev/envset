#!/usr/bin/env bash
# Write THIRD_PARTY_NOTICES.txt: the licences of everything compiled into the
# macOS binaries, which must ship with them. Run again after changing
# dependencies.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
out=THIRD_PARTY_NOTICES.txt
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

go mod download
modules=$(for arch in amd64 arm64; do
  GOOS=darwin GOARCH=$arch go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Version}} {{.Dir}}{{end}}{{end}}' .
done | sort -u)

# Homebrew keeps Go's licence one level above GOROOT.
goroot=$(go env GOROOT)
[ -f "$goroot/LICENSE" ] || goroot=$(dirname "$goroot")

section() { # title, directory
  local found=
  printf '\n%s\n%s\n%s\n' "$rule" "$1" "$rule"
  for f in "$2"/LICENSE* "$2"/LICENCE* "$2"/COPYING* "$2"/NOTICE*; do
    [ -f "$f" ] || continue
    printf '\n--- %s ---\n\n' "${f##*/}"
    cat "$f"
    case "${f##*/}" in NOTICE*) ;; *) found=1 ;; esac
  done
  [ -n "$found" ] || { echo "no licence file for $1 in $2" >&2; return 1; }
}
rule=$(printf '=%.0s' {1..78})

{
  echo "Third-party notices"
  echo
  echo "envset binaries include the Go standard library and runtime and the modules"
  echo "below. Their licences and notices follow."
  echo
  echo "  Go standard library and runtime"
  while read -r path version _; do echo "  $path $version"; done <<<"$modules"
  section "Go standard library and runtime" "$goroot"
  while read -r path version dir; do section "$path $version" "$dir"; done <<<"$modules"
} >"$tmp"
cat "$tmp" >"$out"
echo "wrote $out"
